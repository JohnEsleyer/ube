package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

type patchOpKind int

const (
	opReplace patchOpKind = iota
	opCreateFile
	opDeleteFile
)

type patchOp struct {
	kind   patchOpKind
	path   string
	within string
	search string
	with   string
}

// runPatch parses and applies an atomic patch bundle using an in-memory VFS.
func runPatch(args []string) error {
	var (
		clipIn bool
		clipOut bool
		dryRun bool
		fuzzy  bool
		target string
	)

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-ci", "--clip-in":
			clipIn = true
		case "-c", "--clip":
			clipOut = true
		case "-n", "--dry-run":
			dryRun = true
		case "-f", "--fix":
			fuzzy = true
		default:
			if !strings.HasPrefix(arg, "-") && target == "" {
				target = arg
			}
		}
	}

	var patchText string
	if clipIn {
		c, err := readClipboard()
		if err != nil {
			return fmt.Errorf("reading clipboard: %w", err)
		}
		patchText = c
	} else if target != "" {
		data, err := os.ReadFile(target)
		if err != nil {
			return fmt.Errorf("reading patch file %s: %w", target, err)
		}
		patchText = string(data)
	} else {
		// Read from stdin if available
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			data, err := io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
			patchText = string(data)
		} else {
			return errors.New("no patch input provided. Use -ci (clipboard), a file path, or pipe via stdin")
		}
	}

	if strings.TrimSpace(patchText) == "" {
		return errors.New("patch input is empty")
	}

	ops, err := parsePatchDirectives(patchText)
	if err != nil {
		return err
	}
	if len(ops) == 0 {
		return errors.New("no valid patch directives (<replace>, <file>, or <delete>) found in input")
	}

	// Two-pass execution: Pass 1 validates in-memory VFS.
	vfs := make(map[string]string)
	filesToDelete := make(map[string]bool)

	for _, op := range ops {
		switch op.kind {
		case opCreateFile:
			vfs[op.path] = op.with
		case opDeleteFile:
			filesToDelete[op.path] = true
		case opReplace:
			content, loaded := vfs[op.path]
			if !loaded {
				raw, err := os.ReadFile(op.path)
				if err != nil {
					return fmt.Errorf("cannot read target file %s: %w", op.path, err)
				}
				content = string(raw)
			}

			updated, err := applyReplaceOp(content, op, fuzzy)
			if err != nil {
				return fmt.Errorf("patch failed on %s: %w", op.path, err)
			}
			vfs[op.path] = updated
		}
	}

	if dryRun {
		fmt.Printf("✓ [DRY-RUN] Validated %d patch operation(s) across %d file(s) cleanly.\n", len(ops), len(vfs)+len(filesToDelete))
		return nil
	}

	// Pass 2: Write all validated changes to disk.
	for path, content := range vfs {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing updated file %s: %w", path, err)
		}
		fmt.Printf("✓ Patched %s\n", path)
	}

	for path := range filesToDelete {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("deleting file %s: %w", path, err)
		}
		fmt.Printf("✓ Deleted %s\n", path)
	}

	if clipOut {
		_ = writeClipboard(fmt.Sprintf("✓ Successfully applied %d operation(s)", len(ops)))
	}

	return nil
}

var (
	replaceRe = regexp.MustCompile(`(?s)<replace\s+path="([^"]+)"(?:\s+within="([^"]+)")?[^>]*>(.*?)</replace>`)
	searchRe  = regexp.MustCompile(`(?s)<search>(.*?)</search>`)
	withRe    = regexp.MustCompile(`(?s)<with>(.*?)</with>`)
	fileRe    = regexp.MustCompile(`(?s)<file\s+path="([^"]+)"[^>]*>(.*?)</file>`)
	deleteRe  = regexp.MustCompile(`<delete\s+path="([^"]+)"\s*/>`)
)

