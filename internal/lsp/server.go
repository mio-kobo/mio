package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"sync"
	"time"
)

// ErrExitWithoutShutdown 表示客户端未发送 shutdown 就退出，进程应以非零码结束。
var ErrExitWithoutShutdown = errors.New("lsp: exited without shutdown")

const (
	defaultDiagnosticDelay = 150 * time.Millisecond
	defaultMaxConcurrent   = 4
	maxPreCancelledIDs     = 256
)

// Options 配置 MioCore 语言服务器。
type Options struct {
	// Version 写入 initialize 的 serverInfo.version。
	Version string
	// DiagnosticDelay 是 didOpen/didChange 后等待稳定再分析的时间。
	DiagnosticDelay time.Duration
	// MaxConcurrentAnalysis 限制同时运行的静态分析数量。
	MaxConcurrentAnalysis int
	// MaxMessageSize 限制单帧 JSON 负载大小。
	MaxMessageSize int
	// BunkoRoot 是 initializeOptions.bunkoRoot 的兜底值。
	BunkoRoot string
}

type requestHandler func(ctx context.Context, params json.RawMessage) (any, *responseError)
type notificationHandler func(params json.RawMessage)

type diagnosticJob struct {
	timer      *time.Timer
	cancel     context.CancelFunc
	generation uint64
}

// server 持有一次 stdio 会话的全部状态。
type server struct {
	transport *transport
	options   Options
	logger    *log.Logger
	store     *documentStore
	analyzer  *analyzer
	analyses  *analysisService

	ctx    context.Context
	cancel context.CancelFunc

	mu             sync.Mutex
	initialized    bool
	shutdown       bool
	exited         bool
	workspaceRoots []string

	diagnosticsEnabled bool
	diagnosticsDelay   time.Duration
	markdownHover      bool
	snippetCompletion  bool

	diagMu         sync.Mutex
	diagJobs       map[string]*diagnosticJob
	diagGeneration map[string]uint64

	requestMu      sync.Mutex
	activeRequests map[string]context.CancelFunc
	preCancelled   map[string]struct{}

	requests      map[string]requestHandler
	notifications map[string]notificationHandler
}

// Serve 在 stdio 上运行语言服务器，直到收到 exit 或输入关闭。
func Serve(stdin io.Reader, stdout io.Writer, logWriter io.Writer, options Options) error {
	instance, cancel := newServer(stdin, stdout, logWriter, options)
	defer cancel()

	err := instance.loop()
	instance.finish()
	if err != nil {
		return err
	}
	if instance.wasShutdown() || instance.wasExitedAfterShutdown() {
		return nil
	}
	return ErrExitWithoutShutdown
}

func newServer(stdin io.Reader, stdout io.Writer, logWriter io.Writer, options Options) (*server, context.CancelFunc) {
	if options.DiagnosticDelay <= 0 {
		options.DiagnosticDelay = defaultDiagnosticDelay
	}
	if options.MaxConcurrentAnalysis <= 0 {
		options.MaxConcurrentAnalysis = defaultMaxConcurrent
	}
	if options.MaxMessageSize <= 0 {
		options.MaxMessageSize = defaultMaxMessageSize
	}
	if logWriter == nil {
		logWriter = io.Discard
	}
	ctx, cancel := context.WithCancel(context.Background())

	transport := newTransport(stdin, stdout)
	transport.maxMessageSize = options.MaxMessageSize
	instance := &server{
		transport:          transport,
		options:            options,
		logger:             log.New(logWriter, "mio lsp: ", log.LstdFlags),
		store:              newDocumentStore(),
		analyzer:           newAnalyzer(options.BunkoRoot),
		analyses:           newAnalysisService(options.MaxConcurrentAnalysis, defaultMaxAnalyses),
		ctx:                ctx,
		cancel:             cancel,
		diagnosticsEnabled: true,
		diagnosticsDelay:   options.DiagnosticDelay,
		markdownHover:      true,
		snippetCompletion:  false,
		diagJobs:           make(map[string]*diagnosticJob),
		diagGeneration:     make(map[string]uint64),
		activeRequests:     make(map[string]context.CancelFunc),
		preCancelled:       make(map[string]struct{}),
	}
	instance.registerHandlers()
	return instance, cancel
}

