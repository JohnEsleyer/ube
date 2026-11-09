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

	// Select & Testing
	OpSelect
	OpI32Eqz
	OpI64Eqz

	// Numeric Conversions & Casts
	OpI32WrapI64
	OpI64ExtendI32S
	OpI64ExtendI32U
	OpF32DemoteF64
	OpF64PromoteF32
	OpF32ConvertI32S
	OpF32ConvertI32U
	OpF32ConvertI64S
	OpF32ConvertI64U
	OpF64ConvertI32S
	OpF64ConvertI32U
	OpF64ConvertI64S
	OpF64ConvertI64U
	OpI32TruncF32S
	OpI32TruncF32U
	OpI32TruncF64S
	OpI32TruncF64U
	OpI64TruncF32S
	OpI64TruncF32U
	OpI64TruncF64S
	OpI64TruncF64U
	OpF32Abs
	OpF64Abs
	OpF32Neg
	OpF64Neg
	OpI32ReinterpretF32
	OpF32ReinterpretI32
	OpI64ReinterpretF64
	OpF64ReinterpretI64
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
