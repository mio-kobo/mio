package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"Mio/internal/bunkopm"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type fixtureIndex struct{ record bunkopm.Record }

func (i fixtureIndex) Record(n, v string) (bunkopm.Record, error) {
	if n == i.record.Name && v == i.record.Version {
		return i.record, nil
	}
	return bunkopm.Record{}, fmt.Errorf("missing fixture %s@%s", n, v)
}
func (i fixtureIndex) Names() []string          { return []string{i.record.Name} }
func (i fixtureIndex) Versions(string) []string { return []string{i.record.Version} }
func TestBunkoInstalledImportExecution(t *testing.T) {
	dir := t.TempDir()
	root, err := filepath.Abs("../../bunko")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIO_BUNKO", root)
	cache := t.TempDir()
	t.Setenv("MIO_BUNKO_CACHE", cache)
	code := []byte("koukai waza answer() -> int64 { kaesu 42; }\n")
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)
	if e := tw.WriteHeader(&tar.Header{Name: "json.mio", Mode: 0644, Size: int64(len(code))}); e != nil {
		t.Fatal(e)
	}
	tw.Write(code)
	tw.Close()
	gz.Close()
	digest := sha256.Sum256(archive.Bytes())
	r := bunkopm.Record{Name: "alice/json", Version: "1.0.0", Publisher: "github:alice", Description: "Mio 2.2.0 compatible fixture", Repository: "https://github.com/alice/source", SourceCommit: strings.Repeat("a", 40), ReleaseID: "123", Archive: "https://api.github.com/repos/alice/source/releases/assets/456", SHA256: hex.EncodeToString(digest[:]), License: "MIT", Dependencies: map[string]string{}, Platforms: map[string][]string{"linux": {"x86_64", "arm64"}, "macos": {"x86_64", "arm64"}, "windows": {"x86_64", "arm64"}}}
	r.Mio.Min = "2.1.0"
	r.Mio.Before = "2.3.0"
	fetch := bunkopm.NewFetcher()
	requests := 0
	fetch.HTTP.Transport = fixtureTransport(func(req *http.Request) (*http.Response, error) {
		requests++
		data := ""
		switch {
		case req.URL.String() == r.Archive:
			data = archive.String()
		case strings.Contains(req.URL.Path, "/releases/123"):
			data = fmt.Sprintf(`{"id":123,"draft":false,"immutable":true,"tag_name":"v1.0.0","assets":[{"id":456,"url":%q,"name":"source.tar.gz","state":"uploaded","size":%d}]}`, r.Archive, archive.Len())
		case strings.Contains(req.URL.Path, "/git/ref/tags/"):
			data = fmt.Sprintf(`{"object":{"sha":%q,"type":"commit"}}`, r.SourceCommit)
		default:
			t.Fatalf("unexpected URL: %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data)), Header: http.Header{}, Request: req}, nil
	})
	c := bunkopm.Client{Index: fixtureIndex{r}, Cache: cache, Compiler: "2.2.0", Target: bunkopm.Target(runtime.GOOS, runtime.GOARCH), Fetch: fetch}
	if e := bunkopm.Init(dir); e != nil {
		t.Fatal(e)
	}
	if e := c.Add(dir, "alice/json@1.0.0"); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(cache, "policy.json"), []byte(`{"Revoked":{},"Namespaces":{}}`), 0600); e != nil {
		t.Fatal(e)
	}
	main := filepath.Join(dir, "main.mio")
	if e := os.WriteFile(main, []byte("yobu \"alice/json\";\nyobu \"kiso/math\";\nchant(json.answer());\nchant(math.abs(-2));\n"), 0600); e != nil {
		t.Fatal(e)
	}
	var out, stderr bytes.Buffer
	if status := runCLI([]string{"run", main}, &out, &stderr); status != 0 || out.String() != "42\n2\n" {
		t.Fatalf("exit %d: %s %s", status, &out, &stderr)
	}
	if requests != 3 {
		t.Fatalf("compilation fetched dependencies: %d requests", requests)
	}
	if e := os.RemoveAll(filepath.Join(cache, "alice")); e != nil {
		t.Fatal(e)
	}
	out.Reset()
	stderr.Reset()
	if status := runCLI([]string{"check", main}, &out, &stderr); status == 0 || !strings.Contains(stderr.String(), "mio bunko install") {
		t.Fatalf("missing dependency diagnostic: exit %d, %s", status, &stderr)
	}
	if requests != 3 {
		t.Fatal("missing package triggered download")
	}
}
func TestBunkoHelpAndLocalCommands(t *testing.T) {
	for _, args := range [][]string{{"bunko", "--help"}, {"bunko", "add", "--help"}, {"--lang=ja-JP", "bunko", "--help"}, {"help", "bunko"}} {
		var out, err bytes.Buffer
		if status := runCLI(args, &out, &err); status != 0 || !strings.Contains(out.String(), "mio bunko") {
			t.Fatalf("%v: %d %s %s", args, status, &out, &err)
		}
	}
	cwd, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	defer os.Chdir(cwd)
	dir := t.TempDir()
	if e = os.Chdir(dir); e != nil {
		t.Fatal(e)
	}
	for _, args := range [][]string{{"bunko", "init"}, {"bunko", "init"}, {"bunko", "list"}, {"bunko", "remove", "alice/json"}} {
		var out, err bytes.Buffer
		if status := runCLI(args, &out, &err); status != 0 {
			t.Fatalf("%v: %d %s", args, status, &err)
		}
	}
	var out, stderr bytes.Buffer
	if status := runCLI([]string{"bunko", "add", "alice/json@latest"}, &out, &stderr); status == 0 || !strings.Contains(stderr.String(), "BUNKO_E_SPEC") {
		t.Fatal(status, &stderr)
	}
}

