package compiler

import (
	"fmt"
	"os"
	"path/filepath"
)

type ModuleLoader struct {
	visited, visiting map[string]bool
	allDecls          []Decl
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
	if err = m.loadRecursive(abs); err != nil {
		return nil, err
	}
	return &Program{At: SourceLocation{File: abs, Line: 1, Col: 1}, Decls: m.allDecls}, nil
}
func (m *ModuleLoader) loadRecursive(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	path = filepath.Clean(path)
	if m.visited[path] {
		return nil
	}
	if m.visiting[path] {
		return fmt.Errorf("cyclic import detected: %s", path)
	}
	m.visiting[path] = true
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}
	toks, err := NewLexer(path, string(b)).Tokenize()
	if err != nil {
		return err
	}
	prog, err := NewParser(toks).ParseProgram()
	if err != nil {
		return err
	}
	for _, d := range prog.Decls {
		if imp, ok := d.(*ImportDecl); ok {
			if err := m.loadRecursive(filepath.Join(filepath.Dir(path), imp.Path)); err != nil {
				return err
			}
		} else {
			m.allDecls = append(m.allDecls, d)
		}
	}
	delete(m.visiting, path)
	m.visited[path] = true
	return nil
}
