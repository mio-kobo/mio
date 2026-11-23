// Package asynclower lowers hidouki / matsu to ordinary Mio.
//
// `hidouki waza f(p) -> T { body }` becomes a synchronous function returning `async.Task<T>` whose body is
// `async.spawn[T](waza (__mio_self: async.Task<T>) -> unit { ... })`. Every `matsu e` is hoisted out of its expression (evaluation order
// preserved, `e` evaluated exactly once) and the rest of the function after it becomes a continuation closure passed to
// `async.then`. Structured control flow (moshi, aida, awase) that contains a suspension point is converted so that the statements after
// it live in a continuation closure too; loops become a self-referencing closure re-entered through `async.step`. The frame of a
// suspended task is therefore a chain of ordinary heap closures, traced precisely by the existing collector.
//
// The same algorithm is implemented by selfhost/parser/asynclower.mio; the two must produce identical programs.
package asynclower

import (
	"fmt"
	"sort"

	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/token"
)

// Diagnostic codes (E08xx is the asynchronous range).
const (
	CodeAwaitOutside = "E0801"
	CodeNotTask      = "E0802"
	CodeInvalidDecl  = "E0803"
	CodeInvalidEntry = "E0804"
	CodeTarget       = "E0805"
)

// RuntimePackage is the library package the lowered code calls; its default import alias is the last path element.
const RuntimePackage = "kiso/async"
const runtimeAlias = "async"

// osPackage provides the process exit used by an asynchronous entrypoint.
const osPackage = "kiso/os"
const osAlias = "os"

// Reserved generated names. All start with __mio_ so they cannot clash with ordinary code.
const (
	selfName  = "__mio_self"
	taskTemp  = "__mio_t"
	valueTemp = "__mio_v"
	spillTemp = "__mio_s"
	contTemp  = "__mio_k"
	loopTemp  = "__mio_l"
)

type lowerer struct {
	errors []*diagnostic.Diagnostic
	// per async function
	counter int
	result  *ast.TypeRef
	asyncFn map[string]*ast.FunctionStatement
}

// failAt reports a diagnostic on the first token (length bytes) of a node: both frontends locate lowering errors by the start position
// of the construct, so the span is the keyword or name that starts it.
func (l *lowerer) failAt(code, message string, start token.Position, length int) {
	end := token.Position{Offset: start.Offset + length, Line: start.Line, Column: start.Column + length}
	l.errors = append(l.errors, diagnostic.NewError(code, message, diagnostic.FromPositions(start, end)))
}

// Lower returns the program with every async function and await lowered. Programs without hidouki or matsu are returned unchanged.
// Errors are returned in source order; when there are errors the original program is returned.
func Lower(program *ast.Program) (*ast.Program, []error) {
	if program == nil || !usesAsync(program) {
		return program, nil
	}
	l := &lowerer{asyncFn: map[string]*ast.FunctionStatement{}}
	for _, statement := range program.Statements {
		if fn, ok := statement.(*ast.FunctionStatement); ok && fn.Async {
			l.asyncFn[fn.Name.Name] = fn
		}
	}
	l.validate(program)
	if len(l.errors) != 0 {
		return program, l.sortedErrors()
	}
	out := &ast.Program{Range: program.Range}
	imported, osImported := false, false
	for _, statement := range program.Statements {
		if imp, ok := statement.(*ast.ImportStatement); ok {
			imported = imported || imp.Path == RuntimePackage
			osImported = osImported || imp.Path == osPackage
		}
	}
	entry := l.entryCall(program)
	zero := ast.Span{Start: program.Range.Start, End: program.Range.Start}
	if !imported {
		out.Statements = append(out.Statements, &ast.ImportStatement{Path: RuntimePackage, Alias: runtimeAlias, Range: zero})
	}
	if entry && !osImported {
		out.Statements = append(out.Statements, &ast.ImportStatement{Path: osPackage, Alias: osAlias, Range: zero})
	}
	for _, statement := range program.Statements {
		switch s := statement.(type) {
		case *ast.FunctionStatement:
			if s.Async {
				out.Statements = append(out.Statements, l.lowerFunction(s))
				continue
			}
		case *ast.ExpressionStatement:
			if call, ok := s.Expr.(*ast.CallExpression); ok {
				if name, ok := call.Function.(*ast.Identifier); ok && name.Name == "main" && len(call.Arguments) == 0 {
					if _, isAsync := l.asyncFn["main"]; isAsync {
						// main(); ==> os.exit(async.block_on(main()));  the exit status is the task result.
						drive := l.runtimeCall("block_on", nil, []ast.Expression{call}, s.Range)
						exit := &ast.CallExpression{
							Function:  &ast.FieldExpression{Left: ident(osAlias, s.Range), Name: "exit", Range: s.Range},
							Arguments: []ast.Expression{drive},
							Range:     s.Range,
						}
						out.Statements = append(out.Statements, &ast.ExpressionStatement{Expr: exit, Range: s.Range})
						continue
					}
				}
			}
		}
		out.Statements = append(out.Statements, statement)
	}
	// Errors found while lowering (a kaesu without a value) keep the original program too.
	if len(l.errors) != 0 {
		return program, l.sortedErrors()
	}
	return out, nil
}

