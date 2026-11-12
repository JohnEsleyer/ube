// File: pkg/targets/wasi1/layout.go
//
// Preview 1 linear-memory scratch layout. These low addresses are reserved
// for transient syscall out-parameters; static data starts at
// compiler.StaticDataOff.

package wasi1

// Scratch memory used by the WASI Preview 1 syscall layer and the print
// runtime helpers.
const (
	scratchCiovec = 0  // Offset 0..7: ciovec { ptr: i32, len: i32 }
	scratchNwrite = 8  // Offset 8..11: nwritten i32 (also the clock out-pointer)
	scratchItoa   = 16 // Offset 16..47: itoa string buffer
	// scratchOpenedFd receives the opened descriptor from path_open.
	scratchOpenedFd = 48
	// WASI fd_seek writes a u64 filesize to this out-pointer. Wasmtime
	// requires that pointer to be 8-byte aligned. 52 is not (52%8=4),
	// which produced "Pointer not aligned to 8: Region { start: 52,
	// len: 8 }" on every seek. Use the next free 8-aligned slot after
	// scratchOpenedFd; scratchCiovec (0..8), scratchNwrite (8..12),
	// scratchItoa (16..48) and scratchOpenedFd (48..52) are unchanged.
	scratchSeekPos = 56
)
