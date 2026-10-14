package typecheck

import (
	"fmt"

	"Mio/internal/ast"
)

// StructDefinition 保存结构体静态定义
type StructDefinition struct {
	Name           string
	TypeParams     []string
	Fields         []StructField
	HashableParams map[string]struct{}
}

// StructField 保存结构体字段类型
type StructField struct {
	Name string
	Type Kind
}

// EnumDefinition 保存枚举静态定义
type EnumDefinition struct {
	Name           string
	TypeParams     []string
	Variants       []EnumVariant
	HashableParams map[string]struct{}
}

// EnumVariant 保存枚举变体关联数据类型
type EnumVariant struct {
	Name   string
	Fields []Kind
}

func parameterSet(names []string) map[string]struct{} {
	result := make(map[string]struct{}, len(names))
	for _, name := range names {
		result[name] = struct{}{}
	}
	return result
}

func validateTypeParameters(owner string, names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if _, ok := seen[name]; ok {
			return fmt.Errorf("duplicate type parameter %q in %s", name, owner)
		}
		seen[name] = struct{}{}
	}
	return nil
}

func (c *Checker) resolveType(ref *ast.TypeRef, parameters map[string]struct{}) (Kind, error) {
	if ref == nil {
		return KindInvalid, fmt.Errorf("missing type annotation")
	}
	if ref.Name == "[]" {
		element, err := c.resolveType(ref.Element, parameters)
		if err != nil {
			return KindInvalid, err
		}
		if result := ArrayOf(element); result != KindInvalid {
			return result, nil
		}
		return KindInvalid, fmt.Errorf("invalid array element type %s", element)
	}
	if ref.Name == "?" {
		inner, err := c.resolveType(ref.Element, parameters)
		if err != nil {
			return KindInvalid, err
		}
		if result := NullableOf(inner); result != KindInvalid {
			return result, nil
		}
		return KindInvalid, fmt.Errorf("type %s cannot be nullable", inner)
	}
	if ref.Name == "fn" {
		params := make([]Kind, len(ref.Params))
		for i, param := range ref.Params {
			kind, err := c.resolveType(param, parameters)
			if err != nil {
				return KindInvalid, err
			}
			if kind == KindUnit {
				return KindInvalid, fmt.Errorf("function parameter cannot be unit")
			}
			params[i] = kind
		}
		result, err := c.resolveType(ref.Result, parameters)
		if err != nil {
			return KindInvalid, err
		}
		return FunctionOf(params, result), nil
	}
	switch ref.Name {
	case "int64":
		return KindInt, nil
	case "float64":
		return KindFloat, nil
	case "bool":
		return KindBool, nil
	case "string":
		return KindString, nil
	case "unit":
		return KindUnit, nil
	}
	if _, ok := parameters[ref.Name]; ok {
		if len(ref.Args) != 0 {
			return KindInvalid, fmt.Errorf("type parameter %s cannot have arguments", ref.Name)
		}
		return TypeParameter(ref.Name), nil
	}
	if ref.Name == "Map" {
		if len(ref.Args) != 2 {
			return KindInvalid, fmt.Errorf("Map requires two type arguments")
		}
		key, err := c.resolveType(ref.Args[0], parameters)
		if err != nil {
			return KindInvalid, err
		}
		value, err := c.resolveType(ref.Args[1], parameters)
		if err != nil {
			return KindInvalid, err
		}
		if !isHashableType(key) && !key.IsTypeParameter() {
			return KindInvalid, fmt.Errorf("Map key type %s is not hashable", key)
		}
		return MapOf(key, value), nil
	}
	definitionParams, ok := c.typeParameters(ref.Name)
	if !ok {
		return KindInvalid, fmt.Errorf("unknown type %q", ref.Name)
	}
	if len(ref.Args) != len(definitionParams) {
		return KindInvalid, fmt.Errorf("type %s requires %d type arguments, got %d", ref.Name, len(definitionParams), len(ref.Args))
	}
	args := make([]Kind, len(ref.Args))
	for i, arg := range ref.Args {
		kind, err := c.resolveType(arg, parameters)
		if err != nil {
			return KindInvalid, err
		}
		args[i] = kind
	}
	constraints := c.typeHashableParameters(ref.Name)
	for i, parameter := range definitionParams {
		if _, constrained := constraints[parameter]; constrained && !args[i].IsTypeParameter() && !isHashableType(args[i]) {
			return KindInvalid, fmt.Errorf("type argument %s for %s must be hashable", args[i], parameter)
		}
	}
	return NamedOf(ref.Name, args...), nil
}

func (c *Checker) typeHashableParameters(name string) map[string]struct{} {
	if definition, ok := c.structs[name]; ok {
		return definition.HashableParams
	}
	if definition, ok := c.enums[name]; ok {
		return definition.HashableParams
	}
	return nil
}