// entryCall reports whether the file calls an asynchronous main at top level (that call is rewritten to drive the executor and exit).
func (l *lowerer) entryCall(program *ast.Program) bool {
	if _, ok := l.asyncFn["main"]; !ok {
		return false
	}
	for _, statement := range program.Statements {
		if s, ok := statement.(*ast.ExpressionStatement); ok {
			if call, ok := s.Expr.(*ast.CallExpression); ok {
				if name, ok := call.Function.(*ast.Identifier); ok && name.Name == "main" && len(call.Arguments) == 0 {
					return true
				}
			}
		}
	}
	return false
}

func (l *lowerer) sortedErrors() []error {
	sort.SliceStable(l.errors, func(i, j int) bool {
		return l.errors[i].Labels[0].Span.Start.Offset < l.errors[j].Labels[0].Span.Start.Offset
	})
	errs := make([]error, 0, len(l.errors))
	for _, e := range l.errors {
		errs = append(errs, e)
	}
	return errs
}

// ---------------------------------------------------------------- detection and validation

func usesAsync(program *ast.Program) bool {
	found := false
	for _, statement := range program.Statements {
		if fn, ok := statement.(*ast.FunctionStatement); ok && fn.Async {
			return true
		}
		walkStatement(statement, func(e ast.Expression) {
			if _, ok := e.(*ast.AwaitExpression); ok {
				found = true
			}
		}, nil)
	}
	return found
}

func (l *lowerer) validate(program *ast.Program) {
	for _, statement := range program.Statements {
		fn, ok := statement.(*ast.FunctionStatement)
		if !ok {
			l.checkAwaitPlacement(statement, false)
			continue
		}
		if fn.Async {
			l.validateDeclaration(fn)
		}
		if fn.Body != nil {
			l.checkAwaitPlacement(fn.Body, fn.Async)
		}
	}
	// The generated code is addressed through the package name `async`: no declaration or binding in a file that uses hidouki or matsu may
	// take that name (it would shadow the runtime).
	for _, statement := range program.Statements {
		l.reservedNames(statement)
	}
}

func (l *lowerer) validateDeclaration(fn *ast.FunctionStatement) {
	switch {
	case fn.Native || fn.CABI || fn.CSymbol != "":
		l.failAt(CodeInvalidDecl, "hidouki 函数不能是外部（C）声明", fn.Range.Start, len("hidouki"))
	case fn.ReturnType == nil || fn.ReturnType.Name == "unit":
		l.failAt(CodeInvalidDecl, "hidouki 函数必须声明非 unit 的结果类型", fn.Range.Start, len("hidouki"))
	}
	if fn.Name.Name == "main" {
		if len(fn.Parameters) != 0 || len(fn.TypeParams) != 0 || fn.ReturnType == nil || fn.ReturnType.Name != "int64" {
			l.failAt(CodeInvalidEntry, "异步入口 main 必须是 hidouki waza main() -> int64", fn.Range.Start, len("hidouki"))
		}
	}
}

