// Package bunkopm implements the reference Bunko V1 client. It is separate from
// internal/bunko, which loads Mio source and the bundled kiso standard library.
package bunkopm

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

const MaxRecord = 64 << 10

var segment = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var exact = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)
var repository = regexp.MustCompile(`^https://github\.com/([a-z0-9-]+)/([A-Za-z0-9_.-]+)$`)
var hex40 = regexp.MustCompile(`^[0-9a-f]{40}$`)
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var id = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var license = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]{0,79}$`)

// Error carries a stable index-compatible diagnostic identifier. Detail is
// escaped at the terminal boundary, never interpreted as markup or ANSI.
type Error struct{ Code, Detail string }

func (e *Error) Error() string                    { return e.Code + ": " + e.Detail }
func fail(code, format string, args ...any) error { return &Error{code, fmt.Sprintf(format, args...)} }
func ValidName(name string) bool {
	p := strings.Split(name, "/")
	return len(p) == 2 && len(p[0]) <= 39 && len(p[1]) <= 39 && segment.MatchString(p[0]) && segment.MatchString(p[1])
}
func ValidVersion(v string) bool { return exact.MatchString(v) }
func ParseSpec(s string) (string, string, error) {
	n, v, ok := strings.Cut(s, "@")
	if !ok || !ValidName(n) || !ValidVersion(v) {
		return "", "", fail("BUNKO_E_SPEC", "%q: expected namespace/package@MAJOR.MINOR.PATCH", s)
	}
	return n, v, nil
}
func compare(a, b string) int {
	aa := strings.Split(a, ".")
	bb := strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, _ := strconv.Atoi(aa[i])
		y, _ := strconv.Atoi(bb[i])
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}
func Sorted(m map[string]string) []string {
	a := make([]string, 0, len(m))
	for k := range m {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}
func validateDependencies(deps map[string]string) error {
	for _, n := range Sorted(deps) {
		if !ValidName(n) || !ValidVersion(deps[n]) || strings.HasPrefix(n, "kiso/") {
			return fail("BUNKO_E_DEPENDENCY", "%q = %q", n, deps[n])
		}
	}
	return nil
}

type Record struct {
	// Keep the accepted record bytes: TOML formatting can expand a valid 64 KiB
	// record beyond the index bound, and published records are immutable.
	raw          []byte
	Name         string `toml:"name"`
	Version      string `toml:"version"`
	Publisher    string `toml:"publisher"`
	Description  string `toml:"description"`
	Repository   string `toml:"repository"`
	SourceCommit string `toml:"source_commit"`
	ReleaseID    string `toml:"release_id"`
	Archive      string `toml:"archive"`
	SHA256       string `toml:"sha256"`
	License      string `toml:"license"`
	Mio          struct {
		Min    string `toml:"min"`
		Before string `toml:"before"`
	} `toml:"mio"`
	Dependencies map[string]string   `toml:"dependencies"`
	Platforms    map[string][]string `toml:"platforms"`
}

func decode(data []byte, out any) (toml.MetaData, error) {
	if len(data) > MaxRecord || !utf8.Valid(data) {
		return toml.MetaData{}, fail("BUNKO_E_RECORD", "invalid UTF-8 or record exceeds 64 KiB")
	}
	md, err := toml.Decode(string(data), out)
	if err != nil {
		return md, fail("BUNKO_E_RECORD", "%v", err)
	}
	if len(md.Undecoded()) != 0 {
		return md, fail("BUNKO_E_SCHEMA", "unknown fields: %v", md.Undecoded())
	}
	return md, nil
}
func ParseRecord(data []byte, name, version string) (Record, error) {
	var r Record
	md, err := decode(data, &r)
	if err != nil {
		return r, err
	}
	for _, k := range []string{"name", "version", "publisher", "description", "repository", "source_commit", "release_id", "archive", "sha256", "license", "mio", "dependencies", "platforms"} {
		if !md.IsDefined(k) {
			return r, fail("BUNKO_E_SCHEMA", "missing %s", k)
		}
	}
	if r.Name != name || r.Version != version || !ValidName(r.Name) || !ValidVersion(r.Version) {
		return r, fail("BUNKO_E_PATH", "record identity differs from %s@%s", name, version)
	}
	repo := repository.FindStringSubmatch(r.Repository)
	if repo == nil || repo[2] == "." || repo[2] == ".." || r.Publisher != "github:"+repo[1] || !segment.MatchString(repo[1]) {
		return r, fail("BUNKO_E_IDENTITY", "invalid repository/publisher")
	}
	prefix := "https://api.github.com/repos/" + repo[1] + "/" + repo[2] + "/releases/assets/"
	if !strings.HasPrefix(r.Archive, prefix) || !id.MatchString(strings.TrimPrefix(r.Archive, prefix)) || !id.MatchString(r.ReleaseID) || !hex40.MatchString(r.SourceCommit) || !hex64.MatchString(r.SHA256) || !license.MatchString(r.License) {
		return r, fail("BUNKO_E_SOURCE", "invalid provenance metadata")
	}
	ns := strings.Split(name, "/")[0]
	if ns != "mio" && ns != "kiso" && ns != "bunko" && r.Publisher != "github:"+ns {
		return r, fail("BUNKO_E_IDENTITY", "namespace differs from publisher")
	}
	if utf8.RuneCountInString(r.Description) > 240 || strings.TrimSpace(r.Description) == "" {
		return r, fail("BUNKO_E_SCHEMA", "invalid description")
	}
	for _, c := range r.Description {
		if unicode.IsControl(c) {
			return r, fail("BUNKO_E_SCHEMA", "control in description")
		}
	}
	if !ValidVersion(r.Mio.Min) || !ValidVersion(r.Mio.Before) || compare(r.Mio.Min, r.Mio.Before) >= 0 {
		return r, fail("BUNKO_E_COMPATIBILITY", "invalid compiler range")
	}
	if len(r.Platforms) == 0 {
		return r, fail("BUNKO_E_SCHEMA", "empty platforms")
	}
	for os, archs := range r.Platforms {
		if os != "linux" && os != "macos" && os != "windows" {
			return r, fail("BUNKO_E_SCHEMA", "invalid OS %q", os)
		}
		seen := map[string]bool{}
		if len(archs) == 0 {
			return r, fail("BUNKO_E_SCHEMA", "empty architectures")
		}
		for _, a := range archs {
			if (a != "x86_64" && a != "arm64") || seen[a] {
				return r, fail("BUNKO_E_SCHEMA", "invalid architecture %q", a)
			}
			seen[a] = true
		}
	}
	if err := validateDependencies(r.Dependencies); err != nil {
		return r, err
	}
	r.raw = append([]byte(nil), data...)
	return r, nil
}
func Target(os, arch string) string {
	if os == "darwin" {
		os = "macos"
	}
	if arch == "amd64" {
		arch = "x86_64"
	}
	if arch == "aarch64" {
		arch = "arm64"
	}
	return os + "/" + arch
}
func (r Record) Compatible(compiler, target string) error {
	if !ValidVersion(compiler) || compare(compiler, r.Mio.Min) < 0 || compare(compiler, r.Mio.Before) >= 0 {
		return fail("BUNKO_E_COMPATIBILITY", "%s@%s requires Mio [%s, %s), running %s", r.Name, r.Version, r.Mio.Min, r.Mio.Before, compiler)
	}
	os, arch, _ := strings.Cut(target, "/")
	for _, a := range r.Platforms[os] {
		if a == arch {
			return nil
		}
	}
	return fail("BUNKO_E_PLATFORM", "%s@%s does not declare %s", r.Name, r.Version, target)
}
