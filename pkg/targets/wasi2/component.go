// File: pkg/targets/wasi2/component.go
//
// Pure-Go WebAssembly Component Model binary encoder for WASI 0.2.
// Wraps the compiled core module using the standard Component Model
// shim + fixup table pattern so `canon lower` receives `memory` and
// `cabi_realloc` from the core module with zero external toolchains.

package wasi2

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/wasm"
)

// componentVersion identifies a WebAssembly Component Model binary:
// \0asm followed by version 0x0d, layer 0x01.
var componentVersion = []byte{0x0d, 0x00, 0x01, 0x00}

type coreImport struct {
	module  string
	name    string
	kind    byte
	typeIdx uint32
}

type parsedCoreModule struct {
	types   []wasm.FuncType
	imports []coreImport
}

// WrapCoreModuleIntoComponent wraps a compiled core WebAssembly module into a
// validated WebAssembly Component Model binary for WASI 0.2.
func WrapCoreModuleIntoComponent(coreBytes []byte) ([]byte, error) {
	if err := validateCoreModule(coreBytes); err != nil {
		return nil, err
	}

	parsed, err := parseCoreModuleImports(coreBytes)
	if err != nil {
		return nil, err
	}

	return buildWasi2Component(coreBytes, parsed)
}

func validateCoreModule(coreBytes []byte) error {
	if len(coreBytes) < 8 {
		return errShortCoreModule
	}
	magic := []byte{0x00, 0x61, 0x73, 0x6d}
	for i, b := range magic {
		if coreBytes[i] != b {
			return errBadCoreMagic
		}
	}
	if coreBytes[4] != 0x01 || coreBytes[5] != 0x00 || coreBytes[6] != 0x00 || coreBytes[7] != 0x00 {
		return errBadCoreVersion
	}
	return nil
}

func readUleb(data []byte) (uint32, int, error) {
	var res uint32
	var shift uint
	for i, b := range data {
		res |= uint32(b&0x7f) << shift
		if b&0x80 == 0 {
			return res, i + 1, nil
		}
		shift += 7
		if shift >= 35 {
			return 0, 0, fmt.Errorf("wasi2: invalid uleb128")
		}
	}
	return 0, 0, fmt.Errorf("wasi2: truncated uleb128")
}

func parseCoreModuleImports(coreBytes []byte) (*parsedCoreModule, error) {
	out := &parsedCoreModule{}
	pos := 8
	for pos < len(coreBytes) {
		secID := coreBytes[pos]
		pos++
		secLen, n, err := readUleb(coreBytes[pos:])
		if err != nil {
			return nil, err
		}
		pos += n
		if pos+int(secLen) > len(coreBytes) {
			return nil, fmt.Errorf("wasi2: truncated core section %d", secID)
		}
		sec := coreBytes[pos : pos+int(secLen)]
		pos += int(secLen)

		switch secID {
		case 1: // Type
			cnt, off, err := readUleb(sec)
			if err != nil {
				return nil, err
			}
			for i := uint32(0); i < cnt; i++ {
				if off >= len(sec) || sec[off] != 0x60 {
					return nil, fmt.Errorf("wasi2: expected functype 0x60")
				}
				off++
				pCnt, n, err := readUleb(sec[off:])
				if err != nil {
					return nil, err
				}
				off += n
				params := make([]wasm.ValType, pCnt)
				for j := uint32(0); j < pCnt; j++ {
					params[j] = wasm.ValType(sec[off])
					off++
				}
				rCnt, n, err := readUleb(sec[off:])
				if err != nil {
					return nil, err
				}
				off += n
				results := make([]wasm.ValType, rCnt)
				for j := uint32(0); j < rCnt; j++ {
					results[j] = wasm.ValType(sec[off])
					off++
				}
				out.types = append(out.types, wasm.FuncType{Params: params, Results: results})
			}
		case 2: // Import
			cnt, off, err := readUleb(sec)
			if err != nil {
				return nil, err
			}
			for i := uint32(0); i < cnt; i++ {
				mLen, n, err := readUleb(sec[off:])
				if err != nil {
					return nil, err
				}
				off += n
				mod := string(sec[off : off+int(mLen)])
				off += int(mLen)

				fLen, n, err := readUleb(sec[off:])
				if err != nil {
					return nil, err
				}
				off += n
				field := string(sec[off : off+int(fLen)])
				off += int(fLen)

				kind := sec[off]
				off++
				typeIdx, n, err := readUleb(sec[off:])
				if err != nil {
					return nil, err
				}
				off += n
				out.imports = append(out.imports, coreImport{
					module:  mod,
					name:    field,
					kind:    kind,
					typeIdx: typeIdx,
				})
			}
		}
	}
	return out, nil
}

// Component Model value type tags
const (
	compValBool   byte = 0x7f
	compValU8     byte = 0x7d
	compValU32    byte = 0x79
	compValU64    byte = 0x77
	compValString byte = 0x73
)

type canonOptsSpec struct {
	utf8    bool
	memory  bool
	realloc bool
}