func (l *lowerer) reservedNames(statement ast.Statement) {
	name := func(id *ast.Identifier) {
		if id != nil && id.Name == runtimeAlias {
			l.failAt(CodeInvalidDecl, "在使用 hidouki / matsu 的文件中，名称 async 保留给异步运行时", id.Range.Start, len(runtimeAlias))
		}
	}
	switch s := statement.(type) {
	case *ast.FunctionStatement:
		if s.Name != nil && s.Name.Name == runtimeAlias {
			length := len("waza")
			if s.Async {
				length = len("hidouki")
			}
			l.failAt(CodeInvalidDecl, "在使用 hidouki / matsu 的文件中，名称 async 保留给异步运行时", s.Range.Start, length)
		}
		for _, p := range s.Parameters {
			name(p)
		}
	}
	walkStatement(statement, nil, func(st ast.Statement) {
		if let, ok := st.(*ast.LetStatement); ok && let.Name != nil && let.Name.Name == runtimeAlias {
			l.failAt(CodeInvalidDecl, "在使用 hidouki / matsu 的文件中，名称 async 保留给异步运行时", let.Range.Start, len("kaku"))
		}
	})
}

func (l *lowerer) checkAwaitPlacement(node ast.Node, inAsync bool) {
	var visitStatement func(st ast.Statement, inAsync bool)
	var visitExpression func(e ast.Expression, inAsync bool)
	visitExpression = func(e ast.Expression, inAsync bool) {
		switch x := e.(type) {
		case *ast.AwaitExpression:
			if !inAsync {
				l.failAt(CodeAwaitOutside, "matsu 只能用于 hidouki 函数内部", x.Range.Start, len("matsu"))
			}
			visitExpression(x.Operand, inAsync)
		case *ast.FunctionLiteral:
			// A function literal is its own synchronous context even inside an async function.
			visitStatement(x.Body, false)
		default:
			children(e, func(c ast.Expression) { visitExpression(c, inAsync) })
		}
	}
	visitStatement = func(st ast.Statement, inAsync bool) {
		statementChildren(st, func(e ast.Expression) { visitExpression(e, inAsync) }, func(s ast.Statement) { visitStatement(s, inAsync) })
	}
	switch n := node.(type) {
	case ast.Statement:
		visitStatement(n, inAsync)
	}
}

// ---------------------------------------------------------------- lowering of one function

func (l *lowerer) fresh(prefix string) string {
	l.counter++
	return fmt.Sprintf("%s%d", prefix, l.counter)
}

func ident(name string, span ast.Span) *ast.Identifier {
	return &ast.Identifier{Name: name, Range: span}
}

func (l *lowerer) taskType(result *ast.TypeRef) *ast.TypeRef {
	return &ast.TypeRef{Name: runtimeAlias + ".Task", Args: []*ast.TypeRef{result}, Range: result.Range}
}

func unitType(span ast.Span) *ast.TypeRef { return &ast.TypeRef{Name: "unit", Range: span} }

func thunkType(span ast.Span) *ast.TypeRef {
	return &ast.TypeRef{Name: "fn", Params: []*ast.TypeRef{}, Result: unitType(span), Range: span}
}

// runtimeCall builds `async.name[typeArgs](args)`.
func (l *lowerer) runtimeCall(name string, typeArgs []*ast.TypeRef, args []ast.Expression, span ast.Span) ast.Expression {
	var fn ast.Expression = &ast.FieldExpression{Left: ident(runtimeAlias, span), Name: name, Range: span}
	if len(typeArgs) != 0 {
		fn = &ast.TypeApplyExpression{Function: fn, Args: typeArgs, Range: span}
	}
	return &ast.CallExpression{Function: fn, Arguments: args, Range: span}
}

func (l *lowerer) callStatement(name string, args []ast.Expression, span ast.Span) ast.Statement {
	return &ast.ExpressionStatement{Expr: l.runtimeCall(name, nil, args, span), Range: span}
}

func (l *lowerer) field(left ast.Expression, name string, span ast.Span) ast.Expression {
	return &ast.FieldExpression{Left: left, Name: name, Range: span}
}

func thunk(body []ast.Statement, span ast.Span) *ast.FunctionLiteral {
	return &ast.FunctionLiteral{ReturnType: unitType(span), Body: &ast.BlockStatement{Statements: body, Range: span}, Range: span}
}

