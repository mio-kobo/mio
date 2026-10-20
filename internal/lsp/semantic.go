package lsp

import (
	"sort"
	"strconv"
	"strings"

	"Mio/internal/ast"
	"Mio/internal/bunko"
	"Mio/internal/resolver"
	"Mio/internal/token"
	"Mio/internal/typecheck"
)

// LSP SymbolKind 取值（只使用与 Mio 语义对应的子集）。
const (
	symbolKindModule     = 2
	symbolKindField      = 8
	symbolKindEnum       = 10
	symbolKindFunction   = 12
	symbolKindVariable   = 13
	symbolKindConstant   = 14
	symbolKindEnumMember = 22
	symbolKindStruct     = 23
)

// symbolID 是语义索引中的稳定符号身份。
// Resolver 绑定的符号使用 resolver 分配的 ID；字段/变体使用"类型稳定名#成员名"；
// 常量与未绑定声明使用"文件+偏移"。绝不使用裸名称作为身份。
type symbolID string

// site 表示某个文件中的一个源码范围。
type site struct {
	FileKey string
	Span    ast.Span
}

// semanticParameter 描述函数形参的名称与类型（用于签名提示）。
type semanticParameter struct {
	Name string
	Type string
}

// semanticSignature 保存已解析的函数签名。
type semanticSignature struct {
	TypeParams []string
	Parameters []semanticParameter
	Return     string
}

// label 生成 SignatureHelp 使用的单行签名。
func (s *semanticSignature) label(name string) string {
	parts := make([]string, len(s.Parameters))
	for index, parameter := range s.Parameters {
		switch {
		case parameter.Name == "":
			parts[index] = parameter.Type
		case parameter.Type == "":
			parts[index] = parameter.Name
		default:
			parts[index] = parameter.Name + ": " + parameter.Type
		}
	}
	prefix := name
	if len(s.TypeParams) > 0 {
		prefix += "<" + strings.Join(s.TypeParams, ", ") + ">"
	}
	result := prefix + "(" + strings.Join(parts, ", ") + ")"
	if s.Return != "" {
		result += " -> " + s.Return
	}
	return result
}

// semanticSymbol 是语义索引中的一个符号。
type semanticSymbol struct {
	ID        symbolID
	Name      string // 源码拼写（绝不是 pkg::name 稳定名）
	QualName  string // 稳定名（可能包含 pkg:: 前缀）
	Kind      int
	Keyword   string // 声明关键字（kaku / waza / teisu / kata / sentaku），用于悬停
	Detail    string
	Type      string
	Doc       string
	Decl      *site
	NameSpan  ast.Span
	FileKey   string
	Container symbolID
	Signature *semanticSignature
	Params    []*semanticSymbol
	Fields    []*semanticSymbol
	Variants  []*semanticSymbol
	Exported  bool

	binding    resolver.Symbol
	hasBinding bool
	// typeKind 保存变量的内部类型（用于函数值签名推断）。
	typeKind typecheck.Kind
}

// nameSite 返回声明名称的位置；定义跳转与 includeDeclaration 使用它。
func (s *semanticSymbol) nameSite() site {
	if s == nil {
		return site{}
	}
	if s.FileKey != "" && s.NameSpan.End.Offset > s.NameSpan.Start.Offset {
		return site{FileKey: s.FileKey, Span: s.NameSpan}
	}
	if s.Decl != nil {
		return *s.Decl
	}
	return site{}
}

// fullyQualified 返回用于展示的限定名。
func (s *semanticSymbol) displayName() string {
	if s == nil {
		return ""
	}
	if s.Name != "" {
		return s.Name
	}
	return lastQualifiedSegment(s.QualName)
}

// occurrenceRole 描述一个引用节点的类别。
type occurrenceRole int

const (
	occurrenceReference occurrenceRole = iota
	occurrenceField
	occurrenceVariant
	occurrenceTypeRef
	occurrenceBuiltinType
	occurrenceTypeParameter
)

// occurrence 记录一个可导航的引用位置。
type occurrence struct {
	FileKey string
	Span    ast.Span
	Name    string
	Symbol  *semanticSymbol
	Type    string
	Role    occurrenceRole
}

// exprType 记录已推断类型表达式的位置。
// Type 是展示文本；Named 是具名类型（结构体/枚举）的稳定名，用于成员补全。
type exprType struct {
	FileKey string
	Span    ast.Span
	Type    string
	Named   string
	Call    bool
}

// callSite 记录一次调用，用于 SignatureHelp。
type callSite struct {
	FileKey       string
	Span          ast.Span
	Symbol        *semanticSymbol
	Signature     *semanticSignature
	ArgumentSpans []ast.Span
}

