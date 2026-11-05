// File: pkg/compiler/module.go
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type ModuleLoader struct {
	visited, visiting map[string]bool
	allDecls          []Decl
	entryDir          string
	moduleAliases     map[string]string
}

func NewModuleLoader() *ModuleLoader {
	return &ModuleLoader{
		visited:       map[string]bool{},
		visiting:      map[string]bool{},
		moduleAliases: map[string]string{},
	}
}

func (m *ModuleLoader) Load(entry string) (*Program, error) {
	abs, err := filepath.Abs(entry)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", abs, err)
	}
	return m.LoadSource(abs, string(b))
}

func (m *ModuleLoader) LoadSource(entry, source string) (*Program, error) {
	m.visited = map[string]bool{}
	m.visiting = map[string]bool{}
	m.allDecls = nil
	m.moduleAliases = map[string]string{}
	abs, err := filepath.Abs(entry)
	if err != nil {
		abs = entry
	}
	m.entryDir = filepath.Dir(abs)

	toks, err := NewLexer(abs, source).Tokenize()
	if err != nil {
		return nil, err
	}
	prog, err := NewParser(toks).ParseProgram()
	if err != nil {
		return nil, err
	}

	entryModule := strings.TrimSuffix(filepath.Base(abs), ".ube")
	m.visited[abs] = true

	for _, d := range prog.Decls {
		if imp, ok := d.(*ImportDecl); ok {
			var importPath string
			if _, ok := isStdImport(imp.Path); ok {
				importPath = imp.Path
			} else {
				importPath = filepath.Join(filepath.Dir(abs), imp.Path)
			}
			if err := m.loadModule(importPath); err != nil {
				return nil, err
			}
			m.registerImportAlias(imp)
		} else {
			m.allDecls = append(m.allDecls, d)
		}
	}

	return &Program{
		At:            SourceLocation{File: abs, Line: 1, Col: 1},
		Decls:         m.allDecls,
		ModuleAliases: m.moduleAliases,
		EntryModule:   entryModule,
	}, nil
}

func moduleBaseName(path string) string {
	if key, ok := isStdImport(path); ok {
		parts := strings.Split(key, "/")
		return parts[len(parts)-1]
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".ube")
}

func (m *ModuleLoader) registerImportAlias(imp *ImportDecl) {
	canonical := moduleBaseName(imp.Path)
	if canonical == "" || canonical == "." || canonical == ".." {
		return
	}
	local := imp.Alias
	if local == "" {
		local = canonical
	}
	m.moduleAliases[local] = canonical
}

func isStdImport(p string) (string, bool) {
	if !strings.HasPrefix(p, "std/") {
		return "", false
	}
	key := strings.TrimPrefix(p, "std/")
	key = strings.TrimSuffix(key, ".ube")
	return key, true
}

