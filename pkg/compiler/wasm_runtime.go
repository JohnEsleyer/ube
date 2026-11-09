package compiler

import (
	"bytes"
	"encoding/binary"
	"math"
	"strings"
)

func (e *wasmEmitter) buildPrintStrFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec+4)
	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x41)
	writeSleb128(&body, scratchNwrite)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fdWriteIdx)
	body.WriteByte(0x1a)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec+48)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec+4)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x41)
	writeSleb128(&body, scratchCiovec)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x41)
	writeSleb128(&body, scratchNwrite)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fdWriteIdx)
	body.WriteByte(0x1a)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 0)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildPrintBoolFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x41)
	writeSleb128(&body, int32(e.trueHeader))
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 4)
	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fnIndices["__print_str"])

	body.WriteByte(0x05)

	body.WriteByte(0x41)
	writeSleb128(&body, int32(e.falseHeader))
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fnIndices["__print_str"])

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 0)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildIntToStrFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	emitI32Const(&body, 7)
	body.WriteByte(0x6a)
	emitI32Const(&body, -8)
	body.WriteByte(0x71)
	emitLocalSet(&body, 1)

	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)

	emitI32Const(&body, 0)
	emitLocalSet(&body, 3)

	emitLocalGet(&body, 0)
	emitI32Eqz(&body)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 2)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 48)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x05)

	emitLocalGet(&body, 0)
	emitI32Const(&body, 0)
	body.WriteByte(0x48)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitI32Const(&body, 1)
	emitLocalSet(&body, 3)
	emitI32Const(&body, 0)
	emitLocalGet(&body, 0)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 0)
	body.WriteByte(0x0b)

	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x22)
	writeUleb128(&body, 2)
	emitLocalGet(&body, 0)
	emitI32Const(&body, 10)
	body.WriteByte(0x6f)
	emitI32Const(&body, 48)
	body.WriteByte(0x6a)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	emitLocalGet(&body, 0)
	emitI32Const(&body, 10)
	body.WriteByte(0x6d)
	body.WriteByte(0x22)
	writeUleb128(&body, 0)
	emitI32Eqz(&body)
	emitI32Eqz(&body)
	body.WriteByte(0x0d)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)

	emitLocalGet(&body, 3)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x22)
	writeUleb128(&body, 2)
	emitI32Const(&body, 45)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)

	body.WriteByte(0x0b)

	emitLocalGet(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Store(&body, 0, 2)
	emitLocalGet(&body, 1)
	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	emitLocalGet(&body, 2)
	body.WriteByte(0x6b)
	emitI32Store(&body, 4, 2)
	emitLocalGet(&body, 1)

	var full bytes.Buffer
	writeUleb128(&full, 3)
	for i := 0; i < 3; i++ {
		writeUleb128(&full, 1)
		full.WriteByte(valTypeI32)
	}
	full.Write(body.Bytes())
	full.WriteByte(0x0b)

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildPrintInt64Func() []byte {
	var body bytes.Buffer

	emitLocalGet(&body, 0)
	emitCallIdx(&body, e.fnIndices["__i64_to_str"])
	emitLocalSet(&body, 2)

	emitLocalGet(&body, 2)
	emitI32Load(&body, 0, 2)
	emitLocalGet(&body, 2)
	emitI32Load(&body, 4, 2)
	emitLocalGet(&body, 1)
	emitCallIdx(&body, e.fnIndices["__print_str"])

	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 1)
	writeUleb128(&full, 1)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildPrintF64Func() []byte {
	var body bytes.Buffer

	emitLocalGet(&body, 0)
	emitCallIdx(&body, e.fnIndices["__f64_to_str"])
	emitLocalSet(&body, 2)

	emitLocalGet(&body, 2)
	emitI32Load(&body, 0, 2)
	emitLocalGet(&body, 2)
	emitI32Load(&body, 4, 2)
	emitLocalGet(&body, 1)
	emitCallIdx(&body, e.fnIndices["__print_str"])

	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 1)
	writeUleb128(&full, 1)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildF64ToStrFunc() []byte {
	var body bytes.Buffer
	var zeroF64 [8]byte

	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	emitI32Const(&body, 7)
	body.WriteByte(0x6a)
	emitI32Const(&body, -8)
	body.WriteByte(0x71)
	emitLocalSet(&body, 1)

	emitLocalGet(&body, 1)
	emitI32Const(&body, 64)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	emitLocalGet(&body, 1)
	emitI32Const(&body, 8)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)

	// Check NaN
	emitLocalGet(&body, 0)
	emitLocalGet(&body, 0)
	body.WriteByte(0x62)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'N')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'a')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'N')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 2)
	emitLocalGet(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Store(&body, 0, 2)
	emitLocalGet(&body, 1)
	emitI32Const(&body, 3)
	emitI32Store(&body, 4, 2)
	emitLocalGet(&body, 1)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// Check +Inf
	emitLocalGet(&body, 0)
	body.WriteByte(0xbd)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 0x7FF0000000000000)
	body.WriteByte(0x51)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'I')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'n')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'f')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 2)
	emitLocalGet(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Store(&body, 0, 2)
	emitLocalGet(&body, 1)
	emitI32Const(&body, 3)
	emitI32Store(&body, 4, 2)
	emitLocalGet(&body, 1)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// Check -Inf
	emitLocalGet(&body, 0)
	body.WriteByte(0xbd)
	body.WriteByte(0x42)
	writeSleb128_64(&body, -0x0010000000000000)
	body.WriteByte(0x51)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 2)
	emitI32Const(&body, '-')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'I')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'n')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 2)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 'f')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 3)
	emitLocalGet(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Store(&body, 0, 2)
	emitLocalGet(&body, 1)
	emitI32Const(&body, 4)
	emitI32Store(&body, 4, 2)
	emitLocalGet(&body, 1)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// Sign bit: (reinterpret_f64(val) >> 63) & 1
	emitLocalGet(&body, 0)
	body.WriteByte(0xbd)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 63)
	body.WriteByte(0x88)
	body.WriteByte(0xa7)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 2)
	emitI32Const(&body, '-')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)
	body.WriteByte(0x0b)

	emitLocalGet(&body, 0)
	body.WriteByte(0x99) // f64.abs
	emitLocalSet(&body, 0)

	emitLocalGet(&body, 0)
	body.WriteByte(0xb0) // i64.trunc_f64_s
	emitLocalSet(&body, 6)

	// Write integer digits
	emitLocalGet(&body, 6)
	body.WriteByte(0x50)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 2)
	emitI32Const(&body, '0')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)
	body.WriteByte(0x05)

	emitLocalGet(&body, 6)
	emitLocalSet(&body, 7)
	emitI32Const(&body, 0)
	emitLocalSet(&body, 3)

	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 3)
	emitI32Const(&body, 1)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 3)
	emitLocalGet(&body, 7)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 10)
	body.WriteByte(0x80)
	body.WriteByte(0x22)
	writeUleb128(&body, 7)
	body.WriteByte(0x50)
	emitI32Eqz(&body)
	body.WriteByte(0x0d)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)

	emitLocalGet(&body, 2)
	emitLocalGet(&body, 3)
	body.WriteByte(0x6a)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 5)

	emitLocalGet(&body, 6)
	emitLocalSet(&body, 7)

	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 5)
	emitLocalGet(&body, 7)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 10)
	body.WriteByte(0x82)
	body.WriteByte(0xa7)
	emitI32Const(&body, '0')
	body.WriteByte(0x6a)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 5)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 5)
	emitLocalGet(&body, 7)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 10)
	body.WriteByte(0x80)
	body.WriteByte(0x22)
	writeUleb128(&body, 7)
	body.WriteByte(0x50)
	emitI32Eqz(&body)
	body.WriteByte(0x0d)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)

	emitLocalGet(&body, 2)
	emitLocalGet(&body, 3)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)
	body.WriteByte(0x0b)

	// Write '.'
	emitLocalGet(&body, 2)
	emitI32Const(&body, '.')
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)

	emitLocalGet(&body, 2)
	emitLocalSet(&body, 4)

	// frac = val - f64.convert_i64_s(int_part)
	emitLocalGet(&body, 0)
	emitLocalGet(&body, 6)
	body.WriteByte(0xb9)
	body.WriteByte(0xa1)
	emitLocalSet(&body, 8)

	emitLocalGet(&body, 8)
	body.WriteByte(0x44)
	body.Write(zeroF64[:])
	body.WriteByte(0x63)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x44)
	body.Write(zeroF64[:])
	emitLocalSet(&body, 8)
	body.WriteByte(0x0b)

	// 6 fractional digits
	emitI32Const(&body, 0)
	emitLocalSet(&body, 3)

	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	emitLocalGet(&body, 3)
	emitI32Const(&body, 6)
	body.WriteByte(0x4a)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	emitLocalGet(&body, 8)
	body.WriteByte(0x44)
	var tenF64 [8]byte
	binary.LittleEndian.PutUint64(tenF64[:], math.Float64bits(10.0))
	body.Write(tenF64[:])
	body.WriteByte(0xa2)
	emitLocalSet(&body, 8)

	emitLocalGet(&body, 8)
	body.WriteByte(0xaa)
	emitLocalSet(&body, 5)

	emitLocalGet(&body, 2)
	emitLocalGet(&body, 5)
	emitI32Const(&body, '0')
	body.WriteByte(0x6a)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)

	emitLocalGet(&body, 8)
	emitLocalGet(&body, 5)
	body.WriteByte(0xb7)
	body.WriteByte(0xa1)
	emitLocalSet(&body, 8)

	emitLocalGet(&body, 3)
	emitI32Const(&body, 1)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 3)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	// Strip trailing zeros
	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	emitLocalGet(&body, 2)
	emitLocalGet(&body, 4)
	emitI32Const(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x4c)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	emitI32Const(&body, '0')
	body.WriteByte(0x47)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 2)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	emitLocalGet(&body, 1)
	emitLocalGet(&body, 1)
	emitI32Const(&body, 8)
	body.WriteByte(0x6a)
	emitI32Store(&body, 0, 2)

	emitLocalGet(&body, 1)
	emitLocalGet(&body, 2)
	emitLocalGet(&body, 1)
	emitI32Const(&body, 8)
	body.WriteByte(0x6a)
	body.WriteByte(0x6b)
	emitI32Store(&body, 4, 2)

	emitLocalGet(&body, 1)

	var full bytes.Buffer
	writeUleb128(&full, 3)
	writeUleb128(&full, 5)
	full.WriteByte(valTypeI32)
	writeUleb128(&full, 2)
	full.WriteByte(valTypeI64)
	writeUleb128(&full, 1)
	full.WriteByte(valTypeF64)
	full.Write(body.Bytes())
	full.WriteByte(0x0b)

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildInt64ToStrFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	emitI32Const(&body, 7)
	body.WriteByte(0x6a)
	emitI32Const(&body, -8)
	body.WriteByte(0x71)
	emitLocalSet(&body, 1)

	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	emitLocalSet(&body, 2)

	emitI32Const(&body, 0)
	emitLocalSet(&body, 3)

	emitLocalGet(&body, 0)
	emitLocalSet(&body, 4)

	emitLocalGet(&body, 4)
	body.WriteByte(0x50)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 2)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 48)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x05)

	emitLocalGet(&body, 4)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 0)
	body.WriteByte(0x53)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitI32Const(&body, 1)
	emitLocalSet(&body, 3)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 0)
	emitLocalGet(&body, 4)
	body.WriteByte(0x7d)
	emitLocalSet(&body, 4)
	body.WriteByte(0x0b)

	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x22)
	writeUleb128(&body, 2)
	emitLocalGet(&body, 4)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 10)
	body.WriteByte(0x82)
	body.WriteByte(0xa7)
	emitI32Const(&body, 48)
	body.WriteByte(0x6a)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	emitLocalGet(&body, 4)
	body.WriteByte(0x42)
	writeSleb128_64(&body, 10)
	body.WriteByte(0x80)
	body.WriteByte(0x22)
	writeUleb128(&body, 4)
	body.WriteByte(0x50)
	emitI32Eqz(&body)
	body.WriteByte(0x0d)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)

	emitLocalGet(&body, 3)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x22)
	writeUleb128(&body, 2)
	emitI32Const(&body, 45)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x0b)

	body.WriteByte(0x0b)

	emitLocalGet(&body, 1)
	emitLocalGet(&body, 2)
	emitI32Store(&body, 0, 2)
	emitLocalGet(&body, 1)
	emitLocalGet(&body, 1)
	emitI32Const(&body, 40)
	body.WriteByte(0x6a)
	emitLocalGet(&body, 2)
	body.WriteByte(0x6b)
	emitI32Store(&body, 4, 2)
	emitLocalGet(&body, 1)

	var full bytes.Buffer
	writeUleb128(&full, 4)
	full.WriteByte(1)
	full.WriteByte(valTypeI32)
	full.WriteByte(1)
	full.WriteByte(valTypeI32)
	full.WriteByte(1)
	full.WriteByte(valTypeI32)
	full.WriteByte(1)
	full.WriteByte(valTypeI64)
	full.Write(body.Bytes())
	full.WriteByte(0x0b)

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildPrintIntFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x41)
	writeSleb128(&body, 47)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x45)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fnIndices["__print_str"])
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x48)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x0b)

	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x45)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x6f)
	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x6d)
	body.WriteByte(0x21)
	writeUleb128(&body, 0)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 45)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6b)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6a)

	body.WriteByte(0x41)
	writeSleb128(&body, 47)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x6b)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x10)
	writeUleb128(&body, e.fnIndices["__print_str"])

	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 1)
	writeUleb128(&full, 3)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildStrConcatFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 5)

	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 6)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x41)
	writeSleb128(&body, 8)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 7)

	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x4e)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x6a)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x6a)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 7)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 7)

	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x4e)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x6a)
	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x6a)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x6a)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 7)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 7)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x36)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 1)
	writeUleb128(&full, 6)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildStrEqFunc() []byte {
	var body bytes.Buffer

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)
	body.WriteByte(0x21)
	writeUleb128(&body, 2)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 4)
	body.WriteByte(0x21)
	writeUleb128(&body, 3)

	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x20)
	writeUleb128(&body, 3)
	body.WriteByte(0x47)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 0)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 5)

	body.WriteByte(0x20)
	writeUleb128(&body, 1)
	body.WriteByte(0x28)
	writeUleb128(&body, 2)
	writeUleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 6)

	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	body.WriteByte(0x02)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x03)
	body.WriteByte(valTypeVoid)

	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x20)
	writeUleb128(&body, 2)
	body.WriteByte(0x4e)
	body.WriteByte(0x0d)
	writeUleb128(&body, 1)

	body.WriteByte(0x20)
	writeUleb128(&body, 5)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x6a)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x20)
	writeUleb128(&body, 6)
	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x6a)
	body.WriteByte(0x2d)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x47)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	body.WriteByte(0x41)
	writeSleb128(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	body.WriteByte(0x20)
	writeUleb128(&body, 4)
	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x21)
	writeUleb128(&body, 4)

	body.WriteByte(0x0c)
	writeUleb128(&body, 0)

	body.WriteByte(0x0b)
	body.WriteByte(0x0b)

	body.WriteByte(0x41)
	writeSleb128(&body, 1)
	body.WriteByte(0x0b)

	var full bytes.Buffer
	writeUleb128(&full, 1)
	writeUleb128(&full, 5)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}