// literalContext 记录结构体字面量，用于字段补全。
type literalContext struct {
	Span     ast.Span
	Symbol   *semanticSymbol
	Values   []ast.Span
	Provided map[string]struct{}
}

// scopeEntry 记录一个声明的词法可见范围，用于补全时判断可见性。
type scopeEntry struct {
	FileKey     string
	Start       int
	End         int
	VisibleFrom int
	Symbol      *semanticSymbol
}

// semanticIndex 是一份分析快照的只读语义索引。
type semanticIndex struct {
	analysis *analysis
	docKey   string
	imports  []bunko.Import

	symbols      map[symbolID]*semanticSymbol
	byBinding    map[resolver.Symbol]*semanticSymbol
	byQualName   map[string]*semanticSymbol
	byName       map[string][]*semanticSymbol
	fields       map[string]*semanticSymbol // "类型稳定名#字段名"
	variants     map[string]*semanticSymbol // "类型稳定名#变体名"
	topLevel     map[string][]*semanticSymbol
	declarations map[string][]*semanticSymbol // fileKey → 全部带声明位置的符号
	byDeclSpan   map[string]map[spanKey]*semanticSymbol
	refs         map[symbolID][]site
	occurrences  map[string][]*occurrence
	exprs        map[string][]*exprType
	calls        map[string][]*callSite
	scopes       map[string][]*scopeEntry
	literals     map[string][]*literalContext
}

// buildSemanticIndex 从已完成解析/类型检查的 AST 构建统一语义索引。
func buildSemanticIndex(result *analysis, captured *capturedSource) *semanticIndex {
	index := &semanticIndex{
		analysis:     result,
		docKey:       result.DocKey,
		imports:      append([]bunko.Import(nil), result.Imports...),
		symbols:      make(map[symbolID]*semanticSymbol),
		byBinding:    make(map[resolver.Symbol]*semanticSymbol),
		byQualName:   make(map[string]*semanticSymbol),
		byName:       make(map[string][]*semanticSymbol),
		fields:       make(map[string]*semanticSymbol),
		variants:     make(map[string]*semanticSymbol),
		topLevel:     make(map[string][]*semanticSymbol),
		declarations: make(map[string][]*semanticSymbol),
		byDeclSpan:   make(map[string]map[spanKey]*semanticSymbol),
		refs:         make(map[symbolID][]site),
		occurrences:  make(map[string][]*occurrence),
		exprs:        make(map[string][]*exprType),
		calls:        make(map[string][]*callSite),
		scopes:       make(map[string][]*scopeEntry),
		literals:     make(map[string][]*literalContext),
	}
	builder := &indexBuilder{analysis: result, index: index, captured: captured}
	builder.declareConsts()
	builder.declareProgram(result.Program)
	builder.walkProgram(result.Program)
	builder.finish()
	return index
}

// ---- 声明收集 ----

// indexBuilder 负责一次索引构建。所有方法都只在构建阶段调用。
type indexBuilder struct {
	analysis *analysis
	index    *semanticIndex
	captured *capturedSource
}

// sourceText 返回文件键对应的源码文本。
func (b *indexBuilder) sourceText(fileKey string, span ast.Span) string {
	text := b.analysis.textFor(fileKey)
	if span.Start.Offset < 0 || span.End.Offset > len(text) || span.Start.Offset >= span.End.Offset {
		return ""
	}
	return text[span.Start.Offset:span.End.Offset]
}

// identifierName 优先返回源码拼写，退化时去掉 pkg:: 前缀。
func (b *indexBuilder) identifierName(fileKey string, identifier *ast.Identifier) string {
	if identifier == nil {
		return ""
	}
	if name := strings.TrimSpace(b.sourceText(fileKey, identifier.Span())); name != "" {
		return name
	}
	return lastQualifiedSegment(identifier.Name)
}

// newSymbol 注册一个新的语义符号。
func (b *indexBuilder) newSymbol(id symbolID, name, fileKey string, span ast.Span, kind int) *semanticSymbol {
	symbol := &semanticSymbol{
		ID:       id,
		Name:     name,
		Kind:     kind,
		FileKey:  fileKey,
		NameSpan: span,
		Exported: true,
	}
	b.index.symbols[id] = symbol
	if name != "" {
		b.index.byName[name] = append(b.index.byName[name], symbol)
	}
	return symbol
}

