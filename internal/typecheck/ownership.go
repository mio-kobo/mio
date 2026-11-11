package typecheck

import (
	"fmt"

	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/resolver"
)

// Ownership validation for the explicit transfer expression `yuzuru name` (docs/concurrency.md, Phase 1).
//
// Scope of this pass (decision D-1): the operand is a plain variable name that is statically tracked here. A transfer
// never changes how ordinary assignment, argument passing or `kaesu` behave; it only marks the operand as moved. The pass
// is a conservative, flow-sensitive walk over each function body (and over the top-level code) that rejects
//
//	E0701  an operand that is not a transferable local variable (constant, function, type, parameter, global that functions can
//	       reach, variable captured by a closure, variable not declared in the function that transfers it)
//	E0702  any use of a variable after it was transferred on some path (including the next loop iteration)
//	E0703  a second transfer of an already transferred variable
//	E0704  use of a variable that still aliases the transferred graph (it was assigned from it, or they were passed to the same call)
//
// Aliasing here is intentionally coarse and sound-by-over-approximation: values flowing between reference-bearing variables
// through assignment or a shared call merge them into one alias group; transferring any member invalidates the whole group
// for later use. A precise points-to analysis and effect summaries are the next milestone (PR-2); until then programs
// that need them are rejected, never accepted unsoundly.
type ownershipPass struct {
	c        *Checker
	errors   []error
	reported map[string]struct{}

	consts   map[resolver.Symbol]struct{}
	reached  map[resolver.Symbol]struct{} // top-level variables referenced inside any function body
	captured map[resolver.Symbol]struct{} // variables referenced inside a function literal that does not declare them

	functions  map[resolver.Symbol]*ast.FunctionStatement
	summaries  map[resolver.Symbol]*ownSummary
	inProgress map[resolver.Symbol]bool
	quiet      int // > 0 while a callee is analysed only to compute its summary: diagnostics are reported by the real pass
}

type moveInfo struct {
	name   string
	span   ast.Span
	viaTo  string // non-empty: moved because it aliased this transferred variable
	double bool
}

type ownState struct {
	dead    bool
	escaped map[resolver.Symbol]struct{} // graphs passed to code the analysis cannot see (it may have published them)
	moved   map[resolver.Symbol]moveInfo
	groups  map[resolver.Symbol]map[resolver.Symbol]struct{} // alias group of each reference-bearing variable (includes itself)
}

func newOwnState() *ownState {
	return &ownState{escaped: map[resolver.Symbol]struct{}{}, moved: map[resolver.Symbol]moveInfo{}, groups: map[resolver.Symbol]map[resolver.Symbol]struct{}{}}
}

func (s *ownState) clone() *ownState {
	out := newOwnState()
	out.dead = s.dead
	for k := range s.escaped {
		out.escaped[k] = struct{}{}
	}
	for k, v := range s.moved {
		out.moved[k] = v
	}
	for k, g := range s.groups {
		ng := make(map[resolver.Symbol]struct{}, len(g))
		for m := range g {
			ng[m] = struct{}{}
		}
		out.groups[k] = ng
	}
	return out
}

// join merges control-flow paths: a variable is (maybe) moved if it is moved on any live path, and alias relations are unioned.
func ownJoin(states ...*ownState) *ownState {
	var live []*ownState
	for _, s := range states {
		if s != nil && !s.dead {
			live = append(live, s)
		}
	}
	if len(live) == 0 {
		out := newOwnState()
		out.dead = true
		return out
	}
	out := live[0].clone()
	for _, s := range live[1:] {
		for k := range s.escaped {
			out.escaped[k] = struct{}{}
		}
		for k, v := range s.moved {
			if _, ok := out.moved[k]; !ok {
				out.moved[k] = v
			}
		}
		for k, g := range s.groups {
			if out.groups[k] == nil {
				out.groups[k] = map[resolver.Symbol]struct{}{k: {}}
			}
			for m := range g {
				out.groups[k][m] = struct{}{}
			}
		}
	}
	// Re-close groups symmetrically after the union.
	for k, g := range out.groups {
		for m := range g {
			if out.groups[m] == nil {
				out.groups[m] = map[resolver.Symbol]struct{}{m: {}}
			}
			out.groups[m][k] = struct{}{}
		}
	}
	return out
}

