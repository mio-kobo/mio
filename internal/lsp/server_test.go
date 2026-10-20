package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

func (b *lockedBuffer) bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buffer.Bytes()...)
}

type harness struct {
	t         *testing.T
	conn      *transport
	clientIn  *io.PipeWriter
	clientOut *io.PipeReader
	serverIn  *io.PipeReader
	serverOut *io.PipeWriter
	finished  chan struct{}
	incoming  chan message
	readErr   error
	serveErr  error
	logs      *lockedBuffer
}

func newHarness(t *testing.T, options Options) *harness {
	t.Helper()
	if options.Version == "" {
		options.Version = "test"
	}
	if options.DiagnosticDelay == 0 {
		options.DiagnosticDelay = 5 * time.Millisecond
	}
	serverIn, clientIn := io.Pipe()
	clientOut, serverOut := io.Pipe()
	instance := &harness{
		t: t, conn: newTransport(clientOut, clientIn),
		clientIn: clientIn, clientOut: clientOut, serverIn: serverIn, serverOut: serverOut,
		finished: make(chan struct{}), incoming: make(chan message, 128), logs: &lockedBuffer{},
	}
	go func() {
		instance.serveErr = Serve(serverIn, serverOut, instance.logs, options)
		serverOut.Close()
		close(instance.finished)
	}()
	go func() {
		for {
			payload, err := instance.conn.read()
			if err != nil {
				instance.readErr = err
				close(instance.incoming)
				return
			}
			var msg message
			if err := json.Unmarshal(payload, &msg); err != nil {
				instance.readErr = err
				close(instance.incoming)
				return
			}
			instance.incoming <- msg
		}
	}()
	t.Cleanup(func() {
		clientIn.Close()
		clientOut.Close()
		instance.wait(3 * time.Second)
	})
	return instance
}

func (h *harness) send(value any) {
	h.t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		h.t.Fatal(err)
	}
	if err := h.conn.write(payload); err != nil {
		h.t.Fatalf("write: %v", err)
	}
}

func (h *harness) notify(method string, params any) {
	h.t.Helper()
	h.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (h *harness) next(timeout time.Duration) message {
	h.t.Helper()
	select {
	case msg, ok := <-h.incoming:
		if !ok {
			h.t.Fatalf("language server connection closed: %v", h.readErr)
		}
		return msg
	case <-time.After(timeout):
		h.t.Fatalf("timed out waiting for language server message")
		return message{}
	}
}

func (h *harness) request(id int, method string, params any) message {
	h.t.Helper()
	h.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	want := strconv.Itoa(id)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := h.next(time.Until(deadline))
		if msg.Method != "" {
			continue
		}
		if string(msg.ID) == want {
			return msg
		}
	}
	h.t.Fatalf("no response for request %d", id)
	return message{}
}

func (h *harness) initialize() InitializeResult {
	h.t.Helper()
	response := h.request(1, "initialize", map[string]any{
		"processId":    nil,
		"rootUri":      nil,
		"capabilities": map[string]any{},
		"initializationOptions": map[string]any{
			"diagnosticsDelayMs": 5,
		},
	})
	if response.Error != nil {
		h.t.Fatalf("initialize failed: %#v", response.Error)
	}
	var result InitializeResult
	if err := json.Unmarshal(response.Result, &result); err != nil {
		h.t.Fatalf("decode initialize result: %v", err)
	}
	h.notify("initialized", map[string]any{})
	return result
}

func (h *harness) expectDiagnostics(uri string) PublishDiagnosticsParams {
	h.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := h.next(time.Until(deadline))
		if msg.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var params PublishDiagnosticsParams
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			h.t.Fatalf("decode diagnostics: %v", err)
		}
		if params.URI == uri {
			return params
		}
	}
	h.t.Fatalf("no diagnostics for %s", uri)
	return PublishDiagnosticsParams{}
}

func (h *harness) open(uri, text string, version int) {
	h.t.Helper()
	h.notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{
			"uri": uri, "languageId": "mio", "version": version, "text": text,
		},
	})
}

func (h *harness) change(uri string, version int, text string) {
	h.t.Helper()
	h.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": version},
		"contentChanges": []map[string]any{
			{"text": text},
		},
	})
}