func (e *wasmEmitter) buildStartFunc(mainIdx uint32) []byte {
	var body bytes.Buffer

	body.WriteByte(0x41)
	writeSleb128(&body, 48)
	body.WriteByte(0x41)
	writeSleb128(&body, 10)
	body.WriteByte(0x3a)
	writeUleb128(&body, 0)
	writeUleb128(&body, 0)

	body.WriteByte(0x10)
	writeUleb128(&body, mainIdx)

	var mainRet UbeType
	if rt, ok := e.fnReturns["main"]; ok {
		mainRet = rt
	} else {
		for name, rt := range e.fnReturns {
			if strings.HasSuffix(name, ".main") {
				mainRet = rt
				break
			}
		}
	}

	if res, isRes := mainRet.(ResultType); isRes {
		if _, errIsStr := res.Err.(StringType); errIsStr {
			body.WriteByte(0x21)
			writeUleb128(&body, 0)

			body.WriteByte(0x20)
			writeUleb128(&body, 0)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)
			body.WriteByte(0x45)
			body.WriteByte(0x04)
			body.WriteByte(valTypeVoid)

			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec)
			body.WriteByte(0x20)
			writeUleb128(&body, 0)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 4)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)
			body.WriteByte(0x36)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)

			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec+4)
			body.WriteByte(0x20)
			writeUleb128(&body, 0)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 4)
			body.WriteByte(0x28)
			writeUleb128(&body, 2)
			writeUleb128(&body, 4)
			body.WriteByte(0x36)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)

			body.WriteByte(0x41)
			writeSleb128(&body, 2)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec)
			body.WriteByte(0x41)
			writeSleb128(&body, 1)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchNwrite)
			body.WriteByte(0x10)
			writeUleb128(&body, e.fdWriteIdx)
			body.WriteByte(0x1a)

			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec+48)
			body.WriteByte(0x41)
			writeSleb128(&body, 10)
			body.WriteByte(0x3a)
			writeUleb128(&body, 0)
			writeUleb128(&body, 0)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec+48)
			body.WriteByte(0x36)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec+4)
			body.WriteByte(0x41)
			writeSleb128(&body, 1)
			body.WriteByte(0x36)
			writeUleb128(&body, 2)
			writeUleb128(&body, 0)
			body.WriteByte(0x41)
			writeSleb128(&body, 2)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchCiovec)
			body.WriteByte(0x41)
			writeSleb128(&body, 1)
			body.WriteByte(0x41)
			writeSleb128(&body, scratchNwrite)
			body.WriteByte(0x10)
			writeUleb128(&body, e.fdWriteIdx)
			body.WriteByte(0x1a)

			body.WriteByte(0x41)
			writeSleb128(&body, 1)
			body.WriteByte(0x10)
			writeUleb128(&body, e.wasiImports["proc_exit"])

			body.WriteByte(0x0b)
			body.WriteByte(0x0b)
			return wrapStartWithLocals(body, 1)
		}
	}

	if mainRet != nil && mainRet.String() != "void" {
		body.WriteByte(0x1a)
	}
	body.WriteByte(0x0b)
	return wrapStartWithLocals(body, 0)
}

