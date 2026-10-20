package lsp

import (
	"context"
	"encoding/json"
	"strings"

	"Mio/internal/ast"
	"Mio/internal/bunko"
)

// workspaceOverlay 汇总所有已打开文档的未保存内容，供跨文件分析使用。
func (s *server) workspaceOverlay() bunko.Overlay {
	overlay := bunko.NewOverlay()
	for _, doc := range s.store.all() {
		if path, ok := pathFromURI(doc.URI); ok {
			overlay.Set(path, []byte(doc.Text))
		}
	}
	return overlay
}

// preferredURIs 返回规范化路径 → 客户端 URI 的映射。
// 打开文档保留客户端给出的 URI，避免符号链接导致同一文件出现两个身份。
func (s *server) preferredURIs() map[string]string {
	preferred := make(map[string]string)
	for _, doc := range s.store.all() {
		if path, ok := pathFromURI(doc.URI); ok {
			preferred[canonicalPath(path)] = doc.URI
		}
	}
	return preferred
}

// analyzeDocument 取得文档当前版本的语义快照。
// 返回 nil 表示文档未打开、请求已取消或版本在校验期间发生变化；
// 调用方必须返回空结果，绝不能返回过期位置。
func (s *server) analyzeDocument(ctx context.Context, uri string) *analysis {
	for attempt := 0; attempt < 3; attempt++ {
		doc, ok := s.store.snapshot(uri)
		if !ok {
			return nil
		}
		overlay := s.workspaceOverlay()
		preferred := s.preferredURIs()
		result := s.analyses.get(ctx, doc, func(buildContext context.Context) *analysis {
			return s.analyzer.analyze(buildContext, doc, overlay, preferred)
		})
		if result == nil {
			if ctx.Err() != nil {
				return nil
			}
			// 构建可能被另一个请求取消；用当前请求的上下文重试。
			continue
		}
		latest, ok := s.store.snapshot(uri)
		if !ok {
			return nil
		}
		if latest.Version == doc.Version && latest.Text == doc.Text {
			return result
		}
		if ctx.Err() != nil {
			return nil
		}
		s.analyses.invalidate(uri)
	}
	return nil
}

// decodeRequestParams 解析请求参数，失败时返回 InvalidParams 错误。
func decodeRequestParams(params json.RawMessage, target any) *responseError {
	if len(params) == 0 {
		return &responseError{Code: codeInvalidParams, Message: "missing request parameters"}
	}
	if err := json.Unmarshal(params, target); err != nil {
		return &responseError{Code: codeInvalidParams, Message: "invalid request parameters: " + err.Error()}
	}
	return nil
}

// positionOffset 把 LSP 位置换算为 UTF-8 字节偏移。
func (a *analysis) positionOffset(position Position) int {
	return a.Buffer.offsetAt(position)
}

// rangeFor 把文件内的字节范围换算为 LSP Range。
func (a *analysis) rangeFor(fileKey string, span ast.Span) (Range, bool) {
	buffer := a.bufferFor(fileKey)
	if buffer == nil {
		return Range{}, false
	}
	if span.Start.Offset < 0 || span.End.Offset < span.Start.Offset {
		return Range{}, false
	}
	return Range{
		Start: buffer.positionAt(span.Start.Offset),
		End:   buffer.positionAt(span.End.Offset),
	}, true
}

// locationFor 把语义站点换算为 LSP Location。
func (a *analysis) locationFor(location site) (Location, bool) {
	value, ok := a.rangeFor(location.FileKey, location.Span)
	if !ok {
		return Location{}, false
	}
	return Location{URI: location.FileKey, Range: value}, true
}

// ---- Markdown 渲染 ----

// inlineCode 渲染行内代码；内容中的反引号会改用双反引号包裹。
func inlineCode(text string) string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", " "), "\n", " ")
	if text == "" {
		return "``"
	}
	if strings.Contains(text, "`") {
		return "`` " + text + " ``"
	}
	return "`" + text + "`"
}

// markdownDocumentation 把注释内容渲染为不会解析 HTML 的代码块。
func markdownDocumentation(documentation string) string {
	documentation = strings.TrimSpace(documentation)
	if documentation == "" {
		return ""
	}
	// 代码围栏内不会解析 Markdown/HTML；破坏围栏的序列做等价替换。
	documentation = strings.ReplaceAll(documentation, "```", "` ` `")
	return "```text\n" + documentation + "\n```"
}

// symbolDeclarationHeader 返回符号声明头的 Mio 代码。
func symbolDeclarationHeader(symbol *semanticSymbol) string {
	if symbol == nil {
		return ""
	}
	switch symbol.Kind {
	case symbolKindFunction:
		if symbol.Signature != nil {
			return "waza " + symbol.Signature.label(symbol.displayName())
		}
		return "waza " + symbol.displayName()
	case symbolKindStruct:
		return "kata " + symbol.Detail
	case symbolKindEnum:
		return "sentaku " + symbol.Detail
	case symbolKindConstant:
		header := "teisu " + symbol.displayName()
		if symbol.Type != "" {
			header += ": " + symbol.Type
		}
		return header
	case symbolKindField:
		header := symbol.displayName()
		if symbol.Type != "" {
			header += ": " + symbol.Type
		}
		return header
	case symbolKindEnumMember:
		return symbol.Detail
	case symbolKindModule:
		return "yobu " + symbol.Detail
	default:
		header := symbol.displayName()
		if symbol.Keyword != "" {
			header = symbol.Keyword + " " + header
		}
		if symbol.Type != "" {
			header += ": " + symbol.Type
		}
		return header
	}
}