func (s *server) registerHandlers() {
	s.requests = map[string]requestHandler{
		"shutdown":                    s.requestShutdown,
		"textDocument/hover":          s.requestHover,
		"textDocument/definition":     s.requestDefinition,
		"textDocument/references":     s.requestReferences,
		"textDocument/documentSymbol": s.requestDocumentSymbol,
		"textDocument/completion":     s.requestCompletion,
		"textDocument/signatureHelp":  s.requestSignatureHelp,
	}
	s.notifications = map[string]notificationHandler{
		"initialized":                      s.notificationInitialized,
		"exit":                             s.notificationExit,
		"$/cancelRequest":                  s.notificationCancel,
		"$/setTrace":                       func(json.RawMessage) {},
		"workspace/didChangeConfiguration": s.notificationDidChangeConfiguration,
		"textDocument/didOpen":             s.notificationDidOpen,
		"textDocument/didChange":           s.notificationDidChange,
		"textDocument/didClose":            s.notificationDidClose,
		"textDocument/didSave":             s.notificationDidSave,
	}
}

// loop 串行读取消息；request 分发到独立 goroutine，notification 保持顺序。
func (s *server) loop() error {
	for {
		payload, err := s.transport.read()
		if err != nil {
			if errors.Is(err, errMessageTooLarge) {
				s.respondError(json.RawMessage("null"), codeParseError, err.Error())
			}
			if errors.Is(err, io.EOF) {
				if s.wasShutdown() {
					return nil
				}
				return ErrExitWithoutShutdown
			}
			if errors.Is(err, io.ErrUnexpectedEOF) {
				if s.wasShutdown() {
					return nil
				}
				return ErrExitWithoutShutdown
			}
			return err
		}
		s.handlePayload(payload)
		if s.hasExited() {
			return nil
		}
	}
}

func (s *server) handlePayload(payload []byte) {
	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		s.respondError(json.RawMessage("null"), codeParseError, "Parse error: "+err.Error())
		return
	}
	switch {
	case msg.isRequest():
		// 生命周期请求同步处理，保证 initialize/shutdown 的响应顺序。
		if msg.Method == "initialize" || msg.Method == "shutdown" {
			s.dispatchRequest(msg)
			return
		}
		go s.dispatchRequest(msg)
	case msg.isNotification():
		s.dispatchNotification(msg)
	case msg.Method == "" && len(msg.ID) > 0:
		// 客户端对服务器请求的响应；本阶段服务器不主动发请求。
		return
	default:
		s.respondError(json.RawMessage("null"), codeInvalidRequest, "Invalid request")
	}
}

func (s *server) dispatchRequest(msg message) {
	key := requestKey(msg.ID)
	ctx, cancel := context.WithCancel(s.ctx)
	s.requestMu.Lock()
	s.activeRequests[key] = cancel
	_, cancelledEarly := s.preCancelled[key]
	delete(s.preCancelled, key)
	s.requestMu.Unlock()
	if cancelledEarly {
		cancel()
	}
	defer func() {
		s.requestMu.Lock()
		delete(s.activeRequests, key)
		s.requestMu.Unlock()
		cancel()
	}()

	if msg.Method == "initialize" {
		s.handleInitialize(ctx, msg)
		return
	}
	if !s.isInitialized() {
		s.respondError(msg.ID, codeServerNotInitialized, "Server not initialized")
		return
	}
	if s.isShutdown() {
		s.respondError(msg.ID, codeInvalidRequest, "Server is shutting down")
		return
	}
	handler, ok := s.requests[msg.Method]
	if !ok {
		s.respondError(msg.ID, codeMethodNotFound, "Method not found: "+msg.Method)
		return
	}
	result, rpcError := handler(ctx, msg.Params)
	if ctx.Err() != nil {
		s.respondError(msg.ID, codeRequestCancelled, "Request cancelled")
		return
	}
	if rpcError != nil {
		s.respondRPCError(msg.ID, *rpcError)
		return
	}
	s.respond(msg.ID, result)
}

