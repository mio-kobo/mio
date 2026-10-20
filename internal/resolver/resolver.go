package resolver

import (
	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/token"
	"fmt"
)

// 名称解析器
type Resolver struct {
	scope         *Scope
	functionDepth int
	loopDepth     int

	bindings map[*ast.Identifier]Symbol
	nextID   uint64

	// lenient 为真时收集可恢复错误并继续解析后续语句。
	// 这是 LSP 处理未完成源码所需的只读扩展，编译器仍然使用严格模式。
	lenient bool
	errors  []error
}

// ResolveProgramLenient 解析程序并收集可恢复错误。
// 与 ResolveProgram 不同，单个语句失败不会中止后续语句，
// 已建立的绑定仍然可信；调用方可用 Errors 取得全部错误。
func (r *Resolver) ResolveProgramLenient(program *ast.Program) []error {
	r.lenient = true
	r.errors = nil
	defer func() { r.lenient = false }()
	if err := r.resolveStatements(program.Statements, true); err != nil {
		// 严格模式下不会走到这里；保留兜底以防未来新增不可恢复错误。
		r.errors = append(r.errors, err)
	}
	return r.errors
}

// Errors 返回宽松解析收集到的错误。
func (r *Resolver) Errors() []error {
	return r.errors
}

// recoverable 记录错误并返回是否继续执行。
func (r *Resolver) recoverable(err error) bool {
	if err == nil {
		return true
	}
	if !r.lenient {
		return false
	}
	r.errors = append(r.errors, err)
	return true
}

func (r *Resolver) symbol(name string, kind SymbolKind, decl token.Position) Symbol {
	r.nextID++
	return Symbol{ID: r.nextID, Name: name, Kind: kind, Decl: decl}
}

// 创建名称解析器
func New() *Resolver {
	// 创建内置符号作用域
	builtins := NewScope()
	// 注册内置函数
	for _, name := range []string{"chant", "len", "Map", "$math.sqrt", "$math.floor", "$math.ceil", "$math.round_even", "$math.trunc", "$math.is_nan", "$math.is_inf", "$math.is_finite", "$math.is_negative_zero", "$math.add_overflows", "$math.is_negative", "$math.neg_overflows", "$math.error", "$array.make", "$array.with_capacity", "$array.push", "$array.capacity", "$array.reserve", "$array.clone", "$array.truncate", "$array.append_all", "$data.string_to_bytes", "$data.bytes_to_string", "$data.string_byte_len"} {
		if err := builtins.DefineSymbol(Symbol{
			Name: name,
			Kind: SymbolBuiltin,
		}); err != nil {
			panic(err)
		}
	}

	// 创建用户程序全局作用域
	globals := NewEnclosedScope(builtins)

	return &Resolver{
		scope:    globals,
		bindings: make(map[*ast.Identifier]Symbol),
	}
}

// 解析程序中的名称引用
func (r *Resolver) ResolveProgram(program *ast.Program) error {
	return r.resolveStatements(program.Statements, true)
}

