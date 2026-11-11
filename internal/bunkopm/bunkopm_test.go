package bunkopm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/BurntSushi/toml"
)

type testIndex map[string]Record

func (i testIndex) Record(n, v string) (Record, error) {
	r, ok := i[n+"@"+v]
	if !ok {
		return r, fail("BUNKO_E_MISSING_VERSION", "%s@%s", n, v)
	}
	return r, nil
}
func (i testIndex) Names() []string          { return nil }
func (i testIndex) Versions(string) []string { return nil }
func fixture(n, v string) Record {
	r := Record{Name: n, Version: v, Publisher: "github:" + strings.Split(n, "/")[0], Description: "Fixture", Repository: "https://github.com/" + strings.Split(n, "/")[0] + "/source", SourceCommit: strings.Repeat("a", 40), ReleaseID: "123", SHA256: strings.Repeat("0", 64), License: "MIT", Dependencies: map[string]string{}, Platforms: map[string][]string{"linux": {"x86_64", "arm64"}, "macos": {"x86_64", "arm64"}, "windows": {"x86_64", "arm64"}}}
	r.Mio.Min = "2.2.0"
	r.Mio.Before = "2.3.0"
	r.Archive = strings.Replace(r.Repository, "https://github.com/", api+"/repos/", 1) + "/releases/assets/456"
	return r
}
func recordBytes(r Record) []byte {
	var b bytes.Buffer
	if e := toml.NewEncoder(&b).Encode(r); e != nil {
		panic(e)
	}
	return b.Bytes()
}
func assertCode(t *testing.T, e error, code string) {
	t.Helper()
	if e == nil || !strings.Contains(e.Error(), code) {
		t.Fatalf("want %s, got %v", code, e)
	}
}
func archiveWith(t *testing.T, headers []*tar.Header, payloads [][]byte) []byte {
	t.Helper()
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	tw := tar.NewWriter(gz)
	for i, h := range headers {
		if e := tw.WriteHeader(h); e != nil {
			t.Fatal(e)
		}
		if _, e := tw.Write(payloads[i]); e != nil {
			t.Fatal(e)
		}
	}
	if e := tw.Close(); e != nil {
		t.Fatal(e)
	}
	if e := gz.Close(); e != nil {
		t.Fatal(e)
	}
	return b.Bytes()
}
func goodArchive(t *testing.T) []byte {
	b := []byte("koukai waza answer() -> int64 { kaesu 42; }\n")
	return archiveWith(t, []*tar.Header{{Name: "json.mio", Mode: 0644, Size: int64(len(b)), Typeflag: tar.TypeReg}}, [][]byte{b})
}

