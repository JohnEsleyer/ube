package compiler

import "fmt"

type UbeType interface {
	String() string
	IsFlat(env *TypeEnvironment) bool
}

type PrimitiveType struct{ Name string }

func (t PrimitiveType) String() string                   { return t.Name }
func (t PrimitiveType) IsFlat(env *TypeEnvironment) bool { return true }

type StringType struct{}

func (t StringType) String() string                   { return "string" }
func (t StringType) IsFlat(env *TypeEnvironment) bool { return false }

type PointerType struct{ Base UbeType }

func (t PointerType) String() string                   { return "*" + t.Base.String() }
func (t PointerType) IsFlat(env *TypeEnvironment) bool { return false }

type SliceType struct{ Elem UbeType }

func (t SliceType) String() string                   { return "[]" + t.Elem.String() }
func (t SliceType) IsFlat(env *TypeEnvironment) bool { return false }

type ArrayType struct {
	Elem UbeType
	Size int
}

func (t ArrayType) String() string                   { return fmt.Sprintf("[%d]%s", t.Size, t.Elem.String()) }
func (t ArrayType) IsFlat(env *TypeEnvironment) bool { return t.Elem.IsFlat(env) }

type OptionalType struct{ Inner UbeType }

func (t OptionalType) String() string                   { return t.Inner.String() + "?" }
func (t OptionalType) IsFlat(env *TypeEnvironment) bool { return t.Inner.IsFlat(env) }

type ResultType struct{ Ok, Err UbeType }

func (t ResultType) String() string                   { return fmt.Sprintf("%s ! %s", t.Ok, t.Err) }
func (t ResultType) IsFlat(env *TypeEnvironment) bool { return t.Ok.IsFlat(env) && t.Err.IsFlat(env) }

type ChanType struct{ Elem UbeType }

func (t ChanType) String() string                   { return fmt.Sprintf("Chan[%s]", t.Elem) }
func (t ChanType) IsFlat(env *TypeEnvironment) bool { return t.Elem.IsFlat(env) }

type PoolType struct{ Elem UbeType }

func (t PoolType) String() string                   { return fmt.Sprintf("Pool[%s]", t.Elem) }
func (t PoolType) IsFlat(env *TypeEnvironment) bool { return t.Elem.IsFlat(env) }

type CustomType struct{ Name string }

func (t CustomType) String() string { return t.Name }
func (t CustomType) IsFlat(env *TypeEnvironment) bool {
	if fields, ok := env.Structs[t.Name]; ok {
		for _, f := range fields {
			if !f.Type.IsFlat(env) {
				return false
			}
		}
		return true
	}
	if variants, ok := env.Enums[t.Name]; ok {
		for _, v := range variants {
			for _, vt := range v.Types {
				if !vt.IsFlat(env) {
					return false
				}
			}
		}
		return true
	}
	return false
}
