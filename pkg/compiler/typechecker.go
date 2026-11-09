// File: pkg/compiler/typechecker.go
package compiler

import (
	"fmt"
	"os"
	"strings"
)

type VarInfo struct {
	Type       UbeType
	IsMut      bool
	IsView     bool
	Depth      int
	ValueDepth int
	LoopDepth  int
	Moved      bool
}

type FuncSignature struct {
	Params       []FieldDecl
	RetType      UbeType
	Escaping     []bool
	ReceiverMut  bool
	ReceiverView bool
	IsPub        bool
	Package      string
}

type StructMeta struct {
	Fields  []FieldDecl
	IsPub   bool
	Package string
}

type EnumMeta struct {
	Variants []VariantDecl
	IsPub    bool
	Package  string
}

type TraitMeta struct {
	Trait   *TraitDecl
	IsPub   bool
	Package string
}

type TypeEnvironment struct {
	Parent     *TypeEnvironment
	Vars       map[string]VarInfo
	Structs    map[string][]FieldDecl
	StructMeta map[string]StructMeta
	Enums      map[string][]VariantDecl
	EnumMeta   map[string]EnumMeta
	Traits     map[string]*TraitDecl
	TraitMeta  map[string]TraitMeta
	Funcs      map[string]FuncSignature
	CurModule  string
	CurRet     UbeType
	LoopDepth  int
	ScopeDepth int
}

func NewTypeEnvironment(parent *TypeEnvironment) *TypeEnvironment {
	return &TypeEnvironment{
		Parent:     parent,
		Vars:       make(map[string]VarInfo),
		Structs:    make(map[string][]FieldDecl),
		StructMeta: make(map[string]StructMeta),
		Enums:      make(map[string][]VariantDecl),
		EnumMeta:   make(map[string]EnumMeta),
		Traits:     make(map[string]*TraitDecl),
		TraitMeta:  make(map[string]TraitMeta),
		Funcs:      make(map[string]FuncSignature),
	}
}

func childTypeEnvironment(parent *TypeEnvironment) *TypeEnvironment {
	env := NewTypeEnvironment(parent)
	if parent != nil {
		env.Structs = parent.Structs
		env.StructMeta = parent.StructMeta
		env.Enums = parent.Enums
		env.EnumMeta = parent.EnumMeta
		env.Funcs = parent.Funcs
		env.CurModule = parent.CurModule
		env.CurRet = parent.CurRet
		env.LoopDepth = parent.LoopDepth
		env.ScopeDepth = parent.ScopeDepth
		env.Traits = parent.Traits
		env.TraitMeta = parent.TraitMeta
	}
	return env
}

func (e *TypeEnvironment) LookupVar(name string) (VarInfo, bool) {
	if v, ok := e.Vars[name]; ok {
		return v, true
	}
	if e.Parent != nil {
		return e.Parent.LookupVar(name)
	}
	return VarInfo{}, false
}

func (e *TypeEnvironment) updateVar(name string, update func(*VarInfo)) bool {
	if _, ok := e.Vars[name]; ok {
		value := e.Vars[name]
		update(&value)
		e.Vars[name] = value
		return true
	}
	if e.Parent != nil {
		return e.Parent.updateVar(name, update)
	}
	return false
}

type Typechecker struct {
	Global        *TypeEnvironment
	Externs       map[string]bool
	ModuleAliases map[string]string
}