func (s *server) dispatchNotification(msg message) {
	if s.isShutdown() && msg.Method != "exit" {
		return
	}
	handler, ok := s.notifications[msg.Method]
	if !ok {
		// 协议要求忽略未知通知。
		return
	}
	handler(msg.Params)
}

func (s *server) handleInitialize(ctx context.Context, msg message) {
	var params InitializeParams
	if len(msg.Params) > 0 {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			s.respondError(msg.ID, codeInvalidParams, "Invalid initialize params: "+err.Error())
			return
		}
	}
	var extension initializationOptions
	if len(params.InitializationOptions) > 0 {
		_ = json.Unmarshal(params.InitializationOptions, &extension)
	}
	var capabilities clientCapabilities
	if len(params.Capabilities) > 0 {
		_ = json.Unmarshal(params.Capabilities, &capabilities)
	}
	if encodings := capabilities.General.PositionEncodings; len(encodings) > 0 {
		supported := false
		for _, encoding := range encodings {
			if encoding == "utf-16" {
				supported = true
				break
			}
		}
		if !supported {
			s.logf("client does not offer utf-16 position encoding; using utf-16 anyway")
		}
	}

	delay := s.options.DiagnosticDelay
	if extension.DiagnosticsDelayMS != nil && *extension.DiagnosticsDelayMS >= 0 {
		delay = time.Duration(*extension.DiagnosticsDelayMS) * time.Millisecond
	}
	enabled := true
	if extension.DiagnosticsEnabled != nil {
		enabled = *extension.DiagnosticsEnabled
	}
	if extension.BunkoRoot != "" {
		s.analyzer.bunkoRoot = extension.BunkoRoot
	}
	// 按客户端能力选择 Markdown 悬停与 snippet 补全；默认不假设 snippet 支持。
	markdownSupported := true
	if formats := capabilities.TextDocument.Hover.ContentFormat; len(formats) > 0 {
		markdownSupported = false
		for _, format := range formats {
			if format == "markdown" {
				markdownSupported = true
				break
			}
		}
	}
	snippetSupported := capabilities.TextDocument.Completion.CompletionItem.SnippetSupport

	s.mu.Lock()
	if s.initialized {
		s.mu.Unlock()
		s.respondError(msg.ID, codeInvalidRequest, "initialize called twice")
		return
	}
	s.initialized = true
	s.workspaceRoots = workspacePaths(params)
	s.diagnosticsEnabled = enabled
	s.diagnosticsDelay = delay
	s.markdownHover = markdownSupported
	s.snippetCompletion = snippetSupported
	s.shutdown = false
	s.mu.Unlock()

	s.logf("initialize: roots=%v, diagnostics=%t, delay=%s", s.workspaceRoots, enabled, delay)
	result := InitializeResult{
		Capabilities: ServerCapabilities{
			PositionEncoding: "utf-16",
			TextDocumentSync: TextDocumentSyncOptions{
				OpenClose: true,
				Change:    textDocumentSyncKindIncremental,
				Save:      &SaveOptions{IncludeText: false},
			},
			HoverProvider:          true,
			DefinitionProvider:     true,
			ReferencesProvider:     true,
			DocumentSymbolProvider: true,
			CompletionProvider: &CompletionOptions{
				TriggerCharacters: []string{"."},
			},
			SignatureHelpProvider: &SignatureHelpOptions{
				TriggerCharacters:   []string{"(", ","},
				RetriggerCharacters: []string{","},
			},
		},
		ServerInfo: ServerInfo{Name: "MioCore", Version: s.options.Version},
	}
	s.respond(msg.ID, result)
	// 容忍客户端在 initialize 之前发送 didOpen：初始化完成后补做分析。
	for _, doc := range s.store.all() {
		s.scheduleDiagnostics(doc.URI, 0)
	}
}

