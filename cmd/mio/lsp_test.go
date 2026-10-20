package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRunLSPHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := runLSP([]string{"--help"}, strings.NewReader(""), &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %s", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: mio lsp") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}

func TestRunCLILSPCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"lsp", "--help"}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit = %d, stderr = %s", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Usage: mio lsp [--stdio]") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	stdout.Reset()
	printUsage(&stdout)
	if !strings.Contains(stdout.String(), "mio lsp [--stdio]") {
		t.Fatalf("CLI usage does not advertise LSP: %q", stdout.String())
	}
}

func TestRunLSPRejectsUnknownArgument(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := runLSP([]string{"--tcp"}, strings.NewReader(""), &stdout, &stderr); exit != 2 {
		t.Fatalf("exit = %d", exit)
	}
	if !strings.Contains(stderr.String(), "unknown argument") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunLSPExitsNonZeroWithoutShutdown(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if exit := runLSP([]string{"--stdio"}, strings.NewReader(""), &stdout, &stderr); exit != 1 {
		t.Fatalf("exit = %d", exit)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout must stay clean, got %q", stdout.String())
	}
}

func TestRunLSPServesInitializeAndShutdown(t *testing.T) {
	serverIn, clientIn := io.Pipe()
	clientOut, serverOut := io.Pipe()
	defer clientIn.Close()
	defer clientOut.Close()

	done := make(chan int, 1)
	go func() {
		var stderr bytes.Buffer
		code := runLSP([]string{"--stdio"}, serverIn, serverOut, &stderr)
		serverOut.Close()
		done <- code
	}()

	writer := bufio.NewWriter(clientIn)
	reader := bufio.NewReader(clientOut)

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{},"initializationOptions":{"diagnosticsDelayMs":5}}}`
	if err := writeFrame(writer, initialize); err != nil {
		t.Fatal(err)
	}
	response := readFrame(t, reader)
	var initMessage struct {
		Result struct {
			Capabilities struct {
				TextDocumentSync struct {
					Change int `json:"change"`
				} `json:"textDocumentSync"`
			} `json:"capabilities"`
			ServerInfo struct {
				Name string `json:"name"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(response, &initMessage); err != nil {
		t.Fatal(err)
	}
	if initMessage.Result.ServerInfo.Name != "MioCore" {
		t.Fatalf("server name = %q", initMessage.Result.ServerInfo.Name)
	}
	if initMessage.Result.Capabilities.TextDocumentSync.Change != 2 {
		t.Fatalf("sync change = %d", initMessage.Result.Capabilities.TextDocumentSync.Change)
	}

	if err := writeFrame(writer, `{"jsonrpc":"2.0","id":2,"method":"shutdown","params":null}`); err != nil {
		t.Fatal(err)
	}
	_ = readFrame(t, reader)
	if err := writeFrame(writer, `{"jsonrpc":"2.0","method":"exit","params":null}`); err != nil {
		t.Fatal(err)
	}

	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("exit = %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mio lsp did not exit")
	}
}

func writeFrame(writer *bufio.Writer, payload string) error {
	if _, err := fmt.Fprintf(writer, "Content-Length: %d\r\n\r\n%s", len(payload), payload); err != nil {
		return err
	}
	return writer.Flush()
}

func readFrame(t *testing.T, reader *bufio.Reader) []byte {
	t.Helper()
	length := -1
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read header: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok || !strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			continue
		}
		length, err = strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			t.Fatalf("content length: %v", err)
		}
	}
	if length < 0 {
		t.Fatal("missing content length")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	return payload
}
