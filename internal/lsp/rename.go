package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"Mio/internal/lexer"
	"Mio/internal/token"
)

// 重命名只在可以证明所有引用都位于当前文档时才执行：
// 符号必须声明在当前文件、未导出，并且（字段/变体）所属类型也未导出。
// 对其它情况返回明确错误而不是做一次不完整的跨文件重命名。

// TextEdit 表示单处文本替换。
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// WorkspaceEdit 表示 textDocument/rename 的结果。
type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

// RenameOptions 声明 prepareRename 支持。
type RenameOptions struct {
	PrepareProvider bool `json:"prepareProvider"`
}

type renameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

type prepareRenameResult struct {
	Range       Range  `json:"range"`
	Placeholder string `json:"placeholder"`
}

const codeRequestFailed = -32803 // LSP RequestFailed

func renameFailure(message string) *responseError {
	return &responseError{Code: codeRequestFailed, Message: message}
}

// validRenameIdentifier 要求 name 恰好是一个普通标识符（不是关键字、字面量或多个 token）。
func validRenameIdentifier(name string) bool {
	if name == "" {
		return false
	}
	if _, err := lexer.NewChecked(name, token.CurrentVersion); err != nil {
		return false
	}
	lex := lexer.New(name, token.CurrentVersion)
	first := lex.NextToken()
	if first.Type != token.Ident || first.Literal != name {
		return false
	}
	return lex.NextToken().Type == token.EOF
}

// isTopLevelSymbol 判断符号是否为当前文档的顶层声明。
// Exported 只对顶层声明有意义（局部符号的默认值为 true 但不跨文件可见）。
func isTopLevelSymbol(result *analysis, symbol *semanticSymbol) bool {
	for _, candidate := range result.Index.topLevel[result.DocKey] {
		if candidate == symbol {
			return true
		}
	}
	return false
}

type renamePlan struct {
	analysis *analysis
	symbol   *semanticSymbol
	span     Range
	sites    []site
}

// planRename 解析光标处符号并检查是否可安全重命名。
func (s *server) planRename(ctx context.Context, uri string, position Position) (*renamePlan, *responseError) {
	result := s.analyzeDocument(ctx, uri)
	if result == nil || result.Index == nil {
		return nil, renameFailure("the document could not be analyzed")
	}
	if result.Partial || len(result.Diagnostics) > 0 {
		return nil, renameFailure("rename is unavailable while the file has syntax or type errors")
	}
	offset := result.positionOffset(position)
	symbol, occurrence := result.Index.symbolAt(result.DocKey, offset)
	if symbol == nil {
		return nil, renameFailure("no renameable symbol at this position")
	}
	if symbol.FileKey != result.DocKey {
		return nil, renameFailure("the symbol is declared in another file; cross-file rename is not supported")
	}
	if isTopLevelSymbol(result, symbol) && symbol.Exported {
		return nil, renameFailure("exported symbols cannot be renamed because other files may use them")
	}
	if symbol.Kind == symbolKindField || symbol.Kind == symbolKindEnumMember {
		container := result.Index.symbols[symbol.Container]
		if container == nil || (isTopLevelSymbol(result, container) && container.Exported) || container.FileKey != result.DocKey {
			return nil, renameFailure("members of exported or imported types cannot be renamed")
		}
	}
	if symbol.Name == "" {
		return nil, renameFailure("the symbol has no source spelling")
	}
	sites := []site{symbol.nameSite()}
	sites = append(sites, result.Index.references(symbol)...)
	unique := make([]site, 0, len(sites))
	seen := make(map[[2]int]struct{})
	for _, location := range sites {
		if location.FileKey != result.DocKey {
			return nil, renameFailure("the symbol is referenced from another file; cross-file rename is not supported")
		}
		key := [2]int{location.Span.Start.Offset, location.Span.End.Offset}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		start, end := location.Span.Start.Offset, location.Span.End.Offset
		if start < 0 || end > len(result.Text) || end <= start || result.Text[start:end] != symbol.Name {
			return nil, renameFailure("a reference is not a plain occurrence of the name; rename was refused to avoid corrupting code")
		}
		unique = append(unique, location)
	}
	target := symbol.nameSite()
	if occurrence != nil {
		target = site{FileKey: occurrence.FileKey, Span: occurrence.Span}
	}
	targetRange, ok := result.rangeFor(target.FileKey, target.Span)
	if !ok {
		return nil, renameFailure("the symbol position could not be converted")
	}
	return &renamePlan{analysis: result, symbol: symbol, span: targetRange, sites: unique}, nil
}

func (s *server) requestPrepareRename(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var value TextDocumentPositionParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	if value.TextDocument.URI == "" {
		return nil, &responseError{Code: codeInvalidParams, Message: "missing textDocument.uri"}
	}
	plan, failure := s.planRename(ctx, value.TextDocument.URI, value.Position)
	if failure != nil {
		return nil, failure
	}
	return prepareRenameResult{Range: plan.span, Placeholder: plan.symbol.Name}, nil
}

func (s *server) requestRename(ctx context.Context, params json.RawMessage) (any, *responseError) {
	var value renameParams
	if rpcError := decodeRequestParams(params, &value); rpcError != nil {
		return nil, rpcError
	}
	if value.TextDocument.URI == "" {
		return nil, &responseError{Code: codeInvalidParams, Message: "missing textDocument.uri"}
	}
	if !validRenameIdentifier(value.NewName) {
		return nil, &responseError{Code: codeInvalidParams, Message: fmt.Sprintf("%q is not a valid Mio identifier", value.NewName)}
	}
	plan, failure := s.planRename(ctx, value.TextDocument.URI, value.Position)
	if failure != nil {
		return nil, failure
	}
	if value.NewName == plan.symbol.Name {
		return WorkspaceEdit{Changes: map[string][]TextEdit{}}, nil
	}
	// 冲突检查：新名称不得与任一引用点（及声明点）可见的其它符号同名。
	for _, location := range plan.sites {
		for _, visible := range plan.analysis.Index.visibleSymbols(location.FileKey, location.Span.Start.Offset) {
			if visible != plan.symbol && visible.Name == value.NewName {
				return nil, renameFailure(fmt.Sprintf("%q would conflict with an existing symbol in scope", value.NewName))
			}
		}
	}
	for _, other := range plan.analysis.Index.topLevel[plan.analysis.DocKey] {
		if other != plan.symbol && other.Name == value.NewName && plan.symbol.Container == "" {
			return nil, renameFailure(fmt.Sprintf("%q would conflict with a top-level declaration", value.NewName))
		}
	}
	edits := make([]TextEdit, 0, len(plan.sites))
	for _, location := range plan.sites {
		converted, ok := plan.analysis.rangeFor(location.FileKey, location.Span)
		if !ok {
			return nil, renameFailure("a reference position could not be converted")
		}
		edits = append(edits, TextEdit{Range: converted, NewText: value.NewName})
	}
	sort.SliceStable(edits, func(i, j int) bool {
		a, b := edits[i].Range.Start, edits[j].Range.Start
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Character < b.Character
	})
	return WorkspaceEdit{Changes: map[string][]TextEdit{value.TextDocument.URI: edits}}, nil
}
