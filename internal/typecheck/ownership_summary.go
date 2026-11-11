package typecheck

import (
	"Mio/internal/ast"
	"Mio/internal/resolver"
)

// Interprocedural effect summaries for the ownership pass (docs/concurrency.md section 3.4).
//
// Without a summary a call is a black box: every argument and the result are put in one alias group. A summary replaces that for a
// function whose whole behaviour is visible and simple:
//
//   - it mentions no top-level variable (so it cannot publish an argument to a global),
//   - it creates no closure and makes no indirect call (so it cannot hand an argument to code it cannot see),
//   - every call it makes is to such a function, or to a package/builtin function that is given no function-typed argument,
//   - it is not recursive.
//
// The summary records (a) which parameters the result may alias (empty: the result is a fresh graph) and (b) which parameters the call may
// link to each other (the callee stored one inside another). At a call site only those relations are applied, so
// `kaku copy = deep_copy(source)` no longer makes `copy` an alias of `source`. Anything the analysis cannot prove falls back to the
// conservative black-box rule, so a summary can only reduce rejections, never accept an unsound transfer.
type ownSummary struct {
	resultParams map[int]struct{}
	linked       [][]int // groups of parameter indices (size >= 2) that may alias each other after the call
}

type summaryRecorder struct {
	parameters []resolver.Symbol
	resultHits map[int]struct{}
	pairs      [][2]int
}

func (p *ownershipPass) summaryOf(sym resolver.Symbol) *ownSummary {
	if summary, done := p.summaries[sym]; done {
		return summary
	}
	fn, ok := p.functions[sym]
	if !ok || fn.Native || fn.Body == nil {
		p.summaries[sym] = nil
		return nil
	}
	if p.inProgress[sym] {
		return nil // recursion: no summary, the caller falls back to the conservative rule
	}
	p.inProgress[sym] = true
	summary := p.summarize(fn)
	delete(p.inProgress, sym)
	p.summaries[sym] = summary
	return summary
}

func (p *ownershipPass) summarize(fn *ast.FunctionStatement) *ownSummary {
	if !p.summarizable(fn) {
		return nil
	}
	recorder := &summaryRecorder{resultHits: map[int]struct{}{}}
	f := &ownFunction{pass: p, declared: map[resolver.Symbol]struct{}{}, params: map[resolver.Symbol]struct{}{}, recorder: recorder}
	for _, parameter := range fn.Parameters {
		sym, ok := p.c.resolver.BindingOf(parameter)
		if !ok {
			return nil
		}
		f.params[sym] = struct{}{}
		recorder.parameters = append(recorder.parameters, sym)
	}
	p.quiet++
	end := f.block(fn.Body, newOwnState())
	p.quiet--
	if !end.dead {
		recorder.observe(end, nil)
	}
	summary := &ownSummary{resultParams: recorder.resultHits}
	// union the recorded pairs into groups
	parent := make([]int, len(recorder.parameters))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for _, pair := range recorder.pairs {
		parent[find(pair[0])] = find(pair[1])
	}
	groups := map[int][]int{}
	for i := range parent {
		root := find(i)
		groups[root] = append(groups[root], i)
	}
	for i := 0; i < len(parent); i++ {
		if members := groups[i]; len(members) >= 2 {
			summary.linked = append(summary.linked, members)
		}
	}
	return summary
}

// observe records, at one exit of the function, which parameters the returned sources reach and which parameters share an alias group.
func (r *summaryRecorder) observe(st *ownState, returned []resolver.Symbol) {
	for index, parameter := range r.parameters {
		for _, source := range returned {
			if source == parameter {
				r.resultHits[index] = struct{}{}
			} else if _, same := st.groups[source][parameter]; same {
				r.resultHits[index] = struct{}{}
			}
		}
		for other := index + 1; other < len(r.parameters); other++ {
			if _, same := st.groups[parameter][r.parameters[other]]; same {
				r.pairs = append(r.pairs, [2]int{index, other})
			}
		}
	}
}

