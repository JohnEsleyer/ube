package compiler

import (
	"fmt"
	"strconv"
	"strings"
)

func (g *Generator) genBlock(body *BlockStmt) {
	g.pushDeferScope()
	for _, st := range body.Stmts {
		g.genStmt(st)
	}
	g.finishDeferScope()
}

func (g *Generator) genDecl(decl Decl) {
	switch d := decl.(type) {
	case *ExternFnDecl, *TraitDecl:
		return

	case *FnDecl:
		g.curFnRet = d.RetType
		g.curFnName = d.Name
		g.sliceVars = make(map[string]string)
		g.sliceParamVars = make(map[string]bool)
		var params []string
		fnName := d.Name
		if d.Receiver != nil {
			fnName = fmt.Sprintf("%s_%s", d.Receiver.Type, d.Name)
			recCT := g.toCType(d.Receiver.Type)
			if d.Receiver.IsMut {
				recCT += "*"
				g.sliceParamVars[d.Receiver.Name] = true
			}
			params = append(params, fmt.Sprintf("%s %s", recCT, d.Receiver.Name))
		}
		mutName := d.Name
		if d.Receiver != nil {
			mutName = fmt.Sprintf("%s_%s", d.Receiver.Type, d.Name)
		}
		for i, p := range d.Params {
			ct := g.toCType(p.Type)
			if g.sliceMutParams[mutName][i] {
				ct += "*"
				if sl, ok := p.Type.(SliceType); ok {
					m := mangleTypeName(sl.Elem.String())
					g.sliceVars[p.Name] = m
					g.sliceParamVars[p.Name] = true
				}
			}
			params = append(params, fmt.Sprintf("%s %s", ct, p.Name))
		}

		if d.Name == "main" {
			g.emit("int main(int argc, char** argv) {")
			g.indent++
			g.emit("ube_init_args(argc, argv);")
			g.emit("ube_init();")
			g.emit("UbeWatermark __watermark = ube_scope_save();")
			g.scopeMarks = []string{"__watermark"}
		} else {
			g.emit(fmt.Sprintf("%s %s(%s) {", g.toCType(d.RetType), fnName, strings.Join(params, ", ")))
			g.indent++
			g.scopeMarks = nil
		}
		g.deferScopes = nil
		g.loopDeferDepth = nil
		g.loopScopeDepth = nil
		g.loopLabels = nil
		g.loopSelectDepth = nil
		g.selectGroups = nil
		g.pushDeferScope()

		for _, st := range d.Body.Stmts {
			g.genStmt(st)
		}
		g.finishDeferScope()

		if d.Name == "main" {
			g.emit("ube_scope_restore(__watermark);")
			g.emit("ube_scope_destroy();")
			g.emit("return 0;")
		}
		g.indent--
		g.emit("}")
		g.deferScopes = nil
		g.scopeMarks = nil
		g.sliceParamVars = nil
		g.emit("")
	}
}