func NewTypechecker() *Typechecker {
	tc := &Typechecker{
		Global:        NewTypeEnvironment(nil),
		Externs:       make(map[string]bool),
		ModuleAliases: make(map[string]string),
	}
	void := PrimitiveType{Name: "void"}
	i32 := PrimitiveType{Name: "i32"}
	i64 := PrimitiveType{Name: "i64"}
	boolType := PrimitiveType{Name: "bool"}
	str := StringType{}
	tc.Global.Funcs["println"] = FuncSignature{RetType: void}
	tc.Global.Funcs["println_str"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: void}
	tc.Global.Funcs["print"] = FuncSignature{RetType: void}
	tc.Global.Funcs["time_now_ms"] = FuncSignature{RetType: i64}
	tc.Global.Funcs["time_now_ns"] = FuncSignature{RetType: i64}
	tc.Global.Funcs["sleep_ms"] = FuncSignature{Params: []FieldDecl{{Name: "ms", Type: i32}}, RetType: void}
	tc.Global.Funcs["yield"] = FuncSignature{RetType: void}
	tc.Global.Funcs["exit_process"] = FuncSignature{Params: []FieldDecl{{Name: "code", Type: i32}}, RetType: void}
	tc.Global.Funcs["int_to_str"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: nil}}, RetType: str}
	tc.Global.Funcs["str_to_int"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: ResultType{Ok: i64, Err: str}}
	tc.Global.Funcs["read_file"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: str, Err: str}}
	tc.Global.Funcs["write_file"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}, {Name: "content", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["os_args"] = FuncSignature{RetType: SliceType{Elem: str}}
	tc.Global.Funcs["os_environ"] = FuncSignature{RetType: SliceType{Elem: str}}
	tc.Global.Funcs["random_get_u64"] = FuncSignature{RetType: PrimitiveType{Name: "u64"}}
	tc.Global.Funcs["env_get"] = FuncSignature{Params: []FieldDecl{{Name: "key", Type: str}}, RetType: OptionalType{Inner: str}}
	tc.Global.Funcs["file_exists"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: boolType}
	tc.Global.Funcs["path_join"] = FuncSignature{Params: []FieldDecl{{Name: "a", Type: str}, {Name: "b", Type: str}}, RetType: str}
	tc.Global.Funcs["str_replace"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "old_s", Type: str}, {Name: "new_s", Type: str}}, RetType: str}
	tc.Global.Funcs["str_contains"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "substr", Type: str}}, RetType: boolType}
	tc.Global.Funcs["str_starts_with"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "prefix", Type: str}}, RetType: boolType}
	tc.Global.Funcs["str_ends_with"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "suffix", Type: str}}, RetType: boolType}
	tc.Global.Funcs["str_trim"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: str}
	tc.Global.Funcs["str_index_of"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "substr", Type: str}}, RetType: i32}
	tc.Global.Funcs["str_split"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}, {Name: "delim", Type: str}}, RetType: SliceType{Elem: str}}
	tc.Global.Funcs["dir_list"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: SliceType{Elem: str}, Err: str}}
	tc.Global.Funcs["dir_create"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["file_remove"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["env_set"] = FuncSignature{Params: []FieldDecl{{Name: "key", Type: str}, {Name: "val", Type: str}}, RetType: boolType}
	tc.Global.Funcs["cmd_run"] = FuncSignature{Params: []FieldDecl{{Name: "cmd", Type: str}}, RetType: i32}
	tc.Global.Funcs["abs"] = FuncSignature{Params: []FieldDecl{{Name: "n", Type: i64}}, RetType: i64}
	tc.Global.Funcs["min"] = FuncSignature{Params: []FieldDecl{{Name: "a", Type: i64}, {Name: "b", Type: i64}}, RetType: i64}
	tc.Global.Funcs["max"] = FuncSignature{Params: []FieldDecl{{Name: "a", Type: i64}, {Name: "b", Type: i64}}, RetType: i64}
	tc.Global.Funcs["sqrt"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["pow"] = FuncSignature{Params: []FieldDecl{{Name: "base", Type: PrimitiveType{Name: "f64"}}, {Name: "exp", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["floor"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}
	tc.Global.Funcs["ceil"] = FuncSignature{Params: []FieldDecl{{Name: "f", Type: PrimitiveType{Name: "f64"}}}, RetType: PrimitiveType{Name: "f64"}}

	u8Slice := SliceType{Elem: PrimitiveType{Name: "u8"}}
	tc.Global.Funcs["string_from_bytes"] = FuncSignature{
		Params:  []FieldDecl{{Name: "b", Type: u8Slice}},
		RetType: ResultType{Ok: str, Err: str},
	}
	tc.Global.Funcs["wasi_fd_write"] = FuncSignature{
		Params:  []FieldDecl{{Name: "fd", Type: i32}, {Name: "data", Type: u8Slice}},
		RetType: ResultType{Ok: i32, Err: str},
	}
	tc.Global.Funcs["wasi_fd_read"] = FuncSignature{
		Params:  []FieldDecl{{Name: "fd", Type: i32}, {Name: "buf", Type: u8Slice, IsMut: true}},
		RetType: ResultType{Ok: i32, Err: str},
	}

	tc.Global.Funcs["wasi_fd_write_raw"] = FuncSignature{
		Params:  []FieldDecl{{Name: "fd", Type: i32}, {Name: "data", Type: u8Slice}},
		RetType: i32,
	}
	tc.Global.Funcs["wasi_fd_read_raw"] = FuncSignature{
		Params:  []FieldDecl{{Name: "fd", Type: i32}, {Name: "buf", Type: u8Slice, IsMut: true}},
		RetType: i32,
	}
	tc.Global.Funcs["wasi_fd_write_string_raw"] = FuncSignature{
		Params:  []FieldDecl{{Name: "fd", Type: i32}, {Name: "s", Type: str}},
		RetType: i32,
	}
	tc.Global.Funcs["wasi_path_open_raw"] = FuncSignature{
		Params: []FieldDecl{
			{Name: "path", Type: str},
			{Name: "oflags", Type: i32},
			{Name: "fdflags", Type: i32},
			{Name: "rights_base", Type: i32},
			{Name: "rights_inheriting", Type: i32},
		},
		RetType: i32,
	}
	tc.Global.Funcs["wasi_fd_close_raw"] = FuncSignature{Params: []FieldDecl{{Name: "fd", Type: i32}}, RetType: i32}
	tc.Global.Funcs["wasi_fd_sync_raw"] = FuncSignature{Params: []FieldDecl{{Name: "fd", Type: i32}}, RetType: i32}
	tc.Global.Funcs["wasi_fd_seek_raw"] = FuncSignature{
		Params:  []FieldDecl{{Name: "fd", Type: i32}, {Name: "offset", Type: i32}, {Name: "whence", Type: i32}},
		RetType: i32,
	}
	tc.Global.Funcs["wasi_path_create_dir_raw"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: i32}
	tc.Global.Funcs["wasi_path_unlink_raw"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: i32}
	tc.Global.Funcs["wasi_path_remove_dir_raw"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: i32}
	tc.Global.Funcs["wasi_path_filestat_raw"] = FuncSignature{
		Params:  []FieldDecl{{Name: "path", Type: str}, {Name: "buf", Type: ArrayType{Elem: i32, Size: 16}, IsMut: true}},
		RetType: i32,
	}
	tc.Global.Funcs["wasi_fd_filestat_raw"] = FuncSignature{
		Params:  []FieldDecl{{Name: "fd", Type: i32}, {Name: "buf", Type: ArrayType{Elem: i32, Size: 16}, IsMut: true}},
		RetType: i32,
	}

	tc.Global.Funcs["format"] = FuncSignature{RetType: str}
	tc.Global.Funcs["StringBuilder.append"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_str"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_int"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: i64}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_i64"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: i64}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_char"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: i32}}, RetType: void}
	tc.Global.Funcs["StringBuilder.append_bool"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: boolType}}, RetType: void}
	tc.Global.Funcs["StringBuilder.to_string"] = FuncSignature{RetType: str}

	return tc
}

