// File: pkg/compiler/module.go
package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ModuleLoader implements Ube's Go-style package model:
//
//   - one directory is one package;
//   - every .ube file in that directory must declare the same package name;
//   - all files in the package share one merged declaration scope;
//   - imports name package directories, never individual files.
//
// Declarations from non-root packages are qualified once, at package load time,
// using the package's declared name. That gives the rest of the compiler a
// canonical, unambiguous symbol: `<package>.<Symbol>`.
type ModuleLoader struct {
	visitingDirs  map[string]bool
	dirPackages   map[string]string
	allDecls      []Decl
	entryDir      string
	moduleAliases map[string]string

	// FileReadOverlay, when set, is consulted before the filesystem. The LSP
	// uses it to feed unsaved buffer contents across open tabs so that
	// sibling-file resolution and diagnostics reflect in-editor state.
	FileReadOverlay func(path string) ([]byte, bool)
}

func (m *ModuleLoader) readFile(path string) ([]byte, error) {
	if m.FileReadOverlay != nil {
		if data, ok := m.FileReadOverlay(path); ok {
			return data, nil
		}
	}
	return os.ReadFile(path)
}

func NewModuleLoader() *ModuleLoader {
	return &ModuleLoader{
		visitingDirs:  map[string]bool{},
		dirPackages:   map[string]string{},
		moduleAliases: map[string]string{},
	}
}

func (m *ModuleLoader) reset(entryDir string) {
	m.visitingDirs = map[string]bool{}
	m.dirPackages = map[string]string{}
	m.allDecls = nil
	m.entryDir = entryDir
	m.moduleAliases = map[string]string{}
}

// Load builds a program from a package directory, or from the directory that
// contains the supplied entry file.
func (m *ModuleLoader) Load(target string) (*Program, error) {
	return m.load(target, false)
}

// LoadTests is like Load but also includes `*_test.ube` files from the target
// package directory.
func (m *ModuleLoader) LoadTests(target string) (*Program, error) {
	return m.load(target, true)
}

func (m *ModuleLoader) load(target string, includeTests bool) (*Program, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}

	info, statErr := os.Stat(abs)
	if statErr != nil {
		// Accept an entry file without the .ube suffix.
		if !strings.HasSuffix(abs, ".ube") {
			if fileInfo, err2 := os.Stat(abs + ".ube"); err2 == nil {
				abs = abs + ".ube"
				info = fileInfo
				statErr = nil
			}
		}
		if statErr != nil {
			return nil, fmt.Errorf("failed to read %s: %w", abs, statErr)
		}
	}

	dir := abs
	if !info.IsDir() {
		dir = filepath.Dir(abs)
		if strings.HasSuffix(filepath.Base(abs), "_test.ube") {
			includeTests = true
		}
	}

	m.reset(dir)
	pkgName, err := m.loadPackageDir(dir, true, includeTests)
	if err != nil {
		return nil, err
	}
	return &Program{
		At:            SourceLocation{File: dir, Line: 1, Col: 1},
		Package:       pkgName,
		Decls:         m.allDecls,
		ModuleAliases: m.moduleAliases,
		EntryModule:   pkgName,
	}, nil
}