// cont emits the statements that run when a statement list finishes normally.
type cont func() []ast.Statement

// loop describes the innermost lowered loop: how break and continue are expressed.
type loopContext struct {
	breakWith    func(span ast.Span) []ast.Statement
	continueWith func(span ast.Span) []ast.Statement
}

func (l *lowerer) lowerFunction(fn *ast.FunctionStatement) *ast.FunctionStatement {
	l.counter = 0
	l.result = fn.ReturnType
	span := fn.Range
	body := l.cps(l.normalizeList(fn.Body.Statements), func() []ast.Statement {
		// Falling off the end of an async function without kaesu leaves the task unfinished: fail it deterministically instead.
		return []ast.Statement{&ast.ExpressionStatement{Expr: l.runtimeCall("fail_missing_return", []*ast.TypeRef{l.result}, []ast.Expression{ident(selfName, span)}, span), Range: span}}
	}, nil, span)
	self := ident(selfName, span)
	self.DeclaredType = l.taskType(fn.ReturnType)
	literal := &ast.FunctionLiteral{
		Parameters: []*ast.Identifier{self},
		ReturnType: unitType(span),
		Body:       &ast.BlockStatement{Statements: body, Range: fn.Body.Range},
		Range:      fn.Body.Range,
	}
	spawn := l.runtimeCall("spawn", []*ast.TypeRef{fn.ReturnType}, []ast.Expression{literal}, fn.Body.Range)
	return &ast.FunctionStatement{
		Name:        fn.Name,
		TypeParams:  fn.TypeParams,
		Constraints: fn.Constraints,
		Parameters:  fn.Parameters,
		ReturnType:  l.taskType(fn.ReturnType),
		Body: &ast.BlockStatement{
			Statements: []ast.Statement{&ast.ReturnStatement{Value: spawn, Range: fn.Body.Range}},
			Range:      fn.Body.Range,
		},
		Exported: fn.Exported,
		Range:    fn.Range,
	}
}

// ---------------------------------------------------------------- normalization: hoist awaits out of expressions

// normalizeList rewrites every statement that contains matsu into: plain statements, `kaku v = matsu t;` steps (t is a temporary
// holding the task, v receives its value) and structured statements whose conditions contain no matsu.
func (l *lowerer) normalizeList(list []ast.Statement) []ast.Statement {
	var out []ast.Statement
	for _, s := range list {
		out = append(out, l.normalizeStatement(s)...)
	}
	return out
}

func (l *lowerer) normalizeBlock(block *ast.BlockStatement) *ast.BlockStatement {
	if block == nil {
		return nil
	}
	return &ast.BlockStatement{Statements: l.normalizeList(block.Statements), Range: block.Range}
}

