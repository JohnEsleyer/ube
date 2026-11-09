// File: pkg/compiler/ir.go
package compiler

import "github.com/JohnEsleyer/ube/pkg/wasm"

// IROpcode defines low-level linear intermediate representation operations.
type IROpcode byte

const (
	// Stack & Memory Ops
	OpConstI32 IROpcode = iota
	OpConstI64
	OpConstF32
	OpConstF64
	OpGetLocal
	OpSetLocal
	OpTeeLocal
	OpGetGlobal
	OpSetGlobal
	OpLoad
	OpStore
	OpMemoryCopy
	OpMemoryFill

	// Arithmetic & Bitwise
	OpAdd
	OpSub
	OpMul
	OpDivS
	OpDivU
	OpRemS
	OpRemU
	OpAnd
	OpOr
	OpXor
	OpShl
	OpShrS
	OpShrU

	// Comparisons
	OpEq
	OpNe
	OpLtS
	OpLtU
	OpLeS
	OpLeU
	OpGtS
	OpGtU
	OpGeS
	OpGeU

	// Control Structures
	OpBlock
	OpLoop
	OpIf
	OpElse
	OpEnd
	OpBr
	OpBrIf
	OpReturn
	OpCall
	OpCallIndirect
	OpUnreachable
	OpDrop
)

// IRInstruction represents an individual instruction within an IRFunction.
type IRInstruction struct {
	Op        IROpcode
	Type      wasm.ValType
	TypeIdx   uint32
	ImmI32    int32
	ImmI64    int64
	ImmF32    float32
	ImmF64    float64
	LocalIdx  uint32
	GlobalIdx uint32
	FuncIdx   uint32
	Offset    uint32
	Align     uint32
}

// IRFunction models a lowered function ready for local allocation and binary emission.
type IRFunction struct {
	Name    string
	TypeIdx uint32
	Params  []wasm.ValType
	Results []wasm.ValType
	Locals  []wasm.ValType
	Body    []IRInstruction
}
