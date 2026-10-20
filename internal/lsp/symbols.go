package lsp

import (
	"context"
	"encoding/json"
	"strings"
)

// documentSymbolParams 是 textDocument/documentSymbol 的参数。
type documentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// requestDocumentSymbol 实现 textDocument/documentSymbol。
// 只根据真实 AST 声明生成层级结构，selectionRange 指向符号名称。
func (s *server) requestDocumentSymbol(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var value documentSymbolParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	if value.TextDocument.URI == "" {
		return nil, &responseError{Code: codeInvalidParams, Message: "missing textDocument.uri"}
	}
	result := s.analyzeDocument(ctx, value.TextDocument.URI)
	if result == nil || result.Index == nil {
		return []*DocumentSymbol{}, nil
	}
	return result.documentSymbols(), nil
}

// documentSymbols 返回当前文档的层级符号列表。
func (a *analysis) documentSymbols() []*DocumentSymbol {
	symbols := make([]*DocumentSymbol, 0)
	for _, symbol := range a.Index.topLevel[a.DocKey] {
		if value := a.documentSymbol(symbol); value != nil {
			symbols = append(symbols, value)
		}
	}
	return symbols
}

func (a *analysis) documentSymbol(symbol *semanticSymbol) *DocumentSymbol {
	if symbol == nil || symbol.Decl == nil {
		return nil
	}
	fullRange, ok := a.rangeFor(symbol.Decl.FileKey, symbol.Decl.Span)
	if !ok {
		return nil
	}
	selectionRange, ok := a.rangeFor(symbol.FileKey, symbol.NameSpan)
	if !ok {
		selectionRange = fullRange
	}
	name := symbol.displayName()
	if name == "" {
		return nil
	}
	value := &DocumentSymbol{
		Name:           name,
		Detail:         symbolDetail(symbol),
		Kind:           symbol.Kind,
		Range:          fullRange,
		SelectionRange: selectionRange,
	}
	for _, child := range symbol.Params {
		if converted := a.documentSymbol(child); converted != nil {
			value.Children = append(value.Children, converted)
		}
	}
	for _, child := range symbol.Fields {
		if converted := a.documentSymbol(child); converted != nil {
			value.Children = append(value.Children, converted)
		}
	}
	for _, child := range symbol.Variants {
		if converted := a.documentSymbol(child); converted != nil {
			value.Children = append(value.Children, converted)
		}
	}
	return value
}

// symbolDetail 返回 DocumentSymbol.detail 的简短说明。
func symbolDetail(symbol *semanticSymbol) string {
	switch symbol.Kind {
	case symbolKindFunction:
		if symbol.Signature != nil {
			return symbol.Signature.label("")
		}
		return ""
	case symbolKindStruct, symbolKindEnum:
		if index := strings.Index(symbol.Detail, "<"); index > 0 {
			return symbol.Detail[index:]
		}
		return ""
	case symbolKindModule:
		return symbol.QualName
	default:
		return symbol.Type
	}
}