func (l *lowerer) normalizeStatement(s ast.Statement) []ast.Statement {
	if !statementHasAwait(s) {
		return []ast.Statement{s}
	}
	switch x := s.(type) {
	case *ast.ExpressionStatement:
		pre, e := l.normalizeExpression(x.Expr)
		return append(pre, &ast.ExpressionStatement{Expr: e, Range: x.Range})
	case *ast.LetStatement:
		pre, e := l.normalizeExpression(x.Value)
		return append(pre, &ast.LetStatement{Name: x.Name, Value: e, Range: x.Range})
	case *ast.AssignStatement:
		pre, e := l.normalizeExpression(x.Value)
		return append(pre, &ast.AssignStatement{Name: x.Name, Value: e, Range: x.Range})
	case *ast.IndexAssignStatement:
		pre, parts := l.normalizeSequence([]ast.Expression{x.Target.Left, x.Target.Index, x.Value})
		target := &ast.IndexExpression{Left: parts[0], Index: parts[1], Map: x.Target.Map, Range: x.Target.Range}
		return append(pre, &ast.IndexAssignStatement{Target: target, Value: parts[2], Nullable: x.Nullable, Range: x.Range})
	case *ast.ReturnStatement:
		pre, e := l.normalizeExpression(x.Value)
		return append(pre, &ast.ReturnStatement{Value: e, Range: x.Range})
	case *ast.IfStatement:
		pre, c := l.normalizeExpression(x.Condition)
		return append(pre, &ast.IfStatement{Condition: c, Consequence: l.normalizeBlock(x.Consequence), Alternative: l.normalizeBlock(x.Alternative), Range: x.Range})
	case *ast.WhileStatement:
		if expressionHasAwait(x.Condition) {
			// aida (c) { b }  ==>  aida (hai) { pre; moshi (!c') { nukeru; } b }
			pre, c := l.normalizeExpression(x.Condition)
			exit := &ast.IfStatement{
				Condition:   &ast.PrefixExpression{Operator: tokenBang, Right: &ast.GroupedExpression{Expr: c, Ranges: x.Condition.Span()}, Range: x.Condition.Span()},
				Consequence: &ast.BlockStatement{Statements: []ast.Statement{&ast.BreakStatement{Range: x.Condition.Span()}}, Range: x.Condition.Span()},
				Range:       x.Condition.Span(),
			}
			body := append(append(pre, exit), l.normalizeList(x.Body.Statements)...)
			return []ast.Statement{&ast.WhileStatement{
				Condition: &ast.BooleanLiteral{Value: true, Range: x.Condition.Span()},
				Body:      &ast.BlockStatement{Statements: body, Range: x.Body.Range},
				Range:     x.Range,
			}}
		}
		return []ast.Statement{&ast.WhileStatement{Condition: x.Condition, Body: l.normalizeBlock(x.Body), Range: x.Range}}
	case *ast.MatchStatement:
		pre, v := l.normalizeExpression(x.Value)
		arms := make([]ast.MatchArm, len(x.Arms))
		for i, arm := range x.Arms {
			arm.Body = l.normalizeBlock(arm.Body)
			arms[i] = arm
		}
		return append(pre, &ast.MatchStatement{Value: v, Arms: arms, Range: x.Range})
	}
	return []ast.Statement{s}
}

func isLiteral(e ast.Expression) bool {
	switch e.(type) {
	case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.RuneLiteral, *ast.StringLiteral, *ast.BooleanLiteral, *ast.NilLiteral:
		return true
	}
	return false
}

func (l *lowerer) spill(e ast.Expression) ([]ast.Statement, ast.Expression) {
	if isLiteral(e) {
		return nil, e
	}
	name := l.fresh(spillTemp)
	span := e.Span()
	return []ast.Statement{&ast.LetStatement{Name: ident(name, span), Value: e, Range: span}}, ident(name, span)
}

// normalizeSequence evaluates the expressions left to right: when a later one suspends, the earlier ones are evaluated first into temporaries.
func (l *lowerer) normalizeSequence(list []ast.Expression) ([]ast.Statement, []ast.Expression) {
	last := -1
	for i, e := range list {
		if expressionHasAwait(e) {
			last = i
		}
	}
	var pre []ast.Statement
	out := make([]ast.Expression, len(list))
	for i, e := range list {
		p, v := l.normalizeExpression(e)
		pre = append(pre, p...)
		if i < last {
			p, v = l.spill(v)
			pre = append(pre, p...)
		}
		out[i] = v
	}
	return pre, out
}