// symbolFromBinding 复用 Resolver 绑定创建符号；同一绑定只创建一次。
func (b *indexBuilder) symbolFromBinding(identifier *ast.Identifier, fileKey string, kind int) *semanticSymbol {
	if b.analysis.Resolver == nil || identifier == nil {
		return nil
	}
	binding, ok := b.analysis.Resolver.BindingOf(identifier)
	if !ok {
		return nil
	}
	if existing, found := b.index.byBinding[binding]; found {
		return existing
	}
	symbol := b.newSymbol(
		symbolID("binding:"+strconv.FormatUint(binding.ID, 10)),
		b.identifierName(fileKey, identifier),
		fileKey,
		identifier.Span(),
		kind,
	)
	symbol.binding = binding
	symbol.hasBinding = true
	symbol.QualName = binding.Name
	b.index.byBinding[binding] = symbol
	return symbol
}

// ensureDeclarationSymbol 返回声明符号；没有 Resolver 绑定时退化为按位置标识的符号。
func (b *indexBuilder) ensureDeclarationSymbol(identifier *ast.Identifier, fileKey string, kind int) *semanticSymbol {
	if symbol := b.symbolFromBinding(identifier, fileKey, kind); symbol != nil {
		return symbol
	}
	id := symbolID("decl:" + fileKey + ":" + strconv.Itoa(identifier.Span().Start.Offset))
	if existing, found := b.index.symbols[id]; found {
		return existing
	}
	symbol := b.newSymbol(id, b.identifierName(fileKey, identifier), fileKey, identifier.Span(), kind)
	symbol.QualName = identifier.Name
	return symbol
}

func (b *indexBuilder) addDeclaration(symbol *semanticSymbol) {
	if symbol == nil || symbol.Decl == nil {
		return
	}
	b.index.declarations[symbol.FileKey] = append(b.index.declarations[symbol.FileKey], symbol)
	spans := b.index.byDeclSpan[symbol.FileKey]
	if spans == nil {
		spans = make(map[spanKey]*semanticSymbol)
		b.index.byDeclSpan[symbol.FileKey] = spans
	}
	if _, exists := spans[keyOfSpan(symbol.NameSpan)]; !exists {
		spans[keyOfSpan(symbol.NameSpan)] = symbol
	}
}

// declaredSymbol 返回声明标识符对应的语义符号。
func (b *indexBuilder) declaredSymbol(identifier *ast.Identifier, fileKey string) *semanticSymbol {
	if identifier == nil {
		return nil
	}
	if symbol := b.bindingSymbol(identifier); symbol != nil {
		return symbol
	}
	if spans, ok := b.index.byDeclSpan[fileKey]; ok {
		return spans[keyOfSpan(identifier.Span())]
	}
	return nil
}

func (b *indexBuilder) addTopLevel(fileKey string, symbol *semanticSymbol) {
	if symbol == nil {
		return
	}
	b.index.topLevel[fileKey] = append(b.index.topLevel[fileKey], symbol)
	if symbol.QualName != "" {
		if _, exists := b.index.byQualName[symbol.QualName]; !exists {
			b.index.byQualName[symbol.QualName] = symbol
		}
	}
}

// declareConsts 使用常量折叠前的捕获结果登记 teisu 声明。
func (b *indexBuilder) declareConsts() {
	if b.captured == nil {
		return
	}
	for index, declaration := range b.captured.consts {
		fileKey := b.captured.constFiles[index]
		name := b.identifierName(fileKey, declaration.Name)
		id := symbolID("const:" + fileKey + ":" + strconv.Itoa(declaration.Name.Span().Start.Offset))
		symbol := b.newSymbol(id, name, fileKey, declaration.Name.Span(), symbolKindConstant)
		symbol.QualName = declaration.Name.Name
		symbol.Keyword = "teisu"
		symbol.Exported = declaration.Exported
		symbol.Detail = "teisu " + name
		if declaration.Name.DeclaredType != nil {
			symbol.Type = strings.TrimSpace(b.sourceText(fileKey, declaration.Name.DeclaredType.Span()))
			symbol.Detail += ": " + symbol.Type
		}
		span := declaration.Span()
		symbol.Decl = &site{FileKey: fileKey, Span: span}
		symbol.Doc = docComment(b.analysis.textFor(fileKey), declaration.Span().Start.Line)
		b.addDeclaration(symbol)
		b.addTopLevel(fileKey, symbol)
		b.addScope(fileKey, 0, len(b.analysis.textFor(fileKey)), 0, symbol)
	}
}

