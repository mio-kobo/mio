package lsp

import (
	"context"
	"encoding/json"
	"strconv"
)

// requestReferences 实现 textDocument/references。
// 引用来自 Resolver 绑定（变量/函数/类型）或类型限定的成员身份（字段/枚举变体），
// 因此不会把字符串或注释中的同名文本当成引用，也不会混淆变量遮蔽。
func (s *server) requestReferences(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var value ReferenceParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	if value.TextDocument.URI == "" {
		return nil, &responseError{Code: codeInvalidParams, Message: "missing textDocument.uri"}
	}
	result := s.analyzeDocument(ctx, value.TextDocument.URI)
	if result == nil || result.Index == nil {
		return []Location{}, nil
	}
	offset := result.positionOffset(value.Position)
	symbol, _ := result.Index.symbolAt(result.DocKey, offset)
	if symbol == nil {
		return []Location{}, nil
	}
	locations := make([]Location, 0)
	seen := make(map[string]struct{})
	appendLocation := func(owner *analysis, location site) {
		converted, ok := owner.locationFor(location)
		if !ok {
			return
		}
		key := converted.URI + ":" + strconv.Itoa(location.Span.Start.Offset) + ":" + strconv.Itoa(location.Span.End.Offset)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		locations = append(locations, converted)
	}
	if value.Context.IncludeDeclaration {
		appendLocation(result, symbol.nameSite())
	}
	for _, location := range result.Index.references(symbol) {
		appendLocation(result, location)
	}
	// 顶层符号可以跨打开的文档合并引用：另一个文档把同名声明解析到
	// 同一个物理源码位置时，它的引用同样属于这个符号。
	if symbol.Container == "" || symbol.Kind == symbolKindField || symbol.Kind == symbolKindEnumMember {
		declaration := symbol.nameSite()
		for _, other := range s.store.all() {
			if other.URI == value.TextDocument.URI {
				continue
			}
			otherAnalysis := s.analyzeDocument(ctx, other.URI)
			if otherAnalysis == nil || otherAnalysis.Index == nil {
				continue
			}
			equivalent := otherAnalysis.Index.byDeclarationSite(declaration)
			if equivalent == nil || equivalent.Kind != symbol.Kind {
				continue
			}
			for _, location := range otherAnalysis.Index.references(equivalent) {
				appendLocation(otherAnalysis, location)
			}
		}
	}
	return locations, nil
}
