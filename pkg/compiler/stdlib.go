package compiler

import (
	"embed"
	"io/fs"
	"path"
	"strings"
)

// stdlibFS bundles the Ube standard library into the compiler binary. This is
// what makes `import "std/..."` work out of the box, even when the CLI is
// distributed without a source checkout.
//
//go:embed stdlib/std/*.ube
var stdlibFS embed.FS

// StdlibSources maps a standard library module name ("math", "strings", ...)
// to its Ube source. It is populated from the embedded filesystem at startup.
var StdlibSources = map[string]string{}

func init() {
	entries, err := fs.Glob(stdlibFS, "stdlib/std/*.ube")
	if err != nil {
		return
	}
	for _, entry := range entries {
		name := strings.TrimSuffix(path.Base(entry), ".ube")
		data, err := stdlibFS.ReadFile(entry)
		if err != nil {
			continue
		}
		StdlibSources[name] = string(data)
	}
}

// stdlibSource returns the embedded source for a standard library module.
func stdlibSource(name string) (string, bool) {
	src, ok := StdlibSources[name]
	return src, ok
}