// declareProgram 收集所有顶层声明；必须在遍历引用之前完成。
func (b *indexBuilder) declareProgram(program *ast.Program) {
	if program == nil {
		return
	}
	for _, statement := range program.Statements {
		fileKey := b.analysis.statementFileKey(statement)
		if fileKey == "" {
			continue
		}
		switch declaration := statement.(type) {
		case *ast.FunctionStatement:
			b.declareFunction(declaration, fileKey)
		case *ast.StructStatement:
			b.declareStruct(declaration, fileKey)
		case *ast.EnumStatement:
			b.declareEnum(declaration, fileKey)
		case *ast.LetStatement:
			b.declareTopLevelLet(declaration, fileKey)
		case *ast.ImportStatement:
			b.declareImport(declaration, fileKey)
		}
	}
}

func (b *indexBuilder) declareFunction(function *ast.FunctionStatement, fileKey string) {
	fileText := b.analysis.textFor(fileKey)
	symbol := b.ensureDeclarationSymbol(function.Name, fileKey, symbolKindFunction)
	symbol.Kind = symbolKindFunction
	symbol.Keyword = "waza"
	symbol.Exported = function.Exported
	symbol.Decl = &site{FileKey: fileKey, Span: function.Span()}
	symbol.NameSpan = function.Name.Span()
	symbol.Doc = docComment(fileText, function.Span().Start.Line)
	symbol.Detail = "waza " + symbol.displayName()
	if len(function.TypeParams) > 0 {
		symbol.Detail += "<" + strings.Join(function.TypeParams, ", ") + ">"
	}
	symbol.Signature = b.functionSignature(function, fileKey)
	if symbol.Signature != nil {
		if symbol.Signature.Return != "" {
			symbol.Detail += " -> " + symbol.Signature.Return
		}
		symbol.Type = symbol.Signature.Return
	}
	b.addDeclaration(symbol)
	b.addTopLevel(fileKey, symbol)
	b.addScope(fileKey, 0, len(fileText), 0, symbol)

	bodySpan := function.Span()
	if function.Body != nil {
		bodySpan = function.Body.Span()
	}
	for index, parameter := range function.Parameters {
		parameterSymbol := b.ensureDeclarationSymbol(parameter, fileKey, symbolKindVariable)
		parameterSymbol.Kind = symbolKindVariable
		parameterSymbol.Keyword = ""
		parameterSymbol.Container = symbol.ID
		parameterSymbol.Decl = &site{FileKey: fileKey, Span: parameter.Span()}
		parameterSymbol.NameSpan = parameter.Span()
		parameterSymbol.Doc = ""
		switch {
		case symbol.Signature != nil && index < len(symbol.Signature.Parameters):
			parameterSymbol.Type = symbol.Signature.Parameters[index].Type
		case parameter.DeclaredType != nil:
			parameterSymbol.Type = strings.TrimSpace(b.sourceText(fileKey, parameter.DeclaredType.Span()))
		}
		parameterSymbol.Detail = parameterSymbol.Type
		b.assignVariableKind(parameterSymbol, parameter)
		symbol.Params = append(symbol.Params, parameterSymbol)
		b.addDeclaration(parameterSymbol)
		b.addScope(fileKey, bodySpan.Start.Offset, bodySpan.End.Offset, bodySpan.Start.Offset, parameterSymbol)
	}
}

func (b *indexBuilder) declareStruct(declaration *ast.StructStatement, fileKey string) {
	fileText := b.analysis.textFor(fileKey)
	symbol := b.ensureDeclarationSymbol(declaration.Name, fileKey, symbolKindStruct)
	symbol.Kind = symbolKindStruct
	symbol.Keyword = "kata"
	symbol.Exported = declaration.Exported
	symbol.Decl = &site{FileKey: fileKey, Span: declaration.Span()}
	symbol.NameSpan = declaration.Name.Span()
	symbol.Doc = docComment(fileText, declaration.Span().Start.Line)
	symbol.Detail = "kata " + symbol.displayName()
	if len(declaration.TypeParams) > 0 {
		symbol.Detail += "<" + strings.Join(declaration.TypeParams, ", ") + ">"
	}
	symbol.Type = symbol.displayName()
	b.addDeclaration(symbol)
	b.addTopLevel(fileKey, symbol)
	b.addScope(fileKey, 0, len(fileText), 0, symbol)

	definition, hasDefinition := typecheck.StructDefinition{}, false
	if b.analysis.Checker != nil {
		definition, hasDefinition = b.analysis.Checker.StructDefinitionOf(symbol.QualName)
	}
	for index, field := range declaration.Fields {
		fieldSymbol := b.newSymbol(
			symbolID("field:"+symbol.QualName+"#"+field.Name),
			b.identifierName(fileKey, field),
			fileKey,
			field.Span(),
			symbolKindField,
		)
		fieldSymbol.QualName = symbol.QualName + "#" + field.Name
		fieldSymbol.Container = symbol.ID
		fieldSymbol.Decl = &site{FileKey: fileKey, Span: field.Span()}
		fieldSymbol.NameSpan = field.Span()
		fieldSymbol.Type = b.fieldType(definition, hasDefinition, index, field, fileKey)
		fieldSymbol.Detail = fieldSymbol.Type
		fieldSymbol.Doc = docCommentAt(fileText, field.Span().Start.Line, declaration.Span().Start.Line)
		symbol.Fields = append(symbol.Fields, fieldSymbol)
		b.index.fields[symbol.QualName+"#"+field.Name] = fieldSymbol
		b.addDeclaration(fieldSymbol)
	}
}

