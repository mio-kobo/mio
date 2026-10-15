package typecheck

import (
	"fmt"

	"Mio/internal/ast"
	"Mio/internal/resolver"
)

func (c *Checker) checkCall(call *ast.CallExpression, expectedResult Kind) (Kind, error) {
	if field, ok := call.Function.(*ast.FieldExpression); ok {
		left := field.Left
		if apply, ok := left.(*ast.TypeApplyExpression); ok {
			left = apply.Function
		}
		if identifier, ok := left.(*ast.Identifier); ok {
			if symbol, resolved := c.resolver.BindingOf(identifier); resolved && symbol.Kind == resolver.SymbolType {
				if _, enum := c.enums[identifier.Name]; enum {
					return c.checkEnumConstructor(call, field, expectedResult)
				}
			}
		}
	}
	function := call.Function
	var explicitArgs []*ast.TypeRef
	if apply, ok := function.(*ast.TypeApplyExpression); ok {
		function = apply.Function
		explicitArgs = apply.Args
	}
	callee, identifierCall := function.(*ast.Identifier)
	if !identifierCall {
		return c.checkDynamicCall(call)
	}
	symbol, ok := c.resolver.BindingOf(callee)
	if !ok {
		return KindInvalid, fmt.Errorf("missing call binding for %q", callee.Name)
	}
	if symbol.Kind == resolver.SymbolBuiltin {
		if callee.Name == "Map" {
			if len(call.Arguments) != 0 {
				return KindInvalid, fmt.Errorf("Map constructor takes no arguments")
			}
			mapType := expectedResult
			if len(explicitArgs) != 0 {
				if len(explicitArgs) != 2 {
					return KindInvalid, fmt.Errorf("Map requires two type arguments")
				}
				key, err := c.resolveType(explicitArgs[0], c.activeTypeParameters())
				if err != nil {
					return KindInvalid, err
				}
				value, err := c.resolveType(explicitArgs[1], c.activeTypeParameters())
				if err != nil {
					return KindInvalid, err
				}
				if !isHashableType(key) {
					return KindInvalid, fmt.Errorf("Map key type %s is not hashable", key)
				}
				mapType = MapOf(key, value)
			}
			if !mapType.IsMap() {
				return KindInvalid, fmt.Errorf("Map() requires an explicit Map<K, V> context")
			}
			call.MapCreate = true
			call.HasValue = true
			return mapType, nil
		}
		result, err := c.checkBuiltinCall(callee.Name, call)
		call.HasValue = result != KindUnit
		return result, err
	}
	if symbol.Kind != resolver.SymbolFunction {
		return c.checkDynamicCall(call)
	}
	signature, ok := c.signatures[symbol]
	if !ok {
		return KindInvalid, fmt.Errorf("missing function signature for %q", callee.Name)
	}
	if len(call.Arguments) != len(signature.ParameterTypes) {
		return KindInvalid, fmt.Errorf("function %q expects %d arguments, got %d", callee.Name, len(signature.ParameterTypes), len(call.Arguments))
	}
	bindings := make(map[string]Kind, len(signature.TypeParams))
	if len(explicitArgs) != 0 {
		if len(explicitArgs) != len(signature.TypeParams) {
			return KindInvalid, fmt.Errorf("function %q requires %d type arguments, got %d", callee.Name, len(signature.TypeParams), len(explicitArgs))
		}
		for i, ref := range explicitArgs {
			kind, err := c.resolveType(ref, c.activeTypeParameters())
			if err != nil {
				return KindInvalid, err
			}
			bindings[signature.TypeParams[i]] = kind
		}
	}
	if expectedResult != KindInvalid {
		if !inferBinding(signature.ReturnType, expectedResult, bindings) {
			return KindInvalid, fmt.Errorf("function %q result is inconsistent with expected type %s", callee.Name, expectedResult)
		}
	}
	actuals := make([]Kind, len(call.Arguments))
	for i, argument := range call.Arguments {
		parameterType := substitute(signature.ParameterTypes[i], bindings)
		var actual Kind
		var err error
		if containsTypeParameter(parameterType) {
			actual, err = c.checkExpression(argument)
		} else {
			actual, err = c.checkExpressionExpected(argument, parameterType)
		}
		if err != nil {
			return KindInvalid, err
		}
		actuals[i] = actual
		if containsTypeParameter(parameterType) && !inferBinding(signature.ParameterTypes[i], actual, bindings) {
			return KindInvalid, fmt.Errorf("argument %d to %q is inconsistent with generic type parameters", i+1, callee.Name)
		}
	}
	for _, parameter := range signature.TypeParams {
		if _, ok := bindings[parameter]; !ok {
			return KindInvalid, fmt.Errorf("cannot infer type parameter %s for %q", parameter, callee.Name)
		}
	}
	for parameter := range signature.HashableParams {
		if !isHashableType(bindings[parameter]) {
			return KindInvalid, fmt.Errorf("type argument %s for %s must be hashable", bindings[parameter], parameter)
		}
	}
	for parameter, constraint := range signature.Constraints {
		argument := bindings[parameter]
		argumentConstraint := ""
		if argument.IsTypeParameter() && len(c.constraintScopes) != 0 {
			argumentConstraint = c.constraintScopes[len(c.constraintScopes)-1][argument.TypeParameterName()]
		}
		if constraint == "Number" && !argument.IsInteger() && argument != KindFloat32 && argument != KindFloat && argumentConstraint != "Number" && argumentConstraint != "Integer" && argumentConstraint != "Signed" && argumentConstraint != "Float" {
			return KindInvalid, fmt.Errorf("type argument %s for %s must satisfy Number", argument, parameter)
		}
		if constraint == "Integer" && !argument.IsInteger() && argumentConstraint != "Integer" {
			return KindInvalid, fmt.Errorf("type argument %s for %s must satisfy Integer", argument, parameter)
		}
		if constraint == "Signed" && !argument.IsSignedInteger() && argument != KindFloat32 && argument != KindFloat && argumentConstraint != "Signed" && argumentConstraint != "Float" {
			return KindInvalid, fmt.Errorf("type argument %s for %s must satisfy Signed", argument, parameter)
		}
		if constraint == "Float" && argument != KindFloat32 && argument != KindFloat && argumentConstraint != "Float" {
			return KindInvalid, fmt.Errorf("type argument %s for %s must satisfy Float", argument, parameter)
		}
	}
	for i, actual := range actuals {
		expected := substitute(signature.ParameterTypes[i], bindings)
		if !isAssignable(expected, actual) {
			return KindInvalid, typeDiagnostic("E0303", fmt.Sprintf("函数 %q 的第 %d 个参数类型不正确", callee.Name, i+1), call.Arguments[i].Span(), expected.String(), actual.String(), fmt.Errorf("argument %d to %q: expected %s, got %s", i+1, callee.Name, expected, actual))
		}
	}
	result := substitute(signature.ReturnType, bindings)
	call.TypeArgs = make([]string, len(signature.TypeParams))
	for index, parameter := range signature.TypeParams {
		call.TypeArgs[index] = c.runtimeTypeName(bindings[parameter])
	}
	call.HasValue = result != KindUnit
	c.invalidateGlobalFacts()
	return result, nil
}