func TestNamesVersionsAndRecord(t *testing.T) {
	for _, s := range []string{"alice/json@1.0.0", "a-b/a1@999999999.0.1"} {
		if _, _, e := ParseSpec(s); e != nil {
			t.Fatal(e)
		}
	}
	for _, s := range []string{"alice/json", "alice/json@latest", "alice/json@^1.0.0", "alice/json@~1.0.0", "../json@1.0.0", "Alice/json@1.0.0", "alice/a--b@1.0.0", "alice/json@01.0.0", "alice/json@1.0.0-beta", "alice/json@1.0.0\n", "alice/json@1.0.0@2.0.0"} {
		if _, _, e := ParseSpec(s); e == nil {
			t.Errorf("accepted %q", s)
		}
	}
	r := fixture("alice/json", "1.0.0")
	if _, e := ParseRecord(recordBytes(r), r.Name, r.Version); e != nil {
		t.Fatal(e)
	}
	for _, edit := range []func(*Record){func(r *Record) { r.Archive = "http://example.com/a.tar.gz" }, func(r *Record) { r.Publisher = "github:bob" }, func(r *Record) { r.Platforms = map[string][]string{"darwin": {"amd64"}} }, func(r *Record) { r.Mio.Before = r.Mio.Min }, func(r *Record) { r.Dependencies["kiso/fs"] = "1.0.0" }} {
		r := fixture("alice/json", "1.0.0")
		edit(&r)
		if _, e := ParseRecord(recordBytes(r), r.Name, r.Version); e == nil {
			t.Fatal("accepted invalid metadata")
		}
	}
	if _, e := ParseRecord(append(recordBytes(r), []byte("\n[unknown]\nx=1\n")...), r.Name, r.Version); e == nil {
		t.Fatal("unknown fields")
	}
}
func TestManifestTransactions(t *testing.T) {
	dir := t.TempDir()
	if e := Init(dir); e != nil {
		t.Fatal(e)
	}
	m, e := ReadManifest(dir)
	if e != nil {
		t.Fatal(e)
	}
	m.Dependencies["alice/json"] = "1.0.0"
	if e = writeManifest(dir, m, true); e != nil {
		t.Fatal(e)
	}
	before, _ := os.ReadFile(filepath.Join(dir, ManifestName))
	if e = Init(dir); e != nil {
		t.Fatal(e)
	}
	after, _ := os.ReadFile(filepath.Join(dir, ManifestName))
	if !bytes.Equal(before, after) {
		t.Fatal("init overwrote declarations")
	}
	c := Client{Index: testIndex{}, Compiler: "2.2.0", Target: "linux/x86_64", Cache: t.TempDir()}
	if e = c.Add(dir, "alice/absent@1.0.0"); e == nil {
		t.Fatal("missing package accepted")
	}
	after, _ = os.ReadFile(filepath.Join(dir, ManifestName))
	if !bytes.Equal(before, after) {
		t.Fatal("failed add changed manifest")
	}
	if e = Remove(dir, "alice/json"); e != nil {
		t.Fatal(e)
	}
	m, e = ReadManifest(dir)
	if e != nil || len(m.Dependencies) != 0 {
		t.Fatal(m, e)
	}
	for _, text := range []string{"[dependencies]\n\"alice/json\"=\"latest\"", "[dependencies]\n\"alice/json\"=\"1.0.0\"\n\"alice/json\"=\"2.0.0\"", "[other]\na=1"} {
		if _, e := ParseManifest([]byte(text)); e == nil {
			t.Fatalf("accepted %s", text)
		}
	}
}
func TestResolution(t *testing.T) {
	a := fixture("alice/root", "1.0.0")
	b := fixture("bob/leaf", "1.0.0")
	a.Dependencies[b.Name] = b.Version
	i := testIndex{a.Name + "@" + a.Version: a, b.Name + "@" + b.Version: b}
	c := Client{Index: i, Compiler: "2.2.0", Target: "linux/x86_64"}
	roots := map[string]string{a.Name: a.Version}
	rs, e := c.Resolve(roots)
	if e != nil || len(rs) != 2 || rs[0].Name != b.Name {
		t.Fatal(rs, e)
	}
	for _, target := range []string{"linux/x86_64", "linux/arm64", "macos/x86_64", "macos/arm64", "windows/x86_64", "windows/arm64"} {
		c.Target = target
		if _, e = c.Resolve(roots); e != nil {
			t.Fatal(e)
		}
	}
	c.Target = "linux/x86_64"
	b.Platforms = map[string][]string{"macos": {"arm64"}}
	i[b.Name+"@"+b.Version] = b
	_, e = c.Resolve(roots)
	assertCode(t, e, "BUNKO_E_PLATFORM")
	b = fixture(b.Name, b.Version)
	b.Mio.Min = "2.3.0"
	b.Mio.Before = "3.0.0"
	i[b.Name+"@"+b.Version] = b
	_, e = c.Resolve(roots)
	assertCode(t, e, "BUNKO_E_COMPATIBILITY")
	b = fixture(b.Name, b.Version)
	b.Dependencies[a.Name] = a.Version
	i[b.Name+"@"+b.Version] = b
	_, e = c.Resolve(roots)
	assertCode(t, e, "BUNKO_E_CYCLE")
	b = fixture(b.Name, b.Version)
	i[b.Name+"@"+b.Version] = b
	d := fixture("carol/root", "1.0.0")
	d.Dependencies[b.Name] = "2.0.0"
	i[d.Name+"@"+d.Version] = d
	roots[d.Name] = d.Version
	_, e = c.Resolve(roots)
	assertCode(t, e, "BUNKO_E_VERSION_CONFLICT")
	if !strings.Contains(e.Error(), a.Name) || !strings.Contains(e.Error(), d.Name) {
		t.Fatal("missing conflict paths", e)
	}
	p := Policy{Revoked: map[string]string{b.Name + "@" + b.Version: "https://example.com/advisory"}}
	assertCode(t, p.Check(b), "BUNKO_E_REVOKED")
	for _, v := range []string{"2.1.9", "2.3.0", "3.0.0"} {
		if e = a.Compatible(v, "linux/x86_64"); e == nil {
			t.Fatal("accepted incompatible compiler", v)
		}
	}
}
func TestArchiveSecurity(t *testing.T) {
	if _, e := ReadArchive(goodArchive(t)); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"../escape.mio", "/absolute.mio", "C:/escape.mio", "a\\escape.mio", "a//x.mio", "con.mio", "a./x.mio", "a:bad.mio", "./x.mio", "a/../../x.mio"} {
		t.Run(name, func(t *testing.T) {
			b := archiveWith(t, []*tar.Header{{Name: name, Mode: 0644, Size: 1, Typeflag: tar.TypeReg}}, [][]byte{[]byte("x")})
			if _, e := ReadArchive(b); e == nil {
				t.Fatal("accepted unsafe path")
			}
		})
	}
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeChar, tar.TypeBlock, tar.TypeFifo} {
		b := archiveWith(t, []*tar.Header{{Name: "x.mio", Mode: 0644, Typeflag: kind, Linkname: "outside"}}, [][]byte{nil})
		if _, e := ReadArchive(b); e == nil {
			t.Fatal("accepted link/special entry")
		}
	}
	for _, tc := range []struct {
		name string
		mode int64
		data []byte
	}{{"script.mio", 0644, []byte("#!/bin/sh")}, {"binary.mio", 0644, []byte{0}}, {"invalid.mio", 0644, []byte{255}}, {"exec.mio", 0755, []byte("x")}, {"hook.sh", 0644, []byte("echo bad")}} {
		b := archiveWith(t, []*tar.Header{{Name: tc.name, Mode: tc.mode, Size: int64(len(tc.data)), Typeflag: tar.TypeReg}}, [][]byte{tc.data})
		if _, e := ReadArchive(b); e == nil {
			t.Fatal("accepted unsafe content", tc.name)
		}
	}
	for _, names := range [][]string{{"A.mio", "a.mio"}, {"a.mio", "a.mio/x.mio"}} {
		var hs []*tar.Header
		for _, n := range names {
			hs = append(hs, &tar.Header{Name: n, Mode: 0644, Typeflag: tar.TypeReg})
		}
		if _, e := ReadArchive(archiveWith(t, hs, [][]byte{nil, nil})); e == nil {
			t.Fatal("accepted collision")
		}
	}
	b := goodArchive(t)
	b[len(b)-5] ^= 1
	for _, b := range [][]byte{[]byte("bad"), b, goodArchive(t)[:20]} {
		if _, e := ReadArchive(b); e == nil {
			t.Fatal("accepted corrupt gzip")
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func testFetcher(t *testing.T, r Record, archive []byte, broken bool) *Fetcher {
	t.Helper()
	f := NewFetcher()
	f.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		var data []byte
		switch {
		case req.URL.String() == r.Archive:
			data = archive
			if broken {
				data = []byte("interrupted")
			}
		case strings.Contains(req.URL.Path, "/releases/123"):
			data = []byte(fmt.Sprintf(`{"id":123,"draft":false,"immutable":true,"tag_name":"v1.0.0","assets":[{"id":456,"url":%q,"name":"source.tar.gz","state":"uploaded","size":%d,"digest":%q}]}`, r.Archive, len(archive), "sha256:"+r.SHA256))
		case strings.Contains(req.URL.Path, "/git/ref/tags/"):
			data = []byte(fmt.Sprintf(`{"object":{"type":"commit","sha":%q}}`, r.SourceCommit))
		default:
			t.Errorf("unexpected request %s", req.URL)
			return nil, fmt.Errorf("unexpected URL")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(data)), Header: make(http.Header), Request: req}, nil
	})
	return f
}
func TestInstallCacheAndFailure(t *testing.T) {
	r := fixture("alice/json", "1.0.0")
	archive := goodArchive(t)
	r.SHA256 = archiveDigest(archive)
	cache := t.TempDir()
	c := Client{Index: testIndex{r.Name + "@" + r.Version: r}, Cache: cache, Compiler: "2.2.0", Target: "linux/x86_64", Fetch: testFetcher(t, r, archive, false)}
	dir := t.TempDir()
	if e := Init(dir); e != nil {
		t.Fatal(e)
	}
	policy, _ := json.Marshal(Policy{})
	if e := atomicFile(cache, "policy.json", policy); e != nil {
		t.Fatal(e)
	}
	if e := c.Add(dir, r.Name+"@"+r.Version); e != nil {
		t.Fatal(e)
	}
	if e := c.Install(dir); e != nil {
		t.Fatal(e)
	}
	c.Fetch = nil
	if e := c.Install(dir); e != nil {
		t.Fatal("cache reuse required network", e)
	}
	src, e := VerifyCached(cache, r)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(src, "json.mio"), []byte("tampered"), 0600); e != nil {
		t.Fatal(e)
	}
	_, e = VerifyCached(cache, r)
	assertCode(t, e, "BUNKO_E_CACHE")
	if e = Remove(dir, r.Name); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(src); e != nil {
		t.Fatal("remove deleted shared cache")
	}
	for _, kind := range []string{"interrupted", "checksum", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			rr := r
			data := archive
			if kind == "checksum" {
				rr.SHA256 = strings.Repeat("f", 64)
			}
			if kind == "corrupt" {
				data = []byte("corrupt")
				rr.SHA256 = archiveDigest(data)
			}
			c := Client{Cache: t.TempDir(), Fetch: testFetcher(t, rr, data, kind == "interrupted")}
			if e := c.install(rr); e == nil {
				t.Fatal("invalid installation succeeded")
			}
			p, _ := entryPath(c.Cache, rr.Name, rr.Version)
			if _, e = os.Stat(p); !os.IsNotExist(e) {
				t.Fatal("partial entry published")
			}
			entries, _ := os.ReadDir(filepath.Dir(p))
			if len(entries) != 0 {
				t.Fatal("staging files leaked")
			}
		})
	}
}
func TestConcurrentInstall(t *testing.T) {
	r := fixture("alice/json", "1.0.0")
	a := goodArchive(t)
	r.SHA256 = archiveDigest(a)
	c := Client{Cache: t.TempDir(), Fetch: testFetcher(t, r, a, false)}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for j := 0; j < 8; j++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- c.install(r) }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if _, e := VerifyCached(c.Cache, r); e != nil {
		t.Fatal(e)
	}
}
func TestPolicyAndHTTPS(t *testing.T) {
	r := fixture("mio/json", "1.0.0")
	p := Policy{}
	r.Publisher = "github:attacker"
	r.Repository = "https://github.com/attacker/source"
	p.Namespaces = map[string]struct {
		Publishers []string `toml:"publishers"`
		Submitters []int64  `toml:"submitter_ids"`
	}{"mio": {Publishers: []string{"github:mio-kobo"}}}
	assertCode(t, p.Check(r), "BUNKO_E_IDENTITY")
	f := NewFetcher()
	for _, s := range []string{"http://api.github.com/a", "https://evil.test/a", "https://github.com@evil.test/a", "https://api.github.com:444/a"} {
		if _, e := f.get(s, "text/plain", 100); e == nil {
			t.Fatal("accepted insecure URL", s)
		}
	}
}