// summarizable is the syntactic part of the proof (see the comment at the top of this file).
func (p *ownershipPass) summarizable(fn *ast.FunctionStatement) bool {
	ok := true
	probe := &ast.Program{Statements: []ast.Statement{fn.Body}}
	walkAllExpressions(probe, func(e ast.Expression) {
		if !ok {
			return
		}
		switch n := e.(type) {
		case *ast.FunctionLiteral:
			ok = false
		case *ast.Identifier:
			if sym, bound := p.c.resolver.BindingOf(n); bound {
				if _, global := p.c.globals[sym]; global {
					ok = false
				}
			}
		case *ast.CallExpression:
			if p.callSummary(n) != nil {
				return
			}
			if id := calleeIdentifier(n); id != nil {
				if sym, bound := p.c.resolver.BindingOf(id); bound {
					switch sym.Kind {
					case resolver.SymbolFunction:
						// a user function without a usable summary (recursive, non-summarizable): not provable
						ok = false
						return
					case resolver.SymbolVariable, resolver.SymbolParameter:
						ok = false // an indirect call through a function value
						return
					}
				}
			}
			for _, argument := range n.Arguments {
				if kind, known := p.c.TypeOf(argument); !known || kind.IsFunction() {
					ok = false
				}
			}
		}
	})
	return ok
}

// callSummary returns the summary of a direct call of a user function, or nil.
func (p *ownershipPass) callSummary(call *ast.CallExpression) *ownSummary {
	id := calleeIdentifier(call)
	if id == nil {
		return nil
	}
	sym, bound := p.c.resolver.BindingOf(id)
	if !bound || sym.Kind != resolver.SymbolFunction {
		return nil
	}
	return p.summaryOf(sym)
}

// applyCall evaluates a call whose callee has a summary: only the relations the summary records are applied.
func (f *ownFunction) applyCall(call *ast.CallExpression, summary *ownSummary, st *ownState) []resolver.Symbol {
	perArgument := make([][]resolver.Symbol, len(call.Arguments))
	for i, argument := range call.Arguments {
		perArgument[i] = f.expression(argument, st)
	}
	for _, group := range summary.linked {
		var linked []resolver.Symbol
		for _, index := range group {
			if index < len(perArgument) {
				linked = append(linked, perArgument[index]...)
			}
		}
		f.merge(st, linked)
	}
	var result []resolver.Symbol
	if !f.pass.refBearing(call, resolver.Symbol{}, false) {
		return nil
	}
	for index := range summary.resultParams {
		if index < len(perArgument) {
			result = append(result, perArgument[index]...)
		}
	}
	return result
}

// calleeIdentifier is the identifier a call names directly (`f(x)` and `f[T](x)`), or nil for any other callee (package functions,
// function values, field calls).
func calleeIdentifier(call *ast.CallExpression) *ast.Identifier {
	function := call.Function
	if apply, ok := function.(*ast.TypeApplyExpression); ok {
		function = apply.Function
	}
	id, _ := function.(*ast.Identifier)
	return id
}

// escapingCall reports whether a call without a usable summary may publish its arguments somewhere the analysis cannot follow: a user
// function it could not summarise (it touches a global, creates or calls closures, or recurses), an indirect call, or any call that is given
// a function value. Package and builtin functions given no function value cannot reach this program's globals.
func (p *ownershipPass) escapingCall(call *ast.CallExpression) bool {
	for _, argument := range call.Arguments {
		if kind, known := p.c.TypeOf(argument); !known || kind.IsFunction() {
			return true
		}
	}
	id := calleeIdentifier(call)
	if id == nil {
		return false
	}
	sym, bound := p.c.resolver.BindingOf(id)
	if !bound {
		return true
	}
	switch sym.Kind {
	case resolver.SymbolFunction:
		return p.summaryOf(sym) == nil
	case resolver.SymbolVariable, resolver.SymbolParameter:
		return true
	}
	return false
}
