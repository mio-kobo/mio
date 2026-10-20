package lsp

import (
	"strings"

	"Mio/internal/ast"
	"Mio/internal/token"
	"Mio/internal/typecheck"
)

// walkProgram 遍历全部语句，收集引用、字段/变体、调用与表达式类型。
func (b *indexBuilder) walkProgram(program *ast.Program) {
	if program == nil {
		return
	}
	for _, statement := range program.Statements {
		fileKey := b.analysis.statementFileKey(statement)
		if fileKey == "" {
			continue
		}
		b.walkStatement(statement, fileKey, nil, b.fileSpan(fileKey))
	}
}

func (b *indexBuilder) fileSpan(fileKey string) ast.Span {
	text := b.analysis.textFor(fileKey)
	return ast.Span{
		Start: token.Position{Offset: 0},
		End:   token.Position{Offset: len(text)},
	}
}

// walkStatement 遍历一条语句。
func (b *indexBuilder) walkStatement(statement ast.Statement, fileKey string, container *semanticSymbol, scope ast.Span) {
	switch n := statement.(type) {
	case *ast.FunctionStatement:
		symbol := b.declaredSymbol(n.Name, fileKey)
		current := container
		if symbol != nil {
			current = symbol
		}
		for index, parameter := range n.Parameters {
			if parameter.DeclaredType == nil {
				continue
			}
			resolved := ""
			if symbol != nil && symbol.Signature != nil && index < len(symbol.Signature.Parameters) {
				resolved = symbol.Signature.Parameters[index].Type
			}
			b.walkTypeRef(parameter.DeclaredType, fileKey, current, resolved)
		}
		b.walkTypeRef(n.ReturnType, fileKey, current, "")
		if n.Body != nil {
			bodySpan := n.Body.Span()
			for _, inner := range n.Body.Statements {
				b.walkStatement(inner, fileKey, current, bodySpan)
			}
		}

	case *ast.LetStatement:
		b.walkExpression(n.Value, fileKey, container)
		symbol := b.declaredSymbol(n.Name, fileKey)
		if symbol == nil {
			symbol = b.ensureDeclarationSymbol(n.Name, fileKey, symbolKindVariable)
			symbol.Kind = symbolKindVariable
			symbol.Keyword = "kaku"
			symbol.Container = containerID(container)
			symbol.Decl = &site{FileKey: fileKey, Span: n.Span()}
			symbol.NameSpan = n.Name.Span()
			symbol.Doc = docComment(b.analysis.textFor(fileKey), n.Span().Start.Line)
			symbol.Type = b.variableType(n.Name, fileKey)
			symbol.Detail = symbol.Type
			b.assignVariableKind(symbol, n.Name)
			b.addDeclaration(symbol)
			b.addScope(fileKey, scope.Start.Offset, scope.End.Offset, n.Span().End.Offset, symbol)
		}

	case *ast.AssignStatement:
		b.identifierOccurrence(n.Name, fileKey)
		b.walkExpression(n.Value, fileKey, container)

	case *ast.IndexAssignStatement:
		b.walkExpression(n.Target, fileKey, container)
		b.walkExpression(n.Value, fileKey, container)

	case *ast.ExpressionStatement:
		b.walkExpression(n.Expr, fileKey, container)

	case *ast.ReturnStatement:
		b.walkExpression(n.Value, fileKey, container)

	case *ast.IfStatement:
		b.walkExpression(n.Condition, fileKey, container)
		b.walkBlock(n.Consequence, fileKey, container)
		b.walkBlock(n.Alternative, fileKey, container)

	case *ast.WhileStatement:
		b.walkExpression(n.Condition, fileKey, container)
		b.walkBlock(n.Body, fileKey, container)

	case *ast.BlockStatement:
		b.walkBlock(n, fileKey, container)

	case *ast.MatchStatement:
		b.walkMatch(n, fileKey, container)

	case *ast.StructStatement:
		symbol := b.declaredSymbol(n.Name, fileKey)
		definition, hasDefinition := typecheck.StructDefinition{}, false
		if symbol != nil && b.analysis.Checker != nil {
			definition, hasDefinition = b.analysis.Checker.StructDefinitionOf(symbol.QualName)
		}
		for index, field := range n.Fields {
			if field.DeclaredType == nil {
				continue
			}
			resolved := ""
			if hasDefinition && index < len(definition.Fields) {
				resolved = definition.Fields[index].Type.String()
			}
			b.walkTypeRef(field.DeclaredType, fileKey, container, resolved)
		}

	case *ast.EnumStatement:
		symbol := b.declaredSymbol(n.Name, fileKey)
		definition, hasDefinition := typecheck.EnumDefinition{}, false
		if symbol != nil && b.analysis.Checker != nil {
			definition, hasDefinition = b.analysis.Checker.EnumDefinitionOf(symbol.QualName)
		}
		for index, variant := range n.Variants {
			for fieldIndex, ref := range variant.Fields {
				resolved := ""
				if hasDefinition && index < len(definition.Variants) && fieldIndex < len(definition.Variants[index].Fields) {
					resolved = definition.Variants[index].Fields[fieldIndex].String()
				}
				b.walkTypeRef(ref, fileKey, container, resolved)
			}
		}

	case *ast.ConstStatement:
		b.walkExpression(n.Value, fileKey, container)
	}
}

