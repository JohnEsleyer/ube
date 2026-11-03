package compiler

import (
	"fmt"
	"strings"

	"github.com/JohnEsleyer/ube/pkg/wasm"
)

// UbeType is the universal interface for all Ube types.
type UbeType interface {
	String() string
	IsFlat(env *TypeEnvironment) bool
	Region() int
	WithRegion(r int) UbeType
	WasmType() wasm.ValType
	ByteSize() int
	Align() int
	IsCopy() bool
}

// -------------------------------------------------------------
// Primitive Types
// -------------------------------------------------------------

type PrimitiveType struct {
	Name string
	Reg  int
}

func (t PrimitiveType) String() string                   { return t.Name }
func (t PrimitiveType) IsFlat(env *TypeEnvironment) bool { return true }
func (t PrimitiveType) Region() int                      { return t.Reg }
func (t PrimitiveType) WithRegion(r int) UbeType         { return PrimitiveType{Name: t.Name, Reg: r} }

func (t PrimitiveType) WasmType() wasm.ValType {
	switch t.Name {
	case "i8", "i16", "i32", "u8", "u16", "u32", "bool":
		return wasm.ValI32
	case "i64", "u64":
		return wasm.ValI64
	case "f32":
		return wasm.ValF32
	case "f64":
		return wasm.ValF64
	default:
		return wasm.ValVoid
	}
}

func (t PrimitiveType) ByteSize() int {
	switch t.Name {
	case "i8", "u8", "bool":
		return 1
	case "i16", "u16":
		return 2
	case "i32", "u32", "f32":
		return 4
	case "i64", "u64", "f64":
		return 8
	default:
		return 0
	}
}

func (t PrimitiveType) Align() int {
	return t.ByteSize()
}

func (t PrimitiveType) IsCopy() bool {
	return true
}

// -------------------------------------------------------------
// String Type
// -------------------------------------------------------------

type StringType struct {
	Reg int
}

func (t StringType) String() string                   { return "string" }
func (t StringType) IsFlat(env *TypeEnvironment) bool { return false }
func (t StringType) Region() int                      { return t.Reg }
func (t StringType) WithRegion(r int) UbeType         { return StringType{Reg: r} }
func (t StringType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t StringType) ByteSize() int                    { return 8 } // ptr (4) + len (4)
func (t StringType) Align() int                       { return 4 }
func (t StringType) IsCopy() bool                     { return false }

// -------------------------------------------------------------
// StringBuilder Type
// -------------------------------------------------------------

type StringBuilderType struct {
	Reg int
}

func (t StringBuilderType) String() string                   { return "StringBuilder" }
func (t StringBuilderType) IsFlat(env *TypeEnvironment) bool { return false }
func (t StringBuilderType) Region() int                      { return t.Reg }
func (t StringBuilderType) WithRegion(r int) UbeType         { return StringBuilderType{Reg: r} }
func (t StringBuilderType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t StringBuilderType) ByteSize() int                    { return 12 }
func (t StringBuilderType) Align() int                       { return 4 }
func (t StringBuilderType) IsCopy() bool                     { return false }

// -------------------------------------------------------------
// Slice Type: Dynamic []T
// -------------------------------------------------------------

type SliceType struct {
	Elem UbeType
	Reg  int
}

func (t SliceType) String() string                   { return "[]" + t.Elem.String() }
func (t SliceType) IsFlat(env *TypeEnvironment) bool { return false }
func (t SliceType) Region() int                      { return t.Reg }
func (t SliceType) WithRegion(r int) UbeType         { return SliceType{Elem: t.Elem, Reg: r} }
func (t SliceType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t SliceType) ByteSize() int                    { return 12 } // ptr (4) + len (4) + cap (4)
func (t SliceType) Align() int                       { return 4 }
func (t SliceType) IsCopy() bool                     { return false }

// -------------------------------------------------------------
// Array Type: Fixed-size [N]T
// -------------------------------------------------------------

type ArrayType struct {
	Elem UbeType
	Size int
	Reg  int
}

func (t ArrayType) String() string {
	return fmt.Sprintf("[%d]%s", t.Size, t.Elem.String())
}

func (t ArrayType) IsFlat(env *TypeEnvironment) bool {
	return t.Elem.IsFlat(env)
}