func (l *lowerer) normalizeExpression(e ast.Expression) ([]ast.Statement, ast.Expression) {
	if e == nil || !expressionHasAwait(e) {
		return nil, e
	}
	switch x := e.(type) {
	case *ast.AwaitExpression:
		pre, operand := l.normalizeExpression(x.Operand)
		task := l.fresh(taskTemp)
		value := l.fresh(valueTemp)
		span := x.Operand.Span()
		taskIdent := ident(task, span)
		pre = append(pre, &ast.LetStatement{Name: taskIdent, Value: operand, Range: x.Range})
		pre = append(pre, &ast.LetStatement{Name: ident(value, x.Range), Value: &ast.AwaitExpression{Operand: ident(task, span), Range: x.Range}, Range: x.Range})
		return pre, ident(value, x.Range)
	case *ast.GroupedExpression:
		pre, inner := l.normalizeExpression(x.Expr)
		return pre, &ast.GroupedExpression{Expr: inner, Ranges: x.Ranges}
	case *ast.PrefixExpression:
		pre, right := l.normalizeExpression(x.Right)
		return pre, &ast.PrefixExpression{Operator: x.Operator, Right: right, Range: x.Range}
	case *ast.InfixExpression:
		if (x.Operator == tokenAnd || x.Operator == tokenOr) && expressionHasAwait(x.Right) {
			// a && matsu b  ==>  kaku s = a; moshi (s) { s = (matsu b) }
			pre, left := l.normalizeExpression(x.Left)
			name := l.fresh(spillTemp)
			span := x.Range
			pre = append(pre, &ast.LetStatement{Name: ident(name, span), Value: left, Range: span})
			var guard ast.Expression = ident(name, span)
			if x.Operator == tokenOr {
				guard = &ast.PrefixExpression{Operator: tokenBang, Right: ident(name, span), Range: span}
			}
			assign := &ast.AssignStatement{Name: ident(name, span), Value: x.Right, Range: span}
			branch := &ast.BlockStatement{Statements: l.normalizeStatement(assign), Range: span}
			pre = append(pre, &ast.IfStatement{Condition: guard, Consequence: branch, Range: span})
			return pre, ident(name, span)
		}
		pre, parts := l.normalizeSequence([]ast.Expression{x.Left, x.Right})
		return pre, &ast.InfixExpression{Left: parts[0], Operator: x.Operator, Right: parts[1], Range: x.Range}
	case *ast.CallExpression:
		// The callee is a name in practice: it is hoisted only if it contains matsu itself and is never spilled.
		pre, fn := l.normalizeExpression(x.Function)
		more, parts := l.normalizeSequence(x.Arguments)
		call := *x
		call.Function = fn
		call.Arguments = parts
		return append(pre, more...), &call
	case *ast.TypeApplyExpression:
		pre, fn := l.normalizeExpression(x.Function)
		return pre, &ast.TypeApplyExpression{Function: fn, Args: x.Args, Range: x.Range}
	case *ast.ConversionExpression:
		pre, parts := l.normalizeSequence(x.Arguments)
		conv := *x
		conv.Arguments = parts
		return pre, &conv
	case *ast.ArrayLiteral:
		pre, parts := l.normalizeSequence(x.Elements)
		arr := *x
		arr.Elements = parts
		return pre, &arr
	case *ast.IndexExpression:
		pre, parts := l.normalizeSequence([]ast.Expression{x.Left, x.Index})
		return pre, &ast.IndexExpression{Left: parts[0], Index: parts[1], Map: x.Map, Range: x.Range}
	case *ast.FieldExpression:
		pre, left := l.normalizeExpression(x.Left)
		f := *x
		f.Left = left
		return pre, &f
	case *ast.StructLiteral:
		items := make([]ast.Expression, len(x.Fields))
		for i, f := range x.Fields {
			items[i] = f.Value
		}
		pre, parts := l.normalizeSequence(items)
		lit := *x
		lit.Fields = make([]ast.FieldValue, len(x.Fields))
		for i, f := range x.Fields {
			f.Value = parts[i]
			lit.Fields[i] = f
		}
		return pre, &lit
	case *ast.EnumLiteral:
		pre, parts := l.normalizeSequence(x.Values)
		lit := *x
		lit.Values = parts
		return pre, &lit
	}
	return nil, e
}

// ---------------------------------------------------------------- continuation-passing conversion

func isAwaitStep(s ast.Statement) (*ast.LetStatement, *ast.Identifier, bool) {
	if let, ok := s.(*ast.LetStatement); ok {
		if a, ok := let.Value.(*ast.AwaitExpression); ok {
			if id, ok := a.Operand.(*ast.Identifier); ok {
				return let, id, true
			}
		}
	}
	return nil, nil, false
}

// needsLowering reports whether a normalized statement contains a suspension point.
func needsLowering(s ast.Statement) bool {
	return statementHasAwait(s)
}

