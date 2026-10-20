// Package lsp 实现 MioCore 内置的 Language Server Protocol 服务端。
package lsp

import "encoding/json"

const jsonrpcVersion = "2.0"

// JSON-RPC 2.0 与 LSP 使用的错误码
const (
	codeParseError           = -32700
	codeInvalidRequest       = -32600
	codeMethodNotFound       = -32601
	codeInvalidParams        = -32602
	codeInternalError        = -32603
	codeServerNotInitialized = -32002
	codeRequestCancelled     = -32800
)

// LSP DiagnosticSeverity 取值
const (
	severityError   = 1
	severityWarning = 2
	severityInfo    = 3
)

// LSP MessageType 取值
const (
	messageTypeError   = 1
	messageTypeWarning = 2
	messageTypeInfo    = 3
	messageTypeLog     = 4
)

// textDocumentSyncKindIncremental 对应 LSP TextDocumentSyncKind.Incremental
const textDocumentSyncKindIncremental = 2

// message 表示一条 JSON-RPC 2.0 消息
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *responseError  `json:"error,omitempty"`
}

// responseError 表示 JSON-RPC 错误对象
type responseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// isRequest 判断消息是否携带可用于响应的 ID
func (m message) isRequest() bool {
	return m.Method != "" && len(m.ID) > 0 && string(m.ID) != "null"
}

// isNotification 判断消息是否为通知
func (m message) isNotification() bool {
	return m.Method != "" && !m.isRequest()
}

// Position 表示 UTF-16 行列位置
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range 表示左闭右开位置区间
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location 表示其他文件中的位置
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier 表示文档 URI
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// VersionedTextDocumentIdentifier 表示带版本号的文档
type VersionedTextDocumentIdentifier struct {
	URI     string `json:"uri"`
	Version int    `json:"version"`
}

// TextDocumentItem 表示 didOpen 传入的文档
type TextDocumentItem struct {
	URI        string `json:"uri"`
	LanguageID string `json:"languageId"`
	Version    int    `json:"version"`
	Text       string `json:"text"`
}

// TextDocumentContentChangeEvent 表示一次内容变更
type TextDocumentContentChangeEvent struct {
	Range       *Range `json:"range,omitempty"`
	RangeLength *int   `json:"rangeLength,omitempty"`
	Text        string `json:"text"`
}

// DidOpenTextDocumentParams 表示 textDocument/didOpen 参数
type DidOpenTextDocumentParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// DidChangeTextDocumentParams 表示 textDocument/didChange 参数
type DidChangeTextDocumentParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseTextDocumentParams 表示 textDocument/didClose 参数
type DidCloseTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// DidSaveTextDocumentParams 表示 textDocument/didSave 参数
type DidSaveTextDocumentParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         *string                `json:"text,omitempty"`
}

// PublishDiagnosticsParams 表示 textDocument/publishDiagnostics 参数
type PublishDiagnosticsParams struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// DiagnosticRelatedInformation 表示相关位置信息
type DiagnosticRelatedInformation struct {
	Location Location `json:"location"`
	Message  string   `json:"message"`
}

// Diagnostic 表示一条 LSP 诊断
type Diagnostic struct {
	Range              Range                          `json:"range"`
	Severity           int                            `json:"severity,omitempty"`
	Code               string                         `json:"code,omitempty"`
	Source             string                         `json:"source,omitempty"`
	Message            string                         `json:"message"`
	RelatedInformation []DiagnosticRelatedInformation `json:"relatedInformation,omitempty"`
}

// WorkspaceFolder 表示工作区根目录
type WorkspaceFolder struct {
	URI  string `json:"uri"`
	Name string `json:"name"`
}

// InitializeParams 表示 initialize 参数
type InitializeParams struct {
	ProcessID             *int              `json:"processId"`
	RootURI               *string           `json:"rootUri"`
	RootPath              *string           `json:"rootPath"`
	WorkspaceFolders      []WorkspaceFolder `json:"workspaceFolders,omitempty"`
	InitializationOptions json.RawMessage   `json:"initializationOptions,omitempty"`
	Capabilities          json.RawMessage   `json:"capabilities,omitempty"`
	Trace                 string            `json:"trace,omitempty"`
}

// InitializeResult 表示 initialize 结果
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
	ServerInfo   ServerInfo         `json:"serverInfo"`
}

// ServerInfo 表示服务器身份
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
}

// ServerCapabilities 声明真正实现的语言能力。
// 未实现的功能（Rename、SemanticTokens、Formatting）绝不在此声明。
type ServerCapabilities struct {
	PositionEncoding       string                  `json:"positionEncoding,omitempty"`
	TextDocumentSync       TextDocumentSyncOptions `json:"textDocumentSync"`
	HoverProvider          bool                    `json:"hoverProvider,omitempty"`
	DefinitionProvider     bool                    `json:"definitionProvider,omitempty"`
	ReferencesProvider     bool                    `json:"referencesProvider,omitempty"`
	DocumentSymbolProvider bool                    `json:"documentSymbolProvider,omitempty"`
	CompletionProvider     *CompletionOptions      `json:"completionProvider,omitempty"`
	SignatureHelpProvider  *SignatureHelpOptions   `json:"signatureHelpProvider,omitempty"`
}