func wrapStartWithLocals(body bytes.Buffer, nLocals uint32) []byte {
	var codeBuf bytes.Buffer
	if nLocals > 0 {
		writeUleb128(&codeBuf, 1)
		writeUleb128(&codeBuf, nLocals)
		codeBuf.WriteByte(valTypeI32)
	} else {
		writeUleb128(&codeBuf, 0)
	}
	codeBuf.Write(body.Bytes())

	var fullBuf bytes.Buffer
	writeUleb128(&fullBuf, uint32(codeBuf.Len()))
	fullBuf.Write(codeBuf.Bytes())
	return fullBuf.Bytes()
}

// buildCabiReallocFunc implements the WebAssembly Canonical ABI cabi_realloc function:
// (func (export "cabi_realloc") (param $orig_ptr i32) (param $orig_size i32) (param $align i32) (param $new_size i32) (result i32))
// Backed directly by Ube's regional bump allocator (Global 0).
func (e *wasmEmitter) buildCabiReallocFunc() []byte {
	var body bytes.Buffer

	// if new_size == 0 { return orig_ptr }
	emitLocalGet(&body, 3)
	emitI32Eqz(&body)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// if align < 1 { align = 1 }
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x48)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitI32Const(&body, 1)
	emitLocalSet(&body, 2)
	body.WriteByte(0x0b)

	// align_mask = align - 1 (local 5)
	emitLocalGet(&body, 2)
	emitI32Const(&body, 1)
	body.WriteByte(0x6b)
	emitLocalSet(&body, 5)

	// Check if orig_ptr == 0 (fresh allocation)
	emitLocalGet(&body, 0)
	emitI32Eqz(&body)
	body.WriteByte(0x04)
	body.WriteByte(valTypeI32)

	// aligned_ptr = (global0 + align_mask) & ~align_mask (local 4)
	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 5)
	body.WriteByte(0x6a)
	emitLocalGet(&body, 5)
	emitI32Const(&body, -1)
	body.WriteByte(0x73)
	body.WriteByte(0x71)
	emitLocalSet(&body, 4)

	// global0 = aligned_ptr + new_size
	emitLocalGet(&body, 4)
	emitLocalGet(&body, 3)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	emitLocalGet(&body, 4)

	body.WriteByte(0x05) // else (reallocation)

	// if new_size <= orig_size { return orig_ptr }
	emitLocalGet(&body, 3)
	emitLocalGet(&body, 1)
	body.WriteByte(0x4d)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// if orig_ptr + orig_size == global0 { global0 = orig_ptr + new_size; return orig_ptr }
	emitLocalGet(&body, 0)
	emitLocalGet(&body, 1)
	body.WriteByte(0x6a)
	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	body.WriteByte(0x46)
	body.WriteByte(0x04)
	body.WriteByte(valTypeVoid)
	emitLocalGet(&body, 0)
	emitLocalGet(&body, 3)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 0)
	body.WriteByte(0x0f)
	body.WriteByte(0x0b)

	// Otherwise allocate new buffer and copy
	body.WriteByte(0x23)
	writeUleb128(&body, 0)
	emitLocalGet(&body, 5)
	body.WriteByte(0x6a)
	emitLocalGet(&body, 5)
	emitI32Const(&body, -1)
	body.WriteByte(0x73)
	body.WriteByte(0x71)
	emitLocalSet(&body, 4)

	emitLocalGet(&body, 4)
	emitLocalGet(&body, 3)
	body.WriteByte(0x6a)
	body.WriteByte(0x24)
	writeUleb128(&body, 0)

	// memory.copy: dest, src, len
	emitLocalGet(&body, 4)
	emitLocalGet(&body, 0)
	emitLocalGet(&body, 1)
	body.WriteByte(0xfc)
	body.WriteByte(0x0a)
	body.WriteByte(0x00)
	body.WriteByte(0x00)

	emitLocalGet(&body, 4)

	body.WriteByte(0x0b) // end if/else

	body.WriteByte(0x0b) // end function

	var full bytes.Buffer
	writeUleb128(&full, 1) // 1 local group
	writeUleb128(&full, 2) // 2 locals (local 4, local 5)
	full.WriteByte(valTypeI32)
	full.Write(body.Bytes())

	var res bytes.Buffer
	writeUleb128(&res, uint32(full.Len()))
	res.Write(full.Bytes())
	return res.Bytes()
}
