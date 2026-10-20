package lsp

import (
	"context"
	"encoding/json"
)

// requestHover 实现 textDocument/hover。
// 无法可靠解析时返回 null，绝不构造伪造类型。
func (s *server) requestHover(ctx context.Context, params json.RawMessage) (any, *responseError) {
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
	markdown, plain, span, hasSpan, ok := hoverAtOffset(result.Index, result.DocKey, offset)
	if !ok {
		return nil, nil
	}
	hover := &Hover{Contents: MarkupContent{Kind: "markdown", Value: markdown}}
	if !s.markdownHoverSupported() {
		hover.Contents = MarkupContent{Kind: "plaintext", Value: plain}
	}
	if hasSpan {
		if converted, valid := result.rangeFor(result.DocKey, span); valid {
			hover.Range = &converted
		}
	}
	return hover, nil
}
