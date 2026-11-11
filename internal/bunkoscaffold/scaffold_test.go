package bunkoscaffold

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"Mio/internal/bunkopm"
	"Mio/internal/i18n"
)

func fixture() Spec {
	return Spec{Namespace: "alice", Name: "json", Description: "A source package", Version: "0.1.0", License: "MIT", Targets: []string{"linux/x86_64"}}
}
func TestValidationAndPolicy(t *testing.T) {
	for _, name := range []string{"alice/json", "a-b/json2"} {
		ns, n, _ := strings.Cut(name, "/")
		s := fixture()
		s.Namespace = ns
		s.Name = n
		if e := s.Validate(); e != nil {
			t.Fatal(e)
		}
	}
	for _, name := range []string{"../json", "Alice/json", "a/a--b", "a/../b", "a/", "a/UPPER"} {
		ns, n, _ := strings.Cut(name, "/")
		s := fixture()
		s.Namespace = ns
		s.Name = n
		if e := s.Validate(); e == nil {
			t.Fatal("accepted name", name)
		}
	}
	for _, v := range []string{"latest", "^1.0.0", "~1.0.0", "01.0.0", "1.0.0-rc.1", "1.0.0+meta", "1.0.0\n"} {
		s := fixture()
		s.Version = v
		if e := s.Validate(); e == nil {
			t.Fatal("accepted version", v)
		}
	}
	for _, target := range []string{"linux/arm64", "macos/x86_64", "windows/arm64", "darwin/arm64", "linux/amd64", "*"} {
		s := fixture()
		s.Targets = []string{target}
		if e := s.Validate(); e == nil {
			t.Fatal("accepted unpublished platform", target)
		}
	}
	s := fixture()
	s.Targets = nil
	if e := s.Validate(); e == nil {
		t.Fatal("assumed platform")
	}
	s = fixture()
	s.Targets = []string{"linux/x86_64", "linux/x86_64"}
	if e := s.Validate(); e == nil {
		t.Fatal("duplicate target")
	}
	for _, d := range []string{"", "\n", "escape\x1b[31m", "line\nnext", strings.Repeat("x", 241), "\xff"} {
		s := fixture()
		s.Description = d
		if e := s.Validate(); e == nil {
			t.Fatal("accepted description")
		}
	}
}
func TestFilesAndArchiveBoundary(t *testing.T) {
	for _, license := range []string{"MIT", "Apache-2.0"} {
		s := fixture()
		s.License = license
		s.Description = `A "quoted" \ description`
		s.Workflow = true
		files, e := Files(s)
		if e != nil {
			t.Fatal(e)
		}
		for _, n := range []string{"package.mio", "package_test.mio", "bunko.toml", InfoName, "README.md", "LICENSE", ".gitignore", ".github/workflows/mio-test.yml"} {
			if files[n] == "" {
				t.Fatal("missing file", n)
			}
		}
		expected := license
		if license == "Apache-2.0" {
			expected = "Apache License"
		}
		if !strings.Contains(files["LICENSE"], expected) {
			t.Fatal("license text missing")
		}
		if _, e := bunkopm.ParseManifest([]byte(files["bunko.toml"])); e != nil {
			t.Fatal(e)
		}
		var archive bytes.Buffer
		gz := gzip.NewWriter(&archive)
		tw := tar.NewWriter(gz)
		for _, name := range []string{"package.mio", "package_test.mio", "README.md", "LICENSE"} {
			b := []byte(files[name])
			if e := tw.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(b))}); e != nil {
				t.Fatal(e)
			}
			tw.Write(b)
		}
		tw.Close()
		gz.Close()
		found, e := bunkopm.ReadArchive(archive.Bytes())
		if e != nil || len(found) != 4 {
			t.Fatal(found, e)
		}
		if strings.Contains(files[".github/workflows/mio-test.yml"], "self-hosted") {
			t.Fatal("untrusted private runner")
		}
	}
}
func TestExistingAndUnsafeDestinations(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"../escape", "/absolute", "nested/path", "C:\\escape", "con", "lpt1"} {
		s := fixture()
		s.Directory = name
		if _, e := Generate(root, s); e == nil {
			t.Fatal("unsafe directory", name)
		}
	}
	p := filepath.Join(root, "json")
	if e := os.WriteFile(p, []byte("keep"), 0600); e != nil {
		t.Fatal(e)
	}
	if _, e := Generate(root, fixture()); e == nil {
		t.Fatal("overwrote file")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "keep" {
		t.Fatal("changed file")
	}
	os.Remove(p)
	os.Mkdir(p, 0700)
	os.WriteFile(filepath.Join(p, "keep"), []byte("keep"), 0600)
	if _, e := Generate(root, fixture()); e == nil {
		t.Fatal("overwrote directory")
	}
}
func TestSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(root, "json")
	if e := os.Symlink(outside, link); e != nil {
		t.Skipf("host cannot create symlink fixture: %v", e)
	}
	if _, e := Generate(root, fixture()); e == nil {
		t.Fatal("followed destination symlink")
	}
	entries, _ := os.ReadDir(outside)
	if len(entries) != 0 {
		t.Fatal("escaped project root")
	}
}
func TestPartialWritePreservesParent(t *testing.T) {
	root := t.TempDir()
	keep := filepath.Join(root, "keep")
	os.WriteFile(keep, []byte("keep"), 0600)
	count := 0
	_, e := generate(root, fixture(), func(p string, b []byte, m os.FileMode) error {
		count++
		if count == 2 {
			return errors.New("injected failure")
		}
		return os.WriteFile(p, b, m)
	})
	if e == nil {
		t.Fatal("ignored failed write")
	}
	if _, e := os.Stat(filepath.Join(root, "json")); !os.IsNotExist(e) {
		t.Fatal("left partial package")
	}
	b, _ := os.ReadFile(keep)
	if string(b) != "keep" {
		t.Fatal("changed parent")
	}
}