func (l *lowerer) cps(list []ast.Statement, k cont, lc *loopContext, span ast.Span) []ast.Statement {
	var out []ast.Statement
	for i, s := range list {
		rest := list[i+1:]
		if let, task, ok := isAwaitStep(s); ok {
			// async.then(__mio_self.core, t.core, waza () -> unit { async.check(t.core); kaku v = t.result[0]; rest })
			core := func() ast.Expression { return l.field(ident(task.Name, task.Range), "core", task.Range) }
			read := &ast.IndexExpression{
				Left:  l.field(ident(task.Name, task.Range), "result", let.Range),
				Index: &ast.IntegerLiteral{Value: 0, Raw: "0", Type: "", Range: let.Range},
				Range: let.Range,
			}
			inner := []ast.Statement{
				l.callStatement("check", []ast.Expression{core()}, let.Range),
				&ast.LetStatement{Name: let.Name, Value: read, Range: let.Range},
			}
			inner = append(inner, l.cps(rest, k, lc, span)...)
			next := thunk(inner, let.Range)
			out = append(out, l.callStatement("then", []ast.Expression{l.field(ident(selfName, let.Range), "core", let.Range), core(), next}, let.Range))
			return out
		}
		if !needsLowering(s) {
			out = append(out, l.rewriteSync(s, lc, 0)...)
			continue
		}
		switch x := s.(type) {
		case *ast.IfStatement:
			after, jump := l.continuation(rest, k, lc, x.Range)
			out = append(out, after...)
			out = append(out, &ast.IfStatement{
				Condition:   x.Condition,
				Consequence: &ast.BlockStatement{Statements: l.cps(x.Consequence.Statements, jump, lc, x.Range), Range: x.Consequence.Range},
				Alternative: l.alternative(x, jump, lc),
				Range:       x.Range,
			})
			return out
		case *ast.MatchStatement:
			after, jump := l.continuation(rest, k, lc, x.Range)
			out = append(out, after...)
			arms := make([]ast.MatchArm, len(x.Arms))
			for j, arm := range x.Arms {
				arm.Body = &ast.BlockStatement{Statements: l.cps(arm.Body.Statements, jump, lc, arm.Range), Range: arm.Body.Range}
				arms[j] = arm
			}
			out = append(out, &ast.MatchStatement{Value: x.Value, Arms: arms, Range: x.Range})
			return out
		case *ast.WhileStatement:
			return append(out, l.lowerLoop(x, rest, k, lc)...)
		default:
			out = append(out, s)
		}
	}
	return append(out, k()...)
}

func (l *lowerer) alternative(x *ast.IfStatement, jump cont, lc *loopContext) *ast.BlockStatement {
	if x.Alternative == nil {
		return &ast.BlockStatement{Statements: jump(), Range: x.Range}
	}
	return &ast.BlockStatement{Statements: l.cps(x.Alternative.Statements, jump, lc, x.Range), Range: x.Alternative.Range}
}

// continuation turns the statements after a branching statement into a closure `__mio_kN` (declared before the branch) and returns the
// declaration plus a cont that calls it. When nothing follows, the outer continuation is used directly.
func (l *lowerer) continuation(rest []ast.Statement, k cont, lc *loopContext, span ast.Span) ([]ast.Statement, cont) {
	if len(rest) == 0 {
		return nil, k
	}
	name := l.fresh(contTemp)
	body := l.cps(rest, k, lc, span)
	decl := &ast.LetStatement{Name: ident(name, span), Value: thunk(body, span), Range: span}
	return []ast.Statement{decl}, func() []ast.Statement {
		return []ast.Statement{&ast.ExpressionStatement{Expr: &ast.CallExpression{Function: ident(name, span), Range: span}, Range: span}}
	}
}

