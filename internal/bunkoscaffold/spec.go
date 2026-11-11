// Package bunkoscaffold creates local Bunko development projects. It never
// installs packages, publishes releases, or submits index records.
package bunkoscaffold

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"Mio/internal/bunkopm"
	"github.com/BurntSushi/toml"
)

const InfoName = "bunko.package.toml"

//go:embed templates.json
var assetJSON []byte
var assets = func() map[string]json.RawMessage {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(assetJSON, &m); err != nil {
		panic(err)
	}
	return m
}()

func asset(name string) string {
	var s string
	if err := json.Unmarshal(assets[name], &s); err != nil {
		panic(err)
	}
	return s
}
func Targets() []string {
	var a []string
	if err := json.Unmarshal(assets["targets"], &a); err != nil {
		panic(err)
	}
	return a
}
func PolicyRevision() string { return asset("policy_revision") }

type Error struct{ Kind, Detail string }

func (e *Error) Error() string {
	return "BUNKO_E_SCAFFOLD_" + strings.ToUpper(e.Kind) + ": " + e.Detail
}
func invalid(kind, detail string) error { return &Error{kind, detail} }

type PackageInfo struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
	Version     string `toml:"version"`
	License     string `toml:"license"`
	Repository  string `toml:"repository,omitempty"`
}
type LocalInfo struct {
	Package   PackageInfo         `toml:"package"`
	Platforms map[string][]string `toml:"platforms"`
}
type Spec struct {
	Namespace, Name, Description, Version, License, Directory string
	Targets                                                   []string
	Dependencies                                              map[string]string
	Workflow                                                  bool
}

func (s Spec) FullName() string { return s.Namespace + "/" + s.Name }
func validDescription(s string) bool {
	if !utf8.ValidString(s) || strings.TrimSpace(s) == "" || utf8.RuneCountInString(s) > 240 {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x200e || r == 0x200f {
			return false
		}
	}
	return true
}
func validChild(s string) bool {
	if !bunkopm.ValidName("local/" + s) {
		return false
	}
	if s == "con" || s == "prn" || s == "aux" || s == "nul" {
		return false
	}
	if len(s) == 4 && (strings.HasPrefix(s, "com") || strings.HasPrefix(s, "lpt")) && s[3] >= '1' && s[3] <= '9' {
		return false
	}
	return true
}
func (s *Spec) Validate() error {
	if s.Dependencies == nil {
		s.Dependencies = map[string]string{}
	}
	if !bunkopm.ValidName(s.FullName()) {
		return invalid("invalid_name", s.FullName())
	}
	if !bunkopm.ValidVersion(s.Version) {
		return invalid("invalid_version", s.Version)
	}
	if !validDescription(s.Description) {
		return invalid("invalid_description", "")
	}
	if s.License != "MIT" && s.License != "Apache-2.0" {
		return invalid("invalid_license", s.License)
	}
	if s.Directory == "" {
		s.Directory = s.Name
	}
	if !validChild(s.Directory) {
		return invalid("invalid_path", s.Directory)
	}
	if len(s.Targets) == 0 {
		return invalid("invalid_platform", "")
	}
	allowed := map[string]bool{}
	for _, t := range Targets() {
		allowed[t] = true
	}
	seen := map[string]bool{}
	for _, t := range s.Targets {
		if !allowed[t] || seen[t] {
			return invalid("invalid_platform", t)
		}
		seen[t] = true
	}
	sort.Strings(s.Targets)
	if len(s.Dependencies) > 256 {
		return invalid("invalid_dependency", "256")
	}
	for n, v := range s.Dependencies {
		if _, _, e := bunkopm.ParseSpec(n + "@" + v); e != nil || strings.HasPrefix(n, "kiso/") {
			return invalid("invalid_dependency", n+"@"+v)
		}
	}
	if s.Workflow && len(s.Dependencies) > 0 {
		return invalid("workflow_dependencies", "")
	}
	return nil
}
func (s Spec) Info() LocalInfo {
	p := map[string][]string{}
	for _, t := range s.Targets {
		os, arch, _ := strings.Cut(t, "/")
		p[os] = append(p[os], arch)
	}
	return LocalInfo{PackageInfo{Name: s.FullName(), Description: s.Description, Version: s.Version, License: s.License}, p}
}
func encode(v any) (string, error) {
	var b bytes.Buffer
	if e := toml.NewEncoder(&b).Encode(v); e != nil {
		return "", e
	}
	return b.String(), nil
}
func Files(s Spec) (map[string]string, error) {
	if e := s.Validate(); e != nil {
		return nil, e
	}
	info, e := encode(s.Info())
	if e != nil {
		return nil, e
	}
	deps, e := encode(bunkopm.Manifest{Dependencies: s.Dependencies})
	if e != nil {
		return nil, e
	}
	if _, e = bunkopm.ParseManifest([]byte(deps)); e != nil {
		return nil, e
	}
	platformText, _ := encode(struct {
		Platforms map[string][]string `toml:"platforms"`
	}{s.Info().Platforms})
	descriptionText, _ := encode(struct {
		Description string `toml:"description"`
	}{s.Description})
	descriptionValue := strings.TrimSpace(strings.TrimPrefix(descriptionText, "description = "))
	replace := strings.NewReplacer("{{package}}", s.Name, "{{description_toml}}", descriptionValue, "{{platforms_toml}}", strings.TrimSpace(platformText), "{{dependencies_toml}}", strings.TrimSpace(deps), "{{name}}", s.FullName(), "{{namespace}}", s.Namespace, "{{description}}", s.Description, "{{version}}", s.Version, "{{license}}", s.License, "{{platforms}}", strings.Join(s.Targets, ", "))
	licenseKey := "license_mit"
	if s.License == "Apache-2.0" {
		licenseKey = "license_apache"
	}
	files := map[string]string{"package.mio": asset("source"), "package_test.mio": asset("test"), "README.md": replace.Replace(asset("readme")), "LICENSE": replace.Replace(asset(licenseKey)), ".gitignore": asset("gitignore"), "bunko.toml": deps, InfoName: info}
	if s.Workflow {
		files[".github/workflows/mio-test.yml"] = Workflow(s.Targets)
	}
	return files, nil
}
func Workflow(targets []string) string {
	runners := map[string]string{"linux/x86_64": "ubuntu-24.04", "macos/arm64": "macos-15", "windows/x86_64": "windows-2025"}
	var b strings.Builder
	b.WriteString(asset("workflow_header"))
	for _, t := range targets {
		fmt.Fprintf(&b, "          - {target: %s, runner: %s}\n", t, runners[t])
	}
	b.WriteString(asset("workflow_footer"))
	return b.String()
}