func (c *Checker) runtimeTypeName(kind Kind) string {
	if kind.IsNullable() {
		return "nullable<" + c.runtimeTypeName(kind.NonNullable()) + ">"
	}
	if kind.IsArray() {
		return "array"
	}
	if kind.IsMap() {
		return "map"
	}
	if kind.IsFunction() {
		return "function"
	}
	if name, _, ok := kind.NamedParts(); ok {
		if _, enum := c.enums[name]; enum {
			return "enum"
		}
		return "struct"
	}
	if kind.IsTypeParameter() {
		return ""
	}
	return string(kind)
}

func (c *Checker) checkDynamicCall(call *ast.CallExpression) (Kind, error) {
	functionType, err := c.checkExpression(call.Function)
	if err != nil {
		return KindInvalid, err
	}
	params, result, ok := functionType.FunctionParts()
	if !ok {
		if identifier, identifierCall := call.Function.(*ast.Identifier); identifierCall {
			if symbol, resolved := c.resolver.BindingOf(identifier); resolved {
				return KindInvalid, fmt.Errorf("cannot call %s %q", symbol.Kind, identifier.Name)
			}
		}
		return KindInvalid, fmt.Errorf("call target has non-function type %s", functionType)
	}
	if len(params) != len(call.Arguments) {
		return KindInvalid, fmt.Errorf("function value expects %d arguments, got %d", len(params), len(call.Arguments))
	}
	for i, argument := range call.Arguments {
		actual, err := c.checkExpressionExpected(argument, params[i])
		if err != nil {
			return KindInvalid, err
		}
		if !isAssignable(params[i], actual) {
			return KindInvalid, fmt.Errorf("function value argument %d requires %s, got %s", i+1, params[i], actual)
		}
	}
	call.Dynamic = true
	call.HasValue = result != KindUnit
	c.invalidateAllFacts()
	return result, nil
}

