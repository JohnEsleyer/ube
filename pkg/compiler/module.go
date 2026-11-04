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

// LoadSource parses source code for an entry file and loads all imported modules recursively.
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

	visitKey := abs
	m.visited[visitKey] = true

	for _, d := range prog.Decls {
		if imp, ok := d.(*ImportDecl); ok {
			var importPath string
			if _, ok := isStdImport(imp.Path); ok {
				importPath = imp.Path
			} else {
				importPath = filepath.Join(filepath.Dir(abs), imp.Path)
			}
			childPrefix := ""
			if imp.Alias != "" {
				childPrefix = imp.Alias + "__"
			}
			m.registerImportAlias(imp, childPrefix)
			if err := m.loadModule(importPath, childPrefix); err != nil {
				return nil, err
			}
		} else {
			m.allDecls = append(m.allDecls, d)
		}
	}

	return &Program{
		At:            SourceLocation{File: abs, Line: 1, Col: 1},
		Decls:         m.allDecls,
		ModuleAliases: m.moduleAliases,
	}, nil
}

// moduleBaseName derives the default qualifier for an unaliased import:
// `std/math` -> "math", `./helpers.ube` -> "helpers".
func moduleBaseName(path string) string {
	if key, ok := isStdImport(path); ok {
		parts := strings.Split(key, "/")
		return parts[len(parts)-1]
	}
	base := filepath.Base(path)
	return strings.TrimSuffix(base, ".ube")
}

func (m *ModuleLoader) registerImportAlias(imp *ImportDecl, prefix string) {
	name := imp.Alias
	if name == "" {
		name = moduleBaseName(imp.Path)
	}
	if name == "" || name == "." || name == ".." {
		return
	}
	m.moduleAliases[name] = prefix
}

// isStdImport reports whether p refers to the standard library and returns the
// normalized module key ("math", "collections", ...).
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

// resolveStdSource finds a standard library module. The lookup order is:
//  1. ./std/<module>/<module>.ube (local project override, subdirectory layout)
//  2. ./std/<module>.ube (flat legacy override)
//  3. std/<module>/<module>.ube next to the entry module
//  4. $UBE_PATH entries
//  5. the standard library embedded in the compiler binary
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
	return m.loadModule(path, "")
}

func (m *ModuleLoader) loadModule(path, prefix string) error {
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
		// Allow extensionless local imports (e.g. "./math_helpers" -> "./math_helpers.ube")
		if _, err := os.Stat(abs); os.IsNotExist(err) && !strings.HasSuffix(abs, ".ube") {
			if _, err2 := os.Stat(abs + ".ube"); err2 == nil {
				abs = abs + ".ube"
			}
		}
	}

	// The same source file may be imported unaliased and under one or more
	// aliases. Keep those namespaces independent.
	visitKey := abs
	if prefix != "" {
		visitKey = abs + "#" + prefix
	}
	if m.visited[visitKey] {
		return nil
	}
	if m.visiting[visitKey] {
		return fmt.Errorf("cyclic import detected: %s", path)
	}
	m.visiting[visitKey] = true

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
	if prefix != "" {
		renameAliasedModule(prog, prefix)
	}

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
			childPrefix := ""
			if imp.Alias != "" {
				childPrefix = imp.Alias + "__"
			}
			m.registerImportAlias(imp, childPrefix)
			if err := m.loadModule(importPath, childPrefix); err != nil {
				return err
			}
		} else {
			m.allDecls = append(m.allDecls, d)
		}
	}
	delete(m.visiting, visitKey)
	m.visited[visitKey] = true
	return nil
}