// moduleOf returns the canonical module portion of a (possibly qualified)
// declaration name.
func (tc *Typechecker) moduleOf(qualified string) string {
	if i := strings.IndexByte(qualified, '.'); i > 0 {
		return qualified[:i]
	}
	if tc.Global != nil && tc.Global.CurModule != "" {
		return tc.Global.CurModule
	}
	return ""
}

// moduleOfFn returns the canonical module for a function or method.
// Method names (fn.Name) are not qualified by qualifyDecls, so their
// module is derived from the qualified receiver type instead.
func (tc *Typechecker) moduleOfFn(fn *FnDecl) string {
	if fn.Receiver != nil {
		if i := strings.IndexByte(fn.Receiver.Type.String(), '.'); i > 0 {
			return fn.Receiver.Type.String()[:i]
		}
	}
	if i := strings.IndexByte(fn.Name, '.'); i > 0 {
		return fn.Name[:i]
	}
	if tc.Global != nil && tc.Global.CurModule != "" {
		return tc.Global.CurModule
	}
	return ""
}

// canonicalName maps a type reference to a canonical, module-qualified name.
// Idempotent.
func (tc *Typechecker) canonicalName(name string) string {
	if name == "" {
		return name
	}
	switch name {
	case "i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64",
		"f32", "f64", "bool", "void", "string", "StringBuilder":
		return name
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		alias, rest := name[:i], name[i+1:]
		if mod, ok := tc.ModuleAliases[alias]; ok {
			return mod + "." + rest
		}
		return name
	}
	if tc.Global != nil && tc.Global.CurModule != "" {
		qualified := tc.Global.CurModule + "." + name
		if _, ok := tc.Global.Structs[qualified]; ok {
			if os.Getenv("UBE_DEBUG_TYPES") != "" {
				fmt.Fprintf(os.Stderr, "DEBUG canonical %q -> %q\n", name, qualified)
			}
			return qualified
		}
		if _, ok := tc.Global.Enums[qualified]; ok {
			return qualified
		}
		if _, ok := tc.Global.Traits[qualified]; ok {
			return qualified
		}
		if _, ok := tc.Global.Funcs[qualified]; ok {
			if os.Getenv("UBE_DEBUG_TYPES") != "" {
				fmt.Fprintf(os.Stderr, "DEBUG canonical %q -> %q\n", name, qualified)
			}
			return qualified
		}
		if os.Getenv("UBE_DEBUG_TYPES") != "" {
			fmt.Fprintf(os.Stderr, "DEBUG canonical %q -> UNRESOLVED (curModule=%q)\n", name, tc.Global.CurModule)
		}
	}
	return name
}

