package compiler

func (tc *Typechecker) monomorphize(prog *Program) error {
	return Monomorphize(prog)
}
