package typecheck

import (
	"fmt"

	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/resolver"
)

// FunctionCheckCache 保存已通过的函数体检查结果
type FunctionCheckCache interface {
	HasFunctionCheck(key string) bool
	StoreFunctionCheck(key string)
}

// IncrementalStats 记录函数体检查复用情况
type IncrementalStats struct {
	Hits       int
	Recomputed int
}

// CheckProgram 对完成名称解析的程序执行类型检查
func (c *Checker) CheckProgram(program *ast.Program) error {
	_, err := c.CheckProgramIncremental(program, nil, nil)
	return err
}

// CheckProgramIncremental 对可复用的函数体跳过重复检查
func (c *Checker) CheckProgramIncremental(program *ast.Program, keys map[*ast.FunctionStatement]string, cache FunctionCheckCache) (IncrementalStats, error) {
	var stats IncrementalStats
	if c.resolver == nil {
		return stats, fmt.Errorf("type checker requires a name resolver")
	}

	if program == nil {
		return stats, fmt.Errorf("cannot type-check a nil program")
	}
	if err := c.registerTypeDeclarations(program); err != nil {
		return stats, err
	}

	for _, stmt := range program.Statements {
		fn, ok := stmt.(*ast.FunctionStatement)
		if !ok {
			continue
		}
		if err := c.registerFunctionSignature(fn); err != nil {
			return stats, err
		}
	}

	for _, stmt := range program.Statements {
		fn, ok := stmt.(*ast.FunctionStatement)
		if !ok {
			if _, typeDeclaration := stmt.(*ast.StructStatement); typeDeclaration {
				continue
			}
			if _, typeDeclaration := stmt.(*ast.EnumStatement); typeDeclaration {
				continue
			}
			if err := c.checkStatement(stmt); err != nil {
				return stats, err
			}
			continue
		}
		if fn.Native {
			continue
		}

		key := keys[fn]
		if cache != nil && key != "" && cache.HasFunctionCheck(key) {
			stats.Hits++
			continue
		}
		stats.Recomputed++
		if err := c.checkFunction(fn); err != nil {
			return stats, err
		}
		if cache != nil && key != "" {
			cache.StoreFunctionCheck(key)
		}
	}

	return stats, nil
}

// checkStatements 先注册函数签名，再检查语句
func (c *Checker) checkStatements(statements []ast.Statement) error {
	// 第一遍：预注册当前作用域的函数签名
	for _, stmt := range statements {
		fn, ok := stmt.(*ast.FunctionStatement)
		if !ok {
			continue
		}

		if err := c.registerFunctionSignature(fn); err != nil {
			return err
		}
	}

	// 第二遍：检查语句及函数体
	for _, stmt := range statements {
		if err := c.checkStatement(stmt); err != nil {
			return err
		}
	}

	return nil
}

