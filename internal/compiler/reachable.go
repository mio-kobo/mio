package compiler

import (
	"strings"

	"Mio/internal/ast"
)

// reachableLibraryFunctions 查找程序实际引用的标准库函数
func reachableLibraryFunctions(program *ast.Program) map[string]bool {
	functions := make(map[string]*ast.FunctionStatement)
	for _, statement := range program.Statements {
		if function, ok := statement.(*ast.FunctionStatement); ok && strings.Contains(function.Name.Name, "::") {
			functions[function.Name.Name] = function
		}
	}
	reachable := make(map[string]bool)
	queue := make([]string, 0)
	visit := func(name string) {
		if functions[name] != nil && !reachable[name] {
			reachable[name] = true
			queue = append(queue, name)
		}
	}
	for _, statement := range program.Statements {
		function, isFunction := statement.(*ast.FunctionStatement)
		if isFunction && strings.Contains(function.Name.Name, "::") {
			continue
		}
		walkMathStatement(statement, visit)
	}
	for len(queue) != 0 {
		name := queue[0]
		queue = queue[1:]
		walkMathStatement(functions[name].Body, visit)
	}
	return reachable
}

func walkMathStatement(statement ast.Statement, visit func(string)) {
	switch node := statement.(type) {
	case *ast.ConstStatement:
		walkMathExpression(node.Value, visit)
	case *ast.ExpressionStatement:
		walkMathExpression(node.Expr, visit)
	case *ast.LetStatement:
		walkMathExpression(node.Value, visit)
	case *ast.AssignStatement:
		walkMathExpression(node.Value, visit)
	case *ast.IndexAssignStatement:
		walkMathExpression(node.Target, visit)
		walkMathExpression(node.Value, visit)
	case *ast.BlockStatement:
		for _, child := range node.Statements {
			walkMathStatement(child, visit)
		}
	case *ast.IfStatement:
		walkMathExpression(node.Condition, visit)
		walkMathStatement(node.Consequence, visit)
		if node.Alternative != nil {
			walkMathStatement(node.Alternative, visit)
		}
	case *ast.WhileStatement:
		walkMathExpression(node.Condition, visit)
		walkMathStatement(node.Body, visit)
	case *ast.ReturnStatement:
		if node.Value != nil {
			walkMathExpression(node.Value, visit)
		}
	case *ast.FunctionStatement:
		walkMathStatement(node.Body, visit)
	case *ast.MatchStatement:
		walkMathExpression(node.Value, visit)
		for _, arm := range node.Arms {
			walkMathStatement(arm.Body, visit)
		}
	}
}

func walkMathExpression(expression ast.Expression, visit func(string)) {
	switch node := expression.(type) {
	case *ast.Identifier:
		visit(node.Name)
	case *ast.InfixExpression:
		walkMathExpression(node.Left, visit)
		walkMathExpression(node.Right, visit)
	case *ast.GroupedExpression:
		walkMathExpression(node.Expr, visit)
	case *ast.PrefixExpression:
		walkMathExpression(node.Right, visit)
	case *ast.CallExpression:
		walkMathExpression(node.Function, visit)
		for _, argument := range node.Arguments {
			walkMathExpression(argument, visit)
		}
	case *ast.TypeApplyExpression:
		walkMathExpression(node.Function, visit)
	case *ast.ConversionExpression:
		for _, argument := range node.Arguments {
			walkMathExpression(argument, visit)
		}
	case *ast.ArrayLiteral:
		for _, element := range node.Elements {
			walkMathExpression(element, visit)
		}
	case *ast.IndexExpression:
		walkMathExpression(node.Left, visit)
		walkMathExpression(node.Index, visit)
	case *ast.FieldExpression:
		walkMathExpression(node.Left, visit)
	case *ast.StructLiteral:
		for _, field := range node.Fields {
			walkMathExpression(field.Value, visit)
		}
	case *ast.EnumLiteral:
		for _, item := range node.Values {
			walkMathExpression(item, visit)
		}
	case *ast.FunctionLiteral:
		walkMathStatement(node.Body, visit)
	}
}
