package wasm

// Binary format constants.
var (
	Magic            = [4]byte{0x00, 0x61, 0x73, 0x6D} // \0asm
	Version          = [4]byte{0x01, 0x00, 0x00, 0x00} // 1
	ComponentMagic   = [4]byte{0x00, 0x61, 0x73, 0x6D} // \0asm
	ComponentVersion = [4]byte{0x0D, 0x00, 0x01, 0x00} // WASI 0.2 Component Model
)

// SectionID represents standard Wasm binary section identifiers.
type SectionID byte

const (
	SectionCustom    SectionID = 0
	SectionType      SectionID = 1
	SectionImport    SectionID = 2
	SectionFunction  SectionID = 3
	SectionTable     SectionID = 4
	SectionMemory    SectionID = 5
	SectionGlobal    SectionID = 6
	SectionExport    SectionID = 7
	SectionStart     SectionID = 8
	SectionElement   SectionID = 9
	SectionCode      SectionID = 10
	SectionData      SectionID = 11
	SectionDataCount SectionID = 12

	// WASI 0.2 Component Model Section IDs
	ComponentSectionCustom       SectionID = 0
	ComponentSectionCoreModule   SectionID = 1
	ComponentSectionCoreInstance SectionID = 2
	ComponentSectionCoreType     SectionID = 3
	ComponentSectionComponent    SectionID = 4
	ComponentSectionInstance     SectionID = 5
	ComponentSectionAlias        SectionID = 6
	ComponentSectionType         SectionID = 7
	ComponentSectionCanon        SectionID = 8
	ComponentSectionStart        SectionID = 9
	ComponentSectionImport       SectionID = 10
	ComponentSectionExport       SectionID = 11
)

// ValType represents a WebAssembly primitive value type.
type ValType byte

const (
	ValI32       ValType = 0x7F
	ValI64       ValType = 0x7E
	ValF32       ValType = 0x7D
	ValF64       ValType = 0x7C
	ValV128      ValType = 0x7B
	ValFuncRef   ValType = 0x70
	ValExternRef ValType = 0x6F
	ValVoid      ValType = 0x40 // Block type for no return values
)

// Export and Import external kinds.
type ExternalKind byte

const (
	KindFunc   ExternalKind = 0x00
	KindTable  ExternalKind = 0x01
	KindMemory ExternalKind = 0x02
	KindGlobal ExternalKind = 0x03
)

// Mutability represents global or variable mutability.
type Mutability byte

const (
	MutConst Mutability = 0x00
	MutVar   Mutability = 0x01
)

// Limits represents min and optional max boundaries for memory and tables.
type Limits struct {
	HasMax bool
	Min    uint32
	Max    uint32
}

// FuncType represents a function signature (params -> results).
type FuncType struct {
	Params  []ValType
	Results []ValType
}

// GlobalType specifies the type and mutability of a global variable.
type GlobalType struct {
	Type ValType
	Mut  Mutability
}

// MemoryType specifies the limits of a linear memory instance.
type MemoryType struct {
	Limits Limits
}

// TableType specifies reference type and size limits for a table.
type TableType struct {
	ElemType ValType
	Limits   Limits
}