func (c *Checker) typeParameters(name string) ([]string, bool) {
	if definition, ok := c.structs[name]; ok {
		return definition.TypeParams, true
	}
	if definition, ok := c.enums[name]; ok {
		return definition.TypeParams, true
	}
	return nil, false
}

func (c *Checker) registerTypeDeclarations(program *ast.Program) error {
	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.StructStatement:
			if !isTypeName(declaration.Name.Name) {
				return fmt.Errorf("type name %q must start with an uppercase ASCII letter", declaration.Name.Name)
			}
			if err := validateTypeParameters(declaration.Name.Name, declaration.TypeParams); err != nil {
				return err
			}
			if _, exists := c.structs[declaration.Name.Name]; exists || c.enums[declaration.Name.Name].Name != "" || declaration.Name.Name == "Map" {
				return fmt.Errorf("duplicate type %q", declaration.Name.Name)
			}
			c.structs[declaration.Name.Name] = StructDefinition{Name: declaration.Name.Name, TypeParams: append([]string(nil), declaration.TypeParams...), HashableParams: make(map[string]struct{})}
		case *ast.EnumStatement:
			if !isTypeName(declaration.Name.Name) {
				return fmt.Errorf("type name %q must start with an uppercase ASCII letter", declaration.Name.Name)
			}
			if err := validateTypeParameters(declaration.Name.Name, declaration.TypeParams); err != nil {
				return err
			}
			if _, exists := c.enums[declaration.Name.Name]; exists || c.structs[declaration.Name.Name].Name != "" || declaration.Name.Name == "Map" {
				return fmt.Errorf("duplicate type %q", declaration.Name.Name)
			}
			c.enums[declaration.Name.Name] = EnumDefinition{Name: declaration.Name.Name, TypeParams: append([]string(nil), declaration.TypeParams...), HashableParams: make(map[string]struct{})}
		}
	}
	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case *ast.StructStatement:
			definition := c.structs[declaration.Name.Name]
			seen := make(map[string]struct{})
			params := parameterSet(declaration.TypeParams)
			for _, field := range declaration.Fields {
				if _, ok := seen[field.Name]; ok {
					return fmt.Errorf("duplicate field %q in %s", field.Name, definition.Name)
				}
				seen[field.Name] = struct{}{}
				kind, err := c.resolveType(field.DeclaredType, params)
				if err != nil {
					return err
				}
				if kind == KindUnit {
					return fmt.Errorf("field %s.%s cannot have unit type", definition.Name, field.Name)
				}
				definition.Fields = append(definition.Fields, StructField{Name: field.Name, Type: kind})
				c.collectHashableParameters(kind, definition.HashableParams)
			}
			c.structs[definition.Name] = definition
		case *ast.EnumStatement:
			definition := c.enums[declaration.Name.Name]
			seen := make(map[string]struct{})
			params := parameterSet(declaration.TypeParams)
			for _, variant := range declaration.Variants {
				if _, ok := seen[variant.Name]; ok {
					return fmt.Errorf("duplicate variant %q in %s", variant.Name, definition.Name)
				}
				seen[variant.Name] = struct{}{}
				registered := EnumVariant{Name: variant.Name}
				for _, field := range variant.Fields {
					kind, err := c.resolveType(field, params)
					if err != nil {
						return err
					}
					if kind == KindUnit {
						return fmt.Errorf("variant data cannot have unit type")
					}
					registered.Fields = append(registered.Fields, kind)
					c.collectHashableParameters(kind, definition.HashableParams)
				}
				definition.Variants = append(definition.Variants, registered)
			}
			if len(definition.Variants) == 0 {
				return fmt.Errorf("enum %s requires at least one variant", definition.Name)
			}
			c.enums[definition.Name] = definition
		}
	}
	for pass := 0; pass < len(c.structs)+len(c.enums); pass++ {
		for name, definition := range c.structs {
			for _, field := range definition.Fields {
				c.collectHashableParameters(field.Type, definition.HashableParams)
			}
			c.structs[name] = definition
		}
		for name, definition := range c.enums {
			for _, variant := range definition.Variants {
				for _, field := range variant.Fields {
					c.collectHashableParameters(field, definition.HashableParams)
				}
			}
			c.enums[name] = definition
		}
	}
	for _, definition := range c.structs {
		for _, field := range definition.Fields {
			if err := c.validateTypeConstraints(field.Type); err != nil {
				return err
			}
		}
	}
	for _, definition := range c.enums {
		for _, variant := range definition.Variants {
			for _, field := range variant.Fields {
				if err := c.validateTypeConstraints(field); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (c *Checker) validateTypeConstraints(kind Kind) error {
	if kind.IsNullable() {
		return c.validateTypeConstraints(kind.NonNullable())
	}
	if kind.IsArray() {
		return c.validateTypeConstraints(kind.ElementKind())
	}
	if name, args, ok := kind.NamedParts(); ok {
		parameters, _ := c.typeParameters(name)
		constraints := c.typeHashableParameters(name)
		for i, parameter := range parameters {
			if _, constrained := constraints[parameter]; constrained && i < len(args) && !args[i].IsTypeParameter() && !isHashableType(args[i]) {
				return fmt.Errorf("type argument %s for %s must be hashable", args[i], parameter)
			}
		}
		for _, arg := range args {
			if err := c.validateTypeConstraints(arg); err != nil {
				return err
			}
		}
	}
	return nil
}

func substitute(kind Kind, bindings map[string]Kind) Kind {
	if kind.IsTypeParameter() {
		if result, ok := bindings[kind.TypeParameterName()]; ok {
			return result
		}
		return kind
	}
	if kind.IsNullable() {
		return NullableOf(substitute(kind.NonNullable(), bindings))
	}
	if kind.IsArray() {
		return ArrayOf(substitute(kind.ElementKind(), bindings))
	}
	if name, args, ok := kind.NamedParts(); ok {
		for i := range args {
			args[i] = substitute(args[i], bindings)
		}
		return NamedOf(name, args...)
	}
	if params, result, ok := kind.FunctionParts(); ok {
		for i := range params {
			params[i] = substitute(params[i], bindings)
		}
		return FunctionOf(params, substitute(result, bindings))
	}
	return kind
}

func inferBinding(pattern, actual Kind, bindings map[string]Kind) bool {
	if pattern.IsTypeParameter() {
		name := pattern.TypeParameterName()
		if existing, ok := bindings[name]; ok {
			return existing == actual
		}
		bindings[name] = actual
		return true
	}
	if pattern.IsNullable() {
		if actual.IsNullable() {
			return inferBinding(pattern.NonNullable(), actual.NonNullable(), bindings)
		}
		return inferBinding(pattern.NonNullable(), actual, bindings)
	}
	if pattern.IsArray() && actual.IsArray() {
		return inferBinding(pattern.ElementKind(), actual.ElementKind(), bindings)
	}
	pName, pArgs, pNamed := pattern.NamedParts()
	aName, aArgs, aNamed := actual.NamedParts()
	if pNamed && aNamed && pName == aName && len(pArgs) == len(aArgs) {
		for i := range pArgs {
			if !inferBinding(pArgs[i], aArgs[i], bindings) {
				return false
			}
		}
		return true
	}
	pParams, pResult, pFunction := pattern.FunctionParts()
	aParams, aResult, aFunction := actual.FunctionParts()
	if pFunction && aFunction && len(pParams) == len(aParams) {
		for i := range pParams {
			if !inferBinding(pParams[i], aParams[i], bindings) {
				return false
			}
		}
		return inferBinding(pResult, aResult, bindings)
	}
	return pattern == actual
}

func containsTypeParameter(kind Kind) bool {
	if kind.IsTypeParameter() {
		return true
	}
	if kind.IsNullable() {
		return containsTypeParameter(kind.NonNullable())
	}
	if kind.IsArray() {
		return containsTypeParameter(kind.ElementKind())
	}
	if _, args, ok := kind.NamedParts(); ok {
		for _, arg := range args {
			if containsTypeParameter(arg) {
				return true
			}
		}
	}
	if params, result, ok := kind.FunctionParts(); ok {
		for _, param := range params {
			if containsTypeParameter(param) {
				return true
			}
		}
		return containsTypeParameter(result)
	}
	return false
}

func isHashableType(kind Kind) bool {
	return kind == KindInt || kind == KindBool || kind == KindString
}

func (c *Checker) collectHashableParameters(kind Kind, result map[string]struct{}) {
	if kind.IsNullable() {
		c.collectHashableParameters(kind.NonNullable(), result)
		return
	}
	if kind.IsArray() {
		c.collectHashableParameters(kind.ElementKind(), result)
		return
	}
	if name, args, ok := kind.NamedParts(); ok {
		if name == "Map" && len(args) == 2 && args[0].IsTypeParameter() {
			result[args[0].TypeParameterName()] = struct{}{}
		}
		if constraints := c.typeHashableParameters(name); len(constraints) != 0 {
			parameters, _ := c.typeParameters(name)
			for i, parameter := range parameters {
				if _, constrained := constraints[parameter]; constrained && i < len(args) && args[i].IsTypeParameter() {
					result[args[i].TypeParameterName()] = struct{}{}
				}
			}
		}
		for _, arg := range args {
			c.collectHashableParameters(arg, result)
		}
		return
	}
	if params, returned, ok := kind.FunctionParts(); ok {
		for _, param := range params {
			c.collectHashableParameters(param, result)
		}
		c.collectHashableParameters(returned, result)
	}
}

func isTypeName(name string) bool {
	return len(name) > 0 && name[0] >= 'A' && name[0] <= 'Z'
}