type mockRunner struct {
	calls                                                                                             []Command
	missingGit, missingGh, unauth, multiple, noPermission, memberOnly, failCreate, failGit, failLogin bool
	failAuth                                                                                          bool
	username                                                                                          string
	loginCalls                                                                                        int
	created, origin                                                                                   bool
	onRemote                                                                                          func(string)
}

func (m *mockRunner) LookPath(n string) (string, error) {
	if (n == "git" && m.missingGit) || (n == "gh" && m.missingGh) {
		return "", errors.New("missing")
	}
	return "/mock/" + n, nil
}
func (m *mockRunner) Run(c Command) ([]byte, error) {
	m.calls = append(m.calls, c)
	if c.Timeout <= 0 {
		panic("unbounded process")
	}
	if len(c.Args) > 0 && c.Args[0] == "--version" {
		return []byte(filepath.Base(c.Path) + " fixture 1.0\n"), nil
	}
	if strings.HasSuffix(c.Path, "git") {
		switch c.Args[0] {
		case "init":
			if m.failGit {
				return nil, errors.New("git init failed")
			}
			os.Mkdir(filepath.Join(c.Dir, ".git"), 0700)
			os.WriteFile(filepath.Join(c.Dir, ".git", "config"), []byte("[core]\n"), 0600)
		case "rev-parse":
			return []byte(c.Dir + "\n"), nil
		case "remote":
			if len(c.Args) == 1 {
				if m.origin {
					return []byte("origin\n"), nil
				}
				return nil, nil
			}
			m.origin = true
			if m.onRemote != nil {
				m.onRemote(c.Dir)
			}
		}
		return nil, nil
	}
	login := m.username
	if login == "" {
		login = "alice"
	}
	if c.Args[0] == "auth" {
		if !contains(c.Args, "--no-refresh") || contains(c.Args, "--show-token") {
			panic("unsafe authentication query")
		}
		if m.failAuth {
			return nil, errors.New("fatal auth query")
		}
		if m.unauth {
			return []byte("[]"), nil
		}
		a := []map[string]any{{"login": login, "active": true, "state": "success"}}
		if m.multiple {
			a = append(a, map[string]any{"login": "bob", "active": false, "state": "success"})
		}
		return json.Marshal(a)
	}
	endpoint := c.Args[5]
	switch {
	case endpoint == "user":
		return []byte(fmt.Sprintf(`{"login":%q,"id":42}`, login)), nil
	case strings.HasPrefix(endpoint, "user/memberships/orgs/"):
		role := "admin"
		if m.memberOnly {
			role = "member"
		}
		return []byte(fmt.Sprintf(`{"state":"active","role":%q}`, role)), nil
	case contains(c.Args, "POST"):
		if m.failCreate {
			return nil, errors.New("creation failed")
		}
		var payload map[string]any
		json.Unmarshal(c.Input, &payload)
		if payload["auto_init"] != false {
			panic("unexpected remote initialization")
		}
		m.created = true
		return []byte(`{"full_name":"alice/json"}`), nil
	case strings.HasPrefix(endpoint, "repos/"):
		full := strings.TrimPrefix(endpoint, "repos/")
		owner, _, _ := strings.Cut(full, "/")
		return []byte(fmt.Sprintf(`{"full_name":%q,"private":false,"owner":{"login":%q,"type":"User"},"permissions":{"push":%v,"admin":false}}`, full, owner, !m.noPermission)), nil
	}
	return nil, errors.New("unexpected command")
}
func (m *mockRunner) Interactive(c Command, _ io.Reader, _ io.Writer, _ io.Writer) error {
	m.calls = append(m.calls, c)
	m.loginCalls++
	if m.failLogin {
		return errors.New("login failed")
	}
	m.unauth = false
	return nil
}
func contains(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func runFixture(t *testing.T, m *mockRunner, args []string, input string) (int, string, string, string) {
	t.Helper()
	root := t.TempDir()
	var out, stderr bytes.Buffer
	status := Run("create", args, Context{Root: root, In: strings.NewReader(input), Out: &out, Err: &stderr, Language: "en-US", Compiler: "2.2.0-fixture", Host: bunkopm.Target(runtime.GOOS, runtime.GOARCH), Runner: m})
	return status, out.String(), stderr.String(), root
}
func localArgs() []string {
	return []string{"--non-interactive", "--namespace", "alice", "--name", "json", "--description", "Local fixture", "--version", "1.0.0", "--license", "MIT", "--platform", "linux/x86_64"}
}
func remoteArgs() []string {
	return append(localArgs(), "--github", "--repo", "alice/json", "--create-repo", "--visibility", "public", "--allow-repo-create", "--allow-connect", "--allow-git-init", "--allow-git-config")
}
func TestLocalOnlyAndInteractive(t *testing.T) {
	m := &mockRunner{}
	status, out, err, root := runFixture(t, m, localArgs(), "")
	if status != 0 {
		t.Fatal(status, out, err)
	}
	if _, e := ReadInfo(filepath.Join(root, "json")); e != nil {
		t.Fatal(e)
	}
	for _, c := range m.calls {
		if c.Args[0] != "--version" {
			t.Fatal("local creation contacted service", c)
		}
	}
	m = &mockRunner{missingGit: true, missingGh: true}
	status, out, err, root = runFixture(t, m, nil, "BAD\nalice/json\nInteractive fixture\n\n1,2\n")
	if status != 0 {
		t.Fatal(status, out, err)
	}
	if strings.Contains(out, "https://cli.github.com/") || len(m.calls) != 0 {
		t.Fatal("local wizard probed GitHub")
	}
	if _, e := ReadInfo(filepath.Join(root, "json")); e != nil {
		t.Fatal(e)
	}
	status, _, _, root = runFixture(t, &mockRunner{missingGit: true, missingGh: true}, nil, "alice/json\nDeclined fixture\n\x1b\n")
	if status != 130 {
		t.Fatal(status)
	}
	if _, e := os.Stat(filepath.Join(root, "json")); !os.IsNotExist(e) {
		t.Fatal("ignored local rejection")
	}
}
func TestDependencyAndOptionParsing(t *testing.T) {
	for _, dep := range []string{"alice/json@latest", "alice/json@^1.0.0", "alice/json@~1.0.0", "alice/json@1.0.0,bob/text@2.0.0,alice/json@2.0.0"} {
		if _, e := Parse("create", []string{"--dependency", dep}); e == nil {
			t.Fatal("accepted dependency", dep)
		}
	}
	o, e := Parse("create", []string{"--dependency", "alice/json@1.0.0", "--dependency", "alice/json@1.0.0"})
	if e != nil || len(o.Spec.Dependencies) != 1 {
		t.Fatal(o, e)
	}
	if _, e = Parse("connect", []string{"--push"}); e == nil {
		t.Fatal("implicit push interface")
	}
}
func TestPermissionBoundaries(t *testing.T) {
	for _, deny := range []string{"repo_create", "connect", "git_init", "git_config"} {
		t.Run(deny, func(t *testing.T) {
			args := remoteArgs()
			flag := "--allow-" + strings.ReplaceAll(deny, "_", "-")
			var filtered []string
			for _, a := range args {
				if a != flag {
					filtered = append(filtered, a)
				}
			}
			m := &mockRunner{}
			status, out, err, root := runFixture(t, m, filtered, "")
			if status != 0 {
				t.Fatal(status, out, err)
			}
			if m.origin {
				t.Fatal("one permission authorized another action")
			}
			if deny == "repo_create" && m.created {
				t.Fatal("created without permission")
			}
			if _, e := ReadInfo(filepath.Join(root, "json")); e != nil {
				t.Fatal(e)
			}
		})
	}
	m := &mockRunner{}
	status, out, err, root := runFixture(t, m, remoteArgs(), "")
	if status != 0 || !m.created || !m.origin {
		t.Fatal(status, out, err, m)
	}
	info, e := ReadInfo(filepath.Join(root, "json"))
	if e != nil || info.Package.Repository != "https://github.com/alice/json" {
		t.Fatal(info, e)
	}
	assertNoPublish(t, m)
}
func assertNoPublish(t *testing.T, m *mockRunner) {
	t.Helper()
	for _, c := range m.calls {
		for _, arg := range c.Args {
			if arg == "push" || arg == "commit" || arg == "refresh" || arg == "token" || arg == "--show-token" || strings.Contains(arg, "/releases") {
				t.Fatal("unauthorized future phase", c)
			}
		}
	}
}
func TestMissingToolsAndPartialFailures(t *testing.T) {
	for _, m := range []*mockRunner{{missingGit: true}, {missingGh: true}, {failCreate: true}, {failGit: true}, {noPermission: true}} {
		status, _, _, root := runFixture(t, m, remoteArgs(), "")
		if status == 0 {
			t.Fatal("missing tool or failure reported success", m)
		}
		if _, e := ReadInfo(filepath.Join(root, "json")); e != nil {
			t.Fatal("damaged local package", e)
		}
		if m.origin {
			t.Fatal("changed remote after failed precondition")
		}
		assertNoPublish(t, m)
	}
}
func TestAuthenticationAndMultipleAccounts(t *testing.T) {
	m := &mockRunner{unauth: true}
	status, _, _, _ := runFixture(t, m, remoteArgs(), "")
	if status != 0 || m.loginCalls != 0 || m.created {
		t.Fatal("authentication without consent", status, m)
	}
	m = &mockRunner{unauth: true}
	args := append(remoteArgs(), "--allow-auth-login")
	status, out, err, _ := runFixture(t, m, args, "")
	if status != 0 || m.loginCalls != 1 || !m.origin {
		t.Fatal(status, out, err, m)
	}
	assertNoPublish(t, m)
	m = &mockRunner{multiple: true}
	status, _, _, _ = runFixture(t, m, remoteArgs(), "")
	if status != 0 || m.created {
		t.Fatal("multiple accounts not confirmed")
	}
	m = &mockRunner{multiple: true}
	status, out, err, _ = runFixture(t, m, append(remoteArgs(), "--allow-account", "alice"), "")
	if status != 0 || !m.created {
		t.Fatal(status, out, err)
	}
}
func TestOrganizationNotAssumed(t *testing.T) {
	args := remoteArgs()
	for i, v := range args {
		if v == "alice/json" {
			args[i] = "acme/json"
		}
		if v == "alice" {
			args[i] = "acme"
		}
	}
	m := &mockRunner{memberOnly: true}
	status, _, _, _ := runFixture(t, m, args, "")
	if status == 0 || m.created {
		t.Fatal("assumed organization creation rights")
	}
}
func TestInputAndLocalizedErrors(t *testing.T) {
	for _, lang := range []string{"en-US", "ja-JP", "zh-CN"} {
		var out, stderr bytes.Buffer
		status := Run("create", []string{"--non-interactive", "--namespace", "Bad", "--name", "json"}, Context{Root: t.TempDir(), Out: &out, Err: &stderr, Language: i18n.Language(lang), Runner: &mockRunner{missingGit: true, missingGh: true}})
		if status == 0 || strings.Contains(stderr.String(), "scaffold.invalid_name") {
			t.Fatal("untranslated diagnostic", lang, status, &stderr)
		}
	}
	status, _, _, root := runFixture(t, &mockRunner{missingGit: true, missingGh: true}, nil, strings.Repeat("x", 8193))
	if status == 0 {
		t.Fatal("unbounded input")
	}
	entries, _ := os.ReadDir(root)
	if len(entries) > 0 {
		t.Fatal("partial input created files")
	}
}

// Timeout testing uses an OS process with no GitHub or credentials.
func TestProcessTimeout(t *testing.T) {
	if os.Getenv("MIO_SCAFFOLD_TIMEOUT_HELPER") == "1" {
		time.Sleep(5 * time.Second)
		return
	}
	p, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("MIO_SCAFFOLD_TIMEOUT_HELPER", "1")
	start := time.Now()
	_, e = (OSRunner{}).Run(Command{Path: p, Args: []string{"-test.run=^TestProcessTimeout$"}, Timeout: 100 * time.Millisecond})
	if e == nil || time.Since(start) > 4*time.Second {
		t.Fatal("process timeout not enforced", e)
	}
}

func TestConnectExistingRepositoryAndInteractivePermission(t *testing.T) {
	root := t.TempDir()
	dir, e := Generate(root, fixture())
	if e != nil {
		t.Fatal(e)
	}
	m := &mockRunner{}
	var out, stderr bytes.Buffer
	status := Run("connect", []string{"--repo", "alice/json"}, Context{Root: dir, In: strings.NewReader("n\n"), Out: &out, Err: &stderr, Language: "en-US", Runner: m})
	if status != 0 || m.created || m.origin {
		t.Fatal("declined connect changed state", status, &out, &stderr)
	}
	status = Run("connect", []string{"--repo", "alice/json", "--non-interactive", "--allow-connect", "--allow-git-init", "--allow-git-config"}, Context{Root: dir, Out: &out, Err: &stderr, Language: "en-US", Runner: m})
	if status != 0 || m.created || !m.origin {
		t.Fatal("existing repo connection failed", status, &out, &stderr)
	}
	assertNoPublish(t, m)
}
func TestConcurrentMetadataEditIsPreserved(t *testing.T) {
	m := &mockRunner{onRemote: func(dir string) { os.WriteFile(filepath.Join(dir, InfoName), []byte("# owner edit\n"), 0600) }}
	status, _, _, root := runFixture(t, m, remoteArgs(), "")
	if status == 0 || !m.origin {
		t.Fatal("partial metadata conflict not reported")
	}
	b, e := os.ReadFile(filepath.Join(root, "json", InfoName))
	if e != nil || string(b) != "# owner edit\n" {
		t.Fatal("overwrote concurrent edit", e)
	}
}
func TestFailedAuthenticationDoesNotCreateRepository(t *testing.T) {
	m := &mockRunner{unauth: true, failLogin: true}
	status, _, _, root := runFixture(t, m, append(remoteArgs(), "--allow-auth-login"), "")
	if status == 0 || m.created || m.origin {
		t.Fatal("failed auth continued")
	}
	if _, e := ReadInfo(filepath.Join(root, "json")); e != nil {
		t.Fatal(e)
	}
}

func TestFatalAuthQueryNeverStartsLogin(t *testing.T) {
	m := &mockRunner{failAuth: true}
	status, _, _, _ := runFixture(t, m, append(remoteArgs(), "--allow-auth-login"), "")
	if status == 0 || m.loginCalls != 0 || m.created || m.origin {
		t.Fatal("fatal query initiated login or writes")
	}
}

func TestReservedNamespacesAndGitEnvironment(t *testing.T) {
	for _, test := range []struct {
		namespace, owner string
		id               int64
		allowed          bool
	}{{"bunko", "bunko", 72144156, false}, {"mio", "mio-kobo", 42, false},
		{"kiso", "mio-kobo", 72144156, true}, {"alice", "bob", 42, false}, {"alice", "alice", 42, true}} {
		if ok := namespaceOwner(test.namespace, test.owner, account{ID: test.id}) == nil; ok != test.allowed {
			t.Fatal("namespace policy mismatch", test)
		}
	}
	t.Setenv("GIT_DIR", "/fixture/unrelated")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	ctx, cancel, cmd := process(Command{Path: "git", Git: true, Args: []string{"init"}, Timeout: time.Second})
	defer cancel()
	if ctx.Err() != nil {
		t.Fatal(ctx.Err())
	}
	for _, entry := range cmd.Env {
		key, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(key), "GIT_") {
			t.Fatal("inherited Git override")
		}
	}
}