func (t ArrayType) Region() int              { return t.Reg }
func (t ArrayType) WithRegion(r int) UbeType { return ArrayType{Elem: t.Elem, Size: t.Size, Reg: r} }
func (t ArrayType) WasmType() wasm.ValType   { return wasm.ValI32 }
func (t ArrayType) ByteSize() int            { return t.Elem.ByteSize() * t.Size }
func (t ArrayType) Align() int               { return t.Elem.Align() }
func (t ArrayType) IsCopy() bool             { return t.Elem.IsCopy() }

// -------------------------------------------------------------
// Optional Type: T?
// -------------------------------------------------------------

type OptionalType struct {
	Inner UbeType
	Reg   int
}

func (t OptionalType) String() string                   { return t.Inner.String() + "?" }
func (t OptionalType) IsFlat(env *TypeEnvironment) bool { return t.Inner.IsFlat(env) }
func (t OptionalType) Region() int                      { return t.Reg }
func (t OptionalType) WithRegion(r int) UbeType         { return OptionalType{Inner: t.Inner, Reg: r} }
func (t OptionalType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t OptionalType) ByteSize() int                    { return 4 + t.Inner.ByteSize() }
func (t OptionalType) Align() int                       { return 4 }
func (t OptionalType) IsCopy() bool                     { return t.Inner.IsCopy() }

// -------------------------------------------------------------
// Result Type: T ! E
// -------------------------------------------------------------

type ResultType struct {
	Ok, Err UbeType
	Reg     int
}

func (t ResultType) String() string                   { return fmt.Sprintf("%s ! %s", t.Ok.String(), t.Err.String()) }
func (t ResultType) IsFlat(env *TypeEnvironment) bool { return t.Ok.IsFlat(env) && t.Err.IsFlat(env) }
func (t ResultType) Region() int                      { return t.Reg }
func (t ResultType) WithRegion(r int) UbeType         { return ResultType{Ok: t.Ok, Err: t.Err, Reg: r} }
func (t ResultType) WasmType() wasm.ValType           { return wasm.ValI32 }

func (t ResultType) ByteSize() int {
	payload := t.Ok.ByteSize()
	if t.Err.ByteSize() > payload {
		payload = t.Err.ByteSize()
	}
	return 4 + payload
}

func (t ResultType) Align() int   { return 4 }
func (t ResultType) IsCopy() bool { return false }

// -------------------------------------------------------------
// Tuple Type: (T1, T2, ...)
// -------------------------------------------------------------

type TupleType struct {
	Elems []UbeType
	Reg   int
}