func (h *harness) save(uri string) {
	h.t.Helper()
	h.notify("textDocument/didSave", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

func (h *harness) close(uri string) {
	h.t.Helper()
	h.notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

func (h *harness) shutdown() {
	h.t.Helper()
	response := h.request(99, "shutdown", nil)
	if response.Error != nil {
		h.t.Fatalf("shutdown failed: %#v", response.Error)
	}
	if string(response.Result) != "null" {
		h.t.Fatalf("shutdown result = %s", response.Result)
	}
}

func (h *harness) wait(timeout time.Duration) error {
	h.t.Helper()
	select {
	case <-h.finished:
		return h.serveErr
	case <-time.After(timeout):
		h.t.Fatal("server did not exit")
		return nil
	}
}

func (h *harness) exit() {
	h.t.Helper()
	h.notify("exit", nil)
	if err := h.wait(3 * time.Second); err != nil {
		h.t.Fatalf("serve: %v", err)
	}
}

func TestServerInitializeCapabilities(t *testing.T) {
	h := newHarness(t, Options{})
	result := h.initialize()
	if result.ServerInfo.Name != "MioCore" {
		t.Fatalf("server name = %q", result.ServerInfo.Name)
	}
	if result.ServerInfo.Version != "test" {
		t.Fatalf("server version = %q", result.ServerInfo.Version)
	}
	if result.Capabilities.PositionEncoding != "utf-16" {
		t.Fatalf("positionEncoding = %q", result.Capabilities.PositionEncoding)
	}
	if !result.Capabilities.TextDocumentSync.OpenClose ||
		result.Capabilities.TextDocumentSync.Change != textDocumentSyncKindIncremental {
		t.Fatalf("sync capabilities = %#v", result.Capabilities.TextDocumentSync)
	}
	h.shutdown()
	h.exit()
}

func TestServerPublishesAndClearsDiagnostics(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	uri := uriFromPath(filepath.Join(t.TempDir(), "broken.mio"))
	h.open(uri, brokenSource, 1)
	published := h.expectDiagnostics(uri)
	if published.Version == nil || *published.Version != 1 {
		t.Fatalf("version = %v", published.Version)
	}
	if len(published.Diagnostics) != 1 || published.Diagnostics[0].Code != "E0201" {
		t.Fatalf("diagnostics = %#v", published.Diagnostics)
	}

	h.change(uri, 2, validSource)
	cleared := h.expectDiagnostics(uri)
	if cleared.Version == nil || *cleared.Version != 2 {
		t.Fatalf("version = %v", cleared.Version)
	}
	if len(cleared.Diagnostics) != 0 {
		t.Fatalf("expected no diagnostics, got %#v", cleared.Diagnostics)
	}

	h.close(uri)
	closed := h.expectDiagnostics(uri)
	if closed.Version != nil {
		t.Fatalf("didClose diagnostics version = %v", closed.Version)
	}
	if len(closed.Diagnostics) != 0 {
		t.Fatalf("didClose diagnostics = %#v", closed.Diagnostics)
	}

	h.shutdown()
	h.exit()
}

func TestServerIgnoresStaleDocumentVersion(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	uri := uriFromPath(filepath.Join(t.TempDir(), "stale.mio"))
	h.open(uri, validSource, 5)
	if published := h.expectDiagnostics(uri); len(published.Diagnostics) != 0 {
		t.Fatalf("valid source diagnostics = %#v", published.Diagnostics)
	}

	// 版本 5 的变更必须被丢弃；随后 didSave 触发的分析应仍基于合法源码。
	h.change(uri, 5, brokenSource)
	h.save(uri)
	published := h.expectDiagnostics(uri)
	if len(published.Diagnostics) != 0 {
		t.Fatalf("stale change was applied: %#v", published.Diagnostics)
	}
}

func TestServerIncrementalChange(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	uri := uriFromPath(filepath.Join(t.TempDir(), "incremental.mio"))
	h.open(uri, validSource, 1)
	h.expectDiagnostics(uri)

	// 把 "kaesu a + b;" 中的 a 替换成 a + missing_value。
	h.notify("textDocument/didChange", map[string]any{
		"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{
			{
				"range": map[string]any{
					"start": map[string]any{"line": 1, "character": 10},
					"end":   map[string]any{"line": 1, "character": 11},
				},
				"text": "a + missing_value",
			},
		},
	})
	published := h.expectDiagnostics(uri)
	if published.Version == nil || *published.Version != 2 {
		t.Fatalf("version = %v", published.Version)
	}
	if len(published.Diagnostics) != 1 || published.Diagnostics[0].Code != "E0201" {
		t.Fatalf("diagnostics = %#v", published.Diagnostics)
	}
	if published.Diagnostics[0].Range.Start.Line != 1 {
		t.Fatalf("range = %#v", published.Diagnostics[0].Range)
	}

	h.shutdown()
	h.exit()
}

func TestServerLifecycleErrors(t *testing.T) {
	h := newHarness(t, Options{})
	before := h.request(1, "shutdown", nil)
	if before.Error == nil || before.Error.Code != codeServerNotInitialized {
		t.Fatalf("shutdown before initialize = %#v", before.Error)
	}

	h.initialize()
	again := h.request(2, "initialize", map[string]any{"capabilities": map[string]any{}})
	if again.Error == nil || again.Error.Code != codeInvalidRequest {
		t.Fatalf("second initialize = %#v", again.Error)
	}

	unknown := h.request(3, "textDocument/notImplemented", map[string]any{})
	if unknown.Error == nil || unknown.Error.Code != codeMethodNotFound {
		t.Fatalf("unknown method = %#v", unknown.Error)
	}

	h.shutdown()
	after := h.request(4, "textDocument/notImplemented", map[string]any{})
	if after.Error == nil || after.Error.Code != codeInvalidRequest {
		t.Fatalf("request after shutdown = %#v", after.Error)
	}
	h.exit()
}

func TestServerExitWithoutShutdown(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()
	h.notify("exit", nil)
	if err := h.wait(3 * time.Second); !errors.Is(err, ErrExitWithoutShutdown) {
		t.Fatalf("serve = %v, want ErrExitWithoutShutdown", err)
	}
}

func TestServerShutdownClearsDiagnostics(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()
	uri := uriFromPath(filepath.Join(t.TempDir(), "broken.mio"))
	h.open(uri, brokenSource, 1)
	h.expectDiagnostics(uri)

	h.send(map[string]any{"jsonrpc": "2.0", "id": 10, "method": "shutdown", "params": nil})
	deadline := time.Now().Add(5 * time.Second)
	cleared := false
	for time.Now().Before(deadline) {
		msg := h.next(time.Until(deadline))
		if msg.Method == "textDocument/publishDiagnostics" {
			var params PublishDiagnosticsParams
			if err := json.Unmarshal(msg.Params, &params); err != nil {
				t.Fatal(err)
			}
			if params.URI == uri && len(params.Diagnostics) == 0 {
				cleared = true
			}
			continue
		}
		if string(msg.ID) == "10" {
			break
		}
	}
	if !cleared {
		t.Fatal("shutdown did not clear diagnostics")
	}
	h.exit()
}

func TestServerConfigurationToggle(t *testing.T) {
	h := newHarness(t, Options{})
	h.initialize()

	uri := uriFromPath(filepath.Join(t.TempDir(), "config.mio"))
	h.open(uri, brokenSource, 1)
	if published := h.expectDiagnostics(uri); len(published.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", published.Diagnostics)
	}

	h.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{"mio": map[string]any{"diagnostics": map[string]any{"enabled": false}}},
	})
	if published := h.expectDiagnostics(uri); len(published.Diagnostics) != 0 {
		t.Fatalf("disabled diagnostics = %#v", published.Diagnostics)
	}

	h.notify("workspace/didChangeConfiguration", map[string]any{
		"settings": map[string]any{"mio": map[string]any{"diagnostics": map[string]any{"enabled": true}}},
	})
	if published := h.expectDiagnostics(uri); len(published.Diagnostics) != 1 {
		t.Fatalf("re-enabled diagnostics = %#v", published.Diagnostics)
	}

	h.shutdown()
	h.exit()
}

