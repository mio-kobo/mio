package bunkopm

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const api = "https://api.github.com"
const raw = "https://raw.githubusercontent.com/mio-kobo/bunko-index/"
const MaxArchive = 32 << 20
const maxJSON = 4 << 20

// No environment-controlled index URL: this client trusts only the official
// index. Tests inject an HTTP transport, not a production trust bypass.
type Fetcher struct{ HTTP *http.Client }

func NewFetcher() *Fetcher {
	return &Fetcher{&http.Client{Timeout: 60 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return fmt.Errorf("too many redirects")
		}
		if !allowedURL(req.URL) {
			return fmt.Errorf("unsafe redirect")
		}
		req.Header.Del("Authorization")
		return nil
	}}}
}
func allowedURL(u *url.URL) bool {
	h := u.Hostname()
	return u.Scheme == "https" && u.User == nil && (u.Port() == "" || u.Port() == "443") && (h == "api.github.com" || h == "github.com" || h == "raw.githubusercontent.com" || strings.HasSuffix(h, ".githubusercontent.com"))
}
func (f *Fetcher) get(address, accept string, limit int64) ([]byte, error) {
	u, e := url.Parse(address)
	if e != nil || !allowedURL(u) {
		return nil, fail("BUNKO_E_NETWORK", "unsafe URL %q", address)
	}
	req, e := http.NewRequest("GET", address, nil)
	if e != nil {
		return nil, e
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "mio-bunko-v1")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, e := f.HTTP.Do(req)
	if e != nil {
		return nil, fail("BUNKO_E_NETWORK", "%v", e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fail("BUNKO_E_NETWORK", "HTTP %d fetching %s", resp.StatusCode, address)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if e != nil {
		return nil, fail("BUNKO_E_NETWORK", "%v", e)
	}
	if int64(len(b)) > limit {
		return nil, fail("BUNKO_E_LIMIT", "response too large")
	}
	return b, nil
}
func (f *Fetcher) json(address string, out any) error {
	b, e := f.get(address, "application/vnd.github+json", maxJSON)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(b, out); e != nil {
		return fail("BUNKO_E_NETWORK", "invalid JSON from %s", address)
	}
	return nil
}

type PlatformRule struct {
	Runner     string `toml:"runner"`
	MioVersion string `toml:"mio_version"`
	MioAsset   string `toml:"mio_asset"`
	MioSHA256  string `toml:"mio_sha256"`
	MioBinary  string `toml:"mio_binary"`
}
type Policy struct {
	Targets    map[string]PlatformRule `toml:"targets"`
	Revoked    map[string]string       `toml:"revoked"`
	Namespaces map[string]struct {
		Publishers []string `toml:"publishers"`
		Submitters []int64  `toml:"submitter_ids"`
	} `toml:"namespaces"`
}

func (p Policy) Check(r Record) error {
	if advisory, ok := p.Revoked[r.Name+"@"+r.Version]; ok {
		return fail("BUNKO_E_REVOKED", "%s@%s: %s", r.Name, r.Version, advisory)
	}
	ns := strings.Split(r.Name, "/")[0]
	if rule, reserved := p.Namespaces[ns]; reserved {
		authorized := false
		for _, pub := range rule.Publishers {
			if pub == r.Publisher {
				authorized = true
			}
		}
		if !authorized {
			return fail("BUNKO_E_IDENTITY", "publisher not authorized for %s", ns)
		}
	} else if r.Publisher != "github:"+ns {
		return fail("BUNKO_E_IDENTITY", "publisher differs from namespace")
	}
	if p.Targets != nil {
		for os, archs := range r.Platforms {
			for _, arch := range archs {
				if _, ok := p.Targets[os+"/"+arch]; !ok {
					return fail("BUNKO_E_PLATFORM", "official index policy does not admit %s/%s", os, arch)
				}
			}
		}
	}
	return nil
}

type OfficialIndex struct {
	Revision string
	Policy   Policy
	fetch    *Fetcher
	paths    map[string]string
	records  map[string]Record
}

func OpenIndex(f *Fetcher) (*OfficialIndex, error) {
	var tree struct {
		SHA       string `json:"sha"`
		Truncated bool   `json:"truncated"`
		Tree      []struct{ Path, Mode, Type string }
	}
	if e := f.json(api+"/repos/mio-kobo/bunko-index/git/trees/main?recursive=1", &tree); e != nil {
		return nil, e
	}
	if !hex40.MatchString(tree.SHA) || tree.Truncated {
		return nil, fail("BUNKO_E_INDEX", "invalid or truncated index tree")
	}
	idx := &OfficialIndex{Revision: tree.SHA, fetch: f, paths: map[string]string{}, records: map[string]Record{}}
	for _, p := range []string{"policies/reserved-names.toml", "policies/revoked-versions.toml", "policies/platform-targets.toml"} {
		b, e := f.get(raw+tree.SHA+"/"+p, "text/plain", MaxRecord)
		if e != nil {
			return nil, e
		}
		if _, e = decode(b, &idx.Policy); e != nil {
			return nil, e
		}
	}
	if len(idx.Policy.Targets) == 0 {
		return nil, fail("BUNKO_E_INDEX", "empty platform policy")
	}
	for target, rule := range idx.Policy.Targets {
		os, arch, ok := strings.Cut(target, "/")
		if !ok || (os != "linux" && os != "macos" && os != "windows") || (arch != "x86_64" && arch != "arm64") || rule.Runner == "" || rule.MioAsset == "" || rule.MioBinary == "" || !ValidVersion(rule.MioVersion) || !hex64.MatchString(rule.MioSHA256) {
			return nil, fail("BUNKO_E_INDEX", "invalid platform policy")
		}
	}
	for _, ns := range []string{"kiso", "mio", "bunko"} {
		if _, ok := idx.Policy.Namespaces[ns]; !ok {
			return nil, fail("BUNKO_E_INDEX", "missing reserved namespace %s", ns)
		}
	}
	for label, advisory := range idx.Policy.Revoked {
		if _, _, e := ParseSpec(label); e != nil {
			return nil, e
		}
		u, e := url.Parse(advisory)
		if e != nil || u.Scheme != "https" || u.Host == "" {
			return nil, fail("BUNKO_E_INDEX", "invalid revocation advisory")
		}
	}
	for _, item := range tree.Tree {
		if !strings.HasPrefix(item.Path, "packages/") || item.Type == "tree" {
			continue
		}
		p := strings.Split(item.Path, "/")
		if len(p) != 4 || !strings.HasSuffix(p[3], ".toml") || item.Mode != "100644" {
			return nil, fail("BUNKO_E_INDEX", "invalid index record path %q", item.Path)
		}
		n := p[1] + "/" + p[2]
		v := strings.TrimSuffix(p[3], ".toml")
		if !ValidName(n) || !ValidVersion(v) {
			return nil, fail("BUNKO_E_INDEX", "invalid index identity")
		}
		idx.paths[n+"@"+v] = item.Path
	}
	return idx, nil
}
func (i *OfficialIndex) Record(n, v string) (Record, error) {
	key := n + "@" + v
	if r, ok := i.records[key]; ok {
		return r, nil
	}
	p, ok := i.paths[key]
	if !ok {
		return Record{}, fail("BUNKO_E_MISSING_VERSION", "%s", key)
	}
	b, e := i.fetch.get(raw+i.Revision+"/"+p, "text/plain", MaxRecord)
	if e != nil {
		return Record{}, e
	}
	r, e := ParseRecord(b, n, v)
	if e != nil {
		return r, e
	}
	if e = i.Policy.Check(r); e != nil {
		return r, e
	}
	i.records[key] = r
	return r, nil
}
func (i *OfficialIndex) Names() []string {
	m := map[string]string{}
	for k := range i.paths {
		n, _, _ := strings.Cut(k, "@")
		m[n] = ""
	}
	return Sorted(m)
}
func (i *OfficialIndex) Versions(n string) []string {
	var vs []string
	for k := range i.paths {
		nn, v, _ := strings.Cut(k, "@")
		if nn == n {
			vs = append(vs, v)
		}
	}
	sort.Slice(vs, func(a, b int) bool { return compare(vs[a], vs[b]) < 0 })
	return vs
}
func CacheRoot() (string, error) {
	if p := os.Getenv("MIO_BUNKO_CACHE"); p != "" {
		return filepath.Abs(p)
	}
	p, e := os.UserCacheDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(p, "mio", "bunko", "v1"), nil
}
func atomicFile(dir, name string, data []byte) error {
	if e := os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".tmp-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.Write(data); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return os.Rename(f.Name(), filepath.Join(dir, name))
}
func (i *OfficialIndex) SavePolicy(cache string) error {
	b, e := json.Marshal(i.Policy)
	if e != nil {
		return e
	}
	return atomicFile(cache, "policy.json", b)
}