func (b *indexBuilder) declareEnum(declaration *ast.EnumStatement, fileKey string) {
	fileText := b.analysis.textFor(fileKey)
	symbol := b.ensureDeclarationSymbol(declaration.Name, fileKey, symbolKindEnum)
	symbol.Kind = symbolKindEnum
	symbol.Keyword = "sentaku"
	symbol.Exported = declaration.Exported
	symbol.Decl = &site{FileKey: fileKey, Span: declaration.Span()}
	symbol.NameSpan = declaration.Name.Span()
	symbol.Doc = docComment(fileText, declaration.Span().Start.Line)
	symbol.Detail = "sentaku " + symbol.displayName()
	if len(declaration.TypeParams) > 0 {
		symbol.Detail += "<" + strings.Join(declaration.TypeParams, ", ") + ">"
	}
	symbol.Type = symbol.displayName()
	b.addDeclaration(symbol)
	b.addTopLevel(fileKey, symbol)
	b.addScope(fileKey, 0, len(fileText), 0, symbol)

	definition, hasDefinition := typecheck.EnumDefinition{}, false
	if b.analysis.Checker != nil {
		definition, hasDefinition = b.analysis.Checker.EnumDefinitionOf(symbol.QualName)
	}
	for index, variant := range declaration.Variants {
		variantSymbol := b.newSymbol(
			symbolID("variant:"+symbol.QualName+"#"+variant.Name),
			variant.Name,
			fileKey,
			variant.Range,
			symbolKindEnumMember,
		)
		variantSymbol.QualName = symbol.QualName + "#" + variant.Name
		variantSymbol.Container = symbol.ID
		variantSymbol.Decl = &site{FileKey: fileKey, Span: variant.Range}
		variantSymbol.NameSpan = variant.Range
		variantSymbol.Detail = symbol.displayName() + "." + variant.Name
		if hasDefinition && index < len(definition.Variants) {
			fields := definition.Variants[index].Fields
			if len(fields) > 0 {
				parts := make([]string, len(fields))
				for fieldIndex, field := range fields {
					parts[fieldIndex] = field.String()
				}
				variantSymbol.Type = "(" + strings.Join(parts, ", ") + ")"
				variantSymbol.Detail += variantSymbol.Type
			}
		}
		symbol.Variants = append(symbol.Variants, variantSymbol)
		b.index.variants[symbol.QualName+"#"+variant.Name] = variantSymbol
		b.addDeclaration(variantSymbol)
	}
}

func (b *indexBuilder) declareTopLevelLet(declaration *ast.LetStatement, fileKey string) {
	fileText := b.analysis.textFor(fileKey)
	symbol := b.ensureDeclarationSymbol(declaration.Name, fileKey, symbolKindVariable)
	symbol.Kind = symbolKindVariable
	symbol.Keyword = "kaku"
	symbol.Decl = &site{FileKey: fileKey, Span: declaration.Span()}
	symbol.NameSpan = declaration.Name.Span()
	symbol.Doc = docComment(fileText, declaration.Span().Start.Line)
	symbol.Type = b.variableType(declaration.Name, fileKey)
	symbol.Detail = symbol.Type
	b.assignVariableKind(symbol, declaration.Name)
	b.addDeclaration(symbol)
	b.addTopLevel(fileKey, symbol)
	b.addScope(fileKey, 0, len(fileText), declaration.Span().End.Offset, symbol)
}

func (b *indexBuilder) declareImport(declaration *ast.ImportStatement, fileKey string) {
	symbol := b.newSymbol(
		symbolID("import:"+fileKey+":"+strconv.Itoa(declaration.Span().Start.Offset)),
		declaration.Alias,
		fileKey,
		declaration.Span(),
		symbolKindModule,
	)
	symbol.QualName = declaration.Path
	symbol.Decl = &site{FileKey: fileKey, Span: declaration.Span()}
	symbol.NameSpan = declaration.Span()
	symbol.Detail = "yobu \"" + declaration.Path + "\""
	b.addDeclaration(symbol)
	b.addTopLevel(fileKey, symbol)
}