type loopContext struct {
	breaks    []*ownState
	continues []*ownState
}

type ownFunction struct {
	pass     *ownershipPass
	declared map[resolver.Symbol]struct{} // symbols declared in this function (locals; parameters are tracked separately)
	params   map[resolver.Symbol]struct{}
	loops    []*loopContext
	literal  bool
	recorder *summaryRecorder // non-nil while this function is analysed to compute its effect summary
}

func (c *Checker) checkOwnership(program *ast.Program) []error {
	p := &ownershipPass{
		c:        c,
		reported: map[string]struct{}{},
		consts:   map[resolver.Symbol]struct{}{},
		reached:  map[resolver.Symbol]struct{}{},
		captured: map[resolver.Symbol]struct{}{},

		functions:  map[resolver.Symbol]*ast.FunctionStatement{},
		summaries:  map[resolver.Symbol]*ownSummary{},
		inProgress: map[resolver.Symbol]bool{},
	}
	if c.resolver == nil || program == nil || !programUsesTransfer(program) {
		return nil
	}
	for _, stmt := range program.Statements {
		if k, ok := stmt.(*ast.ConstStatement); ok {
			if sym, ok := c.resolver.BindingOf(k.Name); ok {
				p.consts[sym] = struct{}{}
			}
		}
	}
	for _, stmt := range program.Statements {
		if fn, ok := stmt.(*ast.FunctionStatement); ok {
			if sym, bound := c.resolver.BindingOf(fn.Name); bound {
				p.functions[sym] = fn
			}
		}
	}
	p.scanReach(program)
	// Top-level code is analysed as one straight-line "function"; each function body is analysed on its own.
	top := &ownFunction{pass: p, declared: map[resolver.Symbol]struct{}{}, params: map[resolver.Symbol]struct{}{}}
	state := newOwnState()
	for _, stmt := range program.Statements {
		switch n := stmt.(type) {
		case *ast.FunctionStatement:
			p.function(n.Parameters, n.Body, n.Native)
		case *ast.StructStatement, *ast.EnumStatement, *ast.ImportStatement, *ast.ConstStatement:
		default:
			state = top.statement(stmt, state)
		}
	}
	return p.errors
}

// programUsesTransfer is a cheap pre-check so programs that never write `yuzuru` pay nothing and cannot be affected.
func programUsesTransfer(program *ast.Program) bool {
	found := false
	walkAllExpressions(program, func(e ast.Expression) {
		if _, ok := e.(*ast.TransferExpression); ok {
			found = true
		}
	})
	return found
}

func (p *ownershipPass) function(parameters []*ast.Identifier, body *ast.BlockStatement, native bool) {
	if native || body == nil {
		return
	}
	f := &ownFunction{pass: p, declared: map[resolver.Symbol]struct{}{}, params: map[resolver.Symbol]struct{}{}}
	for _, parameter := range parameters {
		if sym, ok := p.c.resolver.BindingOf(parameter); ok {
			f.params[sym] = struct{}{}
		}
	}
	f.block(body, newOwnState())
}

