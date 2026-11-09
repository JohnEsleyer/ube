package compiler

import (
	"fmt"
	"strings"
)

// computeStructLayouts calculates the field offsets and total memory sizes
// for all defined structs, aligning fields naturally and registering
// promoted fields from anonymous embeddings.
func (e *wasmEmitter) computeStructLayouts(prog *Program, structDecls map[string]*StructDecl, structOrder []string) {
	var sizeofStruct func(name string, seen map[string]bool) int
	sizeofStruct = func(name string, seen map[string]bool) int {
		meta, ok := e.structs[name]
		if !ok {
			return 4
		}
		if meta.totalSize > 0 {
			return meta.totalSize
		}
		if seen[name] {
			return 4
		}
		seen[name] = true
		sd, ok := structDecls[name]
		if !ok {
			return 4
		}
		offset := 0
		for _, f := range sd.Fields {
			sz := structFieldSize(f.Type, sizeofStruct, seen)
			al := f.Type.Align()
			if al < 1 {
				al = 1
			}
			offset = alignUpInt(offset, al)
			offset += sz
		}
		if offset == 0 {
			offset = 4
		}
		offset = alignUpInt(offset, 4)
		meta.totalSize = offset
		e.structs[name] = meta
		return offset
	}

	for _, name := range structOrder {
		sd := structDecls[name]
		meta := e.structs[name]
		offset := 0
		for _, f := range sd.Fields {
			al := f.Type.Align()
			if al < 1 {
				al = 1
			}
			offset = alignUpInt(offset, al)
			meta.fieldOffsets[f.Name] = offset
			meta.fieldTypes[f.Name] = f.Type
			offset += structFieldSize(f.Type, sizeofStruct, map[string]bool{})
		}
		if offset == 0 {
			offset = 4
		}
		offset = alignUpInt(offset, 4)
		meta.totalSize = offset
		e.structs[name] = meta
	}

	for _, decl := range prog.Decls {
		sd, ok := decl.(*StructDecl)
		if !ok {
			continue
		}
		meta, ok := e.structs[sd.Name]
		if !ok {
			continue
		}
		e.registerPromotedFields(&meta, sd, nil, map[string]bool{})
		e.structs[sd.Name] = meta
	}
}

// isInlineAggregate reports whether a struct field stores the aggregate's
// bytes inline. Named structs and fixed arrays are inline.
func (ctx *fnContext) isInlineAggregate(t UbeType) bool {
	switch tt := t.(type) {
	case CustomType:
		_, isStruct := ctx.emitter.structs[tt.Name]
		return isStruct
	case ArrayType:
		return true
	}
	return false
}

// embedReceiverOffset walks mem.EmbedPath from rootType and returns the
// composite byte offset to add to a pointer of type rootType in order to
// reach the embedded receiver, plus the innermost embedded type name.
func (ctx *fnContext) embedReceiverOffset(rootType string, path []string) (int, string, error) {
	cur := rootType
	total := 0
	for _, seg := range path {
		meta, ok := ctx.emitter.structs[cur]
		if !ok {
			return 0, "", fmt.Errorf("unknown struct %s while resolving embed path", cur)
		}
		off, ok := meta.fieldOffsets[seg]
		if !ok {
			return 0, "", fmt.Errorf("unknown embedded field %s on %s", seg, cur)
		}
		total += off
		ft, ok := meta.fieldTypes[seg]
		if !ok {
			return 0, "", fmt.Errorf("unknown embedded field type %s on %s", seg, cur)
		}
		cur = ft.String()
	}
	return total, cur, nil
}

// sizeofInline returns the byte size a value of type t occupies when stored
// inline as a struct field.
func (e *wasmEmitter) sizeofInline(t UbeType) int {
	if ct, ok := t.(CustomType); ok {
		if meta, ok := e.structs[ct.Name]; ok && meta.totalSize > 0 {
			return meta.totalSize
		}
		return 4
	}
	if at, ok := t.(ArrayType); ok {
		if at.Elem != nil {
			return at.Size * e.sizeofInline(at.Elem)
		}
		return at.Size * 4
	}
	if sz := t.ByteSize(); sz > 0 {
		return sz
	}
	return 4
}