func parsePatchDirectives(text string) ([]patchOp, error) {
	var ops []patchOp

	// 1. Files to create/overwrite
	fileMatches := fileRe.FindAllStringSubmatch(text, -1)
	for _, m := range fileMatches {
		ops = append(ops, patchOp{
			kind: opCreateFile,
			path: strings.TrimSpace(m[1]),
			with: strings.TrimPrefix(m[2], "\n"),
		})
	}

	// 2. Deletions
	delMatches := deleteRe.FindAllStringSubmatch(text, -1)
	for _, m := range delMatches {
		ops = append(ops, patchOp{
			kind: opDeleteFile,
			path: strings.TrimSpace(m[1]),
		})
	}

	// 3. Replacements
	repMatches := replaceRe.FindAllStringSubmatch(text, -1)
	for _, m := range repMatches {
		path := strings.TrimSpace(m[1])
		within := strings.TrimSpace(m[2])
		inner := m[3]

		sm := searchRe.FindStringSubmatch(inner)
		wm := withRe.FindStringSubmatch(inner)
		if len(sm) < 2 || len(wm) < 2 {
			return nil, fmt.Errorf("replace block for %s is missing <search> or <with> tags", path)
		}

		ops = append(ops, patchOp{
			kind:   opReplace,
			path:   path,
			within: within,
			search: cleanBoundaryNewlines(sm[1]),
			with:   cleanBoundaryNewlines(wm[1]),
		})
	}

	return ops, nil
}

func cleanBoundaryNewlines(s string) string {
	s = strings.TrimPrefix(s, "\n")
	s = strings.TrimPrefix(s, "\r\n")
	return s
}

func applyReplaceOp(content string, op patchOp, fuzzy bool) (string, error) {
	normContent := strings.ReplaceAll(content, "\r\n", "\n")
	normSearch := strings.ReplaceAll(op.search, "\r\n", "\n")
	normWith := strings.ReplaceAll(op.with, "\r\n", "\n")

	// If bounded by 'within="functionName"', locate function bounds
	if op.within != "" {
		fnIdx := strings.Index(normContent, "fn "+op.within)
		if fnIdx == -1 {
			fnIdx = strings.Index(normContent, "fn (")
		}
		if fnIdx != -1 {
			// Find closing brace of this function
			startBrace := strings.Index(normContent[fnIdx:], "{")
			if startBrace != -1 {
				absStart := fnIdx + startBrace
				depth := 0
				absEnd := -1
				for i := absStart; i < len(normContent); i++ {
					if normContent[i] == '{' {
						depth++
					} else if normContent[i] == '}' {
						depth--
						if depth == 0 {
							absEnd = i
							break
						}
					}
				}
				if absEnd != -1 {
					fnBody := normContent[absStart : absEnd+1]
					idx := strings.Index(fnBody, normSearch)
					if idx != -1 {
						newBody := strings.Replace(fnBody, normSearch, normWith, 1)
						return normContent[:absStart] + newBody + normContent[absEnd+1:], nil
					}
				}
			}
		}
	}

	// Unbounded search
	count := strings.Count(normContent, normSearch)
	if count == 1 {
		return strings.Replace(normContent, normSearch, normWith, 1), nil
	}
	if count > 1 {
		return "", fmt.Errorf("search target occurs %d times in %s (must be unique; specify within=\"...\")", count, op.path)
	}

	// If fuzzy mode is enabled, attempt whitespace-tolerant line matching
	if fuzzy {
		updated, matched := applyFuzzyReplace(normContent, normSearch, normWith)
		if matched {
			return updated, nil
		}
	}

	return "", fmt.Errorf("search block not found in %s:\n--- SEARCH BLOCK ---\n%s\n--------------------", op.path, normSearch)
}

func applyFuzzyReplace(content, search, with string) (string, bool) {
	cLines := strings.Split(content, "\n")
	sLines := strings.Split(search, "\n")
	if len(sLines) == 0 || len(sLines) > len(cLines) {
		return "", false
	}

	matchIdx := -1
	for i := 0; i <= len(cLines)-len(sLines); i++ {
		match := true
		for j := 0; j < len(sLines); j++ {
			if strings.TrimSpace(cLines[i+j]) != strings.TrimSpace(sLines[j]) {
				match = false
				break
			}
		}
		if match {
			if matchIdx != -1 {
				// Multiple matches
				return "", false
			}
			matchIdx = i
		}
	}

	if matchIdx == -1 {
		return "", false
	}

	var res []string
	res = append(res, cLines[:matchIdx]...)
	res = append(res, with)
	res = append(res, cLines[matchIdx+len(sLines):]...)
	return strings.Join(res, "\n"), true
}
