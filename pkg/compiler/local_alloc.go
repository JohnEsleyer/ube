// File: pkg/compiler/local_alloc.go
package compiler

import "github.com/JohnEsleyer/ube/pkg/wasm"

// LocalPool manages virtual registers and scratch local variable reuse to coalesce temporary locals.
type LocalPool struct {
	params       []wasm.ValType
	declaredVars map[string]uint32
	varTypes     map[string]wasm.ValType
	freeI32      []uint32
	freeI64      []uint32
	freeF32      []uint32
	freeF64      []uint32
	allLocals    []wasm.ValType
}

// NewLocalPool creates a new LocalPool initialized with function parameter types.
func NewLocalPool(params []wasm.ValType) *LocalPool {
	return &LocalPool{
		params:       params,
		declaredVars: make(map[string]uint32),
		varTypes:     make(map[string]wasm.ValType),
	}
}

// Params returns the registered parameter types.
func (p *LocalPool) Params() []wasm.ValType {
	return p.params
}

// Locals returns the allocated local variables.
func (p *LocalPool) Locals() []wasm.ValType {
	return p.allLocals
}

// AllocVar allocates a permanent local for a declared binding.
func (p *LocalPool) AllocVar(name string, t wasm.ValType) uint32 {
	idx := uint32(len(p.params) + len(p.allLocals))
	p.allLocals = append(p.allLocals, t)
	p.declaredVars[name] = idx
	p.varTypes[name] = t
	return idx
}

// LookupVar returns the local index and type for a declared binding, if found.
func (p *LocalPool) LookupVar(name string) (uint32, wasm.ValType, bool) {
	idx, ok := p.declaredVars[name]
	if !ok {
		return 0, 0, false
	}
	return idx, p.varTypes[name], true
}

// LeaseTemp leases a temporary local to reduce total function local count.
func (p *LocalPool) LeaseTemp(t wasm.ValType) uint32 {
	switch t {
	case wasm.ValI32:
		if n := len(p.freeI32); n > 0 {
			idx := p.freeI32[n-1]
			p.freeI32 = p.freeI32[:n-1]
			return idx
		}
	case wasm.ValI64:
		if n := len(p.freeI64); n > 0 {
			idx := p.freeI64[n-1]
			p.freeI64 = p.freeI64[:n-1]
			return idx
		}
	case wasm.ValF32:
		if n := len(p.freeF32); n > 0 {
			idx := p.freeF32[n-1]
			p.freeF32 = p.freeF32[:n-1]
			return idx
		}
	case wasm.ValF64:
		if n := len(p.freeF64); n > 0 {
			idx := p.freeF64[n-1]
			p.freeF64 = p.freeF64[:n-1]
			return idx
		}
	}
	idx := uint32(len(p.params) + len(p.allLocals))
	p.allLocals = append(p.allLocals, t)
	return idx
}

// ReleaseTemp marks a temporary local as free for reuse.
func (p *LocalPool) ReleaseTemp(idx uint32, t wasm.ValType) {
	switch t {
	case wasm.ValI32:
		p.freeI32 = append(p.freeI32, idx)
	case wasm.ValI64:
		p.freeI64 = append(p.freeI64, idx)
	case wasm.ValF32:
		p.freeF32 = append(p.freeF32, idx)
	case wasm.ValF64:
		p.freeF64 = append(p.freeF64, idx)
	}
}