func (s *server) requestShutdown(ctx context.Context, params json.RawMessage) (any, *responseError) {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	s.cancelAllDiagnostics()
	s.analyses.invalidateAll()
	for _, doc := range s.store.all() {
		s.publish(doc.URI, nil, []Diagnostic{})
	}
	s.logf("shutdown requested")
	return nil, nil
}

func (s *server) notificationInitialized(params json.RawMessage) {
	s.logf("client initialized")
	s.sendNotification("window/logMessage", LogMessageParams{
		Type:    messageTypeInfo,
		Message: "MioCore language server ready.",
	})
}

func (s *server) notificationExit(params json.RawMessage) {
	s.mu.Lock()
	s.exited = true
	s.mu.Unlock()
	s.cancelAllDiagnostics()
}

func (s *server) notificationCancel(params json.RawMessage) {
	var cancel CancelParams
	if err := json.Unmarshal(params, &cancel); err != nil || len(cancel.ID) == 0 {
		return
	}
	key := requestKey(cancel.ID)
	s.requestMu.Lock()
	if active, ok := s.activeRequests[key]; ok {
		active()
	} else if len(s.preCancelled) < maxPreCancelledIDs {
		s.preCancelled[key] = struct{}{}
	}
	s.requestMu.Unlock()
}

func (s *server) notificationDidOpen(params json.RawMessage) {
	var value DidOpenTextDocumentParams
	if err := json.Unmarshal(params, &value); err != nil {
		s.logf("didOpen: %v", err)
		return
	}
	if value.TextDocument.URI == "" {
		s.logf("didOpen: empty uri")
		return
	}
	doc := s.store.open(value.TextDocument)
	s.analyses.invalidate(doc.URI)
	s.logf("didOpen: %s version=%d bytes=%d", doc.URI, doc.Version, len(doc.Text))
	s.scheduleDiagnostics(doc.URI, s.currentDiagnosticsDelay())
}

func (s *server) notificationDidChange(params json.RawMessage) {
	var value DidChangeTextDocumentParams
	if err := json.Unmarshal(params, &value); err != nil {
		s.logf("didChange: %v", err)
		return
	}
	if value.TextDocument.URI == "" || len(value.ContentChanges) == 0 {
		return
	}
	doc, stale, warnings := s.store.change(value.TextDocument.URI, value.TextDocument.Version, value.ContentChanges)
	for _, warning := range warnings {
		s.logf("%s", warning)
	}
	if stale || doc == nil {
		return
	}
	s.analyses.invalidate(doc.URI)
	s.scheduleDiagnostics(doc.URI, s.currentDiagnosticsDelay())
}

func (s *server) notificationDidClose(params json.RawMessage) {
	var value DidCloseTextDocumentParams
	if err := json.Unmarshal(params, &value); err != nil || value.TextDocument.URI == "" {
		return
	}
	s.cancelDiagnostics(value.TextDocument.URI)
	if s.store.close(value.TextDocument.URI) == nil {
		return
	}
	s.analyses.invalidate(value.TextDocument.URI)
	s.logf("didClose: %s", value.TextDocument.URI)
	s.publish(value.TextDocument.URI, nil, []Diagnostic{})
}

func (s *server) notificationDidSave(params json.RawMessage) {
	var value DidSaveTextDocumentParams
	if err := json.Unmarshal(params, &value); err != nil || value.TextDocument.URI == "" {
		return
	}
	if value.Text != nil {
		s.store.replaceText(value.TextDocument.URI, *value.Text)
	}
	s.analyses.invalidate(value.TextDocument.URI)
	s.scheduleDiagnostics(value.TextDocument.URI, 0)
}