// functionSignature 优先使用 TypeChecker 的解析结果，退化时使用源码类型标注。
func (b *indexBuilder) functionSignature(function *ast.FunctionStatement, fileKey string) *semanticSignature {
	signature := &semanticSignature{TypeParams: append([]string(nil), function.TypeParams...)}
	resolved, hasSignature := typecheck.FunctionSignature{}, false
	if b.analysis.Checker != nil {
		resolved, hasSignature = b.analysis.Checker.SignatureOf(function.Name)
	}
	for index, parameter := range function.Parameters {
		parameterType := ""
		switch {
		case hasSignature && index < len(resolved.ParameterTypes):
			parameterType = resolved.ParameterTypes[index].String()
		case parameter.DeclaredType != nil:
			parameterType = strings.TrimSpace(b.sourceText(fileKey, parameter.DeclaredType.Span()))
		}
		signature.Parameters = append(signature.Parameters, semanticParameter{
			Name: b.identifierName(fileKey, parameter),
			Type: parameterType,
		})
	}
	switch {
	case hasSignature:
		signature.Return = resolved.ReturnType.String()
	case function.ReturnType != nil:
		signature.Return = strings.TrimSpace(b.sourceText(fileKey, function.ReturnType.Span()))
	}
	return signature
}

// assignVariableKind 记录变量/参数/绑定的内部类型。
func (b *indexBuilder) assignVariableKind(symbol *semanticSymbol, identifier *ast.Identifier) {
	if symbol == nil || identifier == nil || b.analysis.Checker == nil {
		return
	}
	kind, ok := b.analysis.Checker.TypeOfIdentifier(identifier)
	if !ok {
		return
	}
	symbol.typeKind = kind
	if symbol.Type == "" {
		symbol.Type = kind.String()
	}
}

// variableType 返回变量声明的静态类型。
func (b *indexBuilder) variableType(identifier *ast.Identifier, fileKey string) string {
	if b.analysis.Checker != nil {
		if kind, ok := b.analysis.Checker.TypeOfIdentifier(identifier); ok {
			return kind.String()
		}
	}
	if identifier.DeclaredType != nil {
		return strings.TrimSpace(b.sourceText(fileKey, identifier.DeclaredType.Span()))
	}
	return ""
}

// fieldType 返回结构体字段的已解析类型。
func (b *indexBuilder) fieldType(definition typecheck.StructDefinition, hasDefinition bool, index int, field *ast.Identifier, fileKey string) string {
	if hasDefinition && index < len(definition.Fields) {
		return definition.Fields[index].Type.String()
	}
	if field.DeclaredType != nil {
		return strings.TrimSpace(b.sourceText(fileKey, field.DeclaredType.Span()))
	}
	return ""
}

// addScope 记录声明的词法可见范围。
func (b *indexBuilder) addScope(fileKey string, start, end, visibleFrom int, symbol *semanticSymbol) {
	if end < start {
		end = start
	}
	b.index.scopes[fileKey] = append(b.index.scopes[fileKey], &scopeEntry{
		FileKey:     fileKey,
		Start:       start,
		End:         end,
		VisibleFrom: visibleFrom,
		Symbol:      symbol,
	})
}

// finish 排序索引中的位置列表，并补全常量引用。
func (b *indexBuilder) finish() {
	for fileKey, occurrences := range b.index.occurrences {
		sort.SliceStable(occurrences, func(left, right int) bool {
			return occurrences[left].Span.Start.Offset < occurrences[right].Span.Start.Offset
		})
		b.index.occurrences[fileKey] = occurrences
	}
	for fileKey, expressions := range b.index.exprs {
		sort.SliceStable(expressions, func(left, right int) bool {
			return expressions[left].Span.Start.Offset < expressions[right].Span.Start.Offset
		})
		b.index.exprs[fileKey] = expressions
	}
	for fileKey, calls := range b.index.calls {
		sort.SliceStable(calls, func(left, right int) bool {
			return calls[left].Span.Start.Offset < calls[right].Span.Start.Offset
		})
		b.index.calls[fileKey] = calls
	}
	for fileKey, declarations := range b.index.declarations {
		sort.SliceStable(declarations, func(left, right int) bool {
			return declarations[left].NameSpan.Start.Offset < declarations[right].NameSpan.Start.Offset
		})
		b.index.declarations[fileKey] = declarations
	}
	for fileKey, topLevel := range b.index.topLevel {
		sort.SliceStable(topLevel, func(left, right int) bool {
			return topLevel[left].NameSpan.Start.Offset < topLevel[right].NameSpan.Start.Offset
		})
		b.index.topLevel[fileKey] = topLevel
	}
	b.resolveConstantReferences()
}