func TestBunkoUndeclaredImports(t *testing.T) {
	dir := t.TempDir()
	cache := t.TempDir()
	t.Setenv("MIO_BUNKO_CACHE", cache)
	stdlib, err := filepath.Abs("../../bunko")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIO_BUNKO", stdlib)
	if err = bunkopm.Init(dir); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mio")
	if err = os.WriteFile(main, []byte("yobu \"alice/undeclared\";\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if status := runCLI([]string{"check", main}, &out, &stderr); status == 0 || !strings.Contains(stderr.String(), "BUNKO_E_UNDECLARED") {
		t.Fatalf("exit %d: %s", status, &stderr)
	}
	// An empty manifest must not interfere with ordinary standard-library imports.
	if err = os.WriteFile(main, []byte("yobu \"kiso/math\"; chant(math.abs(-3));"), 0600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	stderr.Reset()
	if status := runCLI([]string{"run", main}, &out, &stderr); status != 0 || out.String() != "3\n" {
		t.Fatal(status, &out, &stderr)
	}
}

func TestBunkoOnlineCommandFixtures(t *testing.T) {
	dir := t.TempDir()
	cwd, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	defer os.Chdir(cwd)
	if e = os.Chdir(dir); e != nil {
		t.Fatal(e)
	}
	t.Setenv("MIO_BUNKO_CACHE", t.TempDir())
	revision := strings.Repeat("b", 40)
	record := `name="alice/json"
version="1.0.0"
publisher="github:alice"
description="JSON source fixture"
repository="https://github.com/alice/source"
source_commit="` + strings.Repeat("a", 40) + `"
release_id="123"
archive="https://api.github.com/repos/alice/source/releases/assets/456"
sha256="` + strings.Repeat("0", 64) + `"
license="MIT"
[mio]
min="2.2.0"
before="2.3.0"
[dependencies]
[platforms]
linux=["x86_64"]
`
	fetch := bunkopm.NewFetcher()
	fetch.HTTP.Transport = fixtureTransport(func(req *http.Request) (*http.Response, error) {
		body := ""
		switch {
		case strings.Contains(req.URL.Path, "/git/trees/main"):
			body = `{"sha":"` + revision + `","tree":[{"path":"packages/alice/json/1.0.0.toml","type":"blob","mode":"100644"}]}`
		case strings.HasSuffix(req.URL.Path, "reserved-names.toml"):
			body = "[namespaces.mio]\npublishers=[\"github:mio-kobo\"]\n[namespaces.kiso]\npublishers=[\"github:mio-kobo\"]\n[namespaces.bunko]\npublishers=[]\n"
		case strings.HasSuffix(req.URL.Path, "revoked-versions.toml"):
			body = "[revoked]\n"
		case strings.HasSuffix(req.URL.Path, "platform-targets.toml"):
			body = "[targets.\"linux/x86_64\"]\nrunner=\"ubuntu-24.04\"\nmio_version=\"2.2.0\"\nmio_asset=\"fixture.tar.gz\"\nmio_binary=\"bin/mio\"\nmio_sha256=\"" + strings.Repeat("0", 64) + "\"\n"
		case strings.HasSuffix(req.URL.Path, "1.0.0.toml"):
			body = record
		default:
			t.Fatalf("unexpected URL %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: req}, nil
	})
	for _, args := range [][]string{{"info", "alice/json"}, {"search", "json"}, {"search", "JSON"}} {
		var out, stderr bytes.Buffer
		if status := runBunkoWithFetcher(args, &out, &stderr, "en-US", fetch); status != 0 || !strings.Contains(out.String(), "alice/json") {
			t.Fatalf("%v exit %d: %s %s", args, status, &out, &stderr)
		}
	}
}