func (b *indexBuilder) walkBlock(block *ast.BlockStatement, fileKey string, container *semanticSymbol) {
	if block == nil {
		return
	}
	scope := block.Span()
	for _, statement := range block.Statements {
		b.walkStatement(statement, fileKey, container, scope)
	}
}

func (b *indexBuilder) walkMatch(statement *ast.MatchStatement, fileKey string, container *semanticSymbol) {
	b.walkExpression(statement.Value, fileKey, container)
	enumName := ""
	if b.analysis.Checker != nil {
		if kind, ok := b.analysis.Checker.TypeOf(statement.Value); ok {
			if name, _, named := kind.NamedParts(); named {
				enumName = name
			}
		}
	}
	enumSymbol := b.index.byQualName[enumName]
	for index := range statement.Arms {
		arm := &statement.Arms[index]
		variantSymbol := b.matchVariant(enumSymbol, arm)
		span := ast.Span{
			Start: arm.Range.Start,
			End: token.Position{
				Offset: arm.Range.Start.Offset + len(arm.Variant),
				Line:   arm.Range.Start.Line,
				Column: arm.Range.Start.Column + len(arm.Variant),
			},
		}
		occurrence := b.addOccurrence(fileKey, span, arm.Variant, variantSymbol, "", occurrenceVariant)
		if occurrence != nil && variantSymbol != nil {
			b.addReference(variantSymbol, site{FileKey: fileKey, Span: span})
		}
		bodySpan := arm.Body.Span()
		for _, binding := range arm.Bindings {
			local := b.ensureDeclarationSymbol(binding, fileKey, symbolKindVariable)
			local.Kind = symbolKindVariable
			local.Keyword = "baai"
			local.Decl = &site{FileKey: fileKey, Span: binding.Span()}
			local.NameSpan = binding.Span()
			local.Container = containerID(container)
			if local.hasBinding && b.analysis.Checker != nil {
				if kind, ok := b.analysis.Checker.TypeOfSymbol(local.binding); ok {
					local.Type = kind.String()
				}
			}
			local.Detail = local.Type
			b.assignVariableKind(local, binding)
			b.addDeclaration(local)
			b.addScope(fileKey, bodySpan.Start.Offset, bodySpan.End.Offset, bodySpan.Start.Offset, local)
		}
		for _, inner := range arm.Body.Statements {
			b.walkStatement(inner, fileKey, container, bodySpan)
		}
	}
}

func (b *indexBuilder) matchVariant(enumSymbol *semanticSymbol, arm *ast.MatchArm) *semanticSymbol {
	if enumSymbol == nil {
		return nil
	}
	if arm.Tag >= 0 && arm.Tag < len(enumSymbol.Variants) {
		return enumSymbol.Variants[arm.Tag]
	}
	for _, variant := range enumSymbol.Variants {
		if variant.Name == arm.Variant {
			return variant
		}
	}
	return nil
}