func validStdKey(key string) bool {
	if key == "" || key == "." || key == ".." || strings.HasPrefix(key, "/") || strings.Contains(key, `\`) {
		return false
	}
	for _, part := range strings.Split(key, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func (m *ModuleLoader) resolveStdSource(key string) (string, error) {
	if !validStdKey(key) {
		return "", fmt.Errorf("invalid standard library module 'std/%s'", key)
	}
	clean := strings.TrimSuffix(key, ".ube")
	subPath := filepath.Join(clean, clean+".ube")
	flatPath := clean + ".ube"
	candidates := []string{
		filepath.Join("std", subPath),
		filepath.Join("std", flatPath),
	}
	if m.entryDir != "" {
		candidates = append(candidates,
			filepath.Join(m.entryDir, "std", subPath),
			filepath.Join(m.entryDir, "std", flatPath),
		)
	}
	for _, root := range filepath.SplitList(os.Getenv("UBE_PATH")) {
		if root == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, "std", subPath),
			filepath.Join(root, "std", flatPath),
			filepath.Join(root, flatPath),
		)
	}
	for _, candidate := range candidates {
		if src, err := os.ReadFile(candidate); err == nil {
			return string(src), nil
		}
	}
	if src, ok := stdlibSource(clean); ok {
		return src, nil
	}
	return "", fmt.Errorf("unknown standard library module 'std/%s'", key)
}

func (m *ModuleLoader) loadRecursive(path string) error {
	return m.loadModule(path)
}

func (m *ModuleLoader) loadModule(path string) error {
	var (
		abs string
		src string
	)
	stdKey, fromStd := isStdImport(path)
	if fromStd {
		abs = "std:" + stdKey
	} else {
		var err error
		abs, err = filepath.Abs(path)
		if err != nil {
			return err
		}
		abs = filepath.Clean(abs)
		if _, err := os.Stat(abs); os.IsNotExist(err) && !strings.HasSuffix(abs, ".ube") {
			if _, err2 := os.Stat(abs + ".ube"); err2 == nil {
				abs = abs + ".ube"
			}
		}
	}

	if m.visited[abs] {
		return nil
	}
	if m.visiting[abs] {
		return fmt.Errorf("cyclic import detected: %s", path)
	}
	m.visiting[abs] = true

	if fromStd {
		moduleSrc, err := m.resolveStdSource(stdKey)
		if err != nil {
			return err
		}
		src = moduleSrc
	} else {
		b, err := os.ReadFile(abs)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", abs, err)
		}
		src = string(b)
	}

	toks, err := NewLexer(abs, src).Tokenize()
	if err != nil {
		return err
	}
	prog, err := NewParser(toks).ParseProgram()
	if err != nil {
		return err
	}

	canonical := moduleBaseName(abs)
	if fromStd {
		canonical = stdKey
	}
	qualifyDecls(prog, canonical)

	for _, d := range prog.Decls {
		if imp, ok := d.(*ImportDecl); ok {
			var importPath string
			if _, ok := isStdImport(imp.Path); ok {
				importPath = imp.Path
			} else if fromStd {
				return fmt.Errorf("standard library module '%s' may only import other 'std/...' modules (got %q)", abs, imp.Path)
			} else {
				importPath = filepath.Join(filepath.Dir(abs), imp.Path)
			}
			if err := m.loadModule(importPath); err != nil {
				return err
			}
			m.registerImportAlias(imp)
		} else {
			m.allDecls = append(m.allDecls, d)
		}
	}
	delete(m.visiting, abs)
	m.visited[abs] = true
	return nil
}

func qualifyDecls(prog *Program, canonical string) {
	if canonical == "" {
		return
	}
	// Collect the names this module itself declares. Only those get
	// qualified. References to imported names must be left untouched so
	// that the typechecker's canonicalName (which has ModuleAliases and
	// the full symbol table) resolves them correctly. Qualifying a bare
	// reference like `Stack` to `main.Stack` when `Stack` actually lives
	// in `collections` is the "unknown generic struct 'Stack'" bug.
	local := map[string]bool{}
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			local[d.Name] = true
		case *EnumDecl:
			local[d.Name] = true
		case *TraitDecl:
			local[d.Name] = true
		case *FnDecl:
			if d.Receiver == nil {
				local[d.Name] = true
			}
		}
	}
	qualify := func(name string) string {
		if name == "" || strings.Contains(name, ".") || !local[name] {
			return name
		}
		return canonical + "." + name
	}
	for _, decl := range prog.Decls {
		switch d := decl.(type) {
		case *StructDecl:
			d.Name = qualify(d.Name)
			for i := range d.Fields {
				d.Fields[i].Type = qualifyType(d.Fields[i].Type, qualify)
			}
		case *EnumDecl:
			d.Name = qualify(d.Name)
			for i := range d.Variants {
				for j, vt := range d.Variants[i].Types {
					d.Variants[i].Types[j] = qualifyType(vt, qualify)
				}
			}
		case *TraitDecl:
			d.Name = qualify(d.Name)
			for i, st := range d.Supertraits {
				d.Supertraits[i] = qualify(st)
			}
			for i := range d.Methods {
				for j := range d.Methods[i].Params {
					d.Methods[i].Params[j].Type = qualifyType(d.Methods[i].Params[j].Type, qualify)
				}
				d.Methods[i].RetType = qualifyType(d.Methods[i].RetType, qualify)
			}
		case *FnDecl:
			if d.Receiver == nil {
				d.Name = qualify(d.Name)
			} else {
				d.Receiver.Type = qualifyType(d.Receiver.Type, qualify)
			}
			for i := range d.Params {
				d.Params[i].Type = qualifyType(d.Params[i].Type, qualify)
			}
			d.RetType = qualifyType(d.RetType, qualify)
		case *ExternFnDecl:
			for i := range d.Params {
				d.Params[i].Type = qualifyType(d.Params[i].Type, qualify)
			}
			d.RetType = qualifyType(d.RetType, qualify)
		}
	}
}

func qualifyType(t UbeType, qualify func(string) string) UbeType {
	switch v := t.(type) {
	case nil:
		return nil
	case CustomType:
		if strings.Contains(v.Name, ".") {
			return v
		}
		return CustomType{Name: qualify(v.Name), Reg: v.Reg}
	case TraitType:
		if strings.Contains(v.Name, ".") {
			return v
		}
		return TraitType{Name: qualify(v.Name), Reg: v.Reg}
	case SliceType:
		return SliceType{Elem: qualifyType(v.Elem, qualify), Reg: v.Reg}
	case ArrayType:
		return ArrayType{Elem: qualifyType(v.Elem, qualify), Size: v.Size, Reg: v.Reg}
	case OptionalType:
		return OptionalType{Inner: qualifyType(v.Inner, qualify), Reg: v.Reg}
	case ResultType:
		return ResultType{Ok: qualifyType(v.Ok, qualify), Err: qualifyType(v.Err, qualify), Reg: v.Reg}
	case ChanType:
		return ChanType{Elem: qualifyType(v.Elem, qualify), Reg: v.Reg}
	case PoolType:
		return PoolType{Elem: qualifyType(v.Elem, qualify), Reg: v.Reg}
	case TupleType:
		elems := make([]UbeType, len(v.Elems))
		for i, e := range v.Elems {
			elems[i] = qualifyType(e, qualify)
		}
		return TupleType{Elems: elems, Reg: v.Reg}
	case GenericInstanceType:
		args := make([]UbeType, len(v.TypeArgs))
		for i, a := range v.TypeArgs {
			args[i] = qualifyType(a, qualify)
		}
		base := v.Base
		if !strings.Contains(base, ".") {
			base = qualify(base)
		}
		return GenericInstanceType{Base: base, TypeArgs: args, Reg: v.Reg}
	default:
		return t
	}
}