// resolveConstantReferences 处理 constant.Fold 之后消失的常量名引用。
func (b *indexBuilder) resolveConstantReferences() {
	if b.captured == nil {
		return
	}
	// 记录每个文件现有的引用起点，用于判断常量名是否仍以标识符存在。
	existing := make(map[string]map[spanKey]*occurrence)
	for fileKey, occurrences := range b.index.occurrences {
		spans := make(map[spanKey]*occurrence, len(occurrences))
		for _, occurrence := range occurrences {
			spans[keyOfSpan(occurrence.Span)] = occurrence
		}
		existing[fileKey] = spans
	}
	for index, declaration := range b.captured.consts {
		fileKey := b.captured.constFiles[index]
		if fileKey == "" {
			continue
		}
		name := b.identifierName(fileKey, declaration.Name)
		constant, ok := b.index.symbols[symbolID("const:"+fileKey+":"+strconv.Itoa(declaration.Name.Span().Start.Offset))]
		if !ok {
			continue
		}
		spans := b.captured.identifiers[fileKey]
		keys := make([]spanKey, 0, len(spans))
		for key, identifierName := range spans {
			if identifierName != name {
				continue
			}
			keys = append(keys, key)
		}
		sort.Slice(keys, func(left, right int) bool { return keys[left].start < keys[right].start })
		for _, key := range keys {
			span := ast.Span{
				Start: token.Position{Offset: key.start},
				End:   token.Position{Offset: key.end},
			}
			if occurrence, found := existing[fileKey][key]; found {
				if occurrence.Symbol == nil {
					occurrence.Symbol = constant
					occurrence.Type = constant.Type
					b.addReference(constant, site{FileKey: fileKey, Span: span})
				}
				continue
			}
			occurrence := &occurrence{
				FileKey: fileKey,
				Span:    span,
				Name:    name,
				Symbol:  constant,
				Type:    constant.Type,
				Role:    occurrenceReference,
			}
			b.index.occurrences[fileKey] = append(b.index.occurrences[fileKey], occurrence)
			b.addReference(constant, site{FileKey: fileKey, Span: span})
		}
	}
	for fileKey, occurrences := range b.index.occurrences {
		sort.SliceStable(occurrences, func(left, right int) bool {
			return occurrences[left].Span.Start.Offset < occurrences[right].Span.Start.Offset
		})
		b.index.occurrences[fileKey] = occurrences
	}
}

// addReference 记录一次引用；声明位置不计入引用列表。
func (b *indexBuilder) addReference(symbol *semanticSymbol, location site) {
	if symbol == nil {
		return
	}
	b.index.refs[symbol.ID] = append(b.index.refs[symbol.ID], location)
}

// ---- 查询 ----

// declarationAt 返回光标命中的声明名称（最窄者优先）。
func (i *semanticIndex) declarationAt(fileKey string, offset int) *semanticSymbol {
	var best *semanticSymbol
	for _, symbol := range i.declarations[fileKey] {
		if offset < symbol.NameSpan.Start.Offset || offset > symbol.NameSpan.End.Offset {
			continue
		}
		if best == nil || i.spanWidth(symbol.NameSpan) < i.spanWidth(best.NameSpan) {
			best = symbol
		}
	}
	return best
}

// occurrenceAt 返回光标命中的引用（最窄者优先）。
func (i *semanticIndex) occurrenceAt(fileKey string, offset int) *occurrence {
	var best *occurrence
	for _, occurrence := range i.occurrences[fileKey] {
		if offset < occurrence.Span.Start.Offset || offset > occurrence.Span.End.Offset {
			continue
		}
		if best == nil || i.spanWidth(occurrence.Span) < i.spanWidth(best.Span) {
			best = occurrence
		}
	}
	return best
}

// exprAt 返回光标命中的表达式类型（最窄者优先）。
func (i *semanticIndex) exprAt(fileKey string, offset int) *exprType {
	var best *exprType
	for _, expression := range i.exprs[fileKey] {
		if offset < expression.Span.Start.Offset || offset > expression.Span.End.Offset {
			continue
		}
		if best == nil || i.spanWidth(expression.Span) < i.spanWidth(best.Span) {
			best = expression
		}
	}
	return best
}

// callAt 返回包含光标的最内层调用。
func (i *semanticIndex) callAt(fileKey string, offset int) *callSite {
	var best *callSite
	for _, call := range i.calls[fileKey] {
		if offset < call.Span.Start.Offset || offset > call.Span.End.Offset {
			continue
		}
		if best == nil || i.spanWidth(call.Span) < i.spanWidth(best.Span) {
			best = call
		}
	}
	return best
}