var witLowerOpts = map[string]canonOptsSpec{
	"wasi:cli/stdout@0.2.0#get-stdout":                                    {},
	"wasi:cli/stderr@0.2.0#get-stderr":                                    {},
	"wasi:cli/stdin@0.2.0#get-stdin":                                      {},
	"wasi:cli/exit@0.2.0#exit":                                            {},
	"wasi:cli/environment@0.2.0#get-arguments":                            {utf8: true, memory: true, realloc: true},
	"wasi:cli/environment@0.2.0#get-environment":                          {utf8: true, memory: true, realloc: true},
	"wasi:io/streams@0.2.0#[method]output-stream.blocking-write-and-flush": {memory: true},
	"wasi:io/streams@0.2.0#[method]input-stream.blocking-read":             {memory: true, realloc: true},
	"wasi:clocks/monotonic-clock@0.2.0#now":                               {},
	"wasi:clocks/wall-clock@0.2.0#now":                                    {memory: true},
	"wasi:random/random@0.2.0#get-random-bytes":                           {memory: true, realloc: true},
	"wasi:filesystem/preopens@0.2.0#get-directories":                      {utf8: true, memory: true, realloc: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.read-via-stream":      {memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.write-via-stream":     {memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.read":                 {memory: true, realloc: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.write":                {memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.open-at":              {utf8: true, memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.stat":                 {memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.stat-at":              {utf8: true, memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.create-directory-at":  {utf8: true, memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.unlink-file-at":       {utf8: true, memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.remove-directory-at":  {utf8: true, memory: true},
	"wasi:filesystem/types@0.2.0#[method]descriptor.sync":                 {memory: true},
}

func appendCompStr(b *bytes.Buffer, s string) {
	b.Write(wasm.AppendUleb128(nil, uint32(len(s))))
	b.WriteString(s)
}

func appendCompSec(out *bytes.Buffer, id wasm.SectionID, payload []byte) {
	out.WriteByte(byte(id))
	out.Write(wasm.AppendUleb128(nil, uint32(len(payload))))
	out.Write(payload)
}

type instTypeBuilder struct {
	decls    [][]byte
	nextType uint32
}

func (ib *instTypeBuilder) addDefType(raw []byte) uint32 {
	var d bytes.Buffer
	d.WriteByte(0x01)
	d.Write(raw)
	ib.decls = append(ib.decls, d.Bytes())
	idx := ib.nextType
	ib.nextType++
	return idx
}

func (ib *instTypeBuilder) addOuterType(outerIdx uint32) uint32 {
	var d bytes.Buffer
	d.WriteByte(0x02) // decl: alias
	d.WriteByte(0x03) // sort: type
	d.WriteByte(0x02) // outer
	d.Write(wasm.AppendUleb128(nil, 1))
	d.Write(wasm.AppendUleb128(nil, outerIdx))
	ib.decls = append(ib.decls, d.Bytes())
	idx := ib.nextType
	ib.nextType++
	return idx
}

func (ib *instTypeBuilder) exportSubResource(name string) uint32 {
	var d bytes.Buffer
	d.WriteByte(0x04) // export
	d.WriteByte(0x00)
	appendCompStr(&d, name)
	d.WriteByte(0x03) // type
	d.WriteByte(0x01) // sub(resource)
	ib.decls = append(ib.decls, d.Bytes())
	idx := ib.nextType
	ib.nextType++
	return idx
}

func (ib *instTypeBuilder) exportTypeEq(name string, localIdx uint32) uint32 {
	var d bytes.Buffer
	d.WriteByte(0x04)
	d.WriteByte(0x00)
	appendCompStr(&d, name)
	d.WriteByte(0x03)
	d.WriteByte(0x00) // eq
	d.Write(wasm.AppendUleb128(nil, localIdx))
	ib.decls = append(ib.decls, d.Bytes())
	idx := ib.nextType
	ib.nextType++
	return idx
}

func (ib *instTypeBuilder) exportFunc(name string, fnTypeIdx uint32) {
	var d bytes.Buffer
	d.WriteByte(0x04)
	d.WriteByte(0x00)
	appendCompStr(&d, name)
	d.WriteByte(0x01) // func
	d.Write(wasm.AppendUleb128(nil, fnTypeIdx))
	ib.decls = append(ib.decls, d.Bytes())
}

func (ib *instTypeBuilder) encode() []byte {
	var b bytes.Buffer
	b.WriteByte(0x42)
	b.Write(wasm.AppendUleb128(nil, uint32(len(ib.decls))))
	for _, d := range ib.decls {
		b.Write(d)
	}
	return b.Bytes()
}

func encodeFuncType(params []struct {
	name string
	ty   uint32
}, hasResult bool, resTy uint32) []byte {
	var b bytes.Buffer
	b.WriteByte(0x40)
	b.Write(wasm.AppendUleb128(nil, uint32(len(params))))
	for _, p := range params {
		appendCompStr(&b, p.name)
		b.Write(wasm.AppendUleb128(nil, p.ty))
	}
	if hasResult {
		b.WriteByte(0x00)
		b.Write(wasm.AppendUleb128(nil, resTy))
	} else {
		b.WriteByte(0x01)
		b.WriteByte(0x00)
	}
	return b.Bytes()
}

func encodeRecord(fields []struct {
	name string
	ty   uint32
}) []byte {
	var b bytes.Buffer
	b.WriteByte(0x72)
	b.Write(wasm.AppendUleb128(nil, uint32(len(fields))))
	for _, f := range fields {
		appendCompStr(&b, f.name)
		b.Write(wasm.AppendUleb128(nil, f.ty))
	}
	return b.Bytes()
}

func encodeEnum(cases []string) []byte {
	var b bytes.Buffer
	b.WriteByte(0x6d)
	b.Write(wasm.AppendUleb128(nil, uint32(len(cases))))
	for _, c := range cases {
		appendCompStr(&b, c)
	}
	return b.Bytes()
}

func encodeFlags(names []string) []byte {
	var b bytes.Buffer
	b.WriteByte(0x6e)
	b.Write(wasm.AppendUleb128(nil, uint32(len(names))))
	for _, n := range names {
		appendCompStr(&b, n)
	}
	return b.Bytes()
}

func encodeResult(hasOk bool, okTy uint32, hasErr bool, errTy uint32) []byte {
	var b bytes.Buffer
	b.WriteByte(0x6a)
	if hasOk {
		b.WriteByte(0x01)
		b.Write(wasm.AppendUleb128(nil, okTy))
	} else {
		b.WriteByte(0x00)
	}
	if hasErr {
		b.WriteByte(0x01)
		b.Write(wasm.AppendUleb128(nil, errTy))
	} else {
		b.WriteByte(0x00)
	}
	return b.Bytes()
}

type compBuilder struct {
	out           bytes.Buffer
	nextType      uint32
	nextInst      uint32
	nextFunc      uint32
	nextCoreMod   uint32
	nextCoreInst  uint32
	nextCoreFunc  uint32
	nextCoreMem   uint32
	nextCoreTable uint32
}

func (cb *compBuilder) emitCoreModule(raw []byte) uint32 {
	appendCompSec(&cb.out, wasm.ComponentSectionCoreModule, raw)
	idx := cb.nextCoreMod
	cb.nextCoreMod++
	return idx
}

func (cb *compBuilder) emitType(raw []byte) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.Write(raw)
	appendCompSec(&cb.out, wasm.ComponentSectionType, sec.Bytes())
	idx := cb.nextType
	cb.nextType++
	return idx
}

func (cb *compBuilder) emitImportInstance(name string, instTypeIdx uint32) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x00)
	appendCompStr(&sec, name)
	sec.WriteByte(0x05) // instance
	sec.Write(wasm.AppendUleb128(nil, instTypeIdx))
	appendCompSec(&cb.out, wasm.ComponentSectionImport, sec.Bytes())
	idx := cb.nextInst
	cb.nextInst++
	return idx
}

func (cb *compBuilder) aliasCompType(instIdx uint32, name string) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x03) // type
	sec.WriteByte(0x00) // export of instance
	sec.Write(wasm.AppendUleb128(nil, instIdx))
	appendCompStr(&sec, name)
	appendCompSec(&cb.out, wasm.ComponentSectionAlias, sec.Bytes())
	idx := cb.nextType
	cb.nextType++
	return idx
}