func (c *Checker) checkEnumConstructor(call *ast.CallExpression, field *ast.FieldExpression, expectedResult Kind) (Kind, error) {
	left := field.Left
	var explicitArgs []*ast.TypeRef
	if apply, ok := left.(*ast.TypeApplyExpression); ok {
		left = apply.Function
		explicitArgs = apply.Args
	}
	typeName, ok := left.(*ast.Identifier)
	if !ok {
		return KindInvalid, fmt.Errorf("enum constructor must use Type.Variant")
	}
	symbol, ok := c.resolver.BindingOf(typeName)
	if !ok || symbol.Kind != resolver.SymbolType {
		return KindInvalid, fmt.Errorf("%s is not an enum type", typeName.Name)
	}
	definition, ok := c.enums[typeName.Name]
	if !ok {
		return KindInvalid, fmt.Errorf("%s is not an enum type", typeName.Name)
	}
	variantIndex := -1
	var variant EnumVariant
	for i, candidate := range definition.Variants {
		if candidate.Name == field.Name {
			variantIndex = i
			variant = candidate
			break
		}
	}
	if variantIndex < 0 {
		return KindInvalid, fmt.Errorf("enum %s has no variant %s", definition.Name, field.Name)
	}
	if len(call.Arguments) != len(variant.Fields) {
		return KindInvalid, fmt.Errorf("variant %s.%s expects %d values, got %d", definition.Name, field.Name, len(variant.Fields), len(call.Arguments))
	}
	bindings := make(map[string]Kind, len(definition.TypeParams))
	if len(explicitArgs) != 0 {
		if len(explicitArgs) != len(definition.TypeParams) {
			return KindInvalid, fmt.Errorf("enum %s requires %d type arguments, got %d", definition.Name, len(definition.TypeParams), len(explicitArgs))
		}
		for i, ref := range explicitArgs {
			kind, err := c.resolveType(ref, c.activeTypeParameters())
			if err != nil {
				return KindInvalid, err
			}
			bindings[definition.TypeParams[i]] = kind
		}
	}
	if name, args, matches := expectedResult.NamedParts(); matches && name == definition.Name && len(args) == len(definition.TypeParams) {
		for i, parameter := range definition.TypeParams {
			if existing, ok := bindings[parameter]; ok && existing != args[i] {
				return KindInvalid, fmt.Errorf("explicit enum type argument %s conflicts with expected %s", existing, args[i])
			}
			bindings[parameter] = args[i]
		}
	}
	actuals := make([]Kind, len(call.Arguments))
	for i, argument := range call.Arguments {
		fieldType := substitute(variant.Fields[i], bindings)
		var actual Kind
		var err error
		if containsTypeParameter(fieldType) {
			actual, err = c.checkExpression(argument)
		} else {
			actual, err = c.checkExpressionExpected(argument, fieldType)
		}
		if err != nil {
			return KindInvalid, err
		}
		actuals[i] = actual
		if containsTypeParameter(fieldType) && !inferBinding(variant.Fields[i], actual, bindings) {
			return KindInvalid, fmt.Errorf("variant %s.%s value %d has inconsistent type", definition.Name, field.Name, i+1)
		}
	}
	args := make([]Kind, len(definition.TypeParams))
	for i, parameter := range definition.TypeParams {
		kind, ok := bindings[parameter]
		if !ok {
			return KindInvalid, fmt.Errorf("cannot infer enum type parameter %s; add a result type annotation", parameter)
		}
		args[i] = kind
	}
	for i, actual := range actuals {
		expected := substitute(variant.Fields[i], bindings)
		if !isAssignable(expected, actual) {
			return KindInvalid, fmt.Errorf("variant value %d requires %s, got %s", i+1, expected, actual)
		}
	}
	call.EnumType = definition.Name
	call.Variant = variantIndex
	call.HasValue = true
	return NamedOf(definition.Name, args...), nil
}

