package lsp

import (
	"context"
	"encoding/json"
)

// requestDefinition 实现 textDocument/definition。
// 只返回 Resolver 确认的声明位置；未知或无法解析的符号返回 null。
func (s *server) requestDefinition(ctx context.Context, params json.RawMessage) (any, *responseError) {
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
	symbol, _ := result.Index.symbolAt(result.DocKey, offset)
	if symbol == nil {
		return nil, nil
	}
	location, ok := result.locationFor(symbol.nameSite())
	if !ok {
		return nil, nil
	}
	return []Location{location}, nil
}