// LoadSource parses an explicit source buffer (used by the language server and
// tests). Imports are still resolved as package-directory imports relative to
// the entry file's directory.
func (m *ModuleLoader) LoadSource(entry, source string) (*Program, error) {
	abs, err := filepath.Abs(entry)
	if err != nil {
		abs = entry
	}
	dir := filepath.Dir(abs)
	m.reset(dir)

	toks, err := NewLexer(abs, source).Tokenize()
	if err != nil {
		return nil, err
	}
	fileProg, err := NewParser(toks).ParseProgram()
	if err != nil {
		return nil, err
	}

	pkgName := fileProg.Package

	var decls []Decl
	var imports []*ImportDecl
	for _, d := range fileProg.Decls {
		switch d := d.(type) {
		case *PackageDecl:
			// Keep the clause in Decls so LSP definition/hover can locate
			// `package <name>`. The typechecker and wasm emitter skip
			// declaration kinds they do not recognize.
			decls = append(decls, d)
		case *ImportDecl:
			imports = append(imports, d)
		default:
			decls = append(decls, d)
		}
	}

	// Load sibling .ube files in the same directory that declare the same
	// package, so cross-file references resolve inside the editor before the
	// file has been saved. A mismatched package in the same folder is an
	// immediate error.
	includeTests := strings.HasSuffix(filepath.Base(abs), "_test.ube")
	if files, err := ubePackageFiles(dir, includeTests); err == nil {
		for _, file := range files {
			if filepath.Clean(file) == filepath.Clean(abs) {
				continue
			}
			srcBytes, err := m.readFile(file)
			if err != nil {
				continue
			}
			siblingToks, err := NewLexer(file, string(srcBytes)).Tokenize()
			if err != nil {
				return nil, err
			}
			siblingProg, err := NewParser(siblingToks).ParseProgram()
			if err != nil {
				return nil, err
			}
			if siblingProg.Package != pkgName {
				return nil, fmt.Errorf(
					"found packages '%s' (%s) and '%s' (%s) in directory '%s' (all files in a folder must share the same package)",
					pkgName, filepath.Base(abs), siblingProg.Package, filepath.Base(file), dir,
				)
			}
			for _, d := range siblingProg.Decls {
				switch d := d.(type) {
				case *PackageDecl:
					decls = append(decls, d)
				case *ImportDecl:
					imports = append(imports, d)
				default:
					decls = append(decls, d)
				}
			}
		}
	}

	// Match how this package would look when imported by someone else.
	if pkgName != "main" {
		qualifyPackageDecls(decls, pkgName)
	}
	m.allDecls = append(m.allDecls, decls...)

	for _, imp := range imports {
		if _, err := m.resolveAndLoadImport(imp, dir); err != nil {
			return nil, err
		}
	}

	return &Program{
		At:            SourceLocation{File: abs, Line: 1, Col: 1},
		Package:       pkgName,
		Decls:         m.allDecls,
		ModuleAliases: m.moduleAliases,
		EntryModule:   pkgName,
	}, nil
}

// loadPackageDir parses every source file in dir as one package and returns the
// declared package name. The package's declarations are appended to the
// loader-wide declaration list exactly once.
func (m *ModuleLoader) loadPackageDir(dir string, isRoot, includeTests bool) (string, error) {
	cleanDir := filepath.Clean(dir)

	// Check the in-progress set first so cycles are reported correctly.
	if m.visitingDirs[cleanDir] {
		return "", fmt.Errorf("cyclic package import detected: %s", cleanDir)
	}
	if name, ok := m.dirPackages[cleanDir]; ok {
		return name, nil
	}

	m.visitingDirs[cleanDir] = true
	defer delete(m.visitingDirs, cleanDir)

	files, err := ubePackageFiles(cleanDir, includeTests)
	if err != nil {
		return "", fmt.Errorf("reading package directory %s: %w", cleanDir, err)
	}
	if len(files) == 0 {
		return "", fmt.Errorf("no .ube source files found in package directory %s", cleanDir)
	}

	pkgName := ""
	firstFile := ""
	var pkgDecls []Decl
	var imports []*ImportDecl

	for _, file := range files {
		src, err := m.readFile(file)
		if err != nil {
			return "", fmt.Errorf("reading %s: %w", file, err)
		}
		toks, err := NewLexer(file, string(src)).Tokenize()
		if err != nil {
			return "", err
		}
		fileProg, err := NewParser(toks).ParseProgram()
		if err != nil {
			return "", err
		}

		if pkgName == "" {
			pkgName = fileProg.Package
			firstFile = file
		} else if fileProg.Package != pkgName {
			return "", fmt.Errorf(
				"found packages '%s' (%s) and '%s' (%s) in directory '%s' (all files in a folder must share the same package)",
				pkgName, filepath.Base(firstFile), fileProg.Package, filepath.Base(file), cleanDir,
			)
		}

		for _, d := range fileProg.Decls {
			switch d := d.(type) {
			case *PackageDecl:
				// Package clauses are metadata, not declarations to emit.
			case *ImportDecl:
				imports = append(imports, d)
			default:
				pkgDecls = append(pkgDecls, d)
			}
		}
	}

	if !isRoot {
		qualifyPackageDecls(pkgDecls, pkgName)
	}

	m.allDecls = append(m.allDecls, pkgDecls...)

	for _, imp := range imports {
		if _, err := m.resolveAndLoadImport(imp, cleanDir); err != nil {
			return "", err
		}
	}

	m.dirPackages[cleanDir] = pkgName
	return pkgName, nil
}