func (t TupleType) String() string {
	var parts []string
	for _, e := range t.Elems {
		parts = append(parts, e.String())
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func (t TupleType) IsFlat(env *TypeEnvironment) bool {
	for _, e := range t.Elems {
		if !e.IsFlat(env) {
			return false
		}
	}
	return true
}

func (t TupleType) Region() int              { return t.Reg }
func (t TupleType) WithRegion(r int) UbeType { return TupleType{Elems: t.Elems, Reg: r} }
func (t TupleType) WasmType() wasm.ValType   { return wasm.ValI32 }

func (t TupleType) ByteSize() int {
	total := 0
	for _, e := range t.Elems {
		total += e.ByteSize()
	}
	return total
}

func (t TupleType) Align() int { return 4 }
func (t TupleType) IsCopy() bool {
	for _, e := range t.Elems {
		if !e.IsCopy() {
			return false
		}
	}
	return true
}

// -------------------------------------------------------------
// Chan Type: Chan[T]
// -------------------------------------------------------------

type ChanType struct {
	Elem UbeType
	Reg  int
}

func (t ChanType) String() string                   { return fmt.Sprintf("Chan[%s]", t.Elem.String()) }
func (t ChanType) IsFlat(env *TypeEnvironment) bool { return false }
func (t ChanType) Region() int                      { return t.Reg }
func (t ChanType) WithRegion(r int) UbeType         { return ChanType{Elem: t.Elem, Reg: r} }
func (t ChanType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t ChanType) ByteSize() int                    { return 4 }
func (t ChanType) Align() int                       { return 4 }
func (t ChanType) IsCopy() bool                     { return false }

// -------------------------------------------------------------
// Pool Type: Pool[T]
// -------------------------------------------------------------

type PoolType struct {
	Elem UbeType
	Reg  int
}

func (t PoolType) String() string                   { return fmt.Sprintf("Pool[%s]", t.Elem.String()) }
func (t PoolType) IsFlat(env *TypeEnvironment) bool { return false }
func (t PoolType) Region() int                      { return t.Reg }
func (t PoolType) WithRegion(r int) UbeType         { return PoolType{Elem: t.Elem, Reg: r} }
func (t PoolType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t PoolType) ByteSize() int                    { return 4 }
func (t PoolType) Align() int                       { return 4 }
func (t PoolType) IsCopy() bool                     { return false }

// -------------------------------------------------------------
// Range Type
// -------------------------------------------------------------

type RangeType struct {
	Elem UbeType
	Reg  int
}

func (t RangeType) String() string                   { return fmt.Sprintf("range[%s]", t.Elem.String()) }
func (t RangeType) IsFlat(env *TypeEnvironment) bool { return true }
func (t RangeType) Region() int                      { return t.Reg }
func (t RangeType) WithRegion(r int) UbeType         { return RangeType{Elem: t.Elem, Reg: r} }
func (t RangeType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t RangeType) ByteSize() int                    { return 8 } // start (4) + end (4)
func (t RangeType) Align() int                       { return 4 }
func (t RangeType) IsCopy() bool                     { return true }

// -------------------------------------------------------------
// Custom Type (Named Structs & Enums)
// -------------------------------------------------------------

type CustomType struct {
	Name string
	Reg  int
}

func (t CustomType) String() string                   { return t.Name }
func (t CustomType) Region() int                      { return t.Reg }
func (t CustomType) WithRegion(r int) UbeType         { return CustomType{Name: t.Name, Reg: r} }
func (t CustomType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t CustomType) ByteSize() int                    { return 4 }
func (t CustomType) Align() int                       { return 4 }
func (t CustomType) IsCopy() bool                     { return false }

func (t CustomType) IsFlat(env *TypeEnvironment) bool {
	return t.isFlatSeen(env, make(map[string]bool))
}

func (t CustomType) isFlatSeen(env *TypeEnvironment, seen map[string]bool) bool {
	if seen[t.Name] {
		return true
	}
	seen[t.Name] = true

	if fields, ok := env.Structs[t.Name]; ok {
		for _, f := range fields {
			if !isFlatType(f.Type, env, seen) {
				return false
			}
		}
		return true
	}

	if variants, ok := env.Enums[t.Name]; ok {
		for _, v := range variants {
			for _, vt := range v.Types {
				if !isFlatType(vt, env, seen) {
					return false
				}
			}
		}
		return true
	}

	return true
}

// -------------------------------------------------------------
// Trait Fat Pointer: {vtable_ptr: i32, data_ptr: i32}
// -------------------------------------------------------------

type TraitType struct {
	Name string
	Reg  int
}

func (t TraitType) String() string                   { return t.Name }
func (t TraitType) IsFlat(env *TypeEnvironment) bool { return false }
func (t TraitType) Region() int                      { return t.Reg }
func (t TraitType) WithRegion(r int) UbeType         { return TraitType{Name: t.Name, Reg: r} }
func (t TraitType) WasmType() wasm.ValType           { return wasm.ValI32 }
func (t TraitType) ByteSize() int                    { return 8 }
func (t TraitType) Align() int                       { return 4 }
func (t TraitType) IsCopy() bool                     { return false }

// -------------------------------------------------------------
// Generic Instance Type
// -------------------------------------------------------------

type GenericInstanceType struct {
	Base     string
	TypeArgs []UbeType
	Reg      int
}

func (t GenericInstanceType) String() string {
	var args []string
	for _, a := range t.TypeArgs {
		args = append(args, a.String())
	}
	return fmt.Sprintf("%s[%s]", t.Base, strings.Join(args, ", "))
}

func (t GenericInstanceType) IsFlat(env *TypeEnvironment) bool { return false }
func (t GenericInstanceType) Region() int                      { return t.Reg }
func (t GenericInstanceType) WithRegion(r int) UbeType {
	return GenericInstanceType{Base: t.Base, TypeArgs: t.TypeArgs, Reg: r}
}
func (t GenericInstanceType) WasmType() wasm.ValType { return wasm.ValI32 }
func (t GenericInstanceType) ByteSize() int          { return 4 }
func (t GenericInstanceType) Align() int             { return 4 }
func (t GenericInstanceType) IsCopy() bool           { return false }

func isFlatType(t UbeType, env *TypeEnvironment, seen map[string]bool) bool {
	if c, ok := t.(CustomType); ok {
		return c.isFlatSeen(env, seen)
	}
	return t.IsFlat(env)
}
