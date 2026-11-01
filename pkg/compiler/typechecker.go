package compiler

import "fmt"

type VarInfo struct {
	Type       UbeType
	IsMut      bool
	Depth      int
	ValueDepth int
	LoopDepth  int
	Moved      bool
}

type FuncSignature struct {
	Params      []FieldDecl
	RetType     UbeType
	Escaping    []bool
	ReceiverMut bool
	IsPub       bool
	File        string
}

type StructMeta struct {
	Fields []FieldDecl
	IsPub  bool
	File   string
}

type EnumMeta struct {
	Variants []VariantDecl
	IsPub    bool
	File     string
}

type TraitMeta struct {
	Trait *TraitDecl
	IsPub bool
	File  string
}

type TypeEnvironment struct {
	Parent         *TypeEnvironment
	Vars           map[string]VarInfo
	Structs        map[string][]FieldDecl
	StructMeta     map[string]StructMeta
	Enums          map[string][]VariantDecl
	EnumMeta       map[string]EnumMeta
	Traits         map[string]*TraitDecl
	TraitMeta      map[string]TraitMeta
	Funcs          map[string]FuncSignature
	GenericStructs map[string]*StructDecl
	GenericFuncs   map[string]*FnDecl
	CurFile        string
	CurRet         UbeType
	LoopDepth      int
	ScopeDepth     int
}

func NewTypeEnvironment(parent *TypeEnvironment) *TypeEnvironment {
	return &TypeEnvironment{
		Parent:         parent,
		Vars:           make(map[string]VarInfo),
		Structs:        make(map[string][]FieldDecl),
		StructMeta:     make(map[string]StructMeta),
		Enums:          make(map[string][]VariantDecl),
		EnumMeta:       make(map[string]EnumMeta),
		Traits:         make(map[string]*TraitDecl),
		TraitMeta:      make(map[string]TraitMeta),
		Funcs:          make(map[string]FuncSignature),
		GenericStructs: make(map[string]*StructDecl),
		GenericFuncs:   make(map[string]*FnDecl),
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
		env.CurFile = parent.CurFile
		env.CurRet = parent.CurRet
		env.LoopDepth = parent.LoopDepth
		env.ScopeDepth = parent.ScopeDepth
		env.Traits = parent.Traits
		env.TraitMeta = parent.TraitMeta
		env.GenericStructs = parent.GenericStructs
		env.GenericFuncs = parent.GenericFuncs
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
	Global  *TypeEnvironment
	Externs map[string]bool
}

func NewTypechecker() *Typechecker {
	tc := &Typechecker{Global: NewTypeEnvironment(nil), Externs: make(map[string]bool)}
	void := PrimitiveType{Name: "void"}
	i32 := PrimitiveType{Name: "i32"}
	i64 := PrimitiveType{Name: "i64"}
	boolType := PrimitiveType{Name: "bool"}
	str := StringType{}
	tc.Global.Funcs["println"] = FuncSignature{RetType: void}
	tc.Global.Funcs["println_str"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: void}
	tc.Global.Funcs["print"] = FuncSignature{RetType: void}
	tc.Global.Funcs["time_now_ms"] = FuncSignature{RetType: i64}
	tc.Global.Funcs["sleep_ms"] = FuncSignature{Params: []FieldDecl{{Name: "ms", Type: i32}}, RetType: void}
	tc.Global.Funcs["yield"] = FuncSignature{RetType: void}
	tc.Global.Funcs["exit_process"] = FuncSignature{Params: []FieldDecl{{Name: "code", Type: i32}}, RetType: void}
	tc.Global.Funcs["int_to_str"] = FuncSignature{Params: []FieldDecl{{Name: "val", Type: nil}}, RetType: str}
	tc.Global.Funcs["str_to_int"] = FuncSignature{Params: []FieldDecl{{Name: "s", Type: str}}, RetType: ResultType{Ok: i64, Err: str}}
	tc.Global.Funcs["read_file"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}}, RetType: ResultType{Ok: str, Err: str}}
	tc.Global.Funcs["write_file"] = FuncSignature{Params: []FieldDecl{{Name: "path", Type: str}, {Name: "content", Type: str}}, RetType: ResultType{Ok: boolType, Err: str}}
	tc.Global.Funcs["os_args"] = FuncSignature{RetType: SliceType{Elem: str}}
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

	// StringBuilder receiver methods
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