func (i *semanticIndex) spanWidth(span ast.Span) int {
	width := span.End.Offset - span.Start.Offset
	if width < 0 {
		return 0
	}
	return width
}

// references 返回符号的全部引用位置。
func (i *semanticIndex) references(symbol *semanticSymbol) []site {
	if symbol == nil {
		return nil
	}
	locations := i.refs[symbol.ID]
	copied := make([]site, len(locations))
	copy(copied, locations)
	sort.SliceStable(copied, func(left, right int) bool {
		if copied[left].FileKey == copied[right].FileKey {
			return copied[left].Span.Start.Offset < copied[right].Span.Start.Offset
		}
		return copied[left].FileKey < copied[right].FileKey
	})
	return copied
}

// visibleSymbols 返回光标处可见的作用域符号（内层声明遮蔽外层同名符号）。
func (i *semanticIndex) visibleSymbols(fileKey string, offset int) []*semanticSymbol {
	entries := make([]*scopeEntry, 0)
	for _, entry := range i.scopes[fileKey] {
		if entry.Start > offset || entry.End < offset {
			continue
		}
		if entry.VisibleFrom > offset {
			continue
		}
		entries = append(entries, entry)
	}
	// 范围越小越内层，遮蔽外层同名符号。
	sort.SliceStable(entries, func(left, right int) bool {
		return entries[left].End-entries[left].Start < entries[right].End-entries[right].Start
	})
	seen := make(map[string]struct{}, len(entries))
	result := make([]*semanticSymbol, 0, len(entries))
	for _, entry := range entries {
		name := entry.Symbol.displayName()
		if name == "" {
			continue
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, entry.Symbol)
	}
	return result
}

// exportedSymbols 返回某个 Bunko 导入路径导出的符号。
func (i *semanticIndex) exportedSymbols(importPath string) []*semanticSymbol {
	prefix := importPath + "::"
	result := make([]*semanticSymbol, 0)
	for qualName, symbol := range i.byQualName {
		if !strings.HasPrefix(qualName, prefix) || !symbol.Exported {
			continue
		}
		if symbol.Container != "" {
			continue
		}
		result = append(result, symbol)
	}
	sort.SliceStable(result, func(left, right int) bool {
		return result[left].displayName() < result[right].displayName()
	})
	return result
}

// importPathForAlias 返回文档导入别名对应的公开路径。
func (i *semanticIndex) importPathForAlias(alias string) (string, bool) {
	for _, imported := range i.imports {
		if imported.Alias == alias {
			return imported.Path, true
		}
	}
	return "", false
}

// byDeclarationSite 通过物理声明位置查找符号。
// 同一个文件作为入口或被导入时稳定名前缀不同，但源码位置相同，
// 因此这是跨分析合并引用的可靠身份。
func (i *semanticIndex) byDeclarationSite(location site) *semanticSymbol {
	if location.FileKey == "" || location.Span.End.Offset <= location.Span.Start.Offset {
		return nil
	}
	spans := i.byDeclSpan[location.FileKey]
	if spans == nil {
		return nil
	}
	return spans[keyOfSpan(location.Span)]
}

// symbolForQualifiedType 返回具名类型的声明符号。
func (i *semanticIndex) symbolForQualifiedType(name string) *semanticSymbol {
	return i.byQualName[name]
}

// ---- 文本辅助 ----

// lastQualifiedSegment 去掉 pkg:: 前缀，返回源码中的最后一段名称。
func lastQualifiedSegment(name string) string {
	if index := strings.LastIndex(name, "::"); index >= 0 {
		return name[index+2:]
	}
	return name
}

// docComment 收集声明上一段连续的 // 注释。
func docComment(text string, line int) string {
	if line <= 1 || text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	if line > len(lines) {
		return ""
	}
	collected := make([]string, 0, 4)
	for index := line - 2; index >= 0; index-- {
		trimmed := strings.TrimSpace(strings.TrimSuffix(lines[index], "\r"))
		if !strings.HasPrefix(trimmed, "//") {
			break
		}
		collected = append(collected, strings.TrimSpace(strings.TrimPrefix(trimmed, "//")))
	}
	for left, right := 0, len(collected)-1; left < right; left, right = left+1, right-1 {
		collected[left], collected[right] = collected[right], collected[left]
	}
	return strings.TrimSpace(strings.Join(collected, "\n"))
}

// docCommentAt 收集字段上方的注释，但不越过所属声明。
func docCommentAt(text string, line, declarationLine int) string {
	if line <= declarationLine {
		return ""
	}
	return docComment(text, line)
}