// alignUpInt rounds offset up to the next multiple of align.
func alignUpInt(offset, align int) int {
	if align <= 1 {
		return offset
	}
	rem := offset % align
	if rem == 0 {
		return offset
	}
	return offset + (align - rem)
}

// structFieldSize returns the in-memory size of a field of type t when
// stored inline in a struct.
func structFieldSize(t UbeType, sizeofStruct func(string, map[string]bool) int, seen map[string]bool) int {
	if ct, ok := t.(CustomType); ok {
		return sizeofStruct(ct.Name, seen)
	}
	sz := t.ByteSize()
	if sz < 1 {
		sz = 4
	}
	return sz
}

// registerPromotedFields descends into the anonymous embedded fields of sd
// and records each transitively accessible leaf field's composite offset.
func (e *wasmEmitter) registerPromotedFields(meta *structMeta, sd *StructDecl, path []string, seen map[string]bool) {
	if seen[sd.Name] {
		return
	}
	seen[sd.Name] = true
	for _, f := range sd.Fields {
		if !f.Anonymous {
			continue
		}
		ct, ok := f.Type.(CustomType)
		if !ok {
			continue
		}
		base, ok := meta.fieldOffsets[f.Name]
		if !ok {
			continue
		}
		inner, ok := e.structs[ct.Name]
		if !ok {
			continue
		}
		childPath := append(append([]string{}, path...), f.Name)
		for leafName, leafOff := range inner.fieldOffsets {
			dotted := strings.Join(childPath, ".") + "." + leafName
			if _, exists := meta.fieldOffsets[dotted]; !exists {
				meta.fieldOffsets[dotted] = base + leafOff
			}
			if lt, ok := inner.fieldTypes[leafName]; ok {
				if _, exists := meta.fieldTypes[dotted]; !exists {
					meta.fieldTypes[dotted] = lt
				}
			}
		}
		e.registerPromotedFields(meta, &StructDecl{Name: ct.Name, Fields: innerFields(inner, e)}, childPath, seen)
	}
}

func innerFields(meta structMeta, e *wasmEmitter) []FieldDecl {
	var out []FieldDecl
	seen := map[string]bool{}
	for name, t := range meta.fieldTypes {
		if seen[name] {
			continue
		}
		seen[name] = true
		if ct, ok := t.(CustomType); ok {
			out = append(out, FieldDecl{Name: name, Type: ct, Anonymous: true})
		}
	}
	return out
}

func memberFieldKey(e *MemberExpr) string {
	if len(e.EmbedPath) == 0 {
		return e.Member
	}
	return strings.Join(e.EmbedPath, ".") + "." + e.Member
}

// fieldLoadStoreInfo returns the correct load/store opcodes and alignment for
// a struct field's type.
func fieldLoadStoreInfo(t UbeType) (loadOp, storeOp byte, align uint32) {
	if t == nil {
		return 0x28, 0x36, 2
	}
	switch t.String() {
	case "bool", "u8":
		return 0x2d, 0x3a, 0
	case "i8":
		return 0x2c, 0x3a, 0
	case "u16":
		return 0x2f, 0x3b, 1
	case "i16":
		return 0x2e, 0x3b, 1
	case "f32":
		return 0x2a, 0x38, 2
	case "f64":
		return 0x2b, 0x39, 3
	case "i64", "u64":
		return 0x29, 0x37, 3
	default:
		return 0x28, 0x36, 2
	}
}

// sliceElemInfo returns the element width and the correct load/store
// opcodes + alignment for a slice's element type.
func sliceElemInfo(elem UbeType) (width int32, loadOp, storeOp byte, align uint32) {
	if elem == nil {
		return 4, 0x28, 0x36, 2
	}
	switch elem.String() {
	case "i8", "u8", "bool":
		return 1, 0x2d, 0x3a, 0
	case "i16", "u16":
		return 2, 0x2f, 0x3b, 1
	case "i64", "u64":
		return 8, 0x29, 0x37, 3
	case "f32":
		return 4, 0x2a, 0x38, 2
	case "f64":
		return 8, 0x2b, 0x39, 3
	default:
		w := int32(elem.ByteSize())
		if w <= 0 {
			w = 4
		}
		return w, 0x28, 0x36, 2
	}
}