func (cb *compBuilder) aliasCompFunc(instIdx uint32, name string) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x01) // func
	sec.WriteByte(0x00) // export of instance
	sec.Write(wasm.AppendUleb128(nil, instIdx))
	appendCompStr(&sec, name)
	appendCompSec(&cb.out, wasm.ComponentSectionAlias, sec.Bytes())
	idx := cb.nextFunc
	cb.nextFunc++
	return idx
}

func (cb *compBuilder) aliasCoreFunc(coreInstIdx uint32, name string) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x00)
	sec.WriteByte(0x00) // core:func
	sec.WriteByte(0x01) // core export alias
	sec.Write(wasm.AppendUleb128(nil, coreInstIdx))
	appendCompStr(&sec, name)
	appendCompSec(&cb.out, wasm.ComponentSectionAlias, sec.Bytes())
	idx := cb.nextCoreFunc
	cb.nextCoreFunc++
	return idx
}

func (cb *compBuilder) aliasCoreTable(coreInstIdx uint32, name string) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x00)
	sec.WriteByte(0x01) // core:table
	sec.WriteByte(0x01) // core export alias
	sec.Write(wasm.AppendUleb128(nil, coreInstIdx))
	appendCompStr(&sec, name)
	appendCompSec(&cb.out, wasm.ComponentSectionAlias, sec.Bytes())
	idx := cb.nextCoreTable
	cb.nextCoreTable++
	return idx
}

func (cb *compBuilder) aliasCoreMemory(coreInstIdx uint32, name string) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x00)
	sec.WriteByte(0x02) // core:memory
	sec.WriteByte(0x01) // core export alias
	sec.Write(wasm.AppendUleb128(nil, coreInstIdx))
	appendCompStr(&sec, name)
	appendCompSec(&cb.out, wasm.ComponentSectionAlias, sec.Bytes())
	idx := cb.nextCoreMem
	cb.nextCoreMem++
	return idx
}

func (cb *compBuilder) canonResourceDrop(typeIdx uint32) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x03) // resource.drop
	sec.Write(wasm.AppendUleb128(nil, typeIdx))
	appendCompSec(&cb.out, wasm.ComponentSectionCanon, sec.Bytes())
	idx := cb.nextCoreFunc
	cb.nextCoreFunc++
	return idx
}

func (cb *compBuilder) canonLower(fnIdx uint32, opts canonOptsSpec, memIdx, reallocIdx uint32) uint32 {
	var optList [][]byte
	if opts.utf8 {
		optList = append(optList, []byte{0x00})
	}
	if opts.memory {
		var o bytes.Buffer
		o.WriteByte(0x03)
		o.Write(wasm.AppendUleb128(nil, memIdx))
		optList = append(optList, o.Bytes())
	}
	if opts.realloc {
		var o bytes.Buffer
		o.WriteByte(0x04)
		o.Write(wasm.AppendUleb128(nil, reallocIdx))
		optList = append(optList, o.Bytes())
	}
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x01) // lower
	sec.WriteByte(0x00) // func
	sec.Write(wasm.AppendUleb128(nil, fnIdx))
	sec.Write(wasm.AppendUleb128(nil, uint32(len(optList))))
	for _, o := range optList {
		sec.Write(o)
	}
	appendCompSec(&cb.out, wasm.ComponentSectionCanon, sec.Bytes())
	idx := cb.nextCoreFunc
	cb.nextCoreFunc++
	return idx
}

func (cb *compBuilder) canonLift(coreFnIdx, compFnTypeIdx uint32) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x00) // lift
	sec.WriteByte(0x00) // func
	sec.Write(wasm.AppendUleb128(nil, coreFnIdx))
	sec.WriteByte(0x00) // 0 options
	sec.Write(wasm.AppendUleb128(nil, compFnTypeIdx))
	appendCompSec(&cb.out, wasm.ComponentSectionCanon, sec.Bytes())
	idx := cb.nextFunc
	cb.nextFunc++
	return idx
}

type coreExportSpec struct {
	name string
	sort byte
	idx  uint32
}

func (cb *compBuilder) coreInstFromExports(exports []coreExportSpec) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01) // 1 core instance
	sec.WriteByte(0x01) // from exports
	sec.Write(wasm.AppendUleb128(nil, uint32(len(exports))))
	for _, ex := range exports {
		appendCompStr(&sec, ex.name)
		sec.WriteByte(ex.sort)
		sec.Write(wasm.AppendUleb128(nil, ex.idx))
	}
	appendCompSec(&cb.out, wasm.ComponentSectionCoreInstance, sec.Bytes())
	idx := cb.nextCoreInst
	cb.nextCoreInst++
	return idx
}

type coreInstArg struct {
	name    string
	instIdx uint32
}