// symbolHoverContent 同时生成 Markdown 与纯文本悬停内容。
// 纯文本用于不支持 Markdown 的客户端，避免发送无法渲染的语法。
func symbolHoverContent(symbol *semanticSymbol, resolvedType string) (string, string) {
	if symbol == nil {
		return "", ""
	}
	header := strings.TrimSpace(symbolDeclarationHeader(symbol))
	markdownParts := make([]string, 0, 3)
	plainParts := make([]string, 0, 3)
	if header != "" {
		markdownParts = append(markdownParts, "```mio\n"+header+"\n```")
		plainParts = append(plainParts, header)
	}
	if resolvedType != "" && resolvedType != symbol.Type {
		markdownParts = append(markdownParts, "类型: "+inlineCode(resolvedType))
		plainParts = append(plainParts, "类型: "+resolvedType)
	}
	if documentation := strings.TrimSpace(symbol.Doc); documentation != "" {
		markdownParts = append(markdownParts, markdownDocumentation(documentation))
		plainParts = append(plainParts, documentation)
	}
	return strings.Join(markdownParts, "\n\n"), strings.Join(plainParts, "\n\n")
}

// typeHoverContent 生成只有类型的悬停内容。
func typeHoverContent(name, typeText string) (string, string) {
	markdownParts := make([]string, 0, 2)
	plainParts := make([]string, 0, 2)
	if name != "" && typeText != "" && name != typeText {
		markdownParts = append(markdownParts, inlineCode(name))
		plainParts = append(plainParts, name)
	}
	if typeText != "" {
		markdownParts = append(markdownParts, "类型: "+inlineCode(typeText))
		plainParts = append(plainParts, "类型: "+typeText)
	}
	return strings.Join(markdownParts, "\n\n"), strings.Join(plainParts, "\n\n")
}

// hoverAtOffset 计算光标处的悬停内容与高亮范围。
func hoverAtOffset(index *semanticIndex, fileKey string, offset int) (string, string, ast.Span, bool, bool) {
	if index == nil {
		return "", "", ast.Span{}, false, false
	}
	if symbol := index.declarationAt(fileKey, offset); symbol != nil {
		markdown, plain := symbolHoverContent(symbol, "")
		if markdown == "" && plain == "" {
			return "", "", ast.Span{}, false, false
		}
		return markdown, plain, symbol.NameSpan, true, true
	}
	if occurrence := index.occurrenceAt(fileKey, offset); occurrence != nil {
		switch {
		case occurrence.Symbol != nil:
			markdown, plain := symbolHoverContent(occurrence.Symbol, occurrence.Type)
			if markdown == "" && plain == "" {
				return "", "", ast.Span{}, false, false
			}
			return markdown, plain, occurrence.Span, true, true
		case occurrence.Role == occurrenceTypeParameter:
			name := occurrence.Name
			return "类型参数 " + inlineCode(name), "类型参数 " + name, occurrence.Span, true, true
		case occurrence.Role == occurrenceBuiltinType:
			return "内置类型 " + inlineCode(occurrence.Type), "内置类型 " + occurrence.Type, occurrence.Span, true, true
		}
	}
	if expression := index.exprAt(fileKey, offset); expression != nil && expression.Type != "" {
		markdown, plain := typeHoverContent("", expression.Type)
		return markdown, plain, expression.Span, true, true
	}
	return "", "", ast.Span{}, false, false
}

// occurrenceHoverMarkdown 返回 Markdown 悬停结果，供索引级测试与内部复用。
func occurrenceHoverMarkdown(index *semanticIndex, fileKey string, offset int) *Hover {
	markdown, _, _, _, ok := hoverAtOffset(index, fileKey, offset)
	if !ok {
		return nil
	}
	return &Hover{Contents: MarkupContent{Kind: "markdown", Value: markdown}}
}

// ---- 符号查找 ----

// symbolAt 返回光标处的语义符号及命中的引用。
// 声明名称优先于引用，表达式类型仅作为悬停兜底。
func (i *semanticIndex) symbolAt(fileKey string, offset int) (*semanticSymbol, *occurrence) {
	if i == nil {
		return nil, nil
	}
	if symbol := i.declarationAt(fileKey, offset); symbol != nil {
		return symbol, nil
	}
	if occurrence := i.occurrenceAt(fileKey, offset); occurrence != nil && occurrence.Symbol != nil {
		return occurrence.Symbol, occurrence
	}
	return nil, nil
}
