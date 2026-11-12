// Package std embeds the Ube standard library sources into the compiler
// binary. Target-independent libraries live in std/<name>/<name>.ube, while
// target-specific trees live in std/wasi1/ (WASI Preview 1) and std/wasi2/
// (WASI 0.2 component model) so standard libraries stay decoupled from the
// core compiler engine and from each other.
package std

import "embed"

//go:embed */*.ube */*/*.ube
var Content embed.FS