// scanReach records (a) top-level variables referenced from inside any function and (b) variables referenced from inside a function literal.
func (p *ownershipPass) scanReach(program *ast.Program) {
	var visitBody func(block *ast.BlockStatement, literalDepth int, inFunction bool, own map[resolver.Symbol]struct{})
	var visitExpr func(e ast.Expression, literalDepth int, inFunction bool, own map[resolver.Symbol]struct{})
	var visitStmt func(s ast.Statement, literalDepth int, inFunction bool, own map[resolver.Symbol]struct{})
	note := func(id *ast.Identifier, literalDepth int, inFunction bool, own map[resolver.Symbol]struct{}) {
		sym, ok := p.c.resolver.BindingOf(id)
		if !ok {
			return
		}
		if inFunction {
			p.reached[sym] = struct{}{}
		}
		if literalDepth > 0 {
			if _, mine := own[sym]; !mine {
				p.captured[sym] = struct{}{}
			}
		}
	}
	visitBody = func(block *ast.BlockStatement, ld int, inF bool, own map[resolver.Symbol]struct{}) {
		if block == nil {
			return
		}
		for _, s := range block.Statements {
			visitStmt(s, ld, inF, own)
		}
	}
	declare := func(id *ast.Identifier, own map[resolver.Symbol]struct{}) {
		if sym, ok := p.c.resolver.BindingOf(id); ok {
			own[sym] = struct{}{}
		}
	}
	visitExpr = func(e ast.Expression, ld int, inF bool, own map[resolver.Symbol]struct{}) {
		switch n := e.(type) {
		case nil:
		case *ast.Identifier:
			note(n, ld, inF, own)
		case *ast.TransferExpression:
			note(n.Operand, ld, inF, own)
		case *ast.FunctionLiteral:
			inner := map[resolver.Symbol]struct{}{}
			for _, parameter := range n.Parameters {
				declare(parameter, inner)
			}
			// Symbols declared inside the literal are its own; everything else it mentions is captured.
			visitBody(n.Body, ld+1, true, inner)
		default:
			forEachChild(e, func(child ast.Expression) { visitExpr(child, ld, inF, own) })
		}
	}
	visitStmt = func(s ast.Statement, ld int, inF bool, own map[resolver.Symbol]struct{}) {
		switch n := s.(type) {
		case *ast.LetStatement:
			declare(n.Name, own)
			visitExpr(n.Value, ld, inF, own)
		case *ast.AssignStatement:
			note(n.Name, ld, inF, own)
			visitExpr(n.Value, ld, inF, own)
		case *ast.IndexAssignStatement:
			visitExpr(n.Target, ld, inF, own)
			visitExpr(n.Value, ld, inF, own)
		case *ast.ExpressionStatement:
			visitExpr(n.Expr, ld, inF, own)
		case *ast.ReturnStatement:
			visitExpr(n.Value, ld, inF, own)
		case *ast.BlockStatement:
			visitBody(n, ld, inF, own)
		case *ast.IfStatement:
			visitExpr(n.Condition, ld, inF, own)
			visitBody(n.Consequence, ld, inF, own)
			visitBody(n.Alternative, ld, inF, own)
		case *ast.WhileStatement:
			visitExpr(n.Condition, ld, inF, own)
			visitBody(n.Body, ld, inF, own)
		case *ast.MatchStatement:
			visitExpr(n.Value, ld, inF, own)
			for _, arm := range n.Arms {
				for _, b := range arm.Bindings {
					declare(b, own)
				}
				visitBody(arm.Body, ld, inF, own)
			}
		case *ast.FunctionStatement:
			inner := map[resolver.Symbol]struct{}{}
			for _, parameter := range n.Parameters {
				declare(parameter, inner)
			}
			visitBody(n.Body, ld+1, true, inner)
		}
	}
	top := map[resolver.Symbol]struct{}{}
	for _, s := range program.Statements {
		switch n := s.(type) {
		case *ast.FunctionStatement:
			if n.Native {
				continue
			}
			inner := map[resolver.Symbol]struct{}{}
			for _, parameter := range n.Parameters {
				declare(parameter, inner)
			}
			visitBody(n.Body, 0, true, inner)
		case *ast.StructStatement, *ast.EnumStatement, *ast.ImportStatement:
		default:
			visitStmt(s, 0, false, top)
		}
	}
}

func (p *ownershipPass) report(code, message string, span ast.Span, help string, cause error) {
	if p.quiet > 0 {
		return
	}
	key := fmt.Sprintf("%s:%d:%d", code, span.Start.Line, span.Start.Column)
	if _, dup := p.reported[key]; dup {
		return
	}
	p.reported[key] = struct{}{}
	p.errors = append(p.errors, diagnostic.NewError(code, message, diagnostic.FromPositions(span.Start, span.End)).WithHelp(help).WithCause(cause))
}

// refBearing reports whether a value of this type can hold or reach mutable heap state (arrays, maps, structs, enums, closures).
// Unknown types are treated as reference-bearing so the analysis can only over-reject.
func (p *ownershipPass) refBearing(expr ast.Expression, sym resolver.Symbol, haveSym bool) bool {
	var kind Kind
	var ok bool
	if expr != nil {
		kind, ok = p.c.TypeOf(expr)
	}
	if !ok && haveSym {
		kind, ok = p.c.TypeOfSymbol(sym)
	}
	if !ok {
		return true
	}
	return kindRefBearing(kind)
}

