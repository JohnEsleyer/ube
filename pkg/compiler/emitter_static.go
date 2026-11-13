// File: pkg/compiler/emitter_static.go
package compiler

// AllocStatic reserves size zero-initialized bytes in static linear memory
// (8-byte aligned) and returns the base linear-memory offset. Must be called
// during HostLayer.RegisterImports before binary assembly.
func (e *wasmEmitter) AllocStatic(size uint32) uint32 {
	for e.dataOffset%8 != 0 {
		e.data = append(e.data, 0)
		e.dataOffset++
	}
	off := e.dataOffset
	if size > 0 {
		e.data = append(e.data, make([]byte, size)...)
		e.dataOffset += size
	}
	return off
}
