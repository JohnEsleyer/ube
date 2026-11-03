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
}

func NewModuleLoader() *ModuleLoader {
	return &ModuleLoader{visited: map[string]bool{}, visiting: map[string]bool{}}
}

func (m *ModuleLoader) Load(entry string) (*Program, error) {
	m.visited = map[string]bool{}
	m.visiting = map[string]bool{}
	m.allDecls = nil
	abs, err := filepath.Abs(entry)
	if err != nil {
		return nil, err
	}
	m.entryDir = filepath.Dir(abs)
	if err = m.loadRecursive(abs); err != nil {
		return nil, err
	}
	return &Program{At: SourceLocation{File: abs, Line: 1, Col: 1}, Decls: m.allDecls}, nil
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
//  1. $UBE_PATH (each path may contain std/<module>.ube)
//  2. ./std/<module>.ube (a local source checkout / project override)
//  3. std/<module>.ube next to the entry module
//  4. the standard library embedded in the compiler binary
func (m *ModuleLoader) resolveStdSource(key string) (string, error) {
	if !validStdKey(key) {
		return "", fmt.Errorf("invalid standard library module 'std/%s'", key)
	}

	var candidates []string
	for _, root := range filepath.SplitList(os.Getenv("UBE_PATH")) {
		if root == "" {
			continue
		}
		candidates = append(candidates,
			filepath.Join(root, "std", key+".ube"),
			filepath.Join(root, key+".ube"),
		)
	}
	candidates = append(candidates, filepath.Join("std", key+".ube"))
	if m.entryDir != "" {
		candidates = append(candidates, filepath.Join(m.entryDir, "std", key+".ube"))
	}

	for _, candidate := range candidates {
		if src, err := os.ReadFile(candidate); err == nil {
			return string(src), nil
		}
	}
	if src, ok := stdlibSource(key); ok {
		return src, nil
	}
	return "", fmt.Errorf("unknown standard library module 'std/%s'", key)
}

func (m *ModuleLoader) loadRecursive(path string) error {
	var (
		abs string
		src string
	)

	stdKey, fromStd := isStdImport(path)
	if fromStd {
		abs = "std:" + stdKey
		if m.visited[abs] {
			return nil
		}
		if m.visiting[abs] {
			return fmt.Errorf("cyclic import detected: %s", path)
		}
		m.visiting[abs] = true

		moduleSrc, err := m.resolveStdSource(stdKey)
		if err != nil {
			return err
		}
		src = moduleSrc
	} else {
		var err error
		abs, err = filepath.Abs(path)
		if err != nil {
			return err
		}
		abs = filepath.Clean(abs)
		if m.visited[abs] {
			return nil
		}
		if m.visiting[abs] {
			return fmt.Errorf("cyclic import detected: %s", abs)
		}
		m.visiting[abs] = true

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
			if err := m.loadRecursive(importPath); err != nil {
				return err
			}
		} else {
			m.allDecls = append(m.allDecls, d)
		}
	}
	delete(m.visiting, abs)
	m.visited[abs] = true
	return nil
}