func TestEnglishReadmeAcrossAllLocales(t *testing.T) {
	catalogs, err := filepath.Glob("../../selfhost/i18n/locales/*.json")
	if err != nil || len(catalogs) != 39 {
		t.Fatal(err, len(catalogs))
	}
	var baseline []byte
	for _, catalog := range catalogs {
		locale := strings.TrimSuffix(filepath.Base(catalog), ".json")
		t.Run(locale, func(t *testing.T) {
			root := t.TempDir()
			var out, stderr bytes.Buffer
			status := Run("create", localArgs(), Context{Root: root, Language: i18n.Language(locale), Out: &out, Err: &stderr, Runner: &mockRunner{}})
			if status != 0 {
				t.Fatal(status, stderr.String())
			}
			readme, err := os.ReadFile(filepath.Join(root, "json", "README.md"))
			if err != nil {
				t.Fatal(err)
			}
			if baseline == nil {
				baseline = readme
			}
			if !bytes.Equal(readme, baseline) {
				t.Fatal("CLI locale changed the English README")
			}
			for _, required := range []string{"# alice/json", "Local fixture", "Version: **1.0.0**", "**MIT**", "linux/x86_64", "packages/alice/json/1.0.0.toml", "immutable: true", "REPLACE_WITH_REAL_POSITIVE_DECIMAL_RELEASE_ID", "mio check .", "mio test .", "python3 tools/validate.py --base upstream/main"} {
				if !strings.Contains(string(readme), required) {
					t.Errorf("missing %q", required)
				}
			}
			if strings.Contains(string(readme), "{{") {
				t.Fatal("unexpanded template")
			}
		})
	}
}