func containerID(container *semanticSymbol) symbolID {
	if container == nil {
		return ""
	}
	return container.ID
}

// walkExpression 遍历表达式，记录引用、字段、变体与类型。
func (b *indexBuilder) walkExpression(expression ast.Expression, fileKey string, container *semanticSymbol) {
	switch n := expression.(type) {
	case nil:
		return

	case *ast.Identifier:
		b.identifierOccurrence(n, fileKey)
		b.addExpr(n, fileKey, false)

	case *ast.CallExpression:
		b.walkCall(n, fileKey, container)

	case *ast.FieldExpression:
		b.walkExpression(n.Left, fileKey, container)
		nameSpan := trailingIdentifierSpan(n.Range, n.Name)
		var symbol *semanticSymbol
		if name, _, ok := namedTypeOf(b.analysis.Checker, n.Left); ok {
			if containerSymbol := b.index.byQualName[name]; containerSymbol != nil {
				switch containerSymbol.Kind {
				case symbolKindStruct:
					symbol = b.index.fields[name+"#"+n.Name]
				case symbolKindEnum:
					symbol = b.index.variants[name+"#"+n.Name]
				}
			}
		}
		occurrence := b.addOccurrence(fileKey, nameSpan, n.Name, symbol, b.typeTextOf(n), occurrenceField)
		if occurrence != nil && symbol != nil {
			b.addReference(symbol, site{FileKey: fileKey, Span: nameSpan})
		}
		b.addExpr(n, fileKey, false)

	case *ast.TypeApplyExpression:
		b.walkExpression(n.Function, fileKey, container)
		for _, argument := range n.Args {
			b.walkTypeRef(argument, fileKey, container, "")
		}

	case *ast.ConversionExpression:
		span := leadingIdentifierSpan(n.Range, n.Target)
		b.addOccurrence(fileKey, span, n.Target, nil, n.Target, occurrenceBuiltinType)
		for _, argument := range n.Arguments {
			b.walkExpression(argument, fileKey, container)
		}
		b.addExpr(n, fileKey, false)

	case *ast.GroupedExpression:
		b.walkExpression(n.Expr, fileKey, container)

	case *ast.PrefixExpression:
		b.walkExpression(n.Right, fileKey, container)
		b.addExpr(n, fileKey, false)

	case *ast.InfixExpression:
		b.walkExpression(n.Left, fileKey, container)
		b.walkExpression(n.Right, fileKey, container)
		b.addExpr(n, fileKey, false)

	case *ast.ArrayLiteral:
		for _, element := range n.Elements {
			b.walkExpression(element, fileKey, container)
		}
		b.addExpr(n, fileKey, false)

	case *ast.IndexExpression:
		b.walkExpression(n.Left, fileKey, container)
		b.walkExpression(n.Index, fileKey, container)
		b.addExpr(n, fileKey, false)

	case *ast.StructLiteral:
		b.walkStructLiteral(n, fileKey, container)

	case *ast.EnumLiteral:
		b.walkEnumLiteral(n, fileKey, container)

	case *ast.FunctionLiteral:
		b.walkFunctionLiteral(n, fileKey, container)

	case *ast.IntegerLiteral, *ast.RuneLiteral, *ast.FloatLiteral,
		*ast.BooleanLiteral, *ast.StringLiteral, *ast.NilLiteral:
		b.addExpr(n, fileKey, false)
	}
}

