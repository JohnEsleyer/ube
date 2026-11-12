// File: pkg/targets/wasi2/error.go


package wasi2

import "errors"

var (
	errShortCoreModule = errors.New("wasi2: core module too short to be a WebAssembly binary")
	errBadCoreMagic    = errors.New("wasi2: core module has invalid magic bytes")
	errBadCoreVersion  = errors.New("wasi2: core module has invalid version (expected core wasm 0x01 0x00 0x00 0x00)")
)
