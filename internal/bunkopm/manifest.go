package bunkopm

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const ManifestName = "bunko.toml"

type Manifest struct {
	Dependencies map[string]string `toml:"dependencies"`
}

func ParseManifest(data []byte) (Manifest, error) {
	var m Manifest
	md, err := decode(data, &m)
	if err != nil {
		return m, err
	}
	if !md.IsDefined("dependencies") {
		return m, fail("BUNKO_E_MANIFEST", "missing [dependencies]")
	}
	return m, validateDependencies(m.Dependencies)
}
func ReadManifest(dir string) (Manifest, error) {
	b, e := readRegular(filepath.Join(dir, ManifestName), MaxRecord)
	if e != nil {
		return Manifest{}, fail("BUNKO_E_MANIFEST", "%v; run mio bunko init", e)
	}
	return ParseManifest(b)
}
func FindProject(entry string) (string, error) {
	p, e := filepath.Abs(entry)
	if e != nil {
		return "", e
	}
	if s, e := os.Stat(p); e == nil && !s.IsDir() {
		p = filepath.Dir(p)
	}
	for {
		s, e := os.Lstat(filepath.Join(p, ManifestName))
		if e == nil {
			if !s.Mode().IsRegular() {
				return "", fail("BUNKO_E_MANIFEST", "manifest must be a regular file")
			}
			return p, nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return "", e
		}
		next := filepath.Dir(p)
		if p == next {
			return "", nil
		}
		p = next
	}
}

// withProjectLock fails closed on a stale lock. It is never stolen from a live
// installer. Recovery is explicit; no timeout can cause simultaneous writers.
func withProjectLock(dir string, fn func() error) error {
	lock := filepath.Join(dir, ".bunko-manifest-write")
	if e := os.Mkdir(lock, 0700); e != nil {
		return fail("BUNKO_E_BUSY", "%s: %v (remove only after checking no installer is running)", lock, e)
	}
	defer os.Remove(lock)
	return fn()
}
func Init(dir string) error {
	return withProjectLock(dir, func() error {
		p := filepath.Join(dir, ManifestName)
		if _, e := os.Lstat(p); e == nil {
			_, e = ReadManifest(dir)
			return e
		} else if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		// Link publishes the fully written file without ever replacing an existing file.
		return writeManifest(dir, Manifest{map[string]string{}}, false)
	})
}
func writeManifest(dir string, m Manifest, replace bool) error {
	if e := validateDependencies(m.Dependencies); e != nil {
		return e
	}
	var b bytes.Buffer
	if e := toml.NewEncoder(&b).Encode(m); e != nil {
		return e
	}
	if b.Len() == 0 {
		b.WriteString("[dependencies]\n")
	}
	if b.Len() > MaxRecord {
		return fail("BUNKO_E_MANIFEST", "formatted manifest exceeds 64 KiB; existing declarations were preserved")
	}
	f, e := os.CreateTemp(dir, ".bunko-manifest-*")
	if e != nil {
		return e
	}
	p := f.Name()
	defer os.Remove(p)
	if _, e = f.Write(b.Bytes()); e != nil {
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
	dst := filepath.Join(dir, ManifestName)
	if replace {
		return os.Rename(p, dst)
	}
	return os.Link(p, dst)
}
func Remove(dir, name string) error {
	if !ValidName(name) {
		return fail("BUNKO_E_SPEC", "invalid package %q", name)
	}
	return withProjectLock(dir, func() error {
		m, e := ReadManifest(dir)
		if e != nil {
			return e
		}
		delete(m.Dependencies, name)
		return writeManifest(dir, m, true)
	})
}
func (c *Client) Add(dir, spec string) error {
	n, v, e := ParseSpec(spec)
	if e != nil {
		return e
	}
	return withProjectLock(dir, func() error {
		m, e := ReadManifest(dir)
		if e != nil {
			return e
		}
		m.Dependencies[n] = v
		records, e := c.Resolve(m.Dependencies)
		if e != nil {
			return e
		}
		if e = c.InstallRecords(records); e != nil {
			return e
		}
		return writeManifest(dir, m, true)
	})
}

// SafeText makes metadata safe for line-oriented terminal output.
func SafeText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 32 || r == 127 || (r >= 0x80 && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			fmt.Fprintf(&b, "\\u%04x", r)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