func (b *indexBuilder) walkCall(call *ast.CallExpression, fileKey string, container *semanticSymbol) {
	if field, ok := call.Function.(*ast.FieldExpression); ok {
		if enumSymbol, variantSymbol := b.enumConstructor(call, field); enumSymbol != nil && variantSymbol != nil {
			// 枚举变体构造：Type.Variant(...)；
			// 左侧类型名仍是普通引用，变体名单独记录。
			b.walkExpression(field.Left, fileKey, container)
			span := trailingIdentifierSpan(field.Range, field.Name)
			occurrence := b.addOccurrence(fileKey, span, field.Name, variantSymbol, variantSymbol.Type, occurrenceVariant)
			if occurrence != nil {
				b.addReference(variantSymbol, site{FileKey: fileKey, Span: span})
			}
			for _, argument := range call.Arguments {
				b.walkExpression(argument, fileKey, container)
			}
			signature := b.variantSignature(enumSymbol, variantSymbol)
			b.index.calls[fileKey] = append(b.index.calls[fileKey], &callSite{
				FileKey: fileKey, Span: call.Span(), Symbol: variantSymbol,
				Signature: signature, ArgumentSpans: argumentSpans(call.Arguments),
			})
			b.addExpr(call, fileKey, true)
			return
		}
	}

	b.walkExpression(call.Function, fileKey, container)
	for _, argument := range call.Arguments {
		b.walkExpression(argument, fileKey, container)
	}
	if symbol, signature := b.resolveCallee(call); symbol != nil {
		b.index.calls[fileKey] = append(b.index.calls[fileKey], &callSite{
			FileKey: fileKey, Span: call.Span(), Symbol: symbol,
			Signature: signature, ArgumentSpans: argumentSpans(call.Arguments),
		})
	}
	b.addExpr(call, fileKey, true)
}

func (b *indexBuilder) walkStructLiteral(literal *ast.StructLiteral, fileKey string, container *semanticSymbol) {
	structSymbol := b.index.byQualName[literal.TypeName]
	if structSymbol != nil && structSymbol.Kind == symbolKindStruct {
		context := &literalContext{
			Span:     literal.Span(),
			Symbol:   structSymbol,
			Provided: make(map[string]struct{}, len(literal.Fields)),
		}
		for _, field := range literal.Fields {
			context.Provided[field.Name] = struct{}{}
			context.Values = append(context.Values, field.Value.Span())
		}
		b.index.literals[fileKey] = append(b.index.literals[fileKey], context)
	}
	if span, ok := b.structLiteralTypeSpan(literal, fileKey); ok {
		name := strings.TrimSpace(b.sourceText(fileKey, span))
		occurrence := b.addOccurrence(fileKey, span, name, structSymbol, "", occurrenceTypeRef)
		if occurrence != nil && structSymbol != nil {
			b.addReference(structSymbol, site{FileKey: fileKey, Span: span})
		}
	}
	for _, argument := range literal.TypeArgs {
		b.walkTypeRef(argument, fileKey, container, "")
	}
	for _, field := range literal.Fields {
		var fieldSymbol *semanticSymbol
		if structSymbol != nil {
			fieldSymbol = b.index.fields[literal.TypeName+"#"+field.Name]
		}
		span := ast.Span{
			Start: field.Range.Start,
			End: token.Position{
				Offset: field.Range.Start.Offset + len(field.Name),
				Line:   field.Range.Start.Line,
				Column: field.Range.Start.Column + len(field.Name),
			},
		}
		occurrence := b.addOccurrence(fileKey, span, field.Name, fieldSymbol, "", occurrenceField)
		if occurrence != nil && fieldSymbol != nil {
			b.addReference(fieldSymbol, site{FileKey: fileKey, Span: span})
		}
		b.walkExpression(field.Value, fileKey, container)
	}
	b.addExpr(literal, fileKey, false)
}

func (b *indexBuilder) walkEnumLiteral(literal *ast.EnumLiteral, fileKey string, container *semanticSymbol) {
	enumSymbol := b.index.byQualName[literal.TypeName]
	variantSymbol := (*semanticSymbol)(nil)
	if enumSymbol != nil {
		variantSymbol = b.index.variants[literal.TypeName+"#"+literal.Variant]
	}
	if span, ok := b.structLiteralTypeSpan(&ast.StructLiteral{Range: literal.Range, TypeName: literal.TypeName}, fileKey); ok {
		b.addOccurrence(fileKey, span, strings.TrimSpace(b.sourceText(fileKey, span)), enumSymbol, "", occurrenceTypeRef)
	}
	for _, value := range literal.Values {
		b.walkExpression(value, fileKey, container)
	}
	if variantSymbol != nil {
		b.addReference(variantSymbol, site{FileKey: fileKey, Span: literal.Range})
	}
	b.addExpr(literal, fileKey, false)
}

