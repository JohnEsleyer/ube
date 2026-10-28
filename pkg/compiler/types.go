package compiler

import (
	"fmt"
	"strings"
)

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

type StringBuilderType struct{}

func (t StringBuilderType) String() string                   { return "StringBuilder" }
func (t StringBuilderType) IsFlat(env *TypeEnvironment) bool { return false }

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

func (t ArrayType) String() string { return fmt.Sprintf("[%d]%s", t.Size, t.Elem.String()) }
func (t ArrayType) IsFlat(env *TypeEnvironment) bool {
	return isFlatType(t.Elem, env, map[string]bool{})
}

type OptionalType struct{ Inner UbeType }

func (t OptionalType) String() string { return t.Inner.String() + "?" }
func (t OptionalType) IsFlat(env *TypeEnvironment) bool {
	return isFlatType(t.Inner, env, map[string]bool{})
}

type ResultType struct{ Ok, Err UbeType }

func (t ResultType) String() string { return fmt.Sprintf("%s ! %s", t.Ok, t.Err) }
func (t ResultType) IsFlat(env *TypeEnvironment) bool {
	seen := map[string]bool{}
	return isFlatType(t.Ok, env, seen) && isFlatType(t.Err, env, seen)
}

type ChanType struct{ Elem UbeType }

func (t ChanType) String() string { return fmt.Sprintf("Chan[%s]", t.Elem) }
func (t ChanType) IsFlat(env *TypeEnvironment) bool {
	return isFlatType(t.Elem, env, map[string]bool{})
}

type PoolType struct{ Elem UbeType }

func (t PoolType) String() string                   { return fmt.Sprintf("Pool[%s]", t.Elem) }
func (t PoolType) IsFlat(env *TypeEnvironment) bool { return false }

type RangeType struct{ Elem UbeType }

func (t RangeType) String() string                   { return "Range[" + t.Elem.String() + "]" }
func (t RangeType) IsFlat(env *TypeEnvironment) bool { return true }

type CustomType struct{ Name string }

func (t CustomType) String() string { return t.Name }
func (t CustomType) IsFlat(env *TypeEnvironment) bool {
	return t.isFlatSeen(env, map[string]bool{})
}

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

type TraitType struct{ Name string }

func (t TraitType) String() string                   { return t.Name }
func (t TraitType) IsFlat(env *TypeEnvironment) bool { return false }

type GenericInstanceType struct {
	Base     string
	TypeArgs []UbeType
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