func (tc *Typechecker) resolveType(t UbeType) UbeType {
	if t == nil {
		return nil
	}
	switch v := t.(type) {
	case CustomType:
		if _, isTrait := tc.Global.Traits[v.Name]; isTrait {
			return TraitType{Name: v.Name}
		}
		return v
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
	case PointerType:
		return PointerType{Base: tc.resolveType(v.Base)}
	default:
		return t
	}
}

func (tc *Typechecker) Check(prog *Program) error {
	tc.Global.Structs["UbeHandle"] = []FieldDecl{
		{Name: "index", Type: PrimitiveType{Name: "u32"}},
		{Name: "gen", Type: PrimitiveType{Name: "u32"}},
	}

	// 1. Monomorphization Pass for Generics
	if err := tc.monomorphize(prog); err != nil {
		return err
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			tc.Global.Structs[d.Name] = d.Fields
			tc.Global.StructMeta[d.Name] = StructMeta{Fields: d.Fields, IsPub: d.IsPub, File: d.At.File}
		case *EnumDecl:
			tc.Global.Enums[d.Name] = d.Variants
			tc.Global.EnumMeta[d.Name] = EnumMeta{Variants: d.Variants, IsPub: d.IsPub, File: d.At.File}
		case *TraitDecl:
			tc.Global.Traits[d.Name] = d
			tc.Global.TraitMeta[d.Name] = TraitMeta{Trait: d, IsPub: d.IsPub, File: d.At.File}
		}
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			for _, f := range d.Fields {
				if err := tc.validateType(f.Type, d.At); err != nil {
					return err
				}
			}
		case *EnumDecl:
			for _, v := range d.Variants {
				for _, vt := range v.Types {
					if err := tc.validateType(vt, d.At); err != nil {
						return err
					}
				}
			}
		case *TraitDecl:
			for _, m := range d.Methods {
				for _, p := range m.Params {
					if err := tc.validateType(p.Type, m.At); err != nil {
						return err
					}
				}
				if err := tc.validateType(m.RetType, m.At); err != nil {
					return err
				}
			}
		}
	}

	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *FnDecl:
			if d.Receiver != nil {
				if err := tc.validateType(d.Receiver.Type, d.At); err != nil {
					return err
				}
			}
			for _, p := range d.Params {
				if err := tc.validateType(p.Type, d.At); err != nil {
					return err
				}
			}
			if err := tc.validateType(d.RetType, d.At); err != nil {
				return err
			}

			fnKey := d.Name
			if d.Receiver != nil {
				fnKey = fmt.Sprintf("%s.%s", d.Receiver.Type, d.Name)
			}
			tc.Global.Funcs[fnKey] = FuncSignature{
				Params:      d.Params,
				RetType:     d.RetType,
				ReceiverMut: d.Receiver != nil && d.Receiver.IsMut,
				IsPub:       d.IsPub,
				File:        d.At.File,
			}
		case *ExternFnDecl:
			for _, p := range d.Params {
				if err := tc.validateType(p.Type, d.At); err != nil {
					return err
				}
			}
			if err := tc.validateType(d.RetType, d.At); err != nil {
				return err
			}
			tc.Global.Funcs[d.Name] = FuncSignature{Params: d.Params, RetType: d.RetType, IsPub: d.IsPub, File: d.At.File}
			tc.Externs[d.Name] = true
		}
	}

	tc.computeEscapingParams(prog)

	for _, decl := range prog.Decls {
		if fn, ok := decl.(*FnDecl); ok {
			env := NewTypeEnvironment(tc.Global)
			env.CurFile = fn.At.File
			env.CurRet = fn.RetType
			env.ScopeDepth = 0
			if fn.Receiver != nil {
				env.Vars[fn.Receiver.Name] = VarInfo{Type: fn.Receiver.Type, IsMut: fn.Receiver.IsMut, Depth: 0, ValueDepth: 0}
			}
			for _, p := range fn.Params {
				env.Vars[p.Name] = VarInfo{Type: p.Type, IsMut: p.IsMut, Depth: 0, ValueDepth: 0}
			}
			if err := tc.checkBlock(fn.Body, env); err != nil {
				return err
			}
		}
	}
	return nil
}
