package wasm

// ImportEntry defines an external import (function, memory, table, or global).
type ImportEntry struct {
	Module string
	Field  string
	Kind   ExternalKind
	// Index holds the TypeIndex for KindFunc, or 0 for default instances.
	TypeIndex uint32
}

// ExportEntry defines an exported symbol.
type ExportEntry struct {
	Name  string
	Kind  ExternalKind
	Index uint32
}

// LocalEntry defines a group of locals with identical type in a function body.
type LocalEntry struct {
	Count uint32
	Type  ValType
}

// FunctionBody represents the locals and raw instructions of a compiled function.
type FunctionBody struct {
	Locals []LocalEntry
	Code   []byte
}

// DataSegment defines an active memory segment loaded at start.
type DataSegment struct {
	OffsetExpr []byte
	Data       []byte
}

// ElementSegment defines active table elements (function indices for indirect calls).
type ElementSegment struct {
	OffsetExpr []byte
	FuncIndex  []uint32
}

// GlobalEntry defines an initialized global variable.
type GlobalEntry struct {
	Type     GlobalType
	InitExpr []byte
}

// Module represents a complete WebAssembly binary module in construction.
type Module struct {
	Types     []FuncType
	Imports   []ImportEntry
	Functions []uint32 // Type indices for internal functions
	Tables    []TableType
	Memories  []MemoryType
	Globals   []GlobalEntry
	Exports   []ExportEntry
	Elements  []ElementSegment
	Bodies    []FunctionBody
	Data      []DataSegment
}

// NewModule creates an empty, valid WebAssembly module representation.
func NewModule() *Module {
	return &Module{
		Types:     make([]FuncType, 0, 16),
		Imports:   make([]ImportEntry, 0, 8),
		Functions: make([]uint32, 0, 32),
		Exports:   make([]ExportEntry, 0, 16),
		Bodies:    make([]FunctionBody, 0, 32),
	}
}

// AddType registers a FuncType, deduplicating identical signatures.
func (m *Module) AddType(ft FuncType) uint32 {
	for i, existing := range m.Types {
		if funcTypeEqual(existing, ft) {
			return uint32(i)
		}
	}
	idx := uint32(len(m.Types))
	m.Types = append(m.Types, ft)
	return idx
}

func funcTypeEqual(a, b FuncType) bool {
	if len(a.Params) != len(b.Params) || len(a.Results) != len(b.Results) {
		return false
	}
	for i := range a.Params {
		if a.Params[i] != b.Params[i] {
			return false
		}
	}
	for i := range a.Results {
		if a.Results[i] != b.Results[i] {
			return false
		}
	}
	return true
}