// 解析语句中的符号定义与引用
func (r *Resolver) resolveStatement(stmt ast.Statement) error {
	switch n := stmt.(type) {

	// kaku 变量声明
	case *ast.LetStatement:
		recursiveClosure := false
		if _, ok := n.Value.(*ast.FunctionLiteral); ok && n.Name.DeclaredType != nil {
			symbol := r.symbol(n.Name.Name, SymbolVariable, n.Name.Span().Start)
			if err := r.scope.DefineSymbol(symbol); err != nil {
				return duplicateDiagnostic(r.scope, n.Name, err)
			}
			r.bindings[n.Name] = symbol
			recursiveClosure = true
		}
		// 先检查初始化表达式
		if err := r.resolveExpression(n.Value); err != nil {
			return err
		}
		if recursiveClosure {
			return nil
		}

		// 构造变量符号
		symbol := r.symbol(n.Name.Name, SymbolVariable, n.Name.Span().Start)

		// 定义到当前作用域
		if err := r.scope.DefineSymbol(symbol); err != nil {
			return duplicateDiagnostic(r.scope, n.Name, err)
		}

		// 保存声明节点的符号绑定
		r.bindings[n.Name] = symbol

		return nil

	case *ast.AssignStatement:
		// 检查赋值目标是否已经声明
		symbol, ok := r.scope.Resolve(n.Name.Name)

		if !ok {
			cause := fmt.Errorf(
				"undefined assignment target %q at %d:%d",
				n.Name.Name,
				n.Name.Span().Start.Line,
				n.Name.Span().Start.Column,
			)
			return diagnostic.NewError("E0201", "未定义的赋值目标", diagnostic.FromPositions(n.Name.Span().Start, n.Name.Span().End)).
				WithHelp("请先使用 kaku 声明该变量").WithCause(cause)
		}

		// 保存赋值目标的符号绑定
		r.bindings[n.Name] = symbol

		return r.resolveExpression(n.Value)

	case *ast.IndexAssignStatement:
		if err := r.resolveExpression(n.Target); err != nil {
			return err
		}
		return r.resolveExpression(n.Value)

	case *ast.IfStatement:
		// 条件表达式在当前作用域中解析
		if err := r.resolveExpression(n.Condition); err != nil {
			return err
		}

		// 解析 moshi 分支
		if err := r.resolveBlock(n.Consequence); err != nil {
			return err
		}

		// 解析可选的 sore 分支
		if n.Alternative != nil {
			return r.resolveBlock(n.Alternative)
		}
		return nil

	case *ast.WhileStatement:
		if err := r.resolveExpression(n.Condition); err != nil {
			return err
		}
		r.loopDepth++
		defer func() { r.loopDepth-- }()
		return r.resolveBlock(n.Body)

	case *ast.BreakStatement:
		if r.loopDepth == 0 {
			pos := n.Span().Start
			cause := fmt.Errorf("nukeru outside loop at %d:%d", pos.Line, pos.Column)
			return diagnostic.NewError("E0203", "nukeru 只能在循环内使用", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		return nil

	case *ast.ContinueStatement:
		if r.loopDepth == 0 {
			pos := n.Span().Start
			cause := fmt.Errorf("tsuzuku outside loop at %d:%d", pos.Line, pos.Column)
			return diagnostic.NewError("E0203", "tsuzuku 只能在循环内使用", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		return nil

	case *ast.StructStatement, *ast.EnumStatement:
		return nil

	case *ast.MatchStatement:
		if err := r.resolveExpression(n.Value); err != nil {
			return err
		}
		for i := range n.Arms {
			parent := r.scope
			r.scope = NewEnclosedScope(parent)
			for _, binding := range n.Arms[i].Bindings {
				symbol := r.symbol(binding.Name, SymbolVariable, binding.Span().Start)
				if err := r.scope.DefineSymbol(symbol); err != nil {
					r.scope = parent
					return duplicateDiagnostic(r.scope, binding, err)
				}
				r.bindings[binding] = symbol
			}
			err := r.resolveStatements(n.Arms[i].Body.Statements, false)
			r.scope = parent
			if err != nil {
				return err
			}
		}
		return nil

	case *ast.ExpressionStatement:
		return r.resolveExpression(n.Expr)

		// kaesu 返回语句
	case *ast.ReturnStatement:
		// 返回语句只能位于函数内部
		if r.functionDepth == 0 {
			pos := n.Span().Start

			cause := fmt.Errorf(
				"kaesu outside function at %d:%d",
				pos.Line,
				pos.Column,
			)
			return diagnostic.NewError("E0204", "kaesu 只能在函数内使用", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}

		// 无返回表达式时无需解析名称
		if n.Value == nil {
			return nil
		}

		// 检查返回表达式中的名称
		return r.resolveExpression(n.Value)

	// waza 函数定义
	case *ast.FunctionStatement:
		return r.resolveFunctionStatement(n)

	default:
		return fmt.Errorf(
			"unsupported statement %T",
			stmt,
		)
	}
}

func (r *Resolver) resolveExpression(expr ast.Expression) error {
	switch n := expr.(type) {

	// 标识符
	// 标识符引用
	case *ast.Identifier:
		// 查找当前作用域及其父作用域
		symbol, ok := r.scope.Resolve(n.Name)

		if !ok {
			pos := n.Span().Start
			cause := fmt.Errorf(
				"undefined identifier %q at %d:%d",
				n.Name,
				pos.Line,
				pos.Column,
			)
			return diagnostic.NewError("E0201", fmt.Sprintf("未定义的变量 %q", n.Name), diagnostic.FromPositions(n.Span().Start, n.Span().End)).
				WithHelp("请检查名称拼写或在当前作用域中声明它").WithCause(cause)
		}

		// 记录标识符对应的符号
		r.bindings[n] = symbol

		return nil
	// 中缀表达式
	case *ast.InfixExpression:
		if err := r.resolveExpression(n.Left); err != nil {
			return err
		}
		return r.resolveExpression(n.Right)

	// 一元表达式
	case *ast.PrefixExpression:
		return r.resolveExpression(n.Right)

	// 括号分组表达式
	case *ast.GroupedExpression:
		return r.resolveExpression(n.Expr)

	// 函数调用表达式
	case *ast.CallExpression:
		// 检查被调用的名称或表达式
		if err := r.resolveExpression(n.Function); err != nil {
			return err
		}

		// 递归检查每个实参
		for _, arg := range n.Arguments {
			if err := r.resolveExpression(arg); err != nil {
				return err
			}
		}
		return nil

	case *ast.TypeApplyExpression:
		return r.resolveExpression(n.Function)

	case *ast.ConversionExpression:
		for _, arg := range n.Arguments {
			if err := r.resolveExpression(arg); err != nil {
				return err
			}
		}
		return nil

	case *ast.ArrayLiteral:
		for _, element := range n.Elements {
			if err := r.resolveExpression(element); err != nil {
				return err
			}
		}
		return nil

	case *ast.IndexExpression:
		if err := r.resolveExpression(n.Left); err != nil {
			return err
		}
		return r.resolveExpression(n.Index)

	case *ast.FieldExpression:
		return r.resolveExpression(n.Left)

	case *ast.StructLiteral:
		for _, field := range n.Fields {
			if err := r.resolveExpression(field.Value); err != nil {
				return err
			}
		}
		return nil

	case *ast.EnumLiteral:
		for _, value := range n.Values {
			if err := r.resolveExpression(value); err != nil {
				return err
			}
		}
		return nil

	case *ast.FunctionLiteral:
		return r.resolveFunctionLiteral(n)

	// 基础字面量
	case *ast.IntegerLiteral,
		*ast.RuneLiteral,
		*ast.FloatLiteral,
		*ast.StringLiteral,
		*ast.BooleanLiteral,
		*ast.NilLiteral:
		return nil

	default:
		return fmt.Errorf(
			"unsupported expression %T",
			expr,
		)
	}
}

func (r *Resolver) resolveFunctionLiteral(fn *ast.FunctionLiteral) error {
	parent := r.scope
	outerLoopDepth := r.loopDepth
	r.scope = NewEnclosedScope(parent)
	r.loopDepth = 0
	r.functionDepth++
	defer func() {
		r.functionDepth--
		r.loopDepth = outerLoopDepth
		r.scope = parent
	}()
	for _, param := range fn.Parameters {
		symbol := r.symbol(param.Name, SymbolParameter, param.Span().Start)
		if err := r.scope.DefineSymbol(symbol); err != nil {
			return duplicateDiagnostic(r.scope, param, err)
		}
		r.bindings[param] = symbol
	}
	return r.resolveStatements(fn.Body.Statements, false)
}

// 解析代码块中的局部作用域
func (r *Resolver) resolveBlock(block *ast.BlockStatement) error {
	// 保存当前作用域
	parent := r.scope

	// 创建代码作用域
	r.scope = NewEnclosedScope(parent)

	// 离开代码块恢复作用域
	defer func() {
		r.scope = parent
	}()

	return r.resolveStatements(block.Statements, false)
}

func (r *Resolver) resolveFunctionStatement(fn *ast.FunctionStatement) error {
	// 函数名称已经在 resolveStatements 中预声明

	// 保存外层作用域
	parent := r.scope
	outerLoopDepth := r.loopDepth

	// 创建函数作用域
	r.scope = NewEnclosedScope(parent)
	r.loopDepth = 0

	// 进入函数上下文
	r.functionDepth++

	// 离开函数时恢复作用域与上下文
	defer func() {
		r.functionDepth--
		r.loopDepth = outerLoopDepth
		r.scope = parent
	}()

	// 定义函数形参
	// 定义函数形参
	for _, param := range fn.Parameters {
		symbol := r.symbol(param.Name, SymbolParameter, param.Span().Start)

		if err := r.scope.DefineSymbol(symbol); err != nil {
			return duplicateDiagnostic(r.scope, param, err)
		}

		// 保存形参声明节点的绑定
		r.bindings[param] = symbol
	}

	// 解析函数体中的语句
	return r.resolveStatements(fn.Body.Statements, false)
}

// 解析统一作用域中的语句
func (r *Resolver) resolveStatements(statements []ast.Statement, allowFunctions bool) error {
	for _, stmt := range statements {
		var name *ast.Identifier
		switch declaration := stmt.(type) {
		case *ast.StructStatement:
			name = declaration.Name
		case *ast.EnumStatement:
			name = declaration.Name
		}
		if name == nil {
			continue
		}
		if !allowFunctions {
			err := diagnostic.NewError("E0205", "不支持嵌套类型声明", diagnostic.FromPositions(name.Span().Start, name.Span().End)).
				WithCause(fmt.Errorf("nested type declaration %q is not supported", name.Name))
			if !r.recoverable(err) {
				return err
			}
			continue
		}
		symbol := r.symbol(name.Name, SymbolType, name.Span().Start)
		if err := r.scope.DefineSymbol(symbol); err != nil {
			err = duplicateDiagnostic(r.scope, name, err)
			if !r.recoverable(err) {
				return err
			}
			continue
		}
		r.bindings[name] = symbol
	}
	// 预声明函数名称
	// 预声明当前作用域中的函数
	for _, stmt := range statements {
		fn, ok := stmt.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		if !allowFunctions {
			pos := fn.Name.Span().Start
			cause := fmt.Errorf(
				"nested function declaration %q is not supported at %d:%d",
				fn.Name.Name,
				pos.Line,
				pos.Column,
			)
			err := diagnostic.NewError("E0205", "不支持嵌套函数声明", diagnostic.FromPositions(fn.Name.Span().Start, fn.Name.Span().End)).WithCause(cause)
			if !r.recoverable(err) {
				return err
			}
			continue
		}

		// 构造函数符号
		symbol := r.symbol(fn.Name.Name, SymbolFunction, fn.Name.Span().Start)

		// 注册函数名称
		if err := r.scope.DefineSymbol(symbol); err != nil {
			err = duplicateDiagnostic(r.scope, fn.Name, err)
			if !r.recoverable(err) {
				return err
			}
			continue
		}

		// 保存函数声明节点的绑定
		r.bindings[fn.Name] = symbol
	}

	// 按顺序解析所有语句
	for _, stmt := range statements {
		if err := r.resolveStatement(stmt); err != nil {
			if !r.recoverable(err) {
				return err
			}
		}
	}

	return nil
}

// 获取标识符绑定的符号
func (r *Resolver) BindingOf(ident *ast.Identifier) (Symbol, bool) {
	symbol, ok := r.bindings[ident]
	return symbol, ok
}

func duplicateDiagnostic(scope *Scope, ident *ast.Identifier, cause error) error {
	d := diagnostic.NewError("E0202", fmt.Sprintf("重复声明 %q", ident.Name), diagnostic.FromPositions(ident.Span().Start, ident.Span().End)).
		WithHelp("请重命名或移除重复声明").WithCause(cause)
	if previous, ok := scope.ResolveLocal(ident.Name); ok && previous.Decl.Line > 0 {
		end := previous.Decl
		end.Offset += len(ident.Name)
		end.Column += len([]rune(ident.Name))
		d.WithLabel(diagnostic.FromPositions(previous.Decl, end), "首次声明在这里")
	}
	return d
}
