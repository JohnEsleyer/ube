package compiler

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JohnEsleyer/ube/std"
)

// sourceFile is one parsed source buffer belonging to a package.
type sourceFile struct {
	name   string
	source string
}

// stdlibPackageSources returns all .ube files for a standard library package.
// An on-disk directory wins so local development can override the embedded
// standard library; otherwise the embedded FS is used.
func stdlibPackageSources(key string, entryDir string) ([]sourceFile, error) {
	cleanKey := strings.TrimSuffix(key, ".ube")

	var roots []string
	roots = append(roots, ".")
	if entryDir != "" {
		roots = append(roots, entryDir)
	}
	for _, root := range filepath.SplitList(os.Getenv("UBE_PATH")) {
		if root != "" {
			roots = append(roots, root)
		}
	}

	// Directory layout: <root>/std/<key>/*.ube
	for _, root := range roots {
		dir := filepath.Join(root, "std", filepath.FromSlash(cleanKey))
		if files, ok := readUbeDir(dir); ok {
			return files, nil
		}
	}

	// Flat layout: <root>/std/<key>.ube
	for _, root := range roots {
		file := filepath.Join(root, "std", filepath.FromSlash(cleanKey)+".ube")
		if data, err := os.ReadFile(file); err == nil {
			return []sourceFile{{name: file, source: string(data)}}, nil
		}
	}

	// Embedded directory layout: std/<key>/*.ube
	if entries, err := std.Content.ReadDir(cleanKey); err == nil {
		var names []string
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ube") {
				continue
			}
			names = append(names, entry.Name())
		}
		sort.Strings(names)
		if len(names) > 0 {
			files := make([]sourceFile, 0, len(names))
			for _, name := range names {
				data, err := std.Content.ReadFile(cleanKey + "/" + name)
				if err != nil {
					return nil, fmt.Errorf("reading embedded std/%s/%s: %w", cleanKey, name, err)
				}
				files = append(files, sourceFile{
					name:   "std:" + cleanKey + "/" + name,
					source: string(data),
				})
			}
			return files, nil
		}
	}

	// Embedded flat layout: std/<key>.ube
	if data, err := std.Content.ReadFile(cleanKey + ".ube"); err == nil {
		return []sourceFile{{name: "std:" + cleanKey + ".ube", source: string(data)}}, nil
	}

	return nil, fmt.Errorf("unknown standard library package 'std/%s'", key)
}

func readUbeDir(dir string) ([]sourceFile, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, false
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".ube") {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil, false
	}
	sort.Strings(names)
	files := make([]sourceFile, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, false
		}
		files = append(files, sourceFile{name: path, source: string(data)})
	}
	return files, true
}