func (tc *Typechecker) resolveType(t UbeType) UbeType {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case CustomType:
		name := tc.canonicalName(v.Name)
		if _, isTrait := tc.Global.Traits[name]; isTrait {
			return TraitType{Name: name}
		}
		return CustomType{Name: name, Reg: v.Reg}
	case TraitType:
		return TraitType{Name: tc.canonicalName(v.Name), Reg: v.Reg}
	case SliceType:
		return SliceType{Elem: tc.resolveType(v.Elem)}
	case ArrayType:
		return ArrayType{Elem: tc.resolveType(v.Elem), Size: v.Size}
	case OptionalType:
		return OptionalType{Inner: tc.resolveType(v.Inner)}
	case ResultType:
		return ResultType{Ok: tc.resolveType(v.Ok), Err: tc.resolveType(v.Err)}
	case ChanType:
		return ChanType{Elem: tc.resolveType(v.Elem)}
	case PoolType:
		return PoolType{Elem: tc.resolveType(v.Elem)}
	case TupleType:
		elems := make([]UbeType, len(v.Elems))
		for i, e := range v.Elems {
			elems[i] = tc.resolveType(e)
		}
		return TupleType{Elems: elems}
	case GenericInstanceType:
		args := make([]UbeType, len(v.TypeArgs))
		for i, a := range v.TypeArgs {
			args[i] = tc.resolveType(a)
		}
		return GenericInstanceType{Base: tc.canonicalName(v.Base), TypeArgs: args, Reg: v.Reg}
	default:
		return t
	}
}