// Generate exclusively owns a NEW direct child of root. Existing directories,
// files and symlinks are always rejected; there is no overwrite mode.
func Generate(root string, s Spec) (string, error) { return generate(root, s, os.WriteFile) }
func generate(root string, s Spec, write func(string, []byte, os.FileMode) error) (string, error) {
	if e := s.Validate(); e != nil {
		return "", e
	}
	files, e := Files(s)
	if e != nil {
		return "", e
	}
	physical, e := filepath.EvalSymlinks(root)
	if e != nil {
		return "", invalid("io", e.Error())
	}
	physical, e = filepath.Abs(physical)
	if e != nil {
		return "", e
	}
	dst := filepath.Join(physical, s.Directory)
	interrupts := make(chan os.Signal, 1)
	signal.Notify(interrupts, os.Interrupt)
	defer signal.Stop(interrupts)
	if e = os.Mkdir(dst, 0700); e != nil {
		return "", invalid("exists", dst)
	}
	complete := false
	defer func() {
		if !complete {
			for name := range files {
				os.Remove(filepath.Join(dst, filepath.FromSlash(name)))
			}
			os.Remove(filepath.Join(dst, ".github", "workflows"))
			os.Remove(filepath.Join(dst, ".github"))
			os.Remove(dst)
		}
	}()
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		select {
		case <-interrupts:
			return "", invalid("cancelled", "")
		default:
		}
		p := filepath.Join(dst, filepath.FromSlash(name))
		if e = os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			return "", invalid("io", e.Error())
		}
		if e = write(p, []byte(files[name]), 0644); e != nil {
			return "", invalid("io", e.Error())
		}
	}
	select {
	case <-interrupts:
		return "", invalid("cancelled", "")
	default:
	}
	complete = true
	return dst, nil
}
func ReadInfo(root string) (LocalInfo, error) {
	var info LocalInfo
	p := filepath.Join(root, InfoName)
	st, e := os.Lstat(p)
	if e != nil || !st.Mode().IsRegular() || st.Size() > bunkopm.MaxRecord {
		return info, invalid("not_package", root)
	}
	b, e := os.ReadFile(p)
	if e != nil {
		return info, e
	}
	md, e := toml.Decode(string(b), &info)
	if e != nil || len(md.Undecoded()) > 0 {
		return info, invalid("not_package", root)
	}
	ns, n, ok := strings.Cut(info.Package.Name, "/")
	if !ok {
		return info, invalid("invalid_name", info.Package.Name)
	}
	s := Spec{Namespace: ns, Name: n, Description: info.Package.Description, Version: info.Package.Version, License: info.Package.License, Dependencies: map[string]string{}}
	for os, archs := range info.Platforms {
		for _, a := range archs {
			s.Targets = append(s.Targets, os+"/"+a)
		}
	}
	if e = s.Validate(); e != nil {
		return info, e
	}
	if _, e = bunkopm.ReadManifest(root); e != nil {
		return info, e
	}
	return info, nil
}
func saveInfo(root string, info LocalInfo) error {
	p := filepath.Join(root, InfoName)
	st, e := os.Lstat(p)
	if e != nil || !st.Mode().IsRegular() {
		return invalid("not_package", root)
	}
	data, e := encode(info)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(root, ".bunko-package-*")
	if e != nil {
		return e
	}
	defer os.Remove(f.Name())
	if _, e = f.WriteString(data); e != nil {
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
	return os.Rename(f.Name(), p)
}
