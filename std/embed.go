// Package std embeds the Ube standard library sources into the compiler
// binary. Each library lives in its own subdirectory (std/<name>/<name>.ube)
// so standard libraries stay decoupled from the core compiler engine.
package std

import "embed"

//go:embed */*.ube
var Content embed.FS
