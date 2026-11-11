package bunkopm

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

func entryPath(cache, n, v string) (string, error) {
	if !ValidName(n) || !ValidVersion(v) {
		return "", fail("BUNKO_E_SPEC", "invalid cache identity")
	}
	return filepath.Join(cache, filepath.FromSlash(n), v), nil
}
func readRegular(p string, limit int64) ([]byte, error) {
	s, e := os.Lstat(p)
	if e != nil {
		return nil, e
	}
	if !s.Mode().IsRegular() || s.Size() > limit {
		return nil, fail("BUNKO_E_CACHE", "invalid cache file %q", p)
	}
	return os.ReadFile(p)
}
func archiveDigest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func sameRecord(a, b Record) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}
func cacheMetadata(r Record) ([]byte, error) {
	data := r.raw
	if len(data) == 0 {
		var encoded bytes.Buffer
		if err := toml.NewEncoder(&encoded).Encode(r); err != nil {
			return nil, err
		}
		data = encoded.Bytes()
	}
	parsed, err := ParseRecord(data, r.Name, r.Version)
	if err != nil {
		return nil, err
	}
	if !sameRecord(parsed, r) {
		return nil, fail("BUNKO_E_CACHE", "record fields differ from immutable index bytes")
	}
	return data, nil
}
func VerifyCached(cache string, r Record) (string, error) {
	dir, e := entryPath(cache, r.Name, r.Version)
	if e != nil {
		return "", e
	}
	metadata, e := readRegular(filepath.Join(dir, "record.toml"), MaxRecord)
	if e != nil {
		return "", fail("BUNKO_E_MISSING_INSTALL", "%s@%s: run mio bunko install (%v)", r.Name, r.Version, e)
	}
	stored, e := ParseRecord(metadata, r.Name, r.Version)
	if e != nil {
		return "", e
	}
	if !sameRecord(stored, r) {
		return "", fail("BUNKO_E_CACHE", "cached immutable metadata differs for %s@%s", r.Name, r.Version)
	}
	archive, e := readRegular(filepath.Join(dir, "archive.tar.gz"), MaxArchive)
	if e != nil {
		return "", e
	}
	if archiveDigest(archive) != r.SHA256 {
		return "", fail("BUNKO_E_CHECKSUM", "cached archive checksum mismatch")
	}
	files, e := ReadArchive(archive)
	if e != nil {
		return "", e
	}
	root := filepath.Join(dir, "src")
	count := 0
	e = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fail("BUNKO_E_CACHE", "symlink in cache")
		}
		if d.IsDir() {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		expected, ok := files[filepath.ToSlash(rel)]
		if !ok {
			return fail("BUNKO_E_CACHE", "unexpected cached file")
		}
		actual, e := readRegular(p, MaxFileBytes)
		if e != nil {
			return e
		}
		if !bytes.Equal(expected, actual) {
			return fail("BUNKO_E_CACHE", "modified cached source %q", rel)
		}
		count++
		return nil
	})
	if e != nil {
		return "", e
	}
	if count != len(files) {
		return "", fail("BUNKO_E_CACHE", "missing cached files")
	}
	return root, nil
}
func (c *Client) install(r Record) error {
	dir, e := entryPath(c.Cache, r.Name, r.Version)
	if e != nil {
		return e
	}
	if _, e = os.Lstat(dir); e == nil {
		_, e = VerifyCached(c.Cache, r)
		return e
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	metadata, e := cacheMetadata(r)
	if e != nil {
		return e
	}
	parent := filepath.Dir(dir)
	if e = os.MkdirAll(parent, 0700); e != nil {
		return e
	}
	// Unique staging directories plus no-replace publication prevent concurrent
	// installers or interrupted writes from exposing a partial cache entry.
	stage, e := os.MkdirTemp(parent, ".install-*")
	if e != nil {
		return e
	}
	defer os.RemoveAll(stage)
	if c.Fetch == nil {
		return fail("BUNKO_E_NETWORK", "no package fetcher")
	}
	size, e := c.Fetch.VerifyRelease(r)
	if e != nil {
		return e
	}
	archive, e := c.Fetch.get(r.Archive, "application/octet-stream", MaxArchive)
	if e != nil {
		return e
	}
	if int64(len(archive)) != size || archiveDigest(archive) != r.SHA256 {
		return fail("BUNKO_E_CHECKSUM", "archive digest or size mismatch for %s@%s", r.Name, r.Version)
	}
	files, e := ReadArchive(archive)
	if e != nil {
		return e
	}
	for name, data := range files {
		p := filepath.Join(stage, "src", filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return e
		}
		if e = os.WriteFile(p, data, 0600); e != nil {
			return e
		}
	}
	if e = os.WriteFile(filepath.Join(stage, "record.toml"), metadata, 0600); e != nil {
		return e
	}
	if e = os.WriteFile(filepath.Join(stage, "archive.tar.gz"), archive, 0600); e != nil {
		return e
	}
	// A populated directory cannot replace another populated directory on the
	// supported platforms. The winning install is reverified before reuse.
	if e = os.Rename(stage, dir); e != nil {
		if _, v := VerifyCached(c.Cache, r); v == nil {
			return nil
		}
		return fail("BUNKO_E_CACHE", "publish cache: %v", e)
	}
	return nil
}

type CachedIndex struct {
	Cache  string
	Policy Policy
}

func OpenCachedIndex(cache string) (*CachedIndex, error) {
	b, e := readRegular(filepath.Join(cache, "policy.json"), maxJSON)
	if e != nil {
		return nil, fail("BUNKO_E_MISSING_INSTALL", "run mio bunko install to cache official policy: %v", e)
	}
	i := &CachedIndex{Cache: cache}
	if e = json.Unmarshal(b, &i.Policy); e != nil {
		return nil, e
	}
	return i, nil
}
func (i *CachedIndex) Record(n, v string) (Record, error) {
	p, e := entryPath(i.Cache, n, v)
	if e != nil {
		return Record{}, e
	}
	b, e := readRegular(filepath.Join(p, "record.toml"), MaxRecord)
	if e != nil {
		return Record{}, fail("BUNKO_E_MISSING_INSTALL", "%s@%s: run mio bunko install", n, v)
	}
	r, e := ParseRecord(b, n, v)
	if e != nil {
		return r, e
	}
	return r, i.Policy.Check(r)
}
func (i *CachedIndex) Names() []string          { return nil }
func (i *CachedIndex) Versions(string) []string { return nil }

// OfflinePackages never creates directories or performs network IO.
func OfflinePackages(project, compiler, target string) (map[string]string, error) {
	m, e := ReadManifest(project)
	if e != nil {
		return nil, e
	}
	paths := map[string]string{}
	if len(m.Dependencies) == 0 {
		return paths, nil
	}
	cache, e := CacheRoot()
	if e != nil {
		return nil, e
	}
	idx, e := OpenCachedIndex(cache)
	if e != nil {
		return nil, e
	}
	c := Client{Index: idx, Cache: cache, Compiler: compiler, Target: target}
	rs, e := c.Resolve(m.Dependencies)
	if e != nil {
		return nil, e
	}
	for _, r := range rs {
		p, e := VerifyCached(cache, r)
		if e != nil {
			return nil, e
		}
		paths[r.Name] = p
	}
	return paths, nil
}
