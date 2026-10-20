package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"Mio/internal/token"
)

// LSP CompletionItemKind 取值
const (
	completionKindFunction      = 3
	completionKindField         = 5
	completionKindVariable      = 6
	completionKindClass         = 7
	completionKindModule        = 9
	completionKindEnum          = 13
	completionKindKeyword       = 14
	completionKindEnumMember    = 20
	completionKindConstant      = 21
	completionKindStruct        = 22
	completionKindTypeParameter = 25
)

// textDocumentSyncKindFull / Incremental 的补全插入格式
const insertTextFormatSnippet = 2

// requestCompletion 实现 textDocument/completion。
// 候选来自 Resolver 作用域与 TypeChecker 类型；无法判断上下文时只返回合法关键字。
func (s *server) requestCompletion(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var value CompletionParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	if value.TextDocument.URI == "" {
		return nil, &responseError{Code: codeInvalidParams, Message: "missing textDocument.uri"}
	}
	result := s.analyzeDocument(ctx, value.TextDocument.URI)
	if result == nil {
		return emptyCompletionList(), nil
	}
	list := result.completionAt(value.Position)
	if !s.snippetCompletionSupported() {
		downgradeSnippetCompletions(list)
	}
	return list, nil
}

// downgradeSnippetCompletions 在不支持 snippet 的客户端上退回普通插入文本。
func downgradeSnippetCompletions(list *CompletionList) {
	if list == nil {
		return
	}
	for index := range list.Items {
		item := &list.Items[index]
		if item.InsertTextFormat != insertTextFormatSnippet {
			continue
		}
		item.InsertText = item.Label
		item.InsertTextFormat = 0
	}
}

func emptyCompletionList() *CompletionList {
	return &CompletionList{Items: []CompletionItem{}}
}

// completionAt 返回光标位置的补全候选。
func (a *analysis) completionAt(position Position) *CompletionList {
	offset := a.Buffer.offsetAt(position)
	if offset < 0 || offset > len(a.Text) {
		return emptyCompletionList()
	}
	if positionInCommentOrString(a.Text, offset) {
		return emptyCompletionList()
	}
	receiver, prefix, valueEnd, member := memberCompletionContext(a.Text, offset)
	if member {
		items := a.memberCompletions(receiver, valueEnd)
		return &CompletionList{Items: filterCompletionItems(items, prefix)}
	}
	if literal := a.Index.literalAt(a.DocKey, offset); literal != nil {
		return &CompletionList{Items: filterCompletionItems(literalCompletionItems(literal), prefix)}
	}
	items := make([]CompletionItem, 0, 32)
	items = append(items, a.scopeCompletions(offset)...)
	items = append(items, keywordCompletions()...)
	items = append(items, builtinCompletions()...)
	return &CompletionList{Items: filterCompletionItems(items, prefix)}
}

// literalAt 返回包含偏移的最内层结构体字面量。
// 光标位于字段值表达式内部时返回 nil，避免用字段名覆盖普通表达式补全。
func (i *semanticIndex) literalAt(fileKey string, offset int) *literalContext {
	var best *literalContext
	for _, literal := range i.literals[fileKey] {
		if offset < literal.Span.Start.Offset || offset > literal.Span.End.Offset {
			continue
		}
		if best == nil || i.spanWidth(literal.Span) < i.spanWidth(best.Span) {
			best = literal
		}
	}
	if best == nil {
		return nil
	}
	for _, value := range best.Values {
		if offset > value.Start.Offset && offset <= value.End.Offset {
			return nil
		}
	}
	return best
}

// literalCompletionItems 返回结构体字面量中尚未初始化的字段。
func literalCompletionItems(literal *literalContext) []CompletionItem {
	if literal == nil || literal.Symbol == nil {
		return nil
	}
	items := make([]CompletionItem, 0, len(literal.Symbol.Fields))
	for _, field := range literal.Symbol.Fields {
		if _, provided := literal.Provided[field.displayName()]; provided {
			continue
		}
		items = append(items, completionItem{
			label:  field.displayName(),
			kind:   completionKindField,
			detail: field.Type,
			insert: field.displayName(),
			doc:    field.Doc,
			rank:   "0",
		}.build())
	}
	return items
}

