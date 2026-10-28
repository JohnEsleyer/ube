package compiler

import (
	"fmt"
	"strings"
)

type UbeType interface {
	String() string
	IsFlat(env *TypeEnvironment) bool
	Region() int
	WithRegion(r int) UbeType
}

type PrimitiveType struct {
	Name string
	Reg  int
}

func (t PrimitiveType) String() string                   { return t.Name }
func (t PrimitiveType) IsFlat(env *TypeEnvironment) bool { return true }
func (t PrimitiveType) Region() int                      { return t.Reg }
func (t PrimitiveType) WithRegion(r int) UbeType         { return PrimitiveType{Name: t.Name, Reg: r} }

type StringType struct {
	Reg int
}

func (t StringType) String() string                   { return "string" }
func (t StringType) IsFlat(env *TypeEnvironment) bool { return false }
func (t StringType) Region() int                      { return t.Reg }
func (t StringType) WithRegion(r int) UbeType         { return StringType{Reg: r} }

type StringBuilderType struct {
	Reg int
}

func (t StringBuilderType) String() string                   { return "StringBuilder" }
func (t StringBuilderType) IsFlat(env *TypeEnvironment) bool { return false }
func (t StringBuilderType) Region() int                      { return t.Reg }
func (t StringBuilderType) WithRegion(r int) UbeType         { return StringBuilderType{Reg: r} }

type PointerType struct {
	Base UbeType
	Reg  int
}

func (t PointerType) String() string                   { return "*" + t.Base.String() }
func (t PointerType) IsFlat(env *TypeEnvironment) bool { return false }
func (t PointerType) Region() int                      { return t.Reg }
func (t PointerType) WithRegion(r int) UbeType         { return PointerType{Base: t.Base, Reg: r} }

type SliceType struct {
	Elem UbeType
	Reg  int
}

func (t SliceType) String() string                   { return "[]" + t.Elem.String() }
func (t SliceType) IsFlat(env *TypeEnvironment) bool { return false }
func (t SliceType) Region() int                      { return t.Reg }
func (t SliceType) WithRegion(r int) UbeType         { return SliceType{Elem: t.Elem, Reg: r} }

type ArrayType struct {
	Elem UbeType
	Size int
	Reg  int
}

func (t ArrayType) String() string { return fmt.Sprintf("[%d]%s", t.Size, t.Elem.String()) }
func (t ArrayType) IsFlat(env *TypeEnvironment) bool {
	return isFlatType(t.Elem, env, map[string]bool{})
}
func (t ArrayType) Region() int              { return t.Reg }
func (t ArrayType) WithRegion(r int) UbeType { return ArrayType{Elem: t.Elem, Size: t.Size, Reg: r} }

type OptionalType struct {
	Inner UbeType
	Reg   int
}

func (t OptionalType) String() string { return t.Inner.String() + "?" }
func (t OptionalType) IsFlat(env *TypeEnvironment) bool {
	return isFlatType(t.Inner, env, map[string]bool{})
}
func (t OptionalType) Region() int              { return t.Reg }
func (t OptionalType) WithRegion(r int) UbeType { return OptionalType{Inner: t.Inner, Reg: r} }

type ResultType struct {
	Ok, Err UbeType
	Reg     int
}

func (t ResultType) String() string { return fmt.Sprintf("%s ! %s", t.Ok, t.Err) }
func (t ResultType) IsFlat(env *TypeEnvironment) bool {
	seen := map[string]bool{}
	return isFlatType(t.Ok, env, seen) && isFlatType(t.Err, env, seen)
}
func (t ResultType) Region() int              { return t.Reg }
func (t ResultType) WithRegion(r int) UbeType { return ResultType{Ok: t.Ok, Err: t.Err, Reg: r} }

type ChanType struct {
	Elem UbeType
	Reg  int
}

func (t ChanType) String() string { return fmt.Sprintf("Chan[%s]", t.Elem) }
func (t ChanType) IsFlat(env *TypeEnvironment) bool {
	return isFlatType(t.Elem, env, map[string]bool{})
}
func (t ChanType) Region() int              { return t.Reg }
func (t ChanType) WithRegion(r int) UbeType { return ChanType{Elem: t.Elem, Reg: r} }

type PoolType struct {
	Elem UbeType
	Reg  int
}

func (t PoolType) String() string                   { return fmt.Sprintf("Pool[%s]", t.Elem) }
func (t PoolType) IsFlat(env *TypeEnvironment) bool { return false }
func (t PoolType) Region() int                      { return t.Reg }
func (t PoolType) WithRegion(r int) UbeType         { return PoolType{Elem: t.Elem, Reg: r} }

type RangeType struct {
	Elem UbeType
	Reg  int
}

func (t RangeType) String() string                   { return "Range[" + t.Elem.String() + "]" }
func (t RangeType) IsFlat(env *TypeEnvironment) bool { return true }
func (t RangeType) Region() int                      { return t.Reg }
func (t RangeType) WithRegion(r int) UbeType         { return RangeType{Elem: t.Elem, Reg: r} }

type CustomType struct {
	Name string
	Reg  int
}

func (t CustomType) String() string { return t.Name }
func (t CustomType) IsFlat(env *TypeEnvironment) bool {
	return t.isFlatSeen(env, map[string]bool{})
}
func (t CustomType) Region() int              { return t.Reg }
func (t CustomType) WithRegion(r int) UbeType { return CustomType{Name: t.Name, Reg: r} }

func (t CustomType) isFlatSeen(env *TypeEnvironment, seen map[string]bool) bool {
	if seen[t.Name] {
		return false
	}
	seen[t.Name] = true
	if env != nil {
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
	}
	return false
}

type TraitType struct {
	Name string
	Reg  int
}

func (t TraitType) String() string                   { return t.Name }
func (t TraitType) IsFlat(env *TypeEnvironment) bool { return false }
func (t TraitType) Region() int                      { return t.Reg }
func (t TraitType) WithRegion(r int) UbeType         { return TraitType{Name: t.Name, Reg: r} }

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

func (t GenericInstanceType) IsFlat(env *TypeEnvironment) bool {
	return false
}
func (t GenericInstanceType) Region() int { return t.Reg }
func (t GenericInstanceType) WithRegion(r int) UbeType {
	return GenericInstanceType{Base: t.Base, TypeArgs: t.TypeArgs, Reg: r}
}

func isFlatType(t UbeType, env *TypeEnvironment, seen map[string]bool) bool {
	if t == nil {
		return false
	}
	switch v := t.(type) {
	case PrimitiveType:
		return true
	case ArrayType:
		return isFlatType(v.Elem, env, seen)
	case OptionalType:
		return isFlatType(v.Inner, env, seen)
	case ResultType:
		return isFlatType(v.Ok, env, seen) && isFlatType(v.Err, env, seen)
	case CustomType:
		return v.isFlatSeen(env, seen)
	default:
		return false
	}
}