func (cb *compBuilder) coreInstInstantiate(modIdx uint32, args []coreInstArg) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x00) // instantiate
	sec.Write(wasm.AppendUleb128(nil, modIdx))
	sec.Write(wasm.AppendUleb128(nil, uint32(len(args))))
	for _, a := range args {
		appendCompStr(&sec, a.name)
		sec.WriteByte(0x12) // core:instance
		sec.Write(wasm.AppendUleb128(nil, a.instIdx))
	}
	appendCompSec(&cb.out, wasm.ComponentSectionCoreInstance, sec.Bytes())
	idx := cb.nextCoreInst
	cb.nextCoreInst++
	return idx
}

func (cb *compBuilder) compInstFromFuncExport(name string, fnIdx uint32) uint32 {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x01) // from exports
	sec.WriteByte(0x01) // 1 export
	sec.WriteByte(0x00)
	appendCompStr(&sec, name)
	sec.WriteByte(0x01) // func
	sec.Write(wasm.AppendUleb128(nil, fnIdx))
	appendCompSec(&cb.out, wasm.ComponentSectionInstance, sec.Bytes())
	idx := cb.nextInst
	cb.nextInst++
	return idx
}

func (cb *compBuilder) emitExportInstance(name string, instIdx uint32) {
	var sec bytes.Buffer
	sec.WriteByte(0x01)
	sec.WriteByte(0x00)
	appendCompStr(&sec, name)
	sec.WriteByte(0x05) // instance
	sec.Write(wasm.AppendUleb128(nil, instIdx))
	sec.WriteByte(0x00) // option<externdesc> = None
	appendCompSec(&cb.out, wasm.ComponentSectionExport, sec.Bytes())
}

func encodeRawFuncType(ft wasm.FuncType) []byte {
	var b bytes.Buffer
	b.WriteByte(0x60)
	b.Write(wasm.AppendUleb128(nil, uint32(len(ft.Params))))
	for _, p := range ft.Params {
		b.WriteByte(byte(p))
	}
	b.Write(wasm.AppendUleb128(nil, uint32(len(ft.Results))))
	for _, r := range ft.Results {
		b.WriteByte(byte(r))
	}
	return b.Bytes()
}

func buildShimModule(funcTypes []wasm.FuncType) []byte {
	n := uint32(len(funcTypes))
	m := wasm.NewModule()
	typeIndices := make([]uint32, n)
	for i, ft := range funcTypes {
		typeIndices[i] = m.AddType(ft)
	}
	if n > 0 {
		m.Tables = append(m.Tables, wasm.TableType{
			ElemType: wasm.ValFuncRef,
			Limits:   wasm.Limits{HasMax: true, Min: n, Max: n},
		})
		m.Exports = append(m.Exports, wasm.ExportEntry{
			Name:  "table",
			Kind:  wasm.KindTable,
			Index: 0,
		})
	}
	for i, ft := range funcTypes {
		tIdx := typeIndices[i]
		m.Functions = append(m.Functions, tIdx)
		var code wasm.Buffer
		for p := uint32(0); p < uint32(len(ft.Params)); p++ {
			code.Emit(wasm.OpLocalGet)
			code.EmitUleb128(p)
		}
		code.EmitI32Const(int32(i))
		code.Emit(wasm.OpCallIndirect)
		code.EmitUleb128(tIdx)
		code.EmitUleb128(0)
		code.Emit(wasm.OpEnd)
		m.Bodies = append(m.Bodies, wasm.FunctionBody{Code: code.Bytes})
		m.Exports = append(m.Exports, wasm.ExportEntry{
			Name:  fmt.Sprintf("f%d", i),
			Kind:  wasm.KindFunc,
			Index: uint32(i),
		})
	}
	enc, _ := m.Encode()
	return enc
}

func buildFixupModule(funcTypes []wasm.FuncType) []byte {
	n := uint32(len(funcTypes))
	var out bytes.Buffer
	out.Write(wasm.Magic[:])
	out.Write(wasm.Version[:])

	// Type section: funcTypes + void->void + void->i32
	typeMap := map[string]uint32{}
	var typeList []wasm.FuncType
	addType := func(ft wasm.FuncType) uint32 {
		key := string(encodeRawFuncType(ft))
		if idx, ok := typeMap[key]; ok {
			return idx
		}
		idx := uint32(len(typeList))
		typeMap[key] = idx
		typeList = append(typeList, ft)
		return idx
	}
	importTypeIndices := make([]uint32, n)
	for i, ft := range funcTypes {
		importTypeIndices[i] = addType(ft)
	}
	voidVoidIdx := addType(wasm.FuncType{})
	voidI32Idx := addType(wasm.FuncType{Results: []wasm.ValType{wasm.ValI32}})

	var typeSec bytes.Buffer
	typeSec.Write(wasm.AppendUleb128(nil, uint32(len(typeList))))
	for _, ft := range typeList {
		typeSec.Write(encodeRawFuncType(ft))
	}
	appendCompSec(&out, wasm.SectionID(1), typeSec.Bytes())

	// Import section: optional table + f0..f(N-1) + m._start
	importCount := n + 1
	if n > 0 {
		importCount++
	}
	var impSec bytes.Buffer
	impSec.Write(wasm.AppendUleb128(nil, importCount))
	if n > 0 {
		appendCompStr(&impSec, "s")
		appendCompStr(&impSec, "table")
		impSec.WriteByte(0x01) // table
		impSec.WriteByte(byte(wasm.ValFuncRef))
		impSec.WriteByte(0x01) // has max
		impSec.Write(wasm.AppendUleb128(nil, n))
		impSec.Write(wasm.AppendUleb128(nil, n))
	}
	for i := uint32(0); i < n; i++ {
		appendCompStr(&impSec, "f")
		appendCompStr(&impSec, fmt.Sprintf("f%d", i))
		impSec.WriteByte(0x00) // func
		impSec.Write(wasm.AppendUleb128(nil, importTypeIndices[i]))
	}
	appendCompStr(&impSec, "m")
	appendCompStr(&impSec, "_start")
	impSec.WriteByte(0x00)
	impSec.Write(wasm.AppendUleb128(nil, voidVoidIdx))
	appendCompSec(&out, wasm.SectionID(2), impSec.Bytes())

	// Function section: run() -> i32 (index = n + 1)
	var fnSec bytes.Buffer
	fnSec.WriteByte(0x01)
	fnSec.Write(wasm.AppendUleb128(nil, voidI32Idx))
	appendCompSec(&out, wasm.SectionID(3), fnSec.Bytes())

	// Export section: run -> func (n + 1)
	var expSec bytes.Buffer
	expSec.WriteByte(0x01)
	appendCompStr(&expSec, "run")
	expSec.WriteByte(0x00)
	expSec.Write(wasm.AppendUleb128(nil, n+1))
	appendCompSec(&out, wasm.SectionID(7), expSec.Bytes())

	// Element section: populate table[0..N-1] with imported f0..f(N-1)
	if n > 0 {
		var elemSec bytes.Buffer
		elemSec.WriteByte(0x01) // 1 segment
		elemSec.WriteByte(0x00) // active table 0
		elemSec.WriteByte(wasm.OpI32Const)
		elemSec.Write(wasm.AppendSleb128(nil, 0))
		elemSec.WriteByte(wasm.OpEnd)
		elemSec.Write(wasm.AppendUleb128(nil, n))
		for i := uint32(0); i < n; i++ {
			elemSec.Write(wasm.AppendUleb128(nil, i))
		}
		appendCompSec(&out, wasm.SectionID(9), elemSec.Bytes())
	}

	// Code section: call $_start (index n); i32.const 0; end
	var codeBody bytes.Buffer
	codeBody.WriteByte(0x00) // 0 locals
	codeBody.WriteByte(wasm.OpCall)
	codeBody.Write(wasm.AppendUleb128(nil, n))
	codeBody.WriteByte(wasm.OpI32Const)
	codeBody.Write(wasm.AppendSleb128(nil, 0))
	codeBody.WriteByte(wasm.OpEnd)

	var codeSec bytes.Buffer
	codeSec.WriteByte(0x01)
	codeSec.Write(wasm.AppendUleb128(nil, uint32(codeBody.Len())))
	codeSec.Write(codeBody.Bytes())
	appendCompSec(&out, wasm.SectionID(10), codeSec.Bytes())

	return out.Bytes()
}