func kindRefBearing(kind Kind) bool {
	kind = kind.NonNullable()
	switch {
	case kind == KindInvalid:
		return true
	case kind.IsArray(), kind.IsMap(), kind.IsNamed(), kind.IsFunction(), kind.IsTypeParameter():
		return true
	}
	return false
}

// ---- statements ----

func (f *ownFunction) block(block *ast.BlockStatement, st *ownState) *ownState {
	if block == nil {
		return st
	}
	for _, s := range block.Statements {
		st = f.statement(s, st)
	}
	return st
}

func (f *ownFunction) statement(stmt ast.Statement, st *ownState) *ownState {
	if st.dead {
		return st
	}
	switch n := stmt.(type) {
	case *ast.LetStatement:
		sym, haveSym := f.pass.c.resolver.BindingOf(n.Name)
		if haveSym {
			f.declared[sym] = struct{}{}
		}
		sources := f.expression(n.Value, st)
		if haveSym {
			f.assign(sym, n.Value, sources, st)
		}
	case *ast.AssignStatement:
		sources := f.expression(n.Value, st)
		if sym, ok := f.pass.c.resolver.BindingOf(n.Name); ok {
			f.assign(sym, n.Value, sources, st)
		}
	case *ast.IndexAssignStatement:
		target := f.expression(n.Target, st)
		value := f.expression(n.Value, st)
		// Storing a reference into an element makes the container and the stored value one alias group.
		f.merge(st, append(target, value...))
	case *ast.ExpressionStatement:
		f.expression(n.Expr, st)
	case *ast.ReturnStatement:
		returned := f.expression(n.Value, st)
		if f.recorder != nil {
			f.recorder.observe(st, returned)
		}
		st.dead = true
	case *ast.BlockStatement:
		return f.block(n, st)
	case *ast.IfStatement:
		f.expression(n.Condition, st)
		thenState := f.block(n.Consequence, st.clone())
		elseState := st.clone()
		if n.Alternative != nil {
			elseState = f.block(n.Alternative, elseState)
		}
		return ownJoin(thenState, elseState)
	case *ast.WhileStatement:
		return f.loop(n, st)
	case *ast.BreakStatement:
		if len(f.loops) > 0 {
			ctx := f.loops[len(f.loops)-1]
			ctx.breaks = append(ctx.breaks, st.clone())
		}
		st.dead = true
	case *ast.ContinueStatement:
		if len(f.loops) > 0 {
			ctx := f.loops[len(f.loops)-1]
			ctx.continues = append(ctx.continues, st.clone())
		}
		st.dead = true
	case *ast.MatchStatement:
		sources := f.expression(n.Value, st)
		var outs []*ownState
		for _, arm := range n.Arms {
			armState := st.clone()
			for _, binding := range arm.Bindings {
				if sym, ok := f.pass.c.resolver.BindingOf(binding); ok {
					f.declared[sym] = struct{}{}
					f.assignFrom(sym, nil, sources, armState)
				}
			}
			outs = append(outs, f.block(arm.Body, armState))
		}
		if len(outs) == 0 {
			return st
		}
		return ownJoin(outs...)
	case *ast.FunctionStatement:
		f.pass.function(n.Parameters, n.Body, n.Native)
	}
	return st
}

func (f *ownFunction) loop(n *ast.WhileStatement, st *ownState) *ownState {
	head := st.clone()
	var exit *ownState
	// Two passes: the second sees whatever the first iteration moved, which is what the next iteration observes.
	for pass := 0; pass < 2; pass++ {
		ctx := &loopContext{}
		f.loops = append(f.loops, ctx)
		entry := head.clone()
		f.expression(n.Condition, entry)
		afterCondition := entry.clone()
		bodyOut := f.block(n.Body, entry)
		f.loops = f.loops[:len(f.loops)-1]
		next := []*ownState{head, bodyOut}
		next = append(next, ctx.continues...)
		head = ownJoin(next...)
		exit = ownJoin(append([]*ownState{afterCondition}, ctx.breaks...)...)
	}
	return exit
}

// ---- expressions ----

