package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/formatter"
)

// runFmt implements `ube fmt [path ...]`. It formats Ube source files using the
// canonical formatter. With --check it reports files that would change and
// exits non-zero without touching them. With -w/--write it rewrites files in
// place; when given a directory it writes in place by default.
func runFmt(args []string) error {
	check, write := false, false
	var targets []string
	for _, arg := range args {
		switch arg {
		case "--check":
			check = true
		case "-w", "--write":
			write = true
		default:
			if strings.HasPrefix(arg, "-") {
				return fmt.Errorf("unknown fmt option %s", arg)
			}
			targets = append(targets, arg)
		}
	}
	if check && write {
		return fmt.Errorf("choose either --check or -w")
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}

	var files []string
	hasDir := false
	for _, target := range targets {
		info, err := os.Stat(target)
		if err != nil {
			return err
		}
		if info.IsDir() {
			hasDir = true
			discovered, err := discoverUbeFiles(target)
			if err != nil {
				return err
			}
			files = append(files, discovered...)
			continue
		}
		files = append(files, target)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return fmt.Errorf("no .ube files found")
	}

	changed := 0
	for _, file := range files {
		source, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		formatted, err := formatter.Format(file, string(source))
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		if formatted == string(source) {
			continue
		}
		changed++
		switch {
		case check:
			fmt.Println(file)
		case write || hasDir:
			mode := os.FileMode(0644)
			if info, statErr := os.Stat(file); statErr == nil {
				mode = info.Mode().Perm()
			}
			if err := os.WriteFile(file, []byte(formatted), mode); err != nil {
				return err
			}
			fmt.Printf("formatted %s\n", file)
		default:
			fmt.Print(formatted)
		}
	}

	if check && changed > 0 {
		return fmt.Errorf("%d file(s) need formatting", changed)
	}
	return nil
}

// discoverUbeFiles walks a directory and returns every .ube source file,
// skipping common build and dependency directories.
func discoverUbeFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", "bin", "target":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if strings.HasSuffix(path, ".ube") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}