func (b *indexBuilder) walkFunctionLiteral(literal *ast.FunctionLiteral, fileKey string, container *semanticSymbol) {
	bodySpan := literal.Span()
	if literal.Body != nil {
		bodySpan = literal.Body.Span()
	}
	for _, parameter := range literal.Parameters {
		local := b.ensureDeclarationSymbol(parameter, fileKey, symbolKindVariable)
		local.Kind = symbolKindVariable
		local.Keyword = ""
		local.Decl = &site{FileKey: fileKey, Span: parameter.Span()}
		local.NameSpan = parameter.Span()
		local.Container = containerID(container)
		if parameter.DeclaredType != nil {
			local.Type = strings.TrimSpace(b.sourceText(fileKey, parameter.DeclaredType.Span()))
		}
		local.Detail = local.Type
		b.assignVariableKind(local, parameter)
		b.addDeclaration(local)
		b.addScope(fileKey, bodySpan.Start.Offset, bodySpan.End.Offset, bodySpan.Start.Offset, local)
		if parameter.DeclaredType != nil {
			b.walkTypeRef(parameter.DeclaredType, fileKey, container, local.Type)
		}
	}
	b.walkTypeRef(literal.ReturnType, fileKey, container, "")
	if literal.Body != nil {
		for _, statement := range literal.Body.Statements {
			b.walkStatement(statement, fileKey, container, bodySpan)
		}
	}
	b.addExpr(literal, fileKey, false)
}

// walkTypeRef 记录类型标注中的引用。
func (b *indexBuilder) walkTypeRef(ref *ast.TypeRef, fileKey string, container *semanticSymbol, resolved string) {
	if ref == nil {
		return
	}
	simple := ref.Element == nil && ref.Result == nil && len(ref.Args) == 0 && len(ref.Params) == 0 &&
		ref.Name != "[]" && ref.Name != "?" && ref.Name != "fn"
	if simple {
		name := strings.TrimSpace(b.sourceText(fileKey, ref.Span()))
		if name == "" {
			name = lastQualifiedSegment(ref.Name)
		}
		symbol := b.index.byQualName[ref.Name]
		role := occurrenceTypeRef
		typeText := resolved
		switch {
		case symbol != nil:
			if typeText == "" {
				typeText = symbol.Type
			}
		case isBuiltinTypeName(ref.Name):
			role = occurrenceBuiltinType
			if typeText == "" {
				typeText = ref.Name
			}
		case container != nil && container.Signature != nil && containsString(container.Signature.TypeParams, ref.Name):
			role = occurrenceTypeParameter
			if typeText == "" {
				typeText = "类型参数 " + ref.Name
			}
		}
		span := ref.Span()
		occurrence := b.addOccurrence(fileKey, span, name, symbol, typeText, role)
		if occurrence != nil && symbol != nil {
			b.addReference(symbol, site{FileKey: fileKey, Span: span})
		}
		return
	}
	name := strings.TrimSpace(b.sourceText(fileKey, ref.Span()))
	b.addOccurrence(fileKey, ref.Span(), name, nil, resolved, occurrenceTypeRef)
	b.walkTypeRef(ref.Element, fileKey, container, "")
	for _, argument := range ref.Args {
		b.walkTypeRef(argument, fileKey, container, "")
	}
	for _, parameter := range ref.Params {
		b.walkTypeRef(parameter, fileKey, container, "")
	}
	b.walkTypeRef(ref.Result, fileKey, container, "")
}

// identifierOccurrence 记录一个标识符引用。
func (b *indexBuilder) identifierOccurrence(identifier *ast.Identifier, fileKey string) {
	if identifier == nil {
		return
	}
	symbol := b.bindingSymbol(identifier)
	typeText := b.typeTextOf(identifier)
	if typeText == "" && b.analysis.Checker != nil {
		if kind, ok := b.analysis.Checker.TypeOfIdentifier(identifier); ok {
			typeText = kind.String()
		}
	}
	name := b.identifierName(fileKey, identifier)
	span := identifier.Span()
	occurrence := b.addOccurrence(fileKey, span, name, symbol, typeText, occurrenceReference)
	if occurrence != nil && symbol != nil {
		b.addReference(symbol, site{FileKey: fileKey, Span: span})
	}
}