// memberCompletionContext 识别 `表达式.` 上下文。
// 返回值依次为接收者文本、已输入前缀、接收者结束偏移与是否命中。
func memberCompletionContext(text string, offset int) (string, string, int, bool) {
	if offset < 0 || offset > len(text) {
		return "", "", offset, false
	}
	prefixEnd := offset
	prefixStart := prefixEnd
	for prefixStart > 0 && isCompletionIdentifierByte(text[prefixStart-1]) {
		prefixStart--
	}
	prefix := text[prefixStart:prefixEnd]
	if prefixStart == 0 || text[prefixStart-1] != '.' {
		return "", prefix, offset, false
	}
	dot := prefixStart - 1
	receiverEnd := dot
	for receiverEnd > 0 && (text[receiverEnd-1] == ' ' || text[receiverEnd-1] == '\t') {
		receiverEnd--
	}
	start := receiverEnd
	for start > 0 {
		current := text[start-1]
		if isCompletionIdentifierByte(current) || current == '.' {
			start--
			continue
		}
		break
	}
	receiver := strings.Trim(text[start:receiverEnd], ".")
	if receiver == "" {
		return "", prefix, receiverEnd, false
	}
	return receiver, prefix, receiverEnd, true
}

func isCompletionIdentifierByte(value byte) bool {
	return value == '_' ||
		(value >= 'a' && value <= 'z') ||
		(value >= 'A' && value <= 'Z') ||
		(value >= '0' && value <= '9')
}

// memberCompletions 返回成员补全：模块导出、结构体字段或枚举变体。
func (a *analysis) memberCompletions(receiver string, valueEnd int) []CompletionItem {
	segments := strings.Split(receiver, ".")
	// 同名局部变量会遮蔽包别名，此时必须按值处理而不是模块导出。
	shadowed := false
	for _, visible := range a.Index.visibleSymbols(a.DocKey, valueEnd) {
		if visible.displayName() == segments[0] && visible.Kind != symbolKindModule {
			shadowed = true
			break
		}
	}
	if !shadowed && len(segments) > 0 {
		if importPath, ok := a.Index.importPathForAlias(segments[0]); ok {
			switch len(segments) {
			case 1:
				return exportCompletionItems(a.Index.exportedSymbols(importPath))
			case 2:
				member := a.Index.symbolForQualifiedType(importPath + "::" + segments[1])
				if member != nil {
					return memberItems(member)
				}
			}
		}
	}
	// 通过表达式类型解析接收者（例如结构体变量或字段链）。
	if symbol := a.Index.receiverSymbol(a.DocKey, valueEnd); symbol != nil {
		// 枚举变体只能通过类型名构造（Color.Red()），值上的 `.` 不提供变体。
		if symbol.Kind == symbolKindEnum && !a.Index.receiverIsType(valueEnd) {
			return nil
		}
		return memberItems(symbol)
	}
	// 退化路径：同名可见声明的已解析类型（仅在类型已知时使用）。
	for _, visible := range a.Index.visibleSymbols(a.DocKey, valueEnd) {
		if visible.displayName() != segments[len(segments)-1] {
			continue
		}
		if container := a.Index.symbolForTypeName(visible.Type); container != nil {
			return memberItems(container)
		}
	}
	return nil
}

// receiverSymbol 通过表达式类型返回接收者的具名类型符号。
func (i *semanticIndex) receiverSymbol(fileKey string, valueEnd int) *semanticSymbol {
	expressionType := ""
	bestWidth := -1
	for _, expression := range i.exprs[fileKey] {
		if expression.Span.End.Offset != valueEnd {
			continue
		}
		width := i.spanWidth(expression.Span)
		if bestWidth >= 0 && width >= bestWidth {
			continue
		}
		bestWidth = width
		expressionType = expression.Type
		if expression.Named != "" {
			return i.byQualName[expression.Named]
		}
	}
	if expressionType == "" {
		return nil
	}
	return i.symbolForTypeName(expressionType)
}

// symbolForTypeName 把展示用类型文本（可能可空/泛型）映射到具名类型声明。
func (i *semanticIndex) symbolForTypeName(typeText string) *semanticSymbol {
	name := strings.TrimSpace(typeText)
	for strings.HasPrefix(name, "[]") {
		name = strings.TrimPrefix(name, "[]")
	}
	name = strings.TrimSuffix(name, "?")
	if index := strings.IndexByte(name, '<'); index > 0 {
		name = name[:index]
	}
	if name == "" {
		return nil
	}
	return i.byQualName[name]
}

// receiverIsType 判断接收者是否为类型名引用（而非值）。
func (i *semanticIndex) receiverIsType(valueEnd int) bool {
	position := valueEnd - 1
	if position < 0 {
		return false
	}
	if symbol := i.declarationAt(i.docKey, position); symbol != nil {
		return symbol.Kind == symbolKindEnum || symbol.Kind == symbolKindStruct
	}
	if occurrence := i.occurrenceAt(i.docKey, position); occurrence != nil && occurrence.Symbol != nil {
		return occurrence.Symbol.Kind == symbolKindEnum || occurrence.Symbol.Kind == symbolKindStruct
	}
	return false
}