func isDropName(name string) (string, bool) {
	if strings.HasPrefix(name, "[resource-drop]") {
		return strings.TrimPrefix(name, "[resource-drop]"), true
	}
	return "", false
}

func buildWasi2Component(coreBytes []byte, parsed *parsedCoreModule) ([]byte, error) {
	// Classify imports into lowered functions vs resource-drops.
	type loweredItem struct {
		imp      coreImport
		fnType   wasm.FuncType
		shimSlot uint32
	}
	var lowered []loweredItem
	var loweredTypes []wasm.FuncType
	neededMod := map[string]bool{}

	for _, imp := range parsed.imports {
		if !strings.HasPrefix(imp.module, "wasi:") {
			return nil, fmt.Errorf("wasi2: cannot package non-WASI core import %s.%s into a WASI 0.2 component", imp.module, imp.name)
		}
		neededMod[imp.module] = true
		if _, isDrop := isDropName(imp.name); isDrop {
			continue
		}
		if int(imp.typeIdx) >= len(parsed.types) {
			return nil, fmt.Errorf("wasi2: import %s.%s references invalid type index %d", imp.module, imp.name, imp.typeIdx)
		}
		ft := parsed.types[imp.typeIdx]
		slot := uint32(len(lowered))
		lowered = append(lowered, loweredItem{imp: imp, fnType: ft, shimSlot: slot})
		loweredTypes = append(loweredTypes, ft)
	}

	// Ensure transitive interface dependencies are imported.
	if neededMod["wasi:filesystem/preopens@0.2.0"] {
		neededMod["wasi:filesystem/types@0.2.0"] = true
	}
	if neededMod["wasi:filesystem/types@0.2.0"] {
		neededMod["wasi:io/streams@0.2.0"] = true
		neededMod["wasi:clocks/wall-clock@0.2.0"] = true
	}
	if neededMod["wasi:cli/stdin@0.2.0"] || neededMod["wasi:cli/stdout@0.2.0"] || neededMod["wasi:cli/stderr@0.2.0"] {
		neededMod["wasi:io/streams@0.2.0"] = true
	}
	if neededMod["wasi:io/streams@0.2.0"] {
		neededMod["wasi:io/error@0.2.0"] = true
	}

	shimBytes := buildShimModule(loweredTypes)
	fixupBytes := buildFixupModule(loweredTypes)

	var cb compBuilder
	cb.out.Write(wasm.ComponentMagic[:])
	cb.out.Write(componentVersion)

	mainModIdx := cb.emitCoreModule(coreBytes)
	shimModIdx := cb.emitCoreModule(shimBytes)
	fixupModIdx := cb.emitCoreModule(fixupBytes)

	// Track imported component instance types/instances and aliased types/funcs.
	compTypes := map[string]uint32{}
	compFuncs := map[string]uint32{}

	if neededMod["wasi:io/error@0.2.0"] {
		var ib instTypeBuilder
		ib.exportSubResource("error")
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:io/error@0.2.0", tIdx)
		compTypes["error"] = cb.aliasCompType(iIdx, "error")
	}

	if neededMod["wasi:io/streams@0.2.0"] {
		var ib instTypeBuilder
		errT := ib.addOuterType(compTypes["error"])
		inStreamT := ib.exportSubResource("input-stream")
		outStreamT := ib.exportSubResource("output-stream")
		ownErrT := ib.addDefType([]byte{0x69, byte(errT)})
		var varB bytes.Buffer
		varB.WriteByte(0x71)
		varB.WriteByte(0x02)
		appendCompStr(&varB, "last-operation-failed")
		varB.WriteByte(0x01)
		varB.Write(wasm.AppendUleb128(nil, ownErrT))
		varB.WriteByte(0x00)
		appendCompStr(&varB, "closed")
		varB.WriteByte(0x00)
		varB.WriteByte(0x00)
		streamErrDef := ib.addDefType(varB.Bytes())
		streamErrT := ib.exportTypeEq("stream-error", streamErrDef)

		borrowOutT := ib.addDefType([]byte{0x68, byte(outStreamT)})
		listU8T := ib.addDefType([]byte{0x70, compValU8})
		resVoidErrT := ib.addDefType(encodeResult(false, 0, true, streamErrT))
		writeFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowOutT}, {"contents", listU8T}}, true, resVoidErrT))
		ib.exportFunc("[method]output-stream.blocking-write-and-flush", writeFnT)

		borrowInT := ib.addDefType([]byte{0x68, byte(inStreamT)})
		resListErrT := ib.addDefType(encodeResult(true, listU8T, true, streamErrT))
		readFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowInT}, {"len", uint32(compValU64)}}, true, resListErrT))
		ib.exportFunc("[method]input-stream.blocking-read", readFnT)

		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:io/streams@0.2.0", tIdx)
		compTypes["input-stream"] = cb.aliasCompType(iIdx, "input-stream")
		compTypes["output-stream"] = cb.aliasCompType(iIdx, "output-stream")
		compFuncs["wasi:io/streams@0.2.0#[method]output-stream.blocking-write-and-flush"] = cb.aliasCompFunc(iIdx, "[method]output-stream.blocking-write-and-flush")
		compFuncs["wasi:io/streams@0.2.0#[method]input-stream.blocking-read"] = cb.aliasCompFunc(iIdx, "[method]input-stream.blocking-read")
	}

	if neededMod["wasi:cli/stdin@0.2.0"] {
		var ib instTypeBuilder
		inT := ib.addOuterType(compTypes["input-stream"])
		ownInT := ib.addDefType([]byte{0x69, byte(inT)})
		fnT := ib.addDefType(encodeFuncType(nil, true, ownInT))
		ib.exportFunc("get-stdin", fnT)
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:cli/stdin@0.2.0", tIdx)
		compFuncs["wasi:cli/stdin@0.2.0#get-stdin"] = cb.aliasCompFunc(iIdx, "get-stdin")
	}
	if neededMod["wasi:cli/stdout@0.2.0"] {
		var ib instTypeBuilder
		outT := ib.addOuterType(compTypes["output-stream"])
		ownOutT := ib.addDefType([]byte{0x69, byte(outT)})
		fnT := ib.addDefType(encodeFuncType(nil, true, ownOutT))
		ib.exportFunc("get-stdout", fnT)
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:cli/stdout@0.2.0", tIdx)
		compFuncs["wasi:cli/stdout@0.2.0#get-stdout"] = cb.aliasCompFunc(iIdx, "get-stdout")
	}
	if neededMod["wasi:cli/stderr@0.2.0"] {
		var ib instTypeBuilder
		outT := ib.addOuterType(compTypes["output-stream"])
		ownOutT := ib.addDefType([]byte{0x69, byte(outT)})
		fnT := ib.addDefType(encodeFuncType(nil, true, ownOutT))
		ib.exportFunc("get-stderr", fnT)
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:cli/stderr@0.2.0", tIdx)
		compFuncs["wasi:cli/stderr@0.2.0#get-stderr"] = cb.aliasCompFunc(iIdx, "get-stderr")
	}

	if neededMod["wasi:cli/exit@0.2.0"] {
		var ib instTypeBuilder
		resT := ib.addDefType(encodeResult(false, 0, false, 0))
		fnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"status", resT}}, false, 0))
		ib.exportFunc("exit", fnT)
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:cli/exit@0.2.0", tIdx)
		compFuncs["wasi:cli/exit@0.2.0#exit"] = cb.aliasCompFunc(iIdx, "exit")
	}

	if neededMod["wasi:cli/environment@0.2.0"] {
		var ib instTypeBuilder
		listStrT := ib.addDefType([]byte{0x70, compValString})
		argsFnT := ib.addDefType(encodeFuncType(nil, true, listStrT))
		ib.exportFunc("get-arguments", argsFnT)

		tupStrT := ib.addDefType([]byte{0x6f, 0x02, compValString, compValString})
		listEnvT := ib.addDefType([]byte{0x70, byte(tupStrT)})
		envFnT := ib.addDefType(encodeFuncType(nil, true, listEnvT))
		ib.exportFunc("get-environment", envFnT)

		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:cli/environment@0.2.0", tIdx)
		compFuncs["wasi:cli/environment@0.2.0#get-arguments"] = cb.aliasCompFunc(iIdx, "get-arguments")
		compFuncs["wasi:cli/environment@0.2.0#get-environment"] = cb.aliasCompFunc(iIdx, "get-environment")
	}

	if neededMod["wasi:clocks/monotonic-clock@0.2.0"] {
		var ib instTypeBuilder
		fnT := ib.addDefType(encodeFuncType(nil, true, uint32(compValU64)))
		ib.exportFunc("now", fnT)
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:clocks/monotonic-clock@0.2.0", tIdx)
		compFuncs["wasi:clocks/monotonic-clock@0.2.0#now"] = cb.aliasCompFunc(iIdx, "now")
	}

	if neededMod["wasi:clocks/wall-clock@0.2.0"] {
		var ib instTypeBuilder
		dtDef := ib.addDefType(encodeRecord([]struct {
			name string
			ty   uint32
		}{{"seconds", uint32(compValU64)}, {"nanoseconds", uint32(compValU32)}}))
		dtT := ib.exportTypeEq("datetime", dtDef)
		fnT := ib.addDefType(encodeFuncType(nil, true, dtT))
		ib.exportFunc("now", fnT)
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:clocks/wall-clock@0.2.0", tIdx)
		compTypes["datetime"] = cb.aliasCompType(iIdx, "datetime")
		compFuncs["wasi:clocks/wall-clock@0.2.0#now"] = cb.aliasCompFunc(iIdx, "now")
	}

	if neededMod["wasi:random/random@0.2.0"] {
		var ib instTypeBuilder
		listU8T := ib.addDefType([]byte{0x70, compValU8})
		fnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"len", uint32(compValU64)}}, true, listU8T))
		ib.exportFunc("get-random-bytes", fnT)
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:random/random@0.2.0", tIdx)
		compFuncs["wasi:random/random@0.2.0#get-random-bytes"] = cb.aliasCompFunc(iIdx, "get-random-bytes")
	}

	if neededMod["wasi:filesystem/types@0.2.0"] {
		var ib instTypeBuilder
		inStreamT := ib.addOuterType(compTypes["input-stream"])
		outStreamT := ib.addOuterType(compTypes["output-stream"])
		dtT := ib.addOuterType(compTypes["datetime"])
		descT := ib.exportSubResource("descriptor")

		descTypeDef := ib.addDefType(encodeEnum([]string{
			"unknown", "block-device", "character-device", "directory",
			"fifo", "symbolic-link", "regular-file", "socket",
		}))
		descTypeT := ib.exportTypeEq("descriptor-type", descTypeDef)

		descFlagsDef := ib.addDefType(encodeFlags([]string{
			"read", "write", "file-integrity-sync", "data-integrity-sync",
			"requested-write-sync", "mutate-directory",
		}))
		descFlagsT := ib.exportTypeEq("descriptor-flags", descFlagsDef)

		pathFlagsDef := ib.addDefType(encodeFlags([]string{"symlink-follow"}))
		pathFlagsT := ib.exportTypeEq("path-flags", pathFlagsDef)

		openFlagsDef := ib.addDefType(encodeFlags([]string{"create", "directory", "exclusive", "truncate"}))
		openFlagsT := ib.exportTypeEq("open-flags", openFlagsDef)

		optDtT := ib.addDefType([]byte{0x6b, byte(dtT)})
		statDef := ib.addDefType(encodeRecord([]struct {
			name string
			ty   uint32
		}{
			{"type", descTypeT},
			{"link-count", uint32(compValU64)},
			{"size", uint32(compValU64)},
			{"data-access-timestamp", optDtT},
			{"data-modification-timestamp", optDtT},
			{"status-change-timestamp", optDtT},
		}))
		statT := ib.exportTypeEq("descriptor-stat", statDef)

		errCodeDef := ib.addDefType(encodeEnum([]string{
			"access", "would-block", "already", "bad-descriptor", "busy",
			"deadlock", "quota", "exist", "file-too-large", "illegal-byte-sequence",
			"in-progress", "interrupted", "invalid", "io", "is-directory",
			"loop", "too-many-links", "message-size", "name-too-long", "no-device",
			"no-entry", "no-lock", "insufficient-memory", "insufficient-space",
			"not-directory", "not-empty", "not-recoverable", "unsupported",
			"no-tty", "no-such-device", "overflow", "not-permitted", "pipe",
			"read-only", "invalid-seek", "text-file-busy", "cross-device",
		}))
		errCodeT := ib.exportTypeEq("error-code", errCodeDef)

		borrowDescT := ib.addDefType([]byte{0x68, byte(descT)})
		ownInT := ib.addDefType([]byte{0x69, byte(inStreamT)})
		resInT := ib.addDefType(encodeResult(true, ownInT, true, errCodeT))
		readStreamFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowDescT}, {"offset", uint32(compValU64)}}, true, resInT))
		ib.exportFunc("[method]descriptor.read-via-stream", readStreamFnT)

		ownOutT := ib.addDefType([]byte{0x69, byte(outStreamT)})
		resOutT := ib.addDefType(encodeResult(true, ownOutT, true, errCodeT))
		writeStreamFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowDescT}, {"offset", uint32(compValU64)}}, true, resOutT))
		ib.exportFunc("[method]descriptor.write-via-stream", writeStreamFnT)

		listU8T := ib.addDefType([]byte{0x70, compValU8})
		tupReadT := ib.addDefType([]byte{0x6f, 0x02, byte(listU8T), compValBool})
		resReadT := ib.addDefType(encodeResult(true, tupReadT, true, errCodeT))
		readFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowDescT}, {"length", uint32(compValU64)}, {"offset", uint32(compValU64)}}, true, resReadT))
		ib.exportFunc("[method]descriptor.read", readFnT)

		resWriteT := ib.addDefType(encodeResult(true, uint32(compValU64), true, errCodeT))
		writeFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowDescT}, {"buffer", listU8T}, {"offset", uint32(compValU64)}}, true, resWriteT))
		ib.exportFunc("[method]descriptor.write", writeFnT)

		ownDescT := ib.addDefType([]byte{0x69, byte(descT)})
		resOpenT := ib.addDefType(encodeResult(true, ownDescT, true, errCodeT))
		openAtFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{
			{"self", borrowDescT},
			{"path-flags", pathFlagsT},
			{"path", uint32(compValString)},
			{"open-flags", openFlagsT},
			{"flags", descFlagsT},
		}, true, resOpenT))
		ib.exportFunc("[method]descriptor.open-at", openAtFnT)

		resStatT := ib.addDefType(encodeResult(true, statT, true, errCodeT))
		statFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowDescT}}, true, resStatT))
		ib.exportFunc("[method]descriptor.stat", statFnT)

		statAtFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowDescT}, {"path-flags", pathFlagsT}, {"path", uint32(compValString)}}, true, resStatT))
		ib.exportFunc("[method]descriptor.stat-at", statAtFnT)

		resUnitT := ib.addDefType(encodeResult(false, 0, true, errCodeT))
		pathOpFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowDescT}, {"path", uint32(compValString)}}, true, resUnitT))
		ib.exportFunc("[method]descriptor.create-directory-at", pathOpFnT)
		ib.exportFunc("[method]descriptor.unlink-file-at", pathOpFnT)
		ib.exportFunc("[method]descriptor.remove-directory-at", pathOpFnT)

		syncFnT := ib.addDefType(encodeFuncType([]struct {
			name string
			ty   uint32
		}{{"self", borrowDescT}}, true, resUnitT))
		ib.exportFunc("[method]descriptor.sync", syncFnT)

		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:filesystem/types@0.2.0", tIdx)
		compTypes["descriptor"] = cb.aliasCompType(iIdx, "descriptor")
		for _, m := range []string{
			"[method]descriptor.read-via-stream",
			"[method]descriptor.write-via-stream",
			"[method]descriptor.read",
			"[method]descriptor.write",
			"[method]descriptor.open-at",
			"[method]descriptor.stat",
			"[method]descriptor.stat-at",
			"[method]descriptor.create-directory-at",
			"[method]descriptor.unlink-file-at",
			"[method]descriptor.remove-directory-at",
			"[method]descriptor.sync",
		} {
			compFuncs["wasi:filesystem/types@0.2.0#"+m] = cb.aliasCompFunc(iIdx, m)
		}
	}

	if neededMod["wasi:filesystem/preopens@0.2.0"] {
		var ib instTypeBuilder
		descT := ib.addOuterType(compTypes["descriptor"])
		ownDescT := ib.addDefType([]byte{0x69, byte(descT)})
		tupT := ib.addDefType([]byte{0x6f, 0x02, byte(ownDescT), compValString})
		listT := ib.addDefType([]byte{0x70, byte(tupT)})
		fnT := ib.addDefType(encodeFuncType(nil, true, listT))
		ib.exportFunc("get-directories", fnT)
		tIdx := cb.emitType(ib.encode())
		iIdx := cb.emitImportInstance("wasi:filesystem/preopens@0.2.0", tIdx)
		compFuncs["wasi:filesystem/preopens@0.2.0#get-directories"] = cb.aliasCompFunc(iIdx, "get-directories")
	}

	// Emit canon resource.drop for any imported [resource-drop]* functions.
	dropCoreFuncs := map[string]uint32{}
	for _, imp := range parsed.imports {
		if resName, isDrop := isDropName(imp.name); isDrop {
			tIdx, ok := compTypes[resName]
			if !ok {
				return nil, fmt.Errorf("wasi2: unknown resource type %q for %s", resName, imp.name)
			}
			dropCoreFuncs[imp.module+"#"+imp.name] = cb.canonResourceDrop(tIdx)
		}
	}

	// Instantiate shim (0 args) and alias its table + indirect stubs.
	shimInst := cb.coreInstInstantiate(shimModIdx, nil)
	var shimTableIdx uint32
	if len(lowered) > 0 {
		shimTableIdx = cb.aliasCoreTable(shimInst, "table")
	}
	shimFuncs := make([]uint32, len(lowered))
	for i := range lowered {
		shimFuncs[i] = cb.aliasCoreFunc(shimInst, fmt.Sprintf("f%d", i))
	}

	// Build per-module core instances for main's imports.
	modExports := map[string][]coreExportSpec{}
	loweredSlotByKey := map[string]uint32{}
	for _, item := range lowered {
		loweredSlotByKey[item.imp.module+"#"+item.imp.name] = item.shimSlot
	}
	for _, imp := range parsed.imports {
		key := imp.module + "#" + imp.name
		if dropIdx, isDrop := dropCoreFuncs[key]; isDrop {
			modExports[imp.module] = append(modExports[imp.module], coreExportSpec{
				name: imp.name,
				sort: 0x00,
				idx:  dropIdx,
			})
		} else {
			slot := loweredSlotByKey[key]
			modExports[imp.module] = append(modExports[imp.module], coreExportSpec{
				name: imp.name,
				sort: 0x00,
				idx:  shimFuncs[slot],
			})
		}
	}

	var modNames []string
	for m := range modExports {
		modNames = append(modNames, m)
	}
	sort.Strings(modNames)

	var mainArgs []coreInstArg
	for _, m := range modNames {
		cInst := cb.coreInstFromExports(modExports[m])
		mainArgs = append(mainArgs, coreInstArg{name: m, instIdx: cInst})
	}

	mainInst := cb.coreInstInstantiate(mainModIdx, mainArgs)
	mainMem := cb.aliasCoreMemory(mainInst, "memory")
	mainRealloc := cb.aliasCoreFunc(mainInst, "cabi_realloc")
	mainStart := cb.aliasCoreFunc(mainInst, "_start")

	// Lower all imported WIT functions now that memory & cabi_realloc exist.
	var fixupArgs []coreInstArg
	if len(lowered) > 0 {
		var fExports []coreExportSpec
		for i, item := range lowered {
			key := item.imp.module + "#" + item.imp.name
			compFn, ok := compFuncs[key]
			if !ok {
				return nil, fmt.Errorf("wasi2: missing component import mapping for %s", key)
			}
			loweredCore := cb.canonLower(compFn, witLowerOpts[key], mainMem, mainRealloc)
			fExports = append(fExports, coreExportSpec{
				name: fmt.Sprintf("f%d", i),
				sort: 0x00,
				idx:  loweredCore,
			})
		}
		sInst := cb.coreInstFromExports([]coreExportSpec{{name: "table", sort: 0x01, idx: shimTableIdx}})
		fInst := cb.coreInstFromExports(fExports)
		fixupArgs = append(fixupArgs, coreInstArg{name: "s", instIdx: sInst}, coreInstArg{name: "f", instIdx: fInst})
	}
	mInst := cb.coreInstFromExports([]coreExportSpec{{name: "_start", sort: 0x00, idx: mainStart}})
	fixupArgs = append(fixupArgs, coreInstArg{name: "m", instIdx: mInst})

	fixupInst := cb.coreInstInstantiate(fixupModIdx, fixupArgs)
	runCoreFn := cb.aliasCoreFunc(fixupInst, "run")

	// Lift fixup.run () -> i32 into wasi:cli/run@0.2.0#run () -> result.
	runResT := cb.emitType(encodeResult(false, 0, false, 0))
	runFnT := cb.emitType(encodeFuncType(nil, true, runResT))
	runCompFn := cb.canonLift(runCoreFn, runFnT)
	runCompInst := cb.compInstFromFuncExport("run", runCompFn)
	cb.emitExportInstance("wasi:cli/run@0.2.0", runCompInst)

	return cb.out.Bytes(), nil
}