func TestOfficialIndexSnapshotAndPolicy(t *testing.T) {
	r := fixture("alice/json", "1.0.0")
	r.Platforms = map[string][]string{"linux": {"x86_64"}}
	revision := strings.Repeat("b", 40)
	f := NewFetcher()
	var paths []string
	f.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.String())
		var data string
		switch req.URL.String() {
		case api + "/repos/mio-kobo/bunko-index/git/trees/main?recursive=1":
			data = fmt.Sprintf(`{"sha":%q,"truncated":false,"tree":[{"path":"packages/alice/json/1.0.0.toml","type":"blob","mode":"100644"}]}`, revision)
		case raw + revision + "/policies/reserved-names.toml":
			data = "[namespaces.mio]\npublishers=[\"github:mio-kobo\"]\nsubmitter_ids=[72144156]\n[namespaces.kiso]\npublishers=[\"github:mio-kobo\"]\nsubmitter_ids=[72144156]\n[namespaces.bunko]\npublishers=[]\nsubmitter_ids=[]\n"
		case raw + revision + "/policies/revoked-versions.toml":
			data = "[revoked]\n"
		case raw + revision + "/policies/platform-targets.toml":
			data = "[targets.\"linux/x86_64\"]\nrunner=\"ubuntu-24.04\"\nmio_version=\"2.2.0\"\nmio_asset=\"fixture.tar.gz\"\nmio_sha256=\"" + strings.Repeat("0", 64) + "\"\nmio_binary=\"bin/mio\"\n"
		case raw + revision + "/packages/alice/json/1.0.0.toml":
			data = string(recordBytes(r))
		default:
			t.Fatalf("unexpected URL %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data)), Header: http.Header{}, Request: req}, nil
	})
	idx, e := OpenIndex(f)
	if e != nil {
		t.Fatal(e)
	}
	if fmt.Sprint(idx.Names()) != "[alice/json]" || fmt.Sprint(idx.Versions("alice/json")) != "[1.0.0]" {
		t.Fatal("index inventory")
	}
	if _, e = idx.Record(r.Name, r.Version); e != nil {
		t.Fatal(e)
	}
	if len(paths) != 5 {
		t.Fatal(paths)
	}
	r.Platforms["linux"] = append(r.Platforms["linux"], "arm64")
	assertCode(t, idx.Policy.Check(r), "BUNKO_E_PLATFORM")
	idx.Policy.Revoked[r.Name+"@"+r.Version] = "https://example.com/advisory"
	assertCode(t, idx.Policy.Check(r), "BUNKO_E_REVOKED")
	cache := t.TempDir()
	if e = idx.SavePolicy(cache); e != nil {
		t.Fatal(e)
	}
	cached, e := OpenCachedIndex(cache)
	if e != nil {
		t.Fatal(e)
	}
	assertCode(t, cached.Policy.Check(r), "BUNKO_E_REVOKED")
}
func TestArchiveLimitsAndWrapper(t *testing.T) {
	// Root-relative files are retained: no automatic wrapper stripping.
	a := archiveWith(t, []*tar.Header{{Name: "wrapper/source.mio", Mode: 0644, Size: 1}}, [][]byte{[]byte("x")})
	files, e := ReadArchive(a)
	if e != nil || files["wrapper/source.mio"] == nil {
		t.Fatal(files, e)
	}
	var headers []*tar.Header
	var payloads [][]byte
	for j := 0; j <= MaxFiles; j++ {
		headers = append(headers, &tar.Header{Name: fmt.Sprintf("f%d.mio", j), Mode: 0644})
		payloads = append(payloads, nil)
	}
	_, e = ReadArchive(archiveWith(t, headers, payloads))
	assertCode(t, e, "BUNKO_E_ARCHIVE")
	big := bytes.Repeat([]byte("x"), MaxFileBytes+1)
	_, e = ReadArchive(archiveWith(t, []*tar.Header{{Name: "big.mio", Mode: 0644, Size: int64(len(big))}}, [][]byte{big}))
	assertCode(t, e, "BUNKO_E_ARCHIVE")
	for _, n := range []string{strings.Repeat("a/", 12) + "x.mio", strings.Repeat("a", 201) + ".mio"} {
		if _, e := archivePath(n); e == nil {
			t.Fatal("accepted excessive path")
		}
	}
}
func TestReleaseProvenanceFailures(t *testing.T) {
	r := fixture("alice/json", "1.0.0")
	a := goodArchive(t)
	r.SHA256 = archiveDigest(a)
	for _, change := range []func(string) string{
		func(s string) string { return strings.Replace(s, `"immutable":true`, `"immutable":false`, 1) },
		func(s string) string { return strings.Replace(s, `"draft":false`, `"draft":true`, 1) },
		func(s string) string { return strings.Replace(s, `"id":456`, `"id":457`, 1) },
		func(s string) string { return strings.Replace(s, r.SourceCommit, strings.Repeat("b", 40), 1) },
		func(s string) string { return strings.Replace(s, `"state":"uploaded"`, `"state":"new"`, 1) },
	} {
		f := testFetcher(t, r, a, false)
		underlying := f.HTTP.Transport
		f.HTTP.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
			resp, e := underlying.RoundTrip(req)
			if e != nil {
				return nil, e
			}
			b, e := io.ReadAll(resp.Body)
			resp.Body.Close()
			if e != nil {
				return nil, e
			}
			resp.Body = io.NopCloser(strings.NewReader(change(string(b))))
			return resp, nil
		})
		if _, e := f.VerifyRelease(r); e == nil {
			t.Fatal("accepted invalid provenance")
		}
	}
}
func TestManifestBusyAndCacheMissing(t *testing.T) {
	dir := t.TempDir()
	if e := os.Mkdir(filepath.Join(dir, ".bunko-manifest-write"), 0700); e != nil {
		t.Fatal(e)
	}
	assertCode(t, Init(dir), "BUNKO_E_BUSY")
	if _, e := os.Stat(filepath.Join(dir, ManifestName)); !os.IsNotExist(e) {
		t.Fatal("busy init published manifest")
	}
	dir = t.TempDir()
	if e := Init(dir); e != nil {
		t.Fatal(e)
	}
	m, _ := ReadManifest(dir)
	m.Dependencies["alice/json"] = "1.0.0"
	if e := writeManifest(dir, m, true); e != nil {
		t.Fatal(e)
	}
	t.Setenv("MIO_BUNKO_CACHE", t.TempDir())
	_, e := OfflinePackages(dir, "2.2.0", "linux/x86_64")
	assertCode(t, e, "BUNKO_E_MISSING_INSTALL")
}

func TestArchiveTotalSizeLimit(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), MaxFileBytes)
	var headers []*tar.Header
	var payloads [][]byte
	for j := 0; j < 9; j++ {
		headers = append(headers, &tar.Header{Name: fmt.Sprintf("large%d.mio", j), Mode: 0644, Size: MaxFileBytes})
		payloads = append(payloads, payload)
	}
	_, err := ReadArchive(archiveWith(t, headers, payloads))
	assertCode(t, err, "BUNKO_E_ARCHIVE")
}
func TestDuplicatePlatformArrayKey(t *testing.T) {
	r := fixture("alice/json", "1.0.0")
	data := recordBytes(r)
	// TOML duplicate array keys must not silently replace the original platform declaration.
	data = append(data, []byte("linux = [\"x86_64\"]\n")...)
	if _, err := ParseRecord(data, r.Name, r.Version); err == nil {
		t.Fatal("duplicate platform declaration accepted")
	}
}