// memberItems 生成结构体字段或枚举变体补全。
func memberItems(symbol *semanticSymbol) []CompletionItem {
	if symbol == nil {
		return nil
	}
	items := make([]CompletionItem, 0, len(symbol.Fields)+len(symbol.Variants))
	switch symbol.Kind {
	case symbolKindStruct:
		for _, field := range symbol.Fields {
			items = append(items, completionItem{
				label:  field.displayName(),
				kind:   completionKindField,
				detail: field.Type,
				insert: field.displayName(),
				doc:    field.Doc,
				rank:   "0",
			}.build())
		}
	case symbolKindEnum:
		for _, variant := range symbol.Variants {
			items = append(items, completionItem{
				label:  variant.displayName(),
				kind:   completionKindEnumMember,
				detail: variant.Detail,
				insert: enumVariantSnippet(variant),
				doc:    variant.Doc,
				rank:   "0",
			}.build())
		}
	}
	return items
}

// exportCompletionItems 生成模块导出符号补全（插入成员名）。
func exportCompletionItems(symbols []*semanticSymbol) []CompletionItem {
	items := make([]CompletionItem, 0, len(symbols))
	for _, symbol := range symbols {
		item := symbolCompletionItem(symbol, "1")
		item.InsertText = symbol.displayName()
		item.InsertTextFormat = 0
		items = append(items, item)
	}
	return items
}

// scopeCompletions 返回当前作用域可见的声明。
func (a *analysis) scopeCompletions(offset int) []CompletionItem {
	visible := a.Index.visibleSymbols(a.DocKey, offset)
	items := make([]CompletionItem, 0, len(visible))
	for _, symbol := range visible {
		if symbol.FileKey != a.DocKey || symbol.Kind == symbolKindModule {
			continue
		}
		rank := "2"
		switch symbol.Kind {
		case symbolKindVariable:
			rank = "0"
		case symbolKindFunction:
			rank = "1"
		case symbolKindConstant:
			rank = "2"
		}
		items = append(items, symbolCompletionItem(symbol, rank))
	}
	return items
}

// symbolCompletionItem 把语义符号转换为补全候选。
func symbolCompletionItem(symbol *semanticSymbol, rank string) CompletionItem {
	if symbol == nil {
		return CompletionItem{}
	}
	kind := completionKindForSymbol(symbol.Kind)
	insert := symbol.displayName()
	format := 0
	if symbol.Kind == symbolKindFunction {
		insert = functionSnippet(symbol)
		format = insertTextFormatSnippet
	}
	item := completionItem{
		label:  symbol.displayName(),
		kind:   kind,
		detail: completionDetail(symbol),
		insert: insert,
		format: format,
		doc:    symbol.Doc,
		rank:   rank,
	}
	return item.build()
}

// completionKindForSymbol 把 LSP SymbolKind 映射为 CompletionItemKind。
func completionKindForSymbol(kind int) int {
	switch kind {
	case symbolKindFunction:
		return completionKindFunction
	case symbolKindVariable:
		return completionKindVariable
	case symbolKindConstant:
		return completionKindConstant
	case symbolKindStruct:
		return completionKindStruct
	case symbolKindEnum:
		return completionKindEnum
	case symbolKindField:
		return completionKindField
	case symbolKindEnumMember:
		return completionKindEnumMember
	case symbolKindModule:
		return completionKindModule
	default:
		return completionKindVariable
	}
}

func completionDetail(symbol *semanticSymbol) string {
	switch symbol.Kind {
	case symbolKindFunction:
		if symbol.Signature != nil {
			return symbol.Signature.label("")
		}
	case symbolKindStruct:
		return "kata " + symbol.displayName()
	case symbolKindEnum:
		return "sentaku " + symbol.displayName()
	}
	return symbol.Type
}

// functionSnippet 生成符合 Mio 调用语法的片段。
func functionSnippet(symbol *semanticSymbol) string {
	name := symbol.displayName()
	if symbol.Signature == nil || len(symbol.Signature.Parameters) == 0 {
		return name + "()"
	}
	parameters := make([]string, len(symbol.Signature.Parameters))
	for index, parameter := range symbol.Signature.Parameters {
		placeholder := parameter.Name
		if placeholder == "" {
			placeholder = fmt.Sprintf("value%d", index+1)
		}
		parameters[index] = fmt.Sprintf("${%d:%s}", index+1, placeholder)
	}
	return name + "(" + strings.Join(parameters, ", ") + ")"
}

// enumVariantSnippet 生成枚举变体构造片段。
func enumVariantSnippet(variant *semanticSymbol) string {
	name := variant.displayName()
	fields := enumVariantFieldTypes(variant)
	if len(fields) == 0 {
		return name + "()"
	}
	parts := make([]string, len(fields))
	for index := range fields {
		parts[index] = fmt.Sprintf("${%d:value%d}", index+1, index+1)
	}
	return name + "(" + strings.Join(parts, ", ") + ")"
}

