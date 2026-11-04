package compiler

import (
	"fmt"
	"strings"

	"github.com/JohnEsleyer/ube/std"
)

// stdlibSource loads standard library source files from the embedded FS.
// Standard libraries live at std/<name>/<name>.ube.
func stdlibSource(key string) (string, bool) {
	cleanKey := strings.TrimSuffix(key, ".ube")

	// Priority 1: subdirectory layout (std/<lib>/<lib>.ube).
	subPath := fmt.Sprintf("%s/%s.ube", cleanKey, cleanKey)
	if data, err := std.Content.ReadFile(subPath); err == nil {
		return string(data), true
	}

	// Priority 2: direct path (e.g. key is already "fs/fs.ube").
	if data, err := std.Content.ReadFile(key); err == nil {
		return string(data), true
	}
	if data, err := std.Content.ReadFile(cleanKey + ".ube"); err == nil {
		return string(data), true
	}

	return "", false
}
