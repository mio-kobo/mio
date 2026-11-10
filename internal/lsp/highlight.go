package lsp

import (
	"context"
	"encoding/json"
	"strconv"
)

// documentHighlightKindText 是 LSP DocumentHighlightKind.Text。
// 语义索引不区分读取与写入，因此不猜测 Read/Write。
const documentHighlightKindText = 1

// DocumentHighlight 表示 textDocument/documentHighlight 结果项。
type DocumentHighlight struct {
	Range Range `json:"range"`
	Kind  int   `json:"kind,omitempty"`
}

// requestDocumentHighlight 实现 textDocument/documentHighlight。
// 结果与 textDocument/references 使用同一符号身份，只保留当前文档内的位置，
// 因此字符串、注释和被遮蔽的同名符号不会被高亮。
func (s *server) requestDocumentHighlight(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var value TextDocumentPositionParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	if value.TextDocument.URI == "" {
		return nil, &responseError{Code: codeInvalidParams, Message: "missing textDocument.uri"}
	}
	result := s.analyzeDocument(ctx, value.TextDocument.URI)
	if result == nil || result.Index == nil {
		return []DocumentHighlight{}, nil
	}
	symbol, _ := result.Index.symbolAt(result.DocKey, result.positionOffset(value.Position))
	if symbol == nil {
		return []DocumentHighlight{}, nil
	}
	highlights := make([]DocumentHighlight, 0)
	seen := make(map[string]struct{})
	add := func(location site) {
		if location.FileKey != result.DocKey {
			return
		}
		converted, ok := result.rangeFor(location.FileKey, location.Span)
		if !ok {
			return
		}
		key := strconv.Itoa(location.Span.Start.Offset) + ":" + strconv.Itoa(location.Span.End.Offset)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		highlights = append(highlights, DocumentHighlight{Range: converted, Kind: documentHighlightKindText})
	}
	add(symbol.nameSite())
	for _, location := range result.Index.references(symbol) {
		add(location)
	}
	return highlights, nil
}