// expression walks e in evaluation order, enforcing the move rules, and returns the reference-bearing variables whose graphs
// the resulting value may alias (a transferred operand is excluded: the receiver of `yuzuru x` owns x's graph exclusively).
func (f *ownFunction) expression(e ast.Expression, st *ownState) []resolver.Symbol {
	switch n := e.(type) {
	case nil:
		return nil
	case *ast.Identifier:
		return f.use(n, st)
	case *ast.TransferExpression:
		f.transfer(n, st)
		return nil
	case *ast.FunctionLiteral:
		// Creating a closure mentions every captured variable now; transfers inside it are checked as its own function.
		var sources []resolver.Symbol
		f.pass.visitIdentifiers(n.Body, func(id *ast.Identifier) {
			if sym, ok := f.pass.c.resolver.BindingOf(id); ok {
				if _, local := f.declared[sym]; local || f.isParam(sym) || f.pass.isTopVariable(sym) {
					sources = append(sources, f.use(id, st)...)
				}
			}
		})
		f.pass.function(n.Parameters, n.Body, false)
		return sources
	case *ast.CallExpression:
		if summary := f.pass.callSummary(n); summary != nil {
			f.expression(n.Function, st)
			return f.applyCall(n, summary, st)
		}
		var all []resolver.Symbol
		all = append(all, f.expression(n.Function, st)...)
		var args []resolver.Symbol
		for _, a := range n.Arguments {
			args = append(args, f.expression(a, st)...)
		}
		if f.pass.escapingCall(n) {
			f.escape(st, args)
		}
		// A callee may store one argument inside another: everything passed to one call becomes one alias group.
		f.merge(st, args)
		if !f.pass.refBearing(n, resolver.Symbol{}, false) {
			return nil
		}
		return append(all, args...)
	default:
		var out []resolver.Symbol
		forEachChild(e, func(child ast.Expression) { out = append(out, f.expression(child, st)...) })
		// A scalar (or string) value cannot alias heap state, whatever it was computed from: `src[0]`, `len(a)`.
		if !f.pass.refBearing(e, resolver.Symbol{}, false) {
			return nil
		}
		return out
	}
}

func (f *ownFunction) isParam(sym resolver.Symbol) bool { _, ok := f.params[sym]; return ok }

func (p *ownershipPass) isTopVariable(sym resolver.Symbol) bool {
	_, ok := p.c.globals[sym]
	return ok
}

// use records a read of a variable. Reading a moved variable, or one that aliases a transferred graph, is an error.
func (f *ownFunction) use(id *ast.Identifier, st *ownState) []resolver.Symbol {
	sym, ok := f.pass.c.resolver.BindingOf(id)
	if !ok || (sym.Kind != resolver.SymbolVariable && sym.Kind != resolver.SymbolParameter) {
		return nil
	}
	if info, moved := st.moved[sym]; moved && !st.dead {
		if info.viaTo != "" {
			f.pass.report("E0704", fmt.Sprintf("%s 仍然引用已经通过 yuzuru 转移的 %s 的对象图", id.Name, info.viaTo), id.Range,
				fmt.Sprintf("%s 与 %s 之间存在别名（第 %d 行）；转移后不能再使用别名，请先重新赋值", id.Name, info.viaTo, info.span.Start.Line),
				fmt.Errorf("alias %s of transferred %s used after transfer", id.Name, info.viaTo))
		} else {
			f.pass.report("E0702", fmt.Sprintf("%s 在 yuzuru 转移之后被使用", id.Name), id.Range,
				fmt.Sprintf("%s 已在第 %d 行转移；转移后不可再访问，请重新赋值或不要转移", id.Name, info.span.Start.Line),
				fmt.Errorf("use of %s after ownership transfer", id.Name))
		}
	}
	if f.pass.refBearing(id, sym, true) {
		return []resolver.Symbol{sym}
	}
	return nil
}