func TestWizardValidatesBeforeLaterQuestions(t *testing.T) {
	status, out, stderr, root := runFixture(t, &mockRunner{}, nil, "Bad/name\nalice/json\n\nFixture\n\n0\n1,2\n")
	if status != 0 {
		t.Fatal(out, stderr)
	}
	info, err := ReadInfo(filepath.Join(root, "json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Package.Version != "0.1.0" || info.Package.License != "MIT" || len(info.Platforms) != 2 {
		t.Fatal(info)
	}
	if !strings.Contains(stderr, "INVALID_NAME") || !strings.Contains(stderr, "INVALID_DESCRIPTION") || !strings.Contains(stderr, "INVALID_PLATFORM") {
		t.Fatal(stderr)
	}
	for _, unwanted := range []string{"gh:", "Git:", "GitHub integration?", "Exact dependencies,"} {
		if strings.Contains(out, unwanted) {
			t.Fatalf("unexpected default prompt %q", unwanted)
		}
	}
}

func TestInterruptDuringGenerationRollsBackOwnedFiles(t *testing.T) {
	root := t.TempDir()
	writes := 0
	_, err := generate(root, fixture(), func(name string, data []byte, mode os.FileMode) error {
		writes++
		if writes == 1 {
			process, e := os.FindProcess(os.Getpid())
			if e != nil {
				return e
			}
			if e = process.Signal(os.Interrupt); e != nil {
				return e
			}
			time.Sleep(30 * time.Millisecond) // Let the OS signal reach the registered channel.
		}
		return os.WriteFile(name, data, mode)
	})
	var own *Error
	if !errors.As(err, &own) || own.Kind != "cancelled" {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(root, "json")); !os.IsNotExist(err) {
		t.Fatal("interrupted project remains", err)
	}
}
