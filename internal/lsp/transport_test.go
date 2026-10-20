package lsp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

func frameFor(payload string) string {
	return fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
}

func TestTransportReadWriteRoundTrip(t *testing.T) {
	var buffer bytes.Buffer
	writeTransport := newTransport(strings.NewReader(""), &buffer)
	payloads := []string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":"file:///tmp/a.mio","languageId":"mio","version":1,"text":"kaku x = 1;\n"}}}`,
	}
	for _, payload := range payloads {
		if err := writeTransport.write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	readTransport := newTransport(&buffer, io.Discard)
	for _, want := range payloads {
		got, err := readTransport.read()
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != want {
			t.Fatalf("payload = %q, want %q", got, want)
		}
	}
	if _, err := readTransport.read(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected EOF, got %v", err)
	}
}

func TestTransportIgnoresUnknownHeaders(t *testing.T) {
	payload := `{"jsonrpc":"2.0"}`
	input := "Content-Type: application/vscode-jsonrpc; charset=utf-8\r\n" + frameFor(payload)
	transport := newTransport(strings.NewReader(input), io.Discard)
	got, err := transport.read()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != payload {
		t.Fatalf("payload = %q", got)
	}
}

func TestTransportRejectsMissingContentLength(t *testing.T) {
	transport := newTransport(strings.NewReader("Content-Type: application/json\r\n\r\n{}"), io.Discard)
	if _, err := transport.read(); !errors.Is(err, errMissingContentLength) {
		t.Fatalf("expected missing Content-Length, got %v", err)
	}
}

func TestTransportRejectsInvalidContentLength(t *testing.T) {
	transport := newTransport(strings.NewReader("Content-Length: nan\r\n\r\n{}"), io.Discard)
	if _, err := transport.read(); !errors.Is(err, errMalformedHeader) {
		t.Fatalf("expected malformed header, got %v", err)
	}
}

func TestTransportRejectsOversizedMessage(t *testing.T) {
	transport := newTransport(strings.NewReader(frameFor("{}")), io.Discard)
	transport.maxMessageSize = 1
	if _, err := transport.read(); !errors.Is(err, errMessageTooLarge) {
		t.Fatalf("expected message too large, got %v", err)
	}
}

func TestTransportSerializesConcurrentWrites(t *testing.T) {
	var buffer bytes.Buffer
	transport := newTransport(strings.NewReader(""), &buffer)
	const writers = 32
	var wait sync.WaitGroup
	for index := 0; index < writers; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			payload, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "test", "params": map[string]int{"index": index}})
			if err != nil {
				t.Error(err)
				return
			}
			if err := transport.write(payload); err != nil {
				t.Error(err)
			}
		}()
	}
	wait.Wait()

	reader := newTransport(&buffer, io.Discard)
	seen := make(map[int]bool)
	for index := 0; index < writers; index++ {
		payload, err := reader.read()
		if err != nil {
			t.Fatal(err)
		}
		var msg message
		if err := json.Unmarshal(payload, &msg); err != nil {
			t.Fatal(err)
		}
		var params map[string]int
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			t.Fatal(err)
		}
		seen[params["index"]] = true
	}
	if len(seen) != writers {
		t.Fatalf("received %d unique frames, want %d", len(seen), writers)
	}
}

func TestMessageClassification(t *testing.T) {
	request := message{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "shutdown"}
	if !request.isRequest() || request.isNotification() {
		t.Fatal("request should not be a notification")
	}
	notification := message{JSONRPC: "2.0", Method: "exit"}
	if !notification.isNotification() || notification.isRequest() {
		t.Fatal("notification should not be a request")
	}
	nullID := message{JSONRPC: "2.0", ID: json.RawMessage("null"), Method: "exit"}
	if !nullID.isNotification() {
		t.Fatal("null id should be treated as notification")
	}
}