func ubePackageFiles(dir string, includeTests bool) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".ube") {
			continue
		}
		if !includeTests && strings.HasSuffix(name, "_test.ube") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files, nil
}

// resolveAndLoadImport loads one imported package and registers its local alias.
// Relative imports are resolved against the importing package directory, which
// keeps nested packages predictable.
func (m *ModuleLoader) resolveAndLoadImport(imp *ImportDecl, fromDir string) (string, error) {
	if stdKey, ok := isStdImport(imp.Path); ok {
		pkgName, err := m.loadStdPackage(stdKey)
		if err != nil {
			return "", err
		}
		if pkgName == "main" {
			return "", fmt.Errorf("cannot import %q: package main is the executable package, not an importable library", imp.Path)
		}
		if err := m.registerImportAlias(imp, pkgName); err != nil {
			return "", err
		}
		return pkgName, nil
	}

	if strings.HasSuffix(imp.Path, ".ube") {
		return "", fmt.Errorf("import %q targets a file; imports must name package directories, not .ube files", imp.Path)
	}

	targetDir := imp.Path
	if !filepath.IsAbs(targetDir) {
		targetDir = filepath.Join(fromDir, targetDir)
	}
	targetDir = filepath.Clean(targetDir)

	info, statErr := os.Stat(targetDir)
	if statErr != nil || !info.IsDir() {
		return "", fmt.Errorf("cannot find imported package %q (looked for directory %s)", imp.Path, targetDir)
	}

	pkgName, err := m.loadPackageDir(targetDir, false, false)
	if err != nil {
		return "", err
	}
	if pkgName == "main" {
		return "", fmt.Errorf("cannot import %q: package main is the executable package, not an importable library", imp.Path)
	}
	if err := m.registerImportAlias(imp, pkgName); err != nil {
		return "", err
	}
	return pkgName, nil
}

func (m *ModuleLoader) registerImportAlias(imp *ImportDecl, pkgName string) error {
	alias := imp.Alias
	if alias == "" {
		alias = pkgName
	}
	if alias == "" || alias == "." || alias == ".." {
		return fmt.Errorf("invalid import alias for %q", imp.Path)
	}
	if prev, ok := m.moduleAliases[alias]; ok && prev != pkgName {
		return fmt.Errorf("import alias %q refers to both package %q and package %q", alias, prev, pkgName)
	}
	m.moduleAliases[alias] = pkgName
	return nil
}