func (l *lowerer) lowerLoop(x *ast.WhileStatement, rest []ast.Statement, k cont, outer *loopContext) []ast.Statement {
	span := x.Range
	name := l.fresh(loopTemp)
	var out []ast.Statement
	exitDecl, exit := l.continuation(rest, k, outer, span)
	out = append(out, exitDecl...)
	// kaku __mio_lN: []() -> unit = [];  the cell lets the body closure refer to itself.
	cellType := &ast.TypeRef{Name: "[]", Element: thunkType(span), Range: span}
	cellName := ident(name, span)
	cellName.DeclaredType = cellType
	out = append(out, &ast.LetStatement{Name: cellName, Value: &ast.ArrayLiteral{ElementType: "", Range: span}, Range: span})
	again := func(at ast.Span) []ast.Statement {
		entry := &ast.IndexExpression{Left: ident(name, at), Index: &ast.IntegerLiteral{Value: 0, Magnitude: 0, Raw: "0", Range: at}, Range: at}
		return []ast.Statement{l.callStatement("again", []ast.Expression{l.field(ident(selfName, at), "core", at), entry}, at)}
	}
	inner := &loopContext{
		breakWith: func(at ast.Span) []ast.Statement {
			return append(exit(), &ast.ReturnStatement{Range: at})
		},
		continueWith: func(at ast.Span) []ast.Statement {
			return append(again(at), &ast.ReturnStatement{Range: at})
		},
	}
	body := l.cps(x.Body.Statements, func() []ast.Statement { return again(span) }, inner, span)
	guard := &ast.IfStatement{
		Condition:   x.Condition,
		Consequence: &ast.BlockStatement{Statements: body, Range: x.Body.Range},
		Alternative: &ast.BlockStatement{Statements: exit(), Range: span},
		Range:       span,
	}
	bodyDecl := l.fresh(contTemp)
	out = append(out, &ast.LetStatement{Name: ident(bodyDecl, span), Value: thunk([]ast.Statement{guard}, span), Range: span})
	out = append(out, l.callStatement("push_thunk", []ast.Expression{ident(name, span), ident(bodyDecl, span)}, span))
	out = append(out, again(span)...)
	return out
}

// rewriteSync translates the statements of a suspension-free statement that leave the async function body: kaesu completes the task and
// returns from the current closure; nukeru and tsuzuku that target a lowered loop jump through its closures. Function literals are not
// entered.
func (l *lowerer) rewriteSync(s ast.Statement, lc *loopContext, whileDepth int) []ast.Statement {
	switch x := s.(type) {
	case *ast.ReturnStatement:
		if x.Value == nil {
			l.failAt(CodeInvalidDecl, "hidouki 函数的 kaesu 必须带有结果值", x.Range.Start, len("kaesu"))
			return []ast.Statement{s}
		}
		// complete[T]: the result type is explicit so generic async functions (where inference cannot see through type parameters) lower too.
		complete := &ast.ExpressionStatement{Expr: l.runtimeCall("complete", []*ast.TypeRef{l.result}, []ast.Expression{ident(selfName, x.Range), x.Value}, x.Range), Range: x.Range}
		return []ast.Statement{complete, &ast.ReturnStatement{Range: x.Range}}
	case *ast.BreakStatement:
		if lc != nil && whileDepth == 0 {
			return lc.breakWith(x.Range)
		}
	case *ast.ContinueStatement:
		if lc != nil && whileDepth == 0 {
			return lc.continueWith(x.Range)
		}
	case *ast.IfStatement:
		if !statementTouchesControl(x) {
			return []ast.Statement{s}
		}
		out := &ast.IfStatement{Condition: x.Condition, Consequence: l.rewriteBlock(x.Consequence, lc, whileDepth), Range: x.Range}
		if x.Alternative != nil {
			out.Alternative = l.rewriteBlock(x.Alternative, lc, whileDepth)
		}
		return []ast.Statement{out}
	case *ast.WhileStatement:
		if !statementTouchesControl(x) {
			return []ast.Statement{s}
		}
		return []ast.Statement{&ast.WhileStatement{Condition: x.Condition, Body: l.rewriteBlock(x.Body, lc, whileDepth+1), Range: x.Range}}
	case *ast.MatchStatement:
		if !statementTouchesControl(x) {
			return []ast.Statement{s}
		}
		arms := make([]ast.MatchArm, len(x.Arms))
		for i, arm := range x.Arms {
			arm.Body = l.rewriteBlock(arm.Body, lc, whileDepth)
			arms[i] = arm
		}
		return []ast.Statement{&ast.MatchStatement{Value: x.Value, Arms: arms, Range: x.Range}}
	}
	return []ast.Statement{s}
}

func (l *lowerer) rewriteBlock(b *ast.BlockStatement, lc *loopContext, whileDepth int) *ast.BlockStatement {
	var out []ast.Statement
	for _, s := range b.Statements {
		out = append(out, l.rewriteSync(s, lc, whileDepth)...)
	}
	return &ast.BlockStatement{Statements: out, Range: b.Range}
}