func (s *server) notificationDidChangeConfiguration(params json.RawMessage) {
	var value DidChangeConfigurationParams
	if err := json.Unmarshal(params, &value); err != nil || len(value.Settings) == 0 {
		return
	}
	enabled, found := diagnosticsEnabledFromSettings(value.Settings)
	if !found {
		return
	}
	s.mu.Lock()
	s.diagnosticsEnabled = enabled
	s.mu.Unlock()
	s.logf("diagnostics enabled=%t", enabled)
	if !enabled {
		s.cancelAllDiagnostics()
		for _, doc := range s.store.all() {
			s.publish(doc.URI, nil, []Diagnostic{})
		}
		return
	}
	for _, doc := range s.store.all() {
		s.scheduleDiagnostics(doc.URI, 0)
	}
}

func (s *server) currentDiagnosticsDelay() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diagnosticsDelay
}

// markdownHoverSupported 返回客户端是否接受 Markdown 悬停内容。
func (s *server) markdownHoverSupported() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.markdownHover
}

// snippetCompletionSupported 返回客户端是否接受 snippet 插入文本。
func (s *server) snippetCompletionSupported() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snippetCompletion
}

func (s *server) diagnosticsOn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diagnosticsEnabled
}

func (s *server) scheduleDiagnostics(uri string, delay time.Duration) {
	if !s.isInitialized() || !s.diagnosticsOn() {
		return
	}
	if s.isShutdown() {
		return
	}
	s.diagMu.Lock()
	if previous, ok := s.diagJobs[uri]; ok {
		if previous.timer != nil {
			previous.timer.Stop()
		}
		if previous.cancel != nil {
			previous.cancel()
		}
	}
	s.diagGeneration[uri]++
	generation := s.diagGeneration[uri]
	ctx, cancel := context.WithCancel(s.ctx)
	job := &diagnosticJob{cancel: cancel, generation: generation}
	job.timer = time.AfterFunc(delay, func() {
		s.runDiagnostics(uri, generation, ctx)
	})
	s.diagJobs[uri] = job
	s.diagMu.Unlock()
}

func (s *server) runDiagnostics(uri string, generation uint64, ctx context.Context) {
	defer func() {
		s.diagMu.Lock()
		if job, ok := s.diagJobs[uri]; ok && job.generation == generation {
			delete(s.diagJobs, uri)
			if job.cancel != nil {
				job.cancel()
			}
		}
		s.diagMu.Unlock()
	}()

	started := time.Now()
	result := s.analyzeDocument(ctx, uri)
	if result == nil || ctx.Err() != nil {
		return
	}

	s.diagMu.Lock()
	current := s.diagGeneration[uri]
	s.diagMu.Unlock()
	if current != generation {
		return
	}
	if result.Diagnostics == nil {
		return
	}
	version := result.Version
	s.publish(uri, &version, result.Diagnostics)
	s.logf("diagnostics: %s version=%d count=%d took=%s", uri, version, len(result.Diagnostics), time.Since(started).Round(time.Millisecond))
}

func (s *server) cancelDiagnostics(uri string) {
	s.diagMu.Lock()
	if job, ok := s.diagJobs[uri]; ok {
		if job.timer != nil {
			job.timer.Stop()
		}
		if job.cancel != nil {
			job.cancel()
		}
		delete(s.diagJobs, uri)
	}
	s.diagGeneration[uri]++
	s.diagMu.Unlock()
}

func (s *server) cancelAllDiagnostics() {
	s.diagMu.Lock()
	for uri, job := range s.diagJobs {
		if job.timer != nil {
			job.timer.Stop()
		}
		if job.cancel != nil {
			job.cancel()
		}
		delete(s.diagJobs, uri)
		s.diagGeneration[uri]++
	}
	s.diagMu.Unlock()
}