func isStdImport(p string) (string, bool) {
	if !strings.HasPrefix(p, "std/") || strings.HasSuffix(p, ".ube") {
		return "", false
	}
	return strings.TrimPrefix(p, "std/"), true
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

// loadStdPackage loads every file of a standard library package from the
// embedded std FS (with an optional on-disk override for development).
func (m *ModuleLoader) loadStdPackage(key string) (string, error) {
	if !validStdKey(key) {
		return "", fmt.Errorf("invalid standard library package 'std/%s'", key)
	}
	cacheKey := "std:" + key
	if m.visitingDirs[cacheKey] {
		return "", fmt.Errorf("cyclic standard library import detected: std/%s", key)
	}
	if name, ok := m.dirPackages[cacheKey]; ok {
		return name, nil
	}
	m.visitingDirs[cacheKey] = true
	defer delete(m.visitingDirs, cacheKey)

	files, err := stdlibPackageSources(key, m.entryDir)
	if err != nil {
		return "", err
	}

	pkgName := ""
	firstFile := ""
	var pkgDecls []Decl
	var imports []*ImportDecl

	for _, file := range files {
		toks, err := NewLexer(file.name, file.source).Tokenize()
		if err != nil {
			return "", err
		}
		fileProg, err := NewParser(toks).ParseProgram()
		if err != nil {
			return "", err
		}
		if pkgName == "" {
			pkgName = fileProg.Package
			firstFile = file.name
		} else if fileProg.Package != pkgName {
			return "", fmt.Errorf(
				"found packages '%s' (%s) and '%s' (%s) in standard library directory 'std/%s'",
				pkgName, firstFile, fileProg.Package, file.name, key,
			)
		}
		for _, d := range fileProg.Decls {
			switch d := d.(type) {
			case *PackageDecl:
			case *ImportDecl:
				imports = append(imports, d)
			default:
				pkgDecls = append(pkgDecls, d)
			}
		}
	}

	expected := key
	if i := strings.LastIndexByte(expected, '/'); i >= 0 {
		expected = expected[i+1:]
	}
	if pkgName != expected {
		return "", fmt.Errorf("standard library package 'std/%s' must declare 'package %s' (found 'package %s')", key, expected, pkgName)
	}

	qualifyPackageDecls(pkgDecls, pkgName)
	m.allDecls = append(m.allDecls, pkgDecls...)

	for _, imp := range imports {
		if _, ok := isStdImport(imp.Path); !ok {
			return "", fmt.Errorf("standard library package 'std/%s' may only import other 'std/...' packages (got %q)", key, imp.Path)
		}
		if _, err := m.resolveAndLoadImport(imp, "std:"+key); err != nil {
			return "", err
		}
	}

	m.dirPackages[cacheKey] = pkgName
	return pkgName, nil
}

// qualifyPackageDecls rewrites a package's own declared names into canonical
// `<package>.<Symbol>` form. Imported names (which contain a dot) are left
// untouched; the typechecker resolves those through the alias table.
func qualifyPackageDecls(decls []Decl, canonical string) {
	if canonical == "" || canonical == "main" {
		return
	}

	local := map[string]bool{}
	localTraits := map[string]bool{}
	for _, decl := range decls {
		switch d := decl.(type) {
		case *StructDecl:
			local[d.Name] = true
		case *EnumDecl:
			local[d.Name] = true
		case *TraitDecl:
			local[d.Name] = true
			localTraits[d.Name] = true
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
	qualifyConstraint := func(constraint string) string {
		if constraint == "" {
			return constraint
		}
		parts := strings.Split(constraint, "+")
		for i, part := range parts {
			trimmed := strings.TrimSpace(part)
			if localTraits[trimmed] {
				parts[i] = canonical + "." + trimmed
			} else {
				parts[i] = trimmed
			}
		}
		return strings.Join(parts, "+")
	}
	qualifyTypeParams := func(params []TypeParam) {
		for i := range params {
			params[i].Constraint = qualifyConstraint(params[i].Constraint)
		}
	}

	for _, decl := range decls {
		switch d := decl.(type) {
		case *StructDecl:
			d.Name = qualify(d.Name)
			qualifyTypeParams(d.TypeParams)
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
			d.Name = qualify(d.Name)
			qualifyTypeParams(d.TypeParams)
			if d.Receiver != nil {
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