// addOccurrence 记录一个可导航位置。
func (b *indexBuilder) addOccurrence(fileKey string, span ast.Span, name string, symbol *semanticSymbol, typeText string, role occurrenceRole) *occurrence {
	if fileKey == "" || span.End.Offset <= span.Start.Offset {
		return nil
	}
	occurrence := &occurrence{
		FileKey: fileKey,
		Span:    span,
		Name:    name,
		Symbol:  symbol,
		Type:    typeText,
		Role:    role,
	}
	b.index.occurrences[fileKey] = append(b.index.occurrences[fileKey], occurrence)
	return occurrence
}

// addExpr 记录表达式类型。
func (b *indexBuilder) addExpr(expression ast.Expression, fileKey string, call bool) {
	if expression == nil || b.analysis.Checker == nil {
		return
	}
	kind, ok := b.analysis.Checker.TypeOf(expression)
	if !ok || kind == typecheck.KindInvalid {
		return
	}
	named := ""
	if name, _, ok := kind.NamedParts(); ok {
		named = name
	}
	b.index.exprs[fileKey] = append(b.index.exprs[fileKey], &exprType{
		FileKey: fileKey,
		Span:    expression.Span(),
		Type:    kind.String(),
		Named:   named,
		Call:    call,
	})
}

func (b *indexBuilder) typeTextOf(expression ast.Expression) string {
	if expression == nil || b.analysis.Checker == nil {
		return ""
	}
	if kind, ok := b.analysis.Checker.TypeOf(expression); ok {
		return kind.String()
	}
	return ""
}

func (b *indexBuilder) bindingSymbol(identifier *ast.Identifier) *semanticSymbol {
	if b.analysis.Resolver == nil || identifier == nil {
		return nil
	}
	binding, ok := b.analysis.Resolver.BindingOf(identifier)
	if !ok {
		return nil
	}
	return b.index.byBinding[binding]
}

// resolveCallee 解析调用的目标函数及其签名。
func (b *indexBuilder) resolveCallee(call *ast.CallExpression) (*semanticSymbol, *semanticSignature) {
	function := call.Function
	if apply, ok := function.(*ast.TypeApplyExpression); ok {
		function = apply.Function
	}
	identifier, ok := function.(*ast.Identifier)
	if !ok {
		return nil, nil
	}
	symbol := b.bindingSymbol(identifier)
	if symbol == nil {
		return nil, nil
	}
	if symbol.Kind == symbolKindFunction {
		return symbol, symbol.Signature
	}
	// 函数值变量：类型已知时提供仅含类型的签名。
	return symbol, functionValueSignature(symbol)
}

// functionValueSignature 从变量的函数类型构造仅含类型的签名。
func functionValueSignature(symbol *semanticSymbol) *semanticSignature {
	if symbol == nil || !symbol.typeKind.IsFunction() {
		return nil
	}
	parameters, result, ok := symbol.typeKind.FunctionParts()
	if !ok {
		return nil
	}
	signature := &semanticSignature{Return: result.String()}
	for _, parameter := range parameters {
		signature.Parameters = append(signature.Parameters, semanticParameter{Type: parameter.String()})
	}
	return signature
}