func (f *ownFunction) transfer(n *ast.TransferExpression, st *ownState) {
	id := n.Operand
	sym, ok := f.pass.c.resolver.BindingOf(id)
	invalid := func(why, help string) {
		f.pass.report("E0701", fmt.Sprintf("无法转移 %s：%s", id.Name, why), n.Span(), help, fmt.Errorf("invalid yuzuru operand %s: %s", id.Name, why))
	}
	if !ok {
		invalid("它不是已声明的变量", "yuzuru 只能转移局部变量")
		return
	}
	switch sym.Kind {
	case resolver.SymbolVariable:
	case resolver.SymbolParameter:
		invalid("参数是调用者传入的引用，调用者可能仍持有它", "先在函数内创建新值再转移；参数所有权转移留待后续里程碑")
		return
	default:
		invalid("它不是变量", "yuzuru 只能转移局部变量")
		return
	}
	if _, isConst := f.pass.consts[sym]; isConst {
		invalid("它是常量（teisu）", "常量不能被转移")
		return
	}
	if _, global := f.pass.c.globals[sym]; global {
		if _, reached := f.pass.reached[sym]; reached {
			invalid("它是顶层变量且被函数引用，其他代码可能访问它", "把值放入函数内的局部变量后再转移")
			return
		}
	} else if _, local := f.declared[sym]; !local {
		invalid("它不是在当前函数内声明的局部变量", "闭包内不能转移外层变量；请在闭包内创建新值")
		return
	}
	if _, captured := f.pass.captured[sym]; captured {
		invalid("它被闭包捕获，闭包仍可访问它", "结束闭包对它的使用或改用闭包内的新值")
		return
	}
	// A graph that is reachable from a global, or that was handed to code the analysis cannot see, may be reachable from elsewhere.
	members := map[resolver.Symbol]struct{}{sym: {}}
	for member := range st.groups[sym] {
		members[member] = struct{}{}
	}
	for member := range members {
		_, escaped := st.escaped[member]
		_, global := f.pass.c.globals[member]
		if escaped || (global && member != sym) {
			invalid("它可能已被全局变量或分析无法看到的代码持有", "不要把该值传给会保存它的函数，或在转移前重新创建它")
			return
		}
	}
	if info, moved := st.moved[sym]; moved && !st.dead {
		first := info.span.Start.Line
		f.pass.report("E0703", fmt.Sprintf("%s 已经被转移过", id.Name), n.Span(),
			fmt.Sprintf("上一次转移在第 %d 行；同一个变量不能转移两次", first), fmt.Errorf("double transfer of %s", id.Name))
		return
	}
	// Every alias of the graph is invalidated together with the operand: using one later is the live-alias error.
	if group := st.groups[sym]; group != nil {
		for member := range group {
			if member != sym {
				st.moved[member] = moveInfo{name: member.Name, span: n.Span(), viaTo: id.Name}
			}
		}
	}
	st.moved[sym] = moveInfo{name: id.Name, span: n.Span()}
	delete(st.groups, sym)
	for _, g := range st.groups {
		delete(g, sym)
	}
}

// assign binds a variable to the value of an expression: it revives the variable and puts it in one alias group with the
// reference-bearing variables the value was built from (never with a transferred operand).
func (f *ownFunction) assign(sym resolver.Symbol, value ast.Expression, sources []resolver.Symbol, st *ownState) {
	f.assignFrom(sym, value, sources, st)
}

func (f *ownFunction) assignFrom(sym resolver.Symbol, value ast.Expression, sources []resolver.Symbol, st *ownState) {
	delete(st.moved, sym)
	delete(st.escaped, sym)
	// Leaving the old group: the variable no longer refers to its previous graph.
	for _, g := range st.groups {
		delete(g, sym)
	}
	delete(st.groups, sym)
	if !f.pass.refBearing(value, sym, true) {
		return
	}
	f.merge(st, append([]resolver.Symbol{sym}, sources...))
}

// merge makes all the given variables one alias group (symmetric, transitive over existing groups).
func (f *ownFunction) merge(st *ownState, syms []resolver.Symbol) {
	if len(syms) < 2 {
		return
	}
	union := map[resolver.Symbol]struct{}{}
	for _, s := range syms {
		union[s] = struct{}{}
		for m := range st.groups[s] {
			union[m] = struct{}{}
		}
	}
	for m := range union {
		g := make(map[resolver.Symbol]struct{}, len(union))
		for k := range union {
			g[k] = struct{}{}
		}
		st.groups[m] = g
	}
}

// escape marks the graphs of the given variables (and every alias of them) as possibly published by code the analysis cannot see.
func (f *ownFunction) escape(st *ownState, syms []resolver.Symbol) {
	for _, sym := range syms {
		st.escaped[sym] = struct{}{}
		for member := range st.groups[sym] {
			st.escaped[member] = struct{}{}
		}
	}
}