func TestServerRejectsInvalidJSON(t *testing.T) {
	h := newHarness(t, Options{})
	payload := []byte("not-json")
	if err := h.conn.write(payload); err != nil {
		t.Fatal(err)
	}
	msg := h.next(3 * time.Second)
	if msg.Error == nil || msg.Error.Code != codeParseError {
		t.Fatalf("parse error = %#v", msg.Error)
	}
}

func TestRequestCancellation(t *testing.T) {
	writer := &lockedBuffer{}
	instance, cancel := newServer(bytes.NewReader(nil), writer, nil, Options{DiagnosticDelay: 5 * time.Millisecond})
	defer cancel()
	defer instance.finish()
	instance.mu.Lock()
	instance.initialized = true
	instance.mu.Unlock()

	started := make(chan struct{})
	instance.requests["test/slow"] = func(ctx context.Context, params json.RawMessage) (any, *responseError) {
		close(started)
		<-ctx.Done()
		return nil, nil
	}

	go instance.dispatchRequest(message{
		JSONRPC: "2.0",
		ID:      json.RawMessage("7"),
		Method:  "test/slow",
	})
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not start")
	}
	instance.notificationCancel([]byte(`{"id":7}`))

	waitForErrorCode(t, writer, "7", codeRequestCancelled)
}

func TestRequestPreCancellation(t *testing.T) {
	writer := &lockedBuffer{}
	instance, cancel := newServer(bytes.NewReader(nil), writer, nil, Options{DiagnosticDelay: 5 * time.Millisecond})
	defer cancel()
	defer instance.finish()
	instance.mu.Lock()
	instance.initialized = true
	instance.mu.Unlock()

	instance.requestMu.Lock()
	instance.preCancelled[requestKey(json.RawMessage("9"))] = struct{}{}
	instance.requestMu.Unlock()
	instance.requests["test/immediate"] = func(ctx context.Context, params json.RawMessage) (any, *responseError) {
		return nil, nil
	}

	instance.dispatchRequest(message{
		JSONRPC: "2.0",
		ID:      json.RawMessage("9"),
		Method:  "test/immediate",
	})
	waitForErrorCode(t, writer, "9", codeRequestCancelled)
}

func waitForErrorCode(t *testing.T, writer *lockedBuffer, id string, code int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, msg := range messagesIn(t, writer.bytes()) {
			if msg.Error != nil && string(msg.ID) == id {
				if msg.Error.Code != code {
					t.Fatalf("error code = %d, want %d", msg.Error.Code, code)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no error response for request %s", id)
}

func messagesIn(t *testing.T, payloads []byte) []message {
	t.Helper()
	reader := newTransport(bytes.NewReader(payloads), io.Discard)
	var messages []message
	for {
		payload, err := reader.read()
		if err != nil {
			return messages
		}
		var msg message
		if err := json.Unmarshal(payload, &msg); err != nil {
			return messages
		}
		messages = append(messages, msg)
	}
}