func (s *server) publish(uri string, version *int, diagnostics []Diagnostic) {
	if diagnostics == nil {
		diagnostics = []Diagnostic{}
	}
	if err := s.sendNotification("textDocument/publishDiagnostics", PublishDiagnosticsParams{
		URI:         uri,
		Version:     version,
		Diagnostics: diagnostics,
	}); err != nil {
		s.logf("publish diagnostics: %v", err)
	}
}

func (s *server) respond(id json.RawMessage, result any) {
	raw, err := json.Marshal(result)
	if err != nil {
		s.respondError(id, codeInternalError, "Marshal result: "+err.Error())
		return
	}
	payload, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
	}{JSONRPC: jsonrpcVersion, ID: normalizeID(id), Result: raw})
	if err != nil {
		s.logf("marshal response: %v", err)
		return
	}
	if err := s.transport.write(payload); err != nil {
		s.logf("write response: %v", err)
	}
}

func (s *server) respondError(id json.RawMessage, code int, message string) {
	s.respondRPCError(id, responseError{Code: code, Message: message})
}

func (s *server) respondRPCError(id json.RawMessage, rpcError responseError) {
	payload, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Error   responseError   `json:"error"`
	}{JSONRPC: jsonrpcVersion, ID: normalizeID(id), Error: rpcError})
	if err != nil {
		s.logf("marshal error response: %v", err)
		return
	}
	if err := s.transport.write(payload); err != nil {
		s.logf("write error response: %v", err)
	}
}

func (s *server) sendNotification(method string, params any) error {
	raw, err := json.Marshal(params)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params"`
	}{JSONRPC: jsonrpcVersion, Method: method, Params: raw})
	if err != nil {
		return err
	}
	return s.transport.write(payload)
}

func (s *server) isInitialized() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.initialized
}

func (s *server) isShutdown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdown
}

func (s *server) hasExited() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited
}

func (s *server) wasShutdown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdown
}

func (s *server) wasExitedAfterShutdown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.exited && s.shutdown
}

func (s *server) finish() {
	s.cancelAllDiagnostics()
	s.cancel()
}

func (s *server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}

func requestKey(id json.RawMessage) string {
	return string(id)
}

func normalizeID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

// workspacePaths 提取初始化参数中的工作区根路径。
func workspacePaths(params InitializeParams) []string {
	roots := make([]string, 0, 2)
	if params.RootURI != nil {
		if path, ok := pathFromURI(*params.RootURI); ok {
			roots = append(roots, path)
		}
	}
	if params.RootPath != nil && *params.RootPath != "" {
		roots = append(roots, *params.RootPath)
	}
	for _, folder := range params.WorkspaceFolders {
		if path, ok := pathFromURI(folder.URI); ok {
			roots = append(roots, path)
		}
	}
	return roots
}

// diagnosticsEnabledFromSettings 兼容嵌套与扁平两种配置结构。
func diagnosticsEnabledFromSettings(settings json.RawMessage) (bool, bool) {
	var root map[string]any
	if err := json.Unmarshal(settings, &root); err != nil {
		return false, false
	}
	if value, ok := boolAt(root, "diagnosticsEnabled"); ok {
		return value, true
	}
	mio, ok := root["mio"].(map[string]any)
	if !ok {
		mio = root
	}
	if value, ok := boolAt(mio, "diagnosticsEnabled"); ok {
		return value, true
	}
	diagnostics, ok := mio["diagnostics"].(map[string]any)
	if !ok {
		return false, false
	}
	return boolAt(diagnostics, "enabled")
}

func boolAt(object map[string]any, key string) (bool, bool) {
	value, ok := object[key]
	if !ok {
		return false, false
	}
	boolean, ok := value.(bool)
	return boolean, ok
}
