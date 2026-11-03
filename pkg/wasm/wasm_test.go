package wasm_test

import (
	"bytes"
	"testing"

	"github.com/JohnEsleyer/ube/pkg/wasm"
)

func TestLEB128_Unsigned(t *testing.T) {
	cases := []struct {
		val  uint32
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{127, []byte{0x7F}},
		{128, []byte{0x80, 0x01}},
		{624485, []byte{0xE5, 0x8E, 0x26}},
		{0xFFFFFFFF, []byte{0xFF, 0xFF, 0xFF, 0xFF, 0x0F}},
	}

	for _, c := range cases {
		got := wasm.AppendUleb128(nil, c.val)
		if !bytes.Equal(got, c.want) {
			t.Errorf("AppendUleb128(%d) = %x, want %x", c.val, got, c.want)
		}
	}
}

func TestLEB128_Signed(t *testing.T) {
	cases := []struct {
		val  int32
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{-1, []byte{0x7F}},
		{63, []byte{0x3F}},
		{-64, []byte{0x40}},
		{64, []byte{0xC0, 0x00}},
		{-65, []byte{0xBF, 0x7F}},
		{123456, []byte{0xC0, 0xC4, 0x07}},
		{-123456, []byte{0xC0, 0xBB, 0x78}},
	}

	for _, c := range cases {
		got := wasm.AppendSleb128(nil, c.val)
		if !bytes.Equal(got, c.want) {
			t.Errorf("AppendSleb128(%d) = %x, want %x", c.val, got, c.want)
		}
	}
}

func TestBuffer_Emissions(t *testing.T) {
	buf := wasm.NewBuffer(64)
	buf.EmitI32Const(42)
	buf.EmitI64Const(-100)
	buf.EmitF32Const(3.14)
	buf.EmitF64Const(2.71828)
	buf.EmitString("hello")
	buf.EmitMemArg(2, 4)

	if buf.Len() == 0 {
		t.Fatal("expected non-empty buffer")
	}

	// Verify i32.const opcode and SLEB128 42
	if buf.Bytes[0] != wasm.OpI32Const {
		t.Errorf("expected OpI32Const (0x41), got 0x%x", buf.Bytes[0])
	}
	if buf.Bytes[1] != 42 {
		t.Errorf("expected 42, got %d", buf.Bytes[1])
	}

	buf.Reset()
	if buf.Len() != 0 {
		t.Errorf("expected buffer len 0 after reset, got %d", buf.Len())
	}
}

func TestModule_EncodeHeader(t *testing.T) {
	mod := wasm.NewModule()
	typeIdx := mod.AddType(wasm.FuncType{
		Params:  []wasm.ValType{wasm.ValI32, wasm.ValI32},
		Results: []wasm.ValType{wasm.ValI32},
	})
	if typeIdx != 0 {
		t.Errorf("expected first type index 0, got %d", typeIdx)
	}

	// Type deduplication
	dupIdx := mod.AddType(wasm.FuncType{
		Params:  []wasm.ValType{wasm.ValI32, wasm.ValI32},
		Results: []wasm.ValType{wasm.ValI32},
	})
	if dupIdx != 0 {
		t.Errorf("expected deduplicated type index 0, got %d", dupIdx)
	}

	mod.Functions = append(mod.Functions, typeIdx)
	mod.Exports = append(mod.Exports, wasm.ExportEntry{
		Name:  "add",
		Kind:  wasm.KindFunc,
		Index: 0,
	})
	mod.Bodies = append(mod.Bodies, wasm.FunctionBody{
		Locals: nil,
		Code: []byte{
			wasm.OpLocalGet, 0,
			wasm.OpLocalGet, 1,
			wasm.OpI32Add,
			wasm.OpEnd,
		},
	})

	encoded, err := mod.Encode()
	if err != nil {
		t.Fatalf("failed to encode module: %v", err)
	}

	if len(encoded) < 8 {
		t.Fatalf("encoded module too short: %d bytes", len(encoded))
	}
	if !bytes.Equal(encoded[:4], wasm.Magic[:]) {
		t.Errorf("expected magic \\0asm, got %x", encoded[:4])
	}
	if !bytes.Equal(encoded[4:8], wasm.Version[:]) {
		t.Errorf("expected version 1, got %x", encoded[4:8])
	}
}