// enumConstructor 判断调用是否为枚举变体构造。
func (b *indexBuilder) enumConstructor(call *ast.CallExpression, field *ast.FieldExpression) (*semanticSymbol, *semanticSymbol) {
	name := call.EnumType
	if name == "" {
		left := field.Left
		if apply, ok := left.(*ast.TypeApplyExpression); ok {
			left = apply.Function
		}
		if identifier, ok := left.(*ast.Identifier); ok {
			if symbol := b.bindingSymbol(identifier); symbol != nil && symbol.Kind == symbolKindEnum {
				name = symbol.QualName
			}
		}
	}
	if name == "" {
		return nil, nil
	}
	enumSymbol := b.index.byQualName[name]
	if enumSymbol == nil || enumSymbol.Kind != symbolKindEnum {
		return nil, nil
	}
	if call.Variant >= 0 && call.Variant < len(enumSymbol.Variants) {
		return enumSymbol, enumSymbol.Variants[call.Variant]
	}
	for _, variant := range enumSymbol.Variants {
		if variant.Name == field.Name {
			return enumSymbol, variant
		}
	}
	return enumSymbol, nil
}

// variantSignature 用枚举定义构造变体构造器的签名。
func (b *indexBuilder) variantSignature(enumSymbol, variantSymbol *semanticSymbol) *semanticSignature {
	if enumSymbol == nil || variantSymbol == nil {
		return nil
	}
	signature := &semanticSignature{Return: enumSymbol.Type}
	if variantSymbol.Type == "" || !strings.HasPrefix(variantSymbol.Type, "(") {
		return signature
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(variantSymbol.Type, "("), ")")
	if strings.TrimSpace(inner) == "" {
		return signature
	}
	for _, field := range strings.Split(inner, ", ") {
		signature.Parameters = append(signature.Parameters, semanticParameter{Type: field})
	}
	return signature
}

// namedTypeOf 返回表达式的具名类型名（结构体/枚举）。
func namedTypeOf(checker *typecheck.Checker, expression ast.Expression) (string, []typecheck.Kind, bool) {
	if checker == nil || expression == nil {
		return "", nil, false
	}
	kind, ok := checker.TypeOf(expression)
	if !ok {
		return "", nil, false
	}
	name, arguments, named := kind.NamedParts()
	return name, arguments, named
}

// argumentSpans 返回调用实参的范围。
func argumentSpans(arguments []ast.Expression) []ast.Span {
	spans := make([]ast.Span, len(arguments))
	for index, argument := range arguments {
		spans[index] = argument.Span()
	}
	return spans
}

// trailingIdentifierSpan 返回以标识符结尾的范围中该标识符的部分。
func trailingIdentifierSpan(span ast.Span, name string) ast.Span {
	length := len(name)
	if length <= 0 || span.End.Offset-length < span.Start.Offset {
		return span
	}
	return ast.Span{
		Start: token.Position{Offset: span.End.Offset - length},
		End:   span.End,
	}
}

// leadingIdentifierSpan 返回以标识符开头的范围中该标识符的部分。
func leadingIdentifierSpan(span ast.Span, name string) ast.Span {
	length := len(name)
	if length <= 0 || span.Start.Offset+length > span.End.Offset {
		return span
	}
	return ast.Span{
		Start: span.Start,
		End:   token.Position{Offset: span.Start.Offset + length},
	}
}

// structLiteralTypeSpan 返回结构体字面量类型名的源码范围。
// Mio 语法中类型名后紧跟 '<'（泛型实参）或 '{'（字段列表）。
func (b *indexBuilder) structLiteralTypeSpan(literal *ast.StructLiteral, fileKey string) (ast.Span, bool) {
	text := b.analysis.textFor(fileKey)
	start := literal.Range.Start.Offset
	if start < 0 || start >= len(text) {
		return ast.Span{}, false
	}
	end := start
	for end < len(text) {
		switch text[end] {
		case '<', '{':
			if end == start {
				return ast.Span{}, false
			}
			return ast.Span{Start: literal.Range.Start, End: token.Position{Offset: end}}, true
		case ' ', '\t', '\r', '\n':
			if end == start {
				return ast.Span{}, false
			}
			return ast.Span{Start: literal.Range.Start, End: token.Position{Offset: end}}, true
		}
		end++
	}
	return ast.Span{}, false
}

func isBuiltinTypeName(name string) bool {
	switch name {
	case "int8", "int16", "int32", "int64", "uint8", "uint16", "uint32",
		"float32", "float64", "bool", "string", "unit", "rune", "resource", "Map", "[]", "?", "fn":
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