func enumVariantFieldTypes(variant *semanticSymbol) []string {
	if variant == nil || variant.Type == "" || !strings.HasPrefix(variant.Type, "(") {
		return nil
	}
	inner := strings.TrimSuffix(strings.TrimPrefix(variant.Type, "("), ")")
	if strings.TrimSpace(inner) == "" {
		return nil
	}
	return splitTypeList(inner)
}

// splitTypeList 按顶层逗号切分类型列表，保留泛型参数内的逗号。
func splitTypeList(input string) []string {
	parts := make([]string, 0, 2)
	depth := 0
	start := 0
	for index := 0; index < len(input); index++ {
		switch input[index] {
		case '<', '(', '[':
			depth++
		case '>', ')', ']':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(input[start:index]))
				start = index + 1
			}
		}
	}
	parts = append(parts, strings.TrimSpace(input[start:]))
	return parts
}

// keywordCompletions 返回当前语言版本的全部关键字。
func keywordCompletions() []CompletionItem {
	keywords := token.Keywords(token.V05)
	items := make([]CompletionItem, 0, len(keywords))
	for _, keyword := range keywords {
		items = append(items, completionItem{
			label:  keyword,
			kind:   completionKindKeyword,
			insert: keyword,
			rank:   "3",
		}.build())
	}
	return items
}

// builtinCompletions 返回内置类型与内置函数。
func builtinCompletions() []CompletionItem {
	items := make([]CompletionItem, 0, 16)
	for _, name := range []string{
		"int8", "int16", "int32", "int64", "uint8", "uint16", "uint32",
		"float32", "float64", "bool", "string", "unit", "rune", "Map",
	} {
		items = append(items, completionItem{
			label:  name,
			kind:   completionKindClass,
			detail: "内置类型",
			insert: name,
			rank:   "4",
		}.build())
	}
	items = append(items, completionItem{
		label: "chant", kind: completionKindFunction, detail: "内置函数 (value) -> unit",
		insert: "chant(${1:value})", format: insertTextFormatSnippet, rank: "4",
	}.build())
	items = append(items, completionItem{
		label: "len", kind: completionKindFunction, detail: "内置函数 ([]T) -> int64",
		insert: "len(${1:values})", format: insertTextFormatSnippet, rank: "4",
	}.build())
	return items
}

// completionItem 是构造补全候选项的内部结构。
type completionItem struct {
	label  string
	kind   int
	detail string
	insert string
	format int
	doc    string
	rank   string
}

func (c completionItem) build() CompletionItem {
	item := CompletionItem{
		Label:            c.label,
		Kind:             c.kind,
		Detail:           c.detail,
		InsertText:       c.insert,
		InsertTextFormat: c.format,
		FilterText:       c.label,
		SortText:         c.rank + c.label,
	}
	if documentation := markdownDocumentation(c.doc); documentation != "" {
		item.Documentation = &MarkupContent{Kind: "markdown", Value: documentation}
	}
	return item
}

// filterCompletionItems 按前缀过滤、去重并排序。
func filterCompletionItems(items []CompletionItem, prefix string) []CompletionItem {
	result := make([]CompletionItem, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, item := range items {
		if item.Label == "" {
			continue
		}
		if prefix != "" && !strings.HasPrefix(item.Label, prefix) {
			continue
		}
		// 同名候选只保留最内层的语义符号：作用域符号先于关键字和内置项追加。
		if _, exists := seen[item.Label]; exists {
			continue
		}
		seen[item.Label] = struct{}{}
		result = append(result, item)
	}
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].SortText == result[right].SortText {
			return result[left].Label < result[right].Label
		}
		return result[left].SortText < result[right].SortText
	})
	return result
}

// positionInCommentOrString 判断偏移是否位于注释、字符串或字符字面量内部。
// 只用于避免在不可插入位置提供语义补全。
func positionInCommentOrString(text string, offset int) bool {
	inLine := false
	inBlock := false
	inString := false
	inRune := false
	escaped := false
	for index := 0; index < offset && index < len(text); index++ {
		current := text[index]
		switch {
		case inLine:
			if current == '\n' {
				inLine = false
			}
		case inBlock:
			if current == '*' && index+1 < len(text) && text[index+1] == '/' {
				inBlock = false
				index++
			}
		case escaped:
			escaped = false
		case inString:
			if current == '\\' {
				escaped = true
			} else if current == '"' {
				inString = false
			}
		case inRune:
			if current == '\\' {
				escaped = true
			} else if current == '\'' {
				inRune = false
			}
		default:
			switch {
			case current == '/' && index+1 < len(text) && text[index+1] == '/':
				inLine = true
				index++
			case current == '/' && index+1 < len(text) && text[index+1] == '*':
				inBlock = true
				index++
			case current == '"':
				inString = true
			case current == '\'':
				inRune = true
			}
		}
	}
	return inLine || inBlock || inString || inRune
}