func (tc *Typechecker) Check(prog *Program) error {
	if tc.ModuleAliases == nil {
		tc.ModuleAliases = make(map[string]string)
	}
	if prog != nil {
		for alias, canonical := range prog.ModuleAliases {
			tc.ModuleAliases[alias] = canonical
		}
		tc.Global.CurModule = prog.EntryModule
		if os.Getenv("UBE_DEBUG_TYPES") != "" {
			for alias, canonical := range prog.ModuleAliases {
				fmt.Fprintf(os.Stderr, "DEBUG ALIAS %q -> %q\n", alias, canonical)
			}
			for _, decl := range prog.Decls {
				if fn, ok := decl.(*FnDecl); ok {
					fmt.Fprintf(os.Stderr, "DEBUG FN %q\n", fn.Name)
				}
				if st, ok := decl.(*StructDecl); ok {
					fmt.Fprintf(os.Stderr, "DEBUG ST %q\n", st.Name)
				}
			}
		}
	}

	tc.Global.Structs["UbeHandle"] = []FieldDecl{
		{Name: "index", Type: PrimitiveType{Name: "u32"}},
		{Name: "gen", Type: PrimitiveType{Name: "u32"}},
	}

	if err := tc.monomorphize(prog); err != nil {
		return err
	}

	declPackage := func(pkg string) string {
		if pkg != "" {
			return pkg
		}
		return prog.EntryModule
	}
	validateInPackage := func(t UbeType, loc SourceLocation, pkg string) error {
		prev := tc.Global.CurModule
		tc.Global.CurModule = declPackage(pkg)
		err := tc.validateType(t, loc)
		tc.Global.CurModule = prev
		return err
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			tc.Global.Structs[d.Name] = d.Fields
			tc.Global.StructMeta[d.Name] = StructMeta{Fields: d.Fields, IsPub: d.IsPub, Package: d.Package}
		case *EnumDecl:
			tc.Global.Enums[d.Name] = d.Variants
			tc.Global.EnumMeta[d.Name] = EnumMeta{Variants: d.Variants, IsPub: d.IsPub, Package: d.Package}
		case *TraitDecl:
			tc.Global.Traits[d.Name] = d
			tc.Global.TraitMeta[d.Name] = TraitMeta{Trait: d, IsPub: d.IsPub, Package: d.Package}
		}
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			for _, f := range d.Fields {
				if err := validateInPackage(f.Type, d.At, d.Package); err != nil {
					return err
				}
			}
		case *EnumDecl:
			for _, v := range d.Variants {
				for _, vt := range v.Types {
					if err := validateInPackage(vt, d.At, d.Package); err != nil {
						return err
					}
				}
			}
		case *TraitDecl:
			for _, m := range d.Methods {
				for _, p := range m.Params {
					if err := validateInPackage(p.Type, m.At, d.Package); err != nil {
						return err
					}
				}
				if err := validateInPackage(m.RetType, m.At, d.Package); err != nil {
					return err
				}
			}
		}
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *FnDecl:
			if d.Receiver != nil {
				if err := validateInPackage(d.Receiver.Type, d.At, d.Package); err != nil {
					return err
				}
			}
			for _, p := range d.Params {
				if err := validateInPackage(p.Type, d.At, d.Package); err != nil {
					return err
				}
			}
			if err := validateInPackage(d.RetType, d.At, d.Package); err != nil {
				return err
			}

			fnKey := d.Name
			if d.Receiver != nil {
				fnKey = fmt.Sprintf("%s.%s", d.Receiver.Type, d.Name)
			}
			tc.Global.Funcs[fnKey] = FuncSignature{
				Params:       d.Params,
				RetType:      d.RetType,
				ReceiverMut:  d.Receiver != nil && d.Receiver.IsMut,
				ReceiverView: d.Receiver != nil && d.Receiver.IsView,
				IsPub:        d.IsPub,
				Package:      d.Package,
			}
		case *ExternFnDecl:
			for _, p := range d.Params {
				if err := validateInPackage(p.Type, d.At, d.Package); err != nil {
					return err
				}
			}
			if err := validateInPackage(d.RetType, d.At, d.Package); err != nil {
				return err
			}
			tc.Global.Funcs[d.Name] = FuncSignature{Params: d.Params, RetType: d.RetType, IsPub: d.IsPub, Package: d.Package}
			tc.Externs[d.Name] = true
		}
	}

	tc.computeEscapingParams(prog)

	for _, decl := range prog.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			env := NewTypeEnvironment(tc.Global)
			env.CurModule = fn.Package
			if env.CurModule == "" {
				env.CurModule = tc.moduleOfFn(fn)
			}
			env.CurRet = fn.RetType
			env.ScopeDepth = 0
			if fn.Receiver != nil {
				env.Vars[fn.Receiver.Name] = VarInfo{Type: fn.Receiver.Type, IsMut: fn.Receiver.IsMut, IsView: fn.Receiver.IsView, Depth: 0, ValueDepth: 0}
			}
			for _, p := range fn.Params {
				env.Vars[p.Name] = VarInfo{Type: p.Type, IsMut: p.IsMut, IsView: p.IsView, Depth: 0, ValueDepth: 0}
			}
			// canonicalName reads tc.Global.CurModule; make it reflect the
			// current function's module for the duration of this body check.
			prevModule := tc.Global.CurModule
			tc.Global.CurModule = env.CurModule
			err := tc.checkBlock(fn.Body, env)
			tc.Global.CurModule = prevModule
			if err != nil {
				return err
			}
		}
	}
	return nil
}