// Encode serializes the entire module into valid WebAssembly binary format.
func (m *Module) Encode() ([]byte, error) {
	out := NewBuffer(4096)

	// Magic & Version header
	out.EmitBytes(Magic[:])
	out.EmitBytes(Version[:])

	// 1. Type Section
	if len(m.Types) > 0 {
		sec := NewBuffer(256)
		sec.EmitUleb128(uint32(len(m.Types)))
		for _, ft := range m.Types {
			sec.Emit(0x60) // func type indicator
			sec.EmitUleb128(uint32(len(ft.Params)))
			for _, p := range ft.Params {
				sec.Emit(byte(p))
			}
			sec.EmitUleb128(uint32(len(ft.Results)))
			for _, r := range ft.Results {
				sec.Emit(byte(r))
			}
		}
		writeSection(out, SectionType, sec.Bytes)
	}

	// 2. Import Section
	if len(m.Imports) > 0 {
		sec := NewBuffer(256)
		sec.EmitUleb128(uint32(len(m.Imports)))
		for _, imp := range m.Imports {
			sec.EmitString(imp.Module)
			sec.EmitString(imp.Field)
			sec.Emit(byte(imp.Kind))
			switch imp.Kind {
			case KindFunc:
				sec.EmitUleb128(imp.TypeIndex)
			}
		}
		writeSection(out, SectionImport, sec.Bytes)
	}

	// 3. Function Section
	if len(m.Functions) > 0 {
		sec := NewBuffer(128)
		sec.EmitUleb128(uint32(len(m.Functions)))
		for _, typeIdx := range m.Functions {
			sec.EmitUleb128(typeIdx)
		}
		writeSection(out, SectionFunction, sec.Bytes)
	}

	// 4. Table Section
	if len(m.Tables) > 0 {
		sec := NewBuffer(64)
		sec.EmitUleb128(uint32(len(m.Tables)))
		for _, tbl := range m.Tables {
			sec.Emit(byte(tbl.ElemType))
			if tbl.Limits.HasMax {
				sec.Emit(0x01)
				sec.EmitUleb128(tbl.Limits.Min)
				sec.EmitUleb128(tbl.Limits.Max)
			} else {
				sec.Emit(0x00)
				sec.EmitUleb128(tbl.Limits.Min)
			}
		}
		writeSection(out, SectionTable, sec.Bytes)
	}

	// 5. Memory Section
	if len(m.Memories) > 0 {
		sec := NewBuffer(64)
		sec.EmitUleb128(uint32(len(m.Memories)))
		for _, mem := range m.Memories {
			if mem.Limits.HasMax {
				sec.Emit(0x01)
				sec.EmitUleb128(mem.Limits.Min)
				sec.EmitUleb128(mem.Limits.Max)
			} else {
				sec.Emit(0x00)
				sec.EmitUleb128(mem.Limits.Min)
			}
		}
		writeSection(out, SectionMemory, sec.Bytes)
	}

	// 6. Global Section
	if len(m.Globals) > 0 {
		sec := NewBuffer(128)
		sec.EmitUleb128(uint32(len(m.Globals)))
		for _, g := range m.Globals {
			sec.Emit(byte(g.Type.Type))
			sec.Emit(byte(g.Type.Mut))
			sec.EmitBytes(g.InitExpr)
			sec.Emit(OpEnd)
		}
		writeSection(out, SectionGlobal, sec.Bytes)
	}

	// 7. Export Section
	if len(m.Exports) > 0 {
		sec := NewBuffer(256)
		sec.EmitUleb128(uint32(len(m.Exports)))
		for _, exp := range m.Exports {
			sec.EmitString(exp.Name)
			sec.Emit(byte(exp.Kind))
			sec.EmitUleb128(exp.Index)
		}
		writeSection(out, SectionExport, sec.Bytes)
	}

	// 9. Element Section
	if len(m.Elements) > 0 {
		sec := NewBuffer(128)
		sec.EmitUleb128(uint32(len(m.Elements)))
		for _, elem := range m.Elements {
			sec.Emit(0x00) // active table 0
			sec.EmitBytes(elem.OffsetExpr)
			sec.Emit(OpEnd)
			sec.EmitUleb128(uint32(len(elem.FuncIndex)))
			for _, fIdx := range elem.FuncIndex {
				sec.EmitUleb128(fIdx)
			}
		}
		writeSection(out, SectionElement, sec.Bytes)
	}

	// 10. Code Section
	if len(m.Bodies) > 0 {
		sec := NewBuffer(2048)
		sec.EmitUleb128(uint32(len(m.Bodies)))
		for _, body := range m.Bodies {
			funcBuf := NewBuffer(len(body.Code) + 32)
			funcBuf.EmitUleb128(uint32(len(body.Locals)))
			for _, loc := range body.Locals {
				funcBuf.EmitUleb128(loc.Count)
				funcBuf.Emit(byte(loc.Type))
			}
			funcBuf.EmitBytes(body.Code)

			sec.EmitUleb128(uint32(funcBuf.Len()))
			sec.EmitBytes(funcBuf.Bytes)
		}
		writeSection(out, SectionCode, sec.Bytes)
	}

	// 11. Data Section
	if len(m.Data) > 0 {
		sec := NewBuffer(512)
		sec.EmitUleb128(uint32(len(m.Data)))
		for _, d := range m.Data {
			sec.Emit(0x00) // active linear memory 0
			sec.EmitBytes(d.OffsetExpr)
			sec.Emit(OpEnd)
			sec.EmitUleb128(uint32(len(d.Data)))
			sec.EmitBytes(d.Data)
		}
		writeSection(out, SectionData, sec.Bytes)
	}

	return out.Bytes, nil
}

func writeSection(buf *Buffer, id SectionID, content []byte) {
	buf.Emit(byte(id))
	buf.EmitUleb128(uint32(len(content)))
	buf.EmitBytes(content)
}
