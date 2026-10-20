package lsp

import (
	"context"
	"encoding/json"

	"Mio/internal/ast"
	"Mio/internal/lexer"
	"Mio/internal/token"
)

// requestSignatureHelp 实现 textDocument/signatureHelp。
// 签名来自已解析的 Mio 函数定义，参数索引用真实 Token 与实参范围计算。
func (s *server) requestSignatureHelp(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var value TextDocumentPositionParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	if value.TextDocument.URI == "" {
		return nil, &responseError{Code: codeInvalidParams, Message: "missing textDocument.uri"}
	}
	result := s.analyzeDocument(ctx, value.TextDocument.URI)
	if result == nil || result.Index == nil {
		return nil, nil
	}
	offset := result.positionOffset(value.Position)
	return result.signatureHelpAt(offset), nil
}

// signatureHelpAt 返回光标处的签名提示；无法可靠解析时返回 nil。
func (a *analysis) signatureHelpAt(offset int) *SignatureHelp {
	if call := a.Index.callAt(a.DocKey, offset); call != nil && call.Signature != nil {
		return signatureHelpFor(call.Symbol, call.Signature, activeParameter(call.ArgumentSpans, offset))
	}
	// 容错路径：括号尚未闭合时 AST 中不存在调用节点，
	// 用真实 Token 定位最内层未闭合的调用括号。
	name, calleeOffset, active, ok := incompleteCallContext(a.Text, offset)
	if !ok {
		return nil
	}
	symbol, signature := a.Index.calleeAt(a.DocKey, calleeOffset, name)
	if symbol == nil || signature == nil {
		return nil
	}
	return signatureHelpFor(symbol, signature, active)
}

// calleeAt 定位调用目标及其签名。
// 未完成调用先从作用域可见符号（含函数值变量）解析，再退化为唯一顶层同名函数。
func (i *semanticIndex) calleeAt(fileKey string, offset int, name string) (*semanticSymbol, *semanticSignature) {
	if occurrence := i.occurrenceAt(fileKey, offset); occurrence != nil && occurrence.Symbol != nil {
		if occurrence.Symbol.Kind == symbolKindFunction {
			return occurrence.Symbol, occurrence.Symbol.Signature
		}
		if signature := functionValueSignature(occurrence.Symbol); signature != nil {
			return occurrence.Symbol, signature
		}
	}
	for _, visible := range i.visibleSymbols(fileKey, offset) {
		if visible.displayName() != name {
			continue
		}
		if visible.Kind == symbolKindFunction {
			return visible, visible.Signature
		}
		if signature := functionValueSignature(visible); signature != nil {
			return visible, signature
		}
	}
	var candidate *semanticSymbol
	for _, symbol := range i.byName[name] {
		if symbol.Kind != symbolKindFunction || symbol.FileKey != fileKey || symbol.Container != "" {
			continue
		}
		if candidate != nil && candidate != symbol {
			// 同名声明不唯一，不做名称猜测。
			return nil, nil
		}
		candidate = symbol
	}
	if candidate == nil {
		return nil, nil
	}
	return candidate, candidate.Signature
}

// signatureHelpFor 构造 LSP 签名提示。
func signatureHelpFor(symbol *semanticSymbol, signature *semanticSignature, active int) *SignatureHelp {
	if symbol == nil || signature == nil {
		return nil
	}
	parameters := make([]ParameterInformation, len(signature.Parameters))
	for index, parameter := range signature.Parameters {
		parameters[index] = ParameterInformation{Label: parameterLabel(parameter)}
	}
	if len(parameters) > 0 {
		if active < 0 {
			active = 0
		}
		if active >= len(parameters) {
			active = len(parameters) - 1
		}
	} else {
		active = 0
	}
	signatureInformation := SignatureInformation{
		Label:      signature.label(symbol.displayName()),
		Parameters: parameters,
	}
	if documentation := markdownDocumentation(symbol.Doc); documentation != "" {
		signatureInformation.Documentation = &MarkupContent{Kind: "markdown", Value: documentation}
	}
	return &SignatureHelp{
		Signatures:      []SignatureInformation{signatureInformation},
		ActiveSignature: 0,
		ActiveParameter: active,
	}
}

func parameterLabel(parameter semanticParameter) string {
	switch {
	case parameter.Name == "":
		return parameter.Type
	case parameter.Type == "":
		return parameter.Name
	default:
		return parameter.Name + ": " + parameter.Type
	}
}

// activeParameter 根据实参范围计算当前参数下标。
// 光标位于某个实参内部时取该下标；位于两个实参之间时取下一个下标。
func activeParameter(spans []ast.Span, offset int) int {
	active := 0
	for index, span := range spans {
		switch {
		case offset <= span.Start.Offset:
			return index
		case offset <= span.End.Offset:
			return index
		default:
			active = index + 1
		}
	}
	return active
}

// incompleteCallContext 用真实 Token 解析未完成的调用表达式。
// 返回被调用标识符名称、其起始偏移、当前参数下标与是否成功。
func incompleteCallContext(text string, offset int) (string, int, int, bool) {
	if offset < 0 || offset > len(text) {
		return "", 0, 0, false
	}
	lex := lexer.New(text, token.V05)
	tokens := make([]token.Token, 0, 64)
	for {
		current := lex.NextToken()
		if current.Type == token.EOF {
			break
		}
		if current.Start.Offset >= offset {
			break
		}
		tokens = append(tokens, current)
	}
	openIndex := -1
	depth := 0
	for index := len(tokens) - 1; index >= 0; index-- {
		switch tokens[index].Type {
		case token.RParen:
			depth++
		case token.LParen:
			if depth == 0 {
				openIndex = index
			} else {
				depth--
			}
		}
		if openIndex >= 0 {
			break
		}
	}
	if openIndex <= 0 {
		return "", 0, 0, false
	}
	calleeIndex := openIndex - 1
	// 跳过显式类型实参 `ident[T](...)`。
	for calleeIndex >= 0 && tokens[calleeIndex].Type == token.RBracket {
		opened := -1
		bracketDepth := 0
		for index := calleeIndex; index >= 0; index-- {
			switch tokens[index].Type {
			case token.RBracket:
				bracketDepth++
			case token.LBracket:
				bracketDepth--
				if bracketDepth == 0 {
					opened = index
				}
			}
			if opened >= 0 {
				break
			}
		}
		if opened <= 0 {
			return "", 0, 0, false
		}
		calleeIndex = opened - 1
	}
	if calleeIndex < 0 || tokens[calleeIndex].Type != token.Ident {
		return "", 0, 0, false
	}
	active := 0
	nesting := 0
	for index := openIndex + 1; index < len(tokens); index++ {
		current := tokens[index]
		if current.Start.Offset >= offset {
			break
		}
		switch current.Type {
		case token.LParen, token.LBracket, token.LBrace:
			nesting++
		case token.RParen, token.RBracket, token.RBrace:
			if nesting > 0 {
				nesting--
			}
		case token.Comma:
			if nesting == 0 {
				active++
			}
		}
	}
	return tokens[calleeIndex].Literal, tokens[calleeIndex].Start.Offset, active, true
}