// CompletionOptions 表示补全能力选项
type CompletionOptions struct {
	TriggerCharacters []string `json:"triggerCharacters,omitempty"`
	ResolveProvider   bool     `json:"resolveProvider,omitempty"`
}

// SignatureHelpOptions 表示签名提示能力选项
type SignatureHelpOptions struct {
	TriggerCharacters   []string `json:"triggerCharacters,omitempty"`
	RetriggerCharacters []string `json:"retriggerCharacters,omitempty"`
}

// TextDocumentSyncOptions 表示文档同步方式
type TextDocumentSyncOptions struct {
	OpenClose bool         `json:"openClose"`
	Change    int          `json:"change"`
	Save      *SaveOptions `json:"save,omitempty"`
}

// SaveOptions 表示保存通知选项
type SaveOptions struct {
	IncludeText bool `json:"includeText"`
}

// CancelParams 表示 $/cancelRequest 参数
type CancelParams struct {
	ID json.RawMessage `json:"id"`
}

// DidChangeConfigurationParams 表示 workspace/didChangeConfiguration 参数
type DidChangeConfigurationParams struct {
	Settings json.RawMessage `json:"settings"`
}

// LogMessageParams 表示 window/logMessage 参数
type LogMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

// ShowMessageParams 表示 window/showMessage 参数
type ShowMessageParams struct {
	Type    int    `json:"type"`
	Message string `json:"message"`
}

// clientCapabilities 只解析服务器实际使用的客户端能力
type clientCapabilities struct {
	General struct {
		PositionEncodings []string `json:"positionEncodings,omitempty"`
	} `json:"general,omitempty"`
	TextDocument struct {
		Hover struct {
			ContentFormat []string `json:"contentFormat,omitempty"`
		} `json:"hover,omitempty"`
		Completion struct {
			CompletionItem struct {
				SnippetSupport bool `json:"snippetSupport,omitempty"`
			} `json:"completionItem,omitempty"`
		} `json:"completion,omitempty"`
	} `json:"textDocument,omitempty"`
}

// initializationOptions 保存客户端传入的扩展初始化选项
type initializationOptions struct {
	DiagnosticsEnabled *bool  `json:"diagnosticsEnabled,omitempty"`
	DiagnosticsDelayMS *int   `json:"diagnosticsDelayMs,omitempty"`
	BunkoRoot          string `json:"bunkoRoot,omitempty"`
}

// MarkupContent 表示 Markdown 或纯文本内容
type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// Hover 表示 textDocument/hover 结果
type Hover struct {
	Contents MarkupContent `json:"contents"`
	Range    *Range        `json:"range,omitempty"`
}

// TextDocumentPositionParams 表示以位置为参数的请求
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// ReferenceContext 表示查找引用选项
type ReferenceContext struct {
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ReferenceParams 表示 textDocument/references 参数
type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      ReferenceContext       `json:"context"`
}

// DocumentSymbol 表示 textDocument/documentSymbol 结果节点
type DocumentSymbol struct {
	Name           string            `json:"name"`
	Detail         string            `json:"detail,omitempty"`
	Kind           int               `json:"kind"`
	Range          Range             `json:"range"`
	SelectionRange Range             `json:"selectionRange"`
	Children       []*DocumentSymbol `json:"children,omitempty"`
}

// CompletionContext 表示补全触发上下文
type CompletionContext struct {
	TriggerKind      int    `json:"triggerKind"`
	TriggerCharacter string `json:"triggerCharacter,omitempty"`
}

// CompletionParams 表示 textDocument/completion 参数
type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      *CompletionContext     `json:"context,omitempty"`
}

// CompletionItem 表示一条补全候选
type CompletionItem struct {
	Label            string         `json:"label"`
	Kind             int            `json:"kind,omitempty"`
	Detail           string         `json:"detail,omitempty"`
	Documentation    *MarkupContent `json:"documentation,omitempty"`
	InsertText       string         `json:"insertText,omitempty"`
	InsertTextFormat int            `json:"insertTextFormat,omitempty"`
	FilterText       string         `json:"filterText,omitempty"`
	SortText         string         `json:"sortText,omitempty"`
}

// CompletionList 表示补全结果
type CompletionList struct {
	IsIncomplete bool             `json:"isIncomplete"`
	Items        []CompletionItem `json:"items"`
}

// ParameterInformation 表示签名中的一个参数
type ParameterInformation struct {
	Label string `json:"label"`
}

// SignatureInformation 表示一个候选签名
type SignatureInformation struct {
	Label           string                 `json:"label"`
	Documentation   *MarkupContent         `json:"documentation,omitempty"`
	Parameters      []ParameterInformation `json:"parameters,omitempty"`
	ActiveParameter *int                   `json:"activeParameter,omitempty"`
}

// SignatureHelp 表示 textDocument/signatureHelp 结果
type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}