func (g *Generator) genStmt(st Stmt) {
	switch s := st.(type) {
	case *LetStmt:
		if sl, ok := s.Value.(*SliceInitExpr); ok {
			m := g.sliceElemMangle(sl.Elem)
			g.sliceVars[s.Name] = m
			if len(sl.Elements) == 0 {
				g.emit(fmt.Sprintf("UbeSlice_%s %s = ube_slice_%s_new();", m, s.Name, m))
			} else {
				g.emit(fmt.Sprintf("UbeSlice_%s %s = { .len=%d, .cap=%d, .data=(%s*)ube_bump_alloc(%d*sizeof(%s)) };", m, s.Name, len(sl.Elements), len(sl.Elements), g.toCType(sl.Elem), len(sl.Elements), g.toCType(sl.Elem)))
				for i, e := range sl.Elements {
					g.emit(fmt.Sprintf("%s.data[%d] = %s;", s.Name, i, g.genExpr(e)))
				}
			}
			return
		}
		if s.Type != nil {
			if sl, ok := s.Type.(SliceType); ok {
				g.sliceVars[s.Name] = g.sliceElemMangle(sl.Elem)
			}
			cType := g.toCType(s.Type)

			// Trait assignment: box concrete type into fat pointer
			if tr, isTrait := s.Type.(TraitType); isTrait {
				if _, isCoerce := s.Value.(*TraitCoerceExpr); isCoerce {
					g.emit(fmt.Sprintf("%s %s = %s;", cType, s.Name, g.genExpr(s.Value)))
					return
				}
				vtable := g.registerTraitVtable(tr.Name, s.ValueType)
				valTmp := g.newTmp()
				concCT := g.toCType(s.ValueType)
				g.emit(fmt.Sprintf("%s* %s = (%s*)ube_bump_alloc(sizeof(%s));", concCT, valTmp, concCT, concCT))
				g.emit(fmt.Sprintf("*%s = %s;", valTmp, g.genExpr(s.Value)))
				g.emit(fmt.Sprintf("%s %s = (%s){ .self = (void*)%s, .vtable = &%s };", cType, s.Name, cType, valTmp, vtable))
				return
			}

			if _, isOptional := s.Type.(OptionalType); isOptional {
				if lit, isNull := s.Value.(*LiteralExpr); isNull && lit.Val == "null" {
					g.emit(fmt.Sprintf("%s %s = (%s){ .has_val = false };", cType, s.Name, cType))
					return
				}
				if _, alreadyOptional := s.ValueType.(OptionalType); !alreadyOptional {
					g.emit(fmt.Sprintf("%s %s = (%s){ .has_val = true, .val = %s };", cType, s.Name, cType, g.genExpr(s.Value)))
					return
				}
				g.emit(fmt.Sprintf("%s %s = %s;", cType, s.Name, g.genExpr(s.Value)))
				return
			}
			if _, isRes := s.Type.(ResultType); isRes {
				if _, isErr := s.Value.(*ErrorExpr); isErr {
					g.emit(fmt.Sprintf("%s %s = (%s){ .ok = false, .err = %s };", cType, s.Name, cType, g.genExpr(s.Value)))
					return
				}
				if _, alreadyRes := s.ValueType.(ResultType); !alreadyRes {
					g.emit(fmt.Sprintf("%s %s = (%s){ .ok = true, .val = %s };", cType, s.Name, cType, g.genExpr(s.Value)))
					return
				}
				g.emit(fmt.Sprintf("%s %s = %s;", cType, s.Name, g.genExpr(s.Value)))
				return
			}
			g.emit(fmt.Sprintf("%s %s = %s;", cType, s.Name, g.genExpr(s.Value)))
		} else {
			g.emit(fmt.Sprintf("__auto_type %s = %s;", s.Name, g.genExpr(s.Value)))
		}

	case *AssignStmt:
		lhsExpr := g.genExpr(s.LHS)
		rhsExpr := g.genExpr(s.RHS)

		// Trait assignment: box concrete type into fat pointer
		if tr, isTrait := s.LHSType.(TraitType); isTrait {
			if _, isCoerce := s.RHS.(*TraitCoerceExpr); isCoerce {
				g.emit(fmt.Sprintf("%s = %s;", lhsExpr, g.genExpr(s.RHS)))
				return
			}
			vtable := g.registerTraitVtable(tr.Name, s.RHSType)
			valTmp := g.newTmp()
			concCT := g.toCType(s.RHSType)
			g.emit(fmt.Sprintf("%s* %s = (%s*)ube_bump_alloc(sizeof(%s));", concCT, valTmp, concCT, concCT))
			g.emit(fmt.Sprintf("*%s = %s;", valTmp, rhsExpr))
			g.emit(fmt.Sprintf("%s = (%s){ .self = (void*)%s, .vtable = &%s };", lhsExpr, g.toCType(tr), valTmp, vtable))
			return
		}

		if optT, isOpt := s.LHSType.(OptionalType); isOpt {
			cType := g.toCType(optT)
			if lit, isNull := s.RHS.(*LiteralExpr); isNull && lit.Val == "null" {
				g.emit(fmt.Sprintf("%s = (%s){ .has_val = false };", lhsExpr, cType))
				return
			}
			if _, rhsIsOpt := s.RHSType.(OptionalType); rhsIsOpt {
				g.emit(fmt.Sprintf("%s = %s;", lhsExpr, rhsExpr))
				return
			}
			g.emit(fmt.Sprintf("%s = (%s){ .has_val = true, .val = %s };", lhsExpr, cType, rhsExpr))
			return
		}
		if resT, isRes := s.LHSType.(ResultType); isRes {
			cType := g.toCType(resT)
			if _, isErr := s.RHS.(*ErrorExpr); isErr {
				g.emit(fmt.Sprintf("%s = (%s){ .ok = false, .err = %s };", lhsExpr, cType, rhsExpr))
				return
			}
			if _, rhsIsRes := s.RHSType.(ResultType); rhsIsRes {
				g.emit(fmt.Sprintf("%s = %s;", lhsExpr, rhsExpr))
				return
			}
			g.emit(fmt.Sprintf("%s = (%s){ .ok = true, .val = %s };", lhsExpr, cType, rhsExpr))
			return
		}
		if s.Op != "" && s.Op != "=" {
			if s.Op == "+=" && s.LHSType != nil && s.LHSType.String() == "string" {
				g.emit(fmt.Sprintf("%s = ube_str_concat(%s, %s);", lhsExpr, lhsExpr, rhsExpr))
				return
			}
			g.emit(fmt.Sprintf("%s %s %s;", lhsExpr, s.Op, rhsExpr))
			return
		}
		g.emit(fmt.Sprintf("%s = %s;", lhsExpr, rhsExpr))

	case *DeferStmt:
		if len(g.deferScopes) == 0 {
			g.pushDeferScope()
		}
		last := len(g.deferScopes) - 1
		g.deferScopes[last] = append(g.deferScopes[last], g.genExpr(s.Call))

	case *ReturnStmt:
		if s.Value != nil {
			if optT, isOptional := g.curFnRet.(OptionalType); isOptional {
				if lit, isNull := s.Value.(*LiteralExpr); isNull && lit.Val == "null" {
					g.emitDefersFrom(0)
					g.emitReturnArenaCleanup()
					g.emit(fmt.Sprintf("return (%s){ .has_val = false };", g.toCType(optT)))
					return
				}
			}
			value := g.newTmp()
			g.emit(fmt.Sprintf("__auto_type %s = %s;", value, g.genExpr(s.Value)))
			g.emitDefersFrom(0)
			g.emitReturnArenaCleanup()
			if resT, isResult := g.curFnRet.(ResultType); isResult {
				resCType := g.toCType(resT)
				if _, isErr := s.Value.(*ErrorExpr); isErr {
					g.emit(fmt.Sprintf("return (%s){ .ok = false, .err = %s };", resCType, value))
					return
				}
				if _, alreadyRes := s.ValueType.(ResultType); alreadyRes {
					g.emit(fmt.Sprintf("return %s;", value))
					return
				}
				g.emit(fmt.Sprintf("return (%s){ .ok = true, .val = %s };", resCType, value))
				return
			}
			if optT, isOptional := g.curFnRet.(OptionalType); isOptional {
				optionType := g.toCType(optT)
				if _, alreadyOpt := s.ValueType.(OptionalType); alreadyOpt {
					g.emit(fmt.Sprintf("return %s;", value))
					return
				}
				g.emit(fmt.Sprintf("return (%s){ .has_val = true, .val = %s };", optionType, value))
				return
			}
			g.emit(fmt.Sprintf("return %s;", value))
		} else {
			g.emitDefersFrom(0)
			g.emitReturnArenaCleanup()
			if g.curFnName == "main" {
				g.emit("return 0;")
			} else {
				g.emit("return;")
			}
		}

	case *IfStmt:
		g.emit(fmt.Sprintf("if (%s) {", g.genExpr(s.Cond)))
		g.indent++
		g.genBlock(s.Then)
		g.indent--
		if s.Else != nil {
			g.emit("} else {")
			g.indent++
			if blk, ok := s.Else.(*BlockStmt); ok {
				g.genBlock(blk)
			} else {
				g.pushDeferScope()
				g.genStmt(s.Else)
				g.finishDeferScope()
			}
			g.indent--
		}
		g.emit("}")

	case *IfLetStmt:
		tmp := g.newTmp()
		g.emit(fmt.Sprintf("__auto_type %s = %s;", tmp, g.genExpr(s.Expr)))
		g.emit(fmt.Sprintf("if (%s.has_val) {", tmp))
		g.indent++
		g.emit(fmt.Sprintf("__auto_type %s = %s.val;", s.VarName, tmp))
		g.genBlock(s.Then)
		g.indent--
		if s.Else != nil {
			g.emit("} else {")
			g.indent++
			if blk, ok := s.Else.(*BlockStmt); ok {
				g.genBlock(blk)
			} else {
				g.pushDeferScope()
				g.genStmt(s.Else)
				g.finishDeferScope()
			}
			g.indent--
		}
		g.emit("}")

	case *WhileStmt:
		labels := loopLabel{breakTo: g.newTmp() + "_break", continueTo: g.newTmp() + "_continue"}
		g.emit(fmt.Sprintf("while (%s) {", g.genExpr(s.Cond)))
		g.indent++
		g.loopLabels = append(g.loopLabels, labels)
		g.loopSelectDepth = append(g.loopSelectDepth, len(g.selectGroups))
		g.loopDeferDepth = append(g.loopDeferDepth, len(g.deferScopes))
		g.loopScopeDepth = append(g.loopScopeDepth, len(g.scopeMarks))
		g.genBlock(s.Body)
		g.loopDeferDepth = g.loopDeferDepth[:len(g.loopDeferDepth)-1]
		g.loopScopeDepth = g.loopScopeDepth[:len(g.loopScopeDepth)-1]
		g.emit(labels.continueTo + ":;")
		g.loopLabels = g.loopLabels[:len(g.loopLabels)-1]
		g.loopSelectDepth = g.loopSelectDepth[:len(g.loopSelectDepth)-1]
		g.indent--
		g.emit("}")
		g.emit(labels.breakTo + ":;")

	case *WhileLetStmt:
		labels := loopLabel{breakTo: g.newTmp() + "_break", continueTo: g.newTmp() + "_continue"}
		g.emit("while (1) {")
		g.indent++
		g.loopLabels = append(g.loopLabels, labels)
		g.loopSelectDepth = append(g.loopSelectDepth, len(g.selectGroups))
		if mem, ok := s.Call.(*CallExpr); ok {
			if m, isM := mem.Callee.(*MemberExpr); isM && m.Member == "recv" {
				elemType := mem.PoolElem
				if elemType == nil {
					if ch, isChan := m.TargetType.(ChanType); isChan {
						elemType = ch.Elem
					}
				}
				cType := "int32_t"
				if elemType != nil {
					cType = g.toCType(elemType)
				}
				g.emit(fmt.Sprintf("%s %s = {0};", cType, s.VarName))
				g.emit(fmt.Sprintf("if (!ube_raw_chan_recv(%s, &%s)) goto %s;", g.genExpr(m.Target), s.VarName, labels.breakTo))
			} else {
				optTmp := g.newTmp()
				g.emit(fmt.Sprintf("__auto_type %s = %s;", optTmp, g.genExpr(s.Call)))
				g.emit(fmt.Sprintf("if (!%s.has_val) goto %s;", optTmp, labels.breakTo))
				g.emit(fmt.Sprintf("__auto_type %s = %s.val;", s.VarName, optTmp))
			}
		} else {
			optTmp := g.newTmp()
			g.emit(fmt.Sprintf("__auto_type %s = %s;", optTmp, g.genExpr(s.Call)))
			g.emit(fmt.Sprintf("if (!%s.has_val) goto %s;", optTmp, labels.breakTo))
			g.emit(fmt.Sprintf("__auto_type %s = %s.val;", s.VarName, optTmp))
		}
		g.loopDeferDepth = append(g.loopDeferDepth, len(g.deferScopes))
		g.loopScopeDepth = append(g.loopScopeDepth, len(g.scopeMarks))
		g.genBlock(s.Body)
		g.loopDeferDepth = g.loopDeferDepth[:len(g.loopDeferDepth)-1]
		g.loopScopeDepth = g.loopScopeDepth[:len(g.loopScopeDepth)-1]
		g.emit(labels.continueTo + ":;")
		g.loopLabels = g.loopLabels[:len(g.loopLabels)-1]
		g.loopSelectDepth = g.loopSelectDepth[:len(g.loopSelectDepth)-1]
		g.indent--
		g.emit("}")
		g.emit(labels.breakTo + ":;")

	case *ForStmt:
		labels := loopLabel{breakTo: g.newTmp() + "_break", continueTo: g.newTmp() + "_continue"}
		if rng, ok := s.Iter.(*RangeExpr); ok {
			startTmp := g.newTmp()
			endTmp := g.newTmp()
			elemCT := "int32_t"
			if rType, ok := s.IterType.(RangeType); ok {
				elemCT = g.toCType(rType.Elem)
			}
			g.emit(fmt.Sprintf("%s %s = %s;", elemCT, startTmp, g.genExpr(rng.Start)))
			g.emit(fmt.Sprintf("%s %s = %s;", elemCT, endTmp, g.genExpr(rng.End)))
			g.emit(fmt.Sprintf("for (%s %s = %s; %s < %s; %s++) {", elemCT, s.ItemName, startTmp, s.ItemName, endTmp, s.ItemName))
			g.indent++
			g.loopLabels = append(g.loopLabels, labels)
			g.loopSelectDepth = append(g.loopSelectDepth, len(g.selectGroups))
			g.loopDeferDepth = append(g.loopDeferDepth, len(g.deferScopes))
			g.loopScopeDepth = append(g.loopScopeDepth, len(g.scopeMarks))
			g.genBlock(s.Body)
			g.loopDeferDepth = g.loopDeferDepth[:len(g.loopDeferDepth)-1]
			g.loopScopeDepth = g.loopScopeDepth[:len(g.loopScopeDepth)-1]
			g.emit(labels.continueTo + ":;")
			g.loopLabels = g.loopLabels[:len(g.loopLabels)-1]
			g.loopSelectDepth = g.loopSelectDepth[:len(g.loopSelectDepth)-1]
			g.indent--
			g.emit("}")
			g.emit(labels.breakTo + ":;")
			return
		}
		iterTmp := g.newTmp()
		g.emit(fmt.Sprintf("__auto_type %s = %s;", iterTmp, g.genExpr(s.Iter)))
		lenExpr := fmt.Sprintf("(%s).len", iterTmp)
		if arr, ok := s.IterType.(ArrayType); ok {
			lenExpr = strconv.Itoa(arr.Size)
		}
		g.emit(fmt.Sprintf("for (size_t _i = 0; _i < %s; _i++) {", lenExpr))
		g.indent++
		g.loopLabels = append(g.loopLabels, labels)
		g.loopSelectDepth = append(g.loopSelectDepth, len(g.selectGroups))
		g.emit(fmt.Sprintf("__auto_type %s = (%s).data[_i];", s.ItemName, iterTmp))
		g.loopDeferDepth = append(g.loopDeferDepth, len(g.deferScopes))
		g.loopScopeDepth = append(g.loopScopeDepth, len(g.scopeMarks))
		g.genBlock(s.Body)
		g.loopDeferDepth = g.loopDeferDepth[:len(g.loopDeferDepth)-1]
		g.loopScopeDepth = g.loopScopeDepth[:len(g.loopScopeDepth)-1]
		g.emit(labels.continueTo + ":;")
		g.loopLabels = g.loopLabels[:len(g.loopLabels)-1]
		g.loopSelectDepth = g.loopSelectDepth[:len(g.loopSelectDepth)-1]
		g.indent--
		g.emit("}")
		g.emit(labels.breakTo + ":;")

	case *ScopeStmt:
		g.emit("{")
		g.indent++
		frameTmp := g.newTmp()
		g.scopeMarks = append(g.scopeMarks, frameTmp)
		g.emit(fmt.Sprintf("UbeArenaLevel %s;", frameTmp))
		g.emit(fmt.Sprintf("ube_arena_enter(&%s);", frameTmp))
		g.genBlock(s.Body)
		g.emit(fmt.Sprintf("ube_arena_exit(&%s);", frameTmp))
		g.scopeMarks = g.scopeMarks[:len(g.scopeMarks)-1]
		g.indent--
		g.emit("}")

	case *UnsafeStmt:
		g.emit("{")
		g.indent++
		g.genBlock(s.Body)
		g.indent--
		g.emit("}")

	case *LoopControlStmt:
		if len(g.loopDeferDepth) > 0 {
			g.emitDefersFrom(g.loopDeferDepth[len(g.loopDeferDepth)-1])
		}
		if len(g.loopScopeDepth) > 0 {
			for i := len(g.scopeMarks) - 1; i >= g.loopScopeDepth[len(g.loopScopeDepth)-1]; i-- {
				g.emit("ube_scope_restore(" + g.scopeMarks[i] + ");")
			}
		}
		if len(g.loopSelectDepth) > 0 {
			depth := g.loopSelectDepth[len(g.loopSelectDepth)-1]
			for i := len(g.selectGroups) - 1; i >= depth; i-- {
				g.emit("ube_select_group_destroy(&" + g.selectGroups[i] + ");")
			}
		}
		if len(g.loopLabels) > 0 {
			labels := g.loopLabels[len(g.loopLabels)-1]
			if s.Continue {
				g.emit("goto " + labels.continueTo + ";")
			} else {
				g.emit("goto " + labels.breakTo + ";")
			}
		} else if s.Continue {
			g.emit("continue;")
		} else {
			g.emit("break;")
		}

	case *SpawnStmt:
		var calleeName string
		var receiverExpr Expr
		invalidSpawn := false
		switch callee := s.Call.Callee.(type) {
		case *IdentExpr:
			calleeName = callee.Name
		case *MemberExpr:
			if s.Call.ReceiverType == nil {
				invalidSpawn = true
			} else {
				calleeName = fmt.Sprintf("%s_%s", s.Call.ReceiverType.String(), callee.Member)
				receiverExpr = callee.Target
			}
		default:
			invalidSpawn = true
		}
		if invalidSpawn || calleeName == "" || len(s.Call.ArgTypes) != len(s.Call.Args) {
			g.emit("/* invalid spawn call omitted: type information unavailable */")
			break
		}

		fieldTypes := make([]UbeType, 0, len(s.Call.ArgTypes)+1)
		valueExprs := make([]Expr, 0, len(s.Call.Args)+1)
		if receiverExpr != nil {
			fieldTypes = append(fieldTypes, s.Call.ReceiverType)
			valueExprs = append(valueExprs, receiverExpr)
		}
		fieldTypes = append(fieldTypes, s.Call.ArgTypes...)
		valueExprs = append(valueExprs, s.Call.Args...)

		wrapName := g.newTmp() + "_spawn"
		structName := wrapName + "_args"
		var fields, callArgs []string
		for i, t := range fieldTypes {
			fields = append(fields, fmt.Sprintf("%s a%d", g.toCType(t), i))
			callArgs = append(callArgs, fmt.Sprintf("args->a%d", i))
		}
		if len(fields) == 0 {
			fields = append(fields, "uint8_t unused")
		}
		var wrapper strings.Builder
		fmt.Fprintf(&wrapper, "typedef struct { %s; } %s;\n", strings.Join(fields, "; "), structName)
		fmt.Fprintf(&wrapper, "static void* %s(void* raw) { %s* args = (%s*)raw; %s(%s); free(args); return NULL; }\n", wrapName, structName, structName, calleeName, strings.Join(callArgs, ", "))
		g.spawnWrappers = append(g.spawnWrappers, wrapper.String())
		argNames := make([]string, len(valueExprs))
		for i, arg := range valueExprs {
			argNames[i] = g.newTmp()
			g.emit(fmt.Sprintf("__auto_type %s = %s;", argNames[i], g.genExpr(arg)))
		}
		argExpr := "NULL"
		if len(argNames) > 0 {
			values := make([]string, len(argNames))
			for i, name := range argNames {
				values[i] = fmt.Sprintf(".a%d = %s", i, name)
			}
			ptr := g.newTmp()
			argExpr = fmt.Sprintf("({ %s* %s = (%s*)malloc(sizeof(%s)); if (!%s) abort(); *%s = (%s){ %s }; %s; })", structName, ptr, structName, structName, ptr, ptr, structName, strings.Join(values, ", "), ptr)
		}
		g.emit(fmt.Sprintf("ube_spawn(%s, %s);", wrapName, argExpr))

	case *SelectStmt:
		label := g.newTmp() + "_select_done"
		channels := make([]string, len(s.Cases))
		for i, c := range s.Cases {
			channels[i] = g.newTmp()
			g.emit(fmt.Sprintf("__auto_type %s = %s;", channels[i], g.genExpr(c.Chan)))
		}
		if s.Default != nil {
			selected := g.newTmp()
			offset := g.newTmp()
			iter := g.newTmp()
			caseIdx := g.newTmp()
			doneLabel := g.newTmp() + "_default_done"
			g.emit("do {")
			g.indent++
			g.emit(fmt.Sprintf("static _Thread_local size_t %s = 0;", offset))
			if len(s.Cases) > 1 {
				g.emit(fmt.Sprintf("%s = (%s + 1) %% %d;", offset, offset, len(s.Cases)))
			}
			g.emit(fmt.Sprintf("bool %s = false;", selected))
			g.emit(fmt.Sprintf("for (size_t %s = 0; %s < %d; %s++) {", iter, iter, len(s.Cases), iter))
			g.indent++
			g.emit(fmt.Sprintf("size_t %s = (%s + %s) %% %d;", caseIdx, iter, offset, len(s.Cases)))
			g.emit(fmt.Sprintf("switch (%s) {", caseIdx))
			for i, c := range s.Cases {
				g.emit(fmt.Sprintf("case %d: {", i))
				g.indent++
				ct := c.ChanElem
				if ct == nil {
					ct = PrimitiveType{Name: "i32"}
				}
				g.emit(fmt.Sprintf("%s %s;", g.toCType(ct), c.VarName))
				g.emit(fmt.Sprintf("if (ube_raw_chan_try_recv(%s, &%s)) {", channels[i], c.VarName))
				g.indent++
				g.genBlock(c.Body)
				g.emit(fmt.Sprintf("%s = true;", selected))
				g.emit(fmt.Sprintf("goto %s;", doneLabel))
				g.indent--
				g.emit("}")
				g.emit("break;")
				g.indent--
				g.emit("}")
			}
			g.emit("}")
			g.indent--
			g.emit("}")
			g.emit(doneLabel + ":;")
			g.emit(fmt.Sprintf("if (!%s) {", selected))
			g.indent++
			g.genBlock(s.Default)
			g.indent--
			g.emit("}")
			g.indent--
			g.emit("} while (0);")
		} else {
			group := g.newTmp()
			g.selectGroups = append(g.selectGroups, group)
			g.emit(fmt.Sprintf("UbeSelectGroup %s;", group))
			g.emit(fmt.Sprintf("ube_select_group_init(&%s);", group))
			offset := g.newTmp()
			iter := g.newTmp()
			caseIdx := g.newTmp()
			g.emit(fmt.Sprintf("static _Thread_local size_t %s = 0;", offset))
			g.emit("while (1) {")
			g.indent++
			if len(s.Cases) > 1 {
				g.emit(fmt.Sprintf("%s = (%s + 1) %% %d;", offset, offset, len(s.Cases)))
			}
			g.emit(fmt.Sprintf("for (size_t %s = 0; %s < %d; %s++) {", iter, iter, len(s.Cases), iter))
			g.indent++
			g.emit(fmt.Sprintf("size_t %s = (%s + %s) %% %d;", caseIdx, iter, offset, len(s.Cases)))
			g.emit(fmt.Sprintf("switch (%s) {", caseIdx))
			for i, c := range s.Cases {
				g.emit(fmt.Sprintf("case %d: {", i))
				g.indent++
				ct := c.ChanElem
				if ct == nil {
					ct = PrimitiveType{Name: "i32"}
				}
				g.emit(fmt.Sprintf("%s %s;", g.toCType(ct), c.VarName))
				g.emit(fmt.Sprintf("if (ube_raw_chan_try_recv(%s, &%s)) {", channels[i], c.VarName))
				g.indent++
				g.genBlock(c.Body)
				g.emit(fmt.Sprintf("goto %s;", label))
				g.indent--
				g.emit("}")
				g.emit("break;")
				g.indent--
				g.emit("}")
			}
			g.emit("}")
			g.indent--
			g.emit("}")
			var allClosed []string
			if len(channels) > 0 {
				allClosed = make([]string, len(channels))
				for i, ch := range channels {
					allClosed[i] = fmt.Sprintf("ube_raw_chan_is_closed(%s)", ch)
				}
				g.emit(fmt.Sprintf("if (%s) goto %s;", strings.Join(allClosed, " && "), label))
			}
			waiters := make([]string, len(channels))
			for i, ch := range channels {
				waiters[i] = g.newTmp()
				g.emit(fmt.Sprintf("UbeSelectWaiter %s;", waiters[i]))
				g.emit(fmt.Sprintf("ube_select_register(%s, &%s, &%s);", ch, waiters[i], group))
			}
			ready := make([]string, len(channels))
			for i, ch := range channels {
				ready[i] = fmt.Sprintf("ube_raw_chan_has_data(%s)", ch)
			}
			var recheck []string
			if len(allClosed) > 0 {
				recheck = append(recheck, "("+strings.Join(allClosed, " && ")+")")
			}
			if len(ready) > 0 {
				recheck = append(recheck, "("+strings.Join(ready, " || ")+")")
			}
			g.emit(fmt.Sprintf("if (%s) {", strings.Join(recheck, " || ")))
			g.indent++
			for i, ch := range channels {
				g.emit(fmt.Sprintf("ube_select_unregister(%s, &%s);", ch, waiters[i]))
			}
			g.emit("continue;")
			g.indent--
			g.emit("}")
			g.emit(fmt.Sprintf("ube_select_wait(&%s);", group))
			for i, ch := range channels {
				g.emit(fmt.Sprintf("ube_select_unregister(%s, &%s);", ch, waiters[i]))
			}
			g.indent--
			g.emit("}")
			g.emit(label + ":;")
			g.emit(fmt.Sprintf("ube_select_group_destroy(&%s);", group))
			g.selectGroups = g.selectGroups[:len(g.selectGroups)-1]
		}

	case *MatchStmt:
		tmp := g.newTmp()
		g.emit(fmt.Sprintf("__auto_type %s = %s;", tmp, g.genExpr(s.Expr)))
		g.emit(fmt.Sprintf("switch (%s.tag) {", tmp))
		for _, arm := range s.Arms {
			if arm.Variant == "_" {
				g.emit("default: {")
				g.indent++
				if blk, ok := arm.Body.(*BlockStmt); ok {
					g.genBlock(blk)
				} else {
					g.pushDeferScope()
					g.genStmt(arm.Body)
					g.finishDeferScope()
				}
				g.emit("break;")
				g.indent--
				g.emit("}")
				continue
			}
			g.emit(fmt.Sprintf("case %s_%s: {", s.EnumName, arm.Variant))
			g.indent++
			for i, b := range arm.Bindings {
				g.emit(fmt.Sprintf("__auto_type %s = %s.as.%s._%d;", b, tmp, arm.Variant, i))
			}
			if blk, ok := arm.Body.(*BlockStmt); ok {
				g.genBlock(blk)
			} else {
				g.pushDeferScope()
				g.genStmt(arm.Body)
				g.finishDeferScope()
			}
			g.emit("break;")
			g.indent--
			g.emit("}")
		}
		g.emit("}")

	case *ExprStmt:
		g.emit(fmt.Sprintf("%s;", g.genExpr(s.Expr)))
	}
}