// checkStatement 检查语句的静态类型约束
func (c *Checker) checkStatement(stmt ast.Statement) error {
	switch n := stmt.(type) {

	// 变量声明
	case *ast.LetStatement:
		declared := KindInvalid
		var err error
		if n.Name.DeclaredType != nil {
			declared, err = c.resolveType(n.Name.DeclaredType, c.activeTypeParameters())
			if err != nil {
				return err
			}
		}
		if _, recursiveClosure := n.Value.(*ast.FunctionLiteral); recursiveClosure && declared.IsFunction() {
			if symbol, ok := c.resolver.BindingOf(n.Name); ok {
				c.types[symbol] = declared
			}
		}
		kind, err := c.checkExpressionExpected(n.Value, declared)
		if err != nil {
			return err
		}

		// 暂不允许从 mu 推断变量类型
		if kind == KindNil && declared == KindInvalid {
			return fmt.Errorf(
				"cannot infer type of %q from nil",
				n.Name.Name,
			)
		}

		// 不允许将无返回值表达式保存为变量
		if kind == KindUnit {
			return fmt.Errorf(
				"cannot infer type of %q from unit",
				n.Name.Name,
			)
		}
		if declared != KindInvalid && !isAssignable(declared, kind) {
			cause := fmt.Errorf("cannot initialize %q as %s with %s", n.Name.Name, declared, kind)
			return typeDiagnostic("E0301", fmt.Sprintf("变量 %q 的初始化类型不正确", n.Name.Name), n.Value.Span(), declared.String(), kind.String(), cause)
		}

		symbol, ok := c.resolver.BindingOf(n.Name)
		if !ok {
			return fmt.Errorf(
				"missing symbol binding for declaration %q",
				n.Name.Name,
			)
		}

		if declared != KindInvalid {
			kind = declared
		}
		c.types[symbol] = kind
		if c.functionDepth == 0 {
			c.globals[symbol] = struct{}{}
		}
		return nil

	// 变量赋值
	case *ast.AssignStatement:
		symbol, ok := c.resolver.BindingOf(n.Name)
		if !ok {
			return fmt.Errorf(
				"missing symbol binding for assignment %q",
				n.Name.Name,
			)
		}

		if symbol.Kind != resolver.SymbolVariable {
			return fmt.Errorf(
				"cannot assign to %s %q",
				symbol.Kind,
				n.Name.Name,
			)
		}

		expected, ok := c.types[symbol]
		if !ok {
			return fmt.Errorf(
				"type of assignment target %q is not available",
				n.Name.Name,
			)
		}

		actual, err := c.checkExpressionExpected(n.Value, expected)
		if err != nil {
			return err
		}

		if !isAssignable(expected, actual) {
			pos := n.Name.Span().Start
			cause := fmt.Errorf(
				"cannot assign %s to %q (declared %s) at %d:%d",
				actual,
				n.Name.Name,
				expected,
				pos.Line,
				pos.Column,
			)
			return typeDiagnostic("E0301", fmt.Sprintf("不能将 %s 赋给 %q", actual, n.Name.Name), n.Value.Span(), expected.String(), actual.String(), cause)
		}
		c.factVersions[symbol]++
		delete(c.narrowings, symbol)
		if expected.IsNullable() && actual == expected.NonNullable() {
			c.narrowings[symbol] = actual
		}

		return nil

	case *ast.IndexAssignStatement:
		targetType, err := c.checkExpression(n.Target.Left)
		if err != nil {
			return err
		}
		if targetType.IsMap() {
			_, args, _ := targetType.NamedParts()
			keyType, err := c.checkExpressionExpected(n.Target.Index, args[0])
			if err != nil {
				return err
			}
			if !isAssignable(args[0], keyType) {
				return typeDiagnostic("E0301", "Map 键类型不一致", n.Target.Index.Span(), args[0].String(), keyType.String(), fmt.Errorf("map key requires %s, got %s", args[0], keyType))
			}
			actual, err := c.checkExpressionExpected(n.Value, args[1])
			if err != nil {
				return err
			}
			if !isAssignable(args[1], actual) {
				return typeDiagnostic("E0301", "Map 值类型不一致", n.Value.Span(), args[1].String(), actual.String(), fmt.Errorf("map value requires %s, got %s", args[1], actual))
			}
			n.Target.Map = true
			return nil
		}
		if !targetType.IsArray() || targetType.IsNullable() {
			cause := fmt.Errorf("index assignment target must be an array, got %s", targetType)
			return typeDiagnostic("E0305", "只能对数组元素赋值", n.Target.Left.Span(), "array", targetType.String(), cause)
		}
		indexType, err := c.checkExpression(n.Target.Index)
		if err != nil {
			return err
		}
		if indexType != KindInt {
			cause := fmt.Errorf("array index must be int64, got %s", indexType)
			return typeDiagnostic("E0305", "数组索引必须是 int64", n.Target.Index.Span(), KindInt.String(), indexType.String(), cause)
		}
		actual, err := c.checkExpressionExpected(n.Value, targetType.ElementKind())
		if err != nil {
			return err
		}
		expected := targetType.ElementKind()
		if !isAssignable(expected, actual) {
			cause := fmt.Errorf("array element assignment requires %s, got %s", expected, actual)
			return typeDiagnostic("E0301", "数组元素赋值类型不一致", n.Value.Span(), expected.String(), actual.String(), cause)
		}
		n.Nullable = expected.IsNullable()
		return nil

	// 表达式语句
	case *ast.ExpressionStatement:
		_, err := c.checkExpression(n.Expr)
		return err

	// moshi / sore 条件语句
	case *ast.IfStatement:
		conditionType, err := c.checkExpression(n.Condition)
		if err != nil {
			return err
		}

		if conditionType != KindBool {
			pos := n.Condition.Span().Start

			cause := fmt.Errorf(
				"moshi condition requires bool, got %s at %d:%d",
				conditionType,
				pos.Line,
				pos.Column,
			)
			return typeDiagnostic("E0301", "moshi 条件必须是 bool", n.Condition.Span(), KindBool.String(), conditionType.String(), cause)
		}

		symbol, nonNilWhenTrue, canNarrow := c.conditionNarrowing(n.Condition)
		consequenceVersion := c.factVersions[symbol]
		if err := c.checkBlockWithNarrowing(n.Consequence, symbol, canNarrow && nonNilWhenTrue); err != nil {
			return err
		}
		consequenceUnchanged := c.factVersions[symbol] == consequenceVersion

		alternativeUnchanged := true
		if n.Alternative != nil {
			alternativeVersion := c.factVersions[symbol]
			if err := c.checkBlockWithNarrowing(n.Alternative, symbol, canNarrow && !nonNilWhenTrue); err != nil {
				return err
			}
			alternativeUnchanged = c.factVersions[symbol] == alternativeVersion
		}
		if canNarrow {
			if blockAlwaysReturns(n.Consequence) && n.Alternative == nil && !nonNilWhenTrue {
				c.narrowings[symbol] = c.types[symbol].NonNullable()
			} else if n.Alternative != nil && blockAlwaysReturns(n.Consequence) && !blockAlwaysReturns(n.Alternative) && !nonNilWhenTrue && alternativeUnchanged {
				c.narrowings[symbol] = c.types[symbol].NonNullable()
			} else if n.Alternative != nil && blockAlwaysReturns(n.Alternative) && !blockAlwaysReturns(n.Consequence) && nonNilWhenTrue && consequenceUnchanged {
				c.narrowings[symbol] = c.types[symbol].NonNullable()
			}
		}

		return nil

	case *ast.WhileStatement:
		conditionType, err := c.checkExpression(n.Condition)
		if err != nil {
			return err
		}
		if conditionType != KindBool {
			pos := n.Condition.Span().Start
			cause := fmt.Errorf(
				"aida condition requires bool, got %s at %d:%d",
				conditionType,
				pos.Line,
				pos.Column,
			)
			return typeDiagnostic("E0301", "aida 条件必须是 bool", n.Condition.Span(), KindBool.String(), conditionType.String(), cause)
		}
		symbol, nonNilWhenTrue, canNarrow := c.conditionNarrowing(n.Condition)
		c.loopDepth++
		defer func() { c.loopDepth-- }()
		return c.checkBlockWithNarrowing(n.Body, symbol, canNarrow && nonNilWhenTrue)

	case *ast.BreakStatement:
		if c.loopDepth == 0 {
			cause := fmt.Errorf("nukeru outside loop")
			return diagnostic.NewError("E0203", "nukeru 只能在循环内使用", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		return nil

	case *ast.ContinueStatement:
		if c.loopDepth == 0 {
			cause := fmt.Errorf("tsuzuku outside loop")
			return diagnostic.NewError("E0203", "tsuzuku 只能在循环内使用", diagnostic.FromPositions(n.Span().Start, n.Span().End)).WithCause(cause)
		}
		return nil

	case *ast.StructStatement, *ast.EnumStatement:
		return nil

	case *ast.MatchStatement:
		valueType, err := c.checkExpression(n.Value)
		if err != nil {
			return err
		}
		if valueType.IsNullable() {
			return fmt.Errorf("awase requires a non-null enum, got %s", valueType)
		}
		name, args, ok := valueType.NamedParts()
		definition, exists := c.enums[name]
		if !ok || !exists {
			return fmt.Errorf("awase requires an enum value, got %s", valueType)
		}
		bindings := make(map[string]Kind, len(definition.TypeParams))
		for i, parameter := range definition.TypeParams {
			bindings[parameter] = args[i]
		}
		seen := make(map[string]struct{})
		for armIndex := range n.Arms {
			arm := &n.Arms[armIndex]
			variantIndex := -1
			var variant EnumVariant
			for i, candidate := range definition.Variants {
				if candidate.Name == arm.Variant {
					variantIndex = i
					variant = candidate
					break
				}
			}
			if variantIndex < 0 {
				return fmt.Errorf("enum %s has no variant %s", name, arm.Variant)
			}
			if _, duplicate := seen[arm.Variant]; duplicate {
				return fmt.Errorf("duplicate baai %s", arm.Variant)
			}
			seen[arm.Variant] = struct{}{}
			if len(arm.Bindings) != len(variant.Fields) {
				return fmt.Errorf("variant %s expects %d bindings, got %d", arm.Variant, len(variant.Fields), len(arm.Bindings))
			}
			arm.Tag = variantIndex
			for i, binding := range arm.Bindings {
				symbol, ok := c.resolver.BindingOf(binding)
				if !ok {
					return fmt.Errorf("missing match binding %q", binding.Name)
				}
				c.types[symbol] = substitute(variant.Fields[i], bindings)
			}
			if err := c.checkBlock(arm.Body); err != nil {
				return err
			}
		}
		if len(seen) != len(definition.Variants) {
			missing := make([]string, 0)
			for _, variant := range definition.Variants {
				if _, ok := seen[variant.Name]; !ok {
					missing = append(missing, variant.Name)
				}
			}
			return fmt.Errorf("non-exhaustive awase for %s; missing %v", name, missing)
		}
		return nil

	// 函数定义
	case *ast.FunctionStatement:
		return c.checkFunction(n)

		// kaesu 返回语句
	case *ast.ReturnStatement:
		if len(c.returnTypes) == 0 {
			return fmt.Errorf("kaesu outside function")
		}

		// 获取当前函数的返回类型
		expected := c.returnTypes[len(c.returnTypes)-1]

		// 没有返回表达式时视为 unit
		actual := KindUnit

		if n.Value != nil {
			kind, err := c.checkExpressionExpected(n.Value, expected)
			if err != nil {
				return err
			}

			actual = kind
		}

		// 检查声明类型与实际返回类型
		if !isAssignable(expected, actual) {
			pos := n.Span().Start

			cause := fmt.Errorf(
				"return type mismatch: expected %s, got %s at %d:%d",
				expected,
				actual,
				pos.Line,
				pos.Column,
			)
			span := n.Span()
			if n.Value != nil {
				span = n.Value.Span()
			}
			return typeDiagnostic("E0304", "函数返回值类型不正确", span, expected.String(), actual.String(), cause)
		}

		// unit 函数不允许返回具体表达式
		if expected == KindUnit && n.Value != nil {
			pos := n.Span().Start

			return fmt.Errorf(
				"unit function cannot return a value at %d:%d",
				pos.Line,
				pos.Column,
			)
		}

		return nil

	default:
		return fmt.Errorf(
			"unsupported statement %T",
			stmt,
		)
	}
}

// checkFunction 检查函数形参、函数体与返回类型
func (c *Checker) checkFunction(fn *ast.FunctionStatement) error {
	// 获取预注册的函数签名
	signature, ok := c.SignatureOf(fn.Name)
	if !ok {
		return fmt.Errorf(
			"missing function signature for %q",
			fn.Name.Name,
		)
	}

	if len(signature.ParameterTypes) != len(fn.Parameters) {
		return fmt.Errorf(
			"incorrect parameter count in signature for %q",
			fn.Name.Name,
		)
	}

	savedNarrowings := c.narrowings
	savedVersions := copyVersions(c.factVersions)
	c.narrowings = make(map[resolver.Symbol]Kind)
	c.functionDepth++
	c.typeParameterScopes = append(c.typeParameterScopes, parameterSet(signature.TypeParams))
	c.constraintScopes = append(c.constraintScopes, signature.Constraints)
	defer func() {
		c.functionDepth--
		c.typeParameterScopes = c.typeParameterScopes[:len(c.typeParameterScopes)-1]
		c.constraintScopes = c.constraintScopes[:len(c.constraintScopes)-1]
		c.narrowings = savedNarrowings
		c.factVersions = savedVersions
	}()

	// 将形参符号与声明类型绑定
	for i, param := range fn.Parameters {
		symbol, ok := c.resolver.BindingOf(param)
		if !ok {
			return fmt.Errorf(
				"missing symbol binding for parameter %q",
				param.Name,
			)
		}

		if symbol.Kind != resolver.SymbolParameter {
			return fmt.Errorf(
				"%q is not a parameter symbol",
				param.Name,
			)
		}

		c.types[symbol] = signature.ParameterTypes[i]
	}

	// 进入函数时保存预期返回类型
	c.returnTypes = append(c.returnTypes, signature.ReturnType)
	outerLoopDepth := c.loopDepth
	c.loopDepth = 0

	// 离开函数时恢复外层返回类型
	defer func() {
		c.returnTypes = c.returnTypes[:len(c.returnTypes)-1]
		c.loopDepth = outerLoopDepth
	}()

	// 检查函数体中的全部语句
	if err := c.checkBlock(fn.Body); err != nil {
		return err
	}

	// 非 unit 函数必须在所有路径上返回
	if signature.ReturnType != KindUnit &&
		!blockAlwaysReturns(fn.Body) {
		cause := fmt.Errorf(
			"function %q may exit without returning %s",
			fn.Name.Name,
			signature.ReturnType,
		)
		return typeDiagnostic("E0304", fmt.Sprintf("函数 %q 可能没有返回值", fn.Name.Name), fn.Name.Span(), signature.ReturnType.String(), KindUnit.String(), cause)
	}

	return nil
}

// checkFunctionLiteral 检查匿名函数签名与函数体
func (c *Checker) checkFunctionLiteral(fn *ast.FunctionLiteral) (Kind, error) {
	if fn.ReturnType == nil {
		return KindInvalid, fmt.Errorf("anonymous function requires a return type")
	}
	result, err := c.resolveType(fn.ReturnType, c.activeTypeParameters())
	if err != nil {
		return KindInvalid, err
	}
	params := make([]Kind, len(fn.Parameters))
	for i, param := range fn.Parameters {
		if param.DeclaredType == nil {
			return KindInvalid, fmt.Errorf("parameter %q requires a type annotation", param.Name)
		}
		kind, err := c.resolveType(param.DeclaredType, c.activeTypeParameters())
		if err != nil {
			return KindInvalid, err
		}
		if kind == KindUnit {
			return KindInvalid, fmt.Errorf("function parameter cannot be unit")
		}
		params[i] = kind
		symbol, ok := c.resolver.BindingOf(param)
		if !ok {
			return KindInvalid, fmt.Errorf("missing symbol binding for parameter %q", param.Name)
		}
		c.types[symbol] = kind
	}
	savedNarrowings := c.narrowings
	savedVersions := copyVersions(c.factVersions)
	outerLoopDepth := c.loopDepth
	c.narrowings = make(map[resolver.Symbol]Kind)
	c.functionDepth++
	c.loopDepth = 0
	c.returnTypes = append(c.returnTypes, result)
	defer func() {
		c.returnTypes = c.returnTypes[:len(c.returnTypes)-1]
		c.functionDepth--
		c.loopDepth = outerLoopDepth
		c.narrowings = savedNarrowings
		c.factVersions = savedVersions
	}()
	if err := c.checkBlock(fn.Body); err != nil {
		return KindInvalid, err
	}
	if result != KindUnit && !blockAlwaysReturns(fn.Body) {
		return KindInvalid, fmt.Errorf("anonymous function may exit without returning %s", result)
	}
	return FunctionOf(params, result), nil
}

// checkBlock 检查代码块中的所有语句
func (c *Checker) checkBlock(block *ast.BlockStatement) error {
	if block == nil {
		return fmt.Errorf("missing block statement")
	}

	return c.checkStatements(block.Statements)
}

// blockAlwaysReturns 保守判断代码块是否在所有路径上返回
func blockAlwaysReturns(block *ast.BlockStatement) bool {
	if block == nil {
		return false
	}

	for _, stmt := range block.Statements {
		switch n := stmt.(type) {

		case *ast.ReturnStatement:
			return true

		case *ast.IfStatement:
			// 两个分支都返回，条件语句才保证返回
			if n.Alternative != nil &&
				blockAlwaysReturns(n.Consequence) &&
				blockAlwaysReturns(n.Alternative) {
				return true
			}

		case *ast.MatchStatement:
			if len(n.Arms) != 0 {
				allReturn := true
				for _, arm := range n.Arms {
					if !blockAlwaysReturns(arm.Body) {
						allReturn = false
						break
					}
				}
				if allReturn {
					return true
				}
			}
		}
	}

	return false
}