func (c *Checker) checkStructLiteral(literal *ast.StructLiteral, expected Kind) (Kind, error) {
	definition, ok := c.structs[literal.TypeName]
	if !ok {
		return KindInvalid, fmt.Errorf("unknown struct type %q", literal.TypeName)
	}
	if len(literal.Fields) != len(definition.Fields) {
		return KindInvalid, fmt.Errorf("struct %s requires %d fields, got %d", definition.Name, len(definition.Fields), len(literal.Fields))
	}
	provided := make(map[string]ast.FieldValue, len(literal.Fields))
	for _, field := range literal.Fields {
		if _, duplicate := provided[field.Name]; duplicate {
			return KindInvalid, fmt.Errorf("field %s initialized more than once", field.Name)
		}
		provided[field.Name] = field
	}
	bindings := make(map[string]Kind, len(definition.TypeParams))
	if len(literal.TypeArgs) != 0 {
		if len(literal.TypeArgs) != len(definition.TypeParams) {
			return KindInvalid, fmt.Errorf("struct %s requires %d type arguments, got %d", definition.Name, len(definition.TypeParams), len(literal.TypeArgs))
		}
		for i, ref := range literal.TypeArgs {
			kind, err := c.resolveType(ref, c.activeTypeParameters())
			if err != nil {
				return KindInvalid, err
			}
			bindings[definition.TypeParams[i]] = kind
		}
	}
	if name, args, matches := expected.NamedParts(); matches && name == definition.Name && len(args) == len(definition.TypeParams) {
		for i, parameter := range definition.TypeParams {
			if existing, ok := bindings[parameter]; ok && existing != args[i] {
				return KindInvalid, fmt.Errorf("explicit struct type argument %s conflicts with expected %s", existing, args[i])
			}
			bindings[parameter] = args[i]
		}
	}
	ordered := make([]ast.FieldValue, len(definition.Fields))
	actuals := make([]Kind, len(definition.Fields))
	for i, field := range definition.Fields {
		value, ok := provided[field.Name]
		if !ok {
			return KindInvalid, fmt.Errorf("missing field %s.%s", definition.Name, field.Name)
		}
		fieldType := substitute(field.Type, bindings)
		var actual Kind
		var err error
		if containsTypeParameter(fieldType) {
			actual, err = c.checkExpression(value.Value)
		} else {
			actual, err = c.checkExpressionExpected(value.Value, fieldType)
		}
		if err != nil {
			return KindInvalid, err
		}
		if containsTypeParameter(fieldType) && !inferBinding(field.Type, actual, bindings) {
			return KindInvalid, fmt.Errorf("field %s.%s has inconsistent type", definition.Name, field.Name)
		}
		ordered[i] = value
		actuals[i] = actual
	}
	args := make([]Kind, len(definition.TypeParams))
	for i, parameter := range definition.TypeParams {
		kind, ok := bindings[parameter]
		if !ok {
			return KindInvalid, fmt.Errorf("cannot infer struct type parameter %s", parameter)
		}
		args[i] = kind
	}
	for i, field := range definition.Fields {
		fieldType := substitute(field.Type, bindings)
		if !isAssignable(fieldType, actuals[i]) {
			return KindInvalid, fmt.Errorf("field %s.%s requires %s, got %s", definition.Name, field.Name, fieldType, actuals[i])
		}
	}
	literal.Fields = ordered
	return NamedOf(definition.Name, args...), nil
}
