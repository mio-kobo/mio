package lsp

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestPathFromURI(t *testing.T) {
	path, ok := pathFromURI("file:///tmp/a%20b.mio")
	if !ok || path != "/tmp/a b.mio" {
		t.Fatalf("path = %q, ok = %t", path, ok)
	}
	if _, ok := pathFromURI("untitled:Untitled-1"); ok {
		t.Fatal("untitled URI must not map to a path")
	}
	if _, ok := pathFromURI("https://example.com/a.mio"); ok {
		t.Fatal("https URI must not map to a path")
	}
	if _, ok := pathFromURI("file://remote-host/tmp/a.mio"); ok {
		t.Fatal("remote host URI must not map to a local path")
	}
}

func TestURIRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "目录 with space", "a.mio")
	uri := uriFromPath(path)
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("uri = %q", uri)
	}
	got, ok := pathFromURI(uri)
	if !ok {
		t.Fatalf("cannot parse %q", uri)
	}
	if got != path {
		t.Fatalf("path = %q, want %q", got, path)
	}
}
