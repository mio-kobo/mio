package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyTemplate copies templates/<name> into a directory whose path contains a
// space and Japanese characters, so path handling is part of the check.
func copyTemplate(t *testing.T, name string) string {
	t.Helper()
	source := filepath.Join("..", "..", "templates", name)
	target := filepath.Join(t.TempDir(), "テンプレート dir", name)
	err := filepath.WalkDir(source, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, _ := filepath.Rel(source, path)
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(destination, data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func runTemplateCommand(t *testing.T, bunkoRoot string, args ...string) (int, string, string) {
	t.Helper()
	if bunkoRoot == "" {
		t.Setenv("MIO_BUNKO", "")
		os.Unsetenv("MIO_BUNKO")
	} else {
		t.Setenv("MIO_BUNKO", bunkoRoot)
	}
	var stdout, stderr bytes.Buffer
	code := runCLI(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

// The cli template reads kiso/os arguments, which only the native compiler
// supports; it is verified against the native CLI by scripts/templates/verify.py.
func TestProjectTemplates(t *testing.T) {
	bunko, err := filepath.Abs(filepath.Join("..", "..", "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		template string
		bunko    string
		args     []string
		want     []string
	}{
		{"hello", "", []string{"run", "main.mio"}, []string{"Hello, Mio!"}},
		{"multi-file", "", []string{"run", "main.mio"}, []string{"12"}},
		{"library", "", []string{"run", "main.mio"}, []string{"10"}},
		{"library", bunko, []string{"test", "mathx", "--format=short"}, []string{"mathx:test_clamp_inside_range: PASS", "mathx:test_clamp_outside_range: PASS", "2 passed, 0 failed"}},
		{"unit-test", bunko, []string{"test", "tests", "--format=short"}, []string{"tests:test_double: PASS", "tests:test_double_zero: PASS", "2 passed, 0 failed"}},
		{"stdlib-app", bunko, []string{"run", "main.mio"}, []string{"readme.mio", ".mio", "hai"}},
	}
	for _, tc := range cases {
		t.Run(tc.template+"/"+tc.args[0], func(t *testing.T) {
			dir := copyTemplate(t, tc.template)
			args := append([]string{}, tc.args...)
			// Source files and test directories are given relative to the template.
			for i := 1; i < len(args); i++ {
				if !strings.HasPrefix(args[i], "-") {
					args[i] = filepath.Join(dir, args[i])
				}
			}
			code, stdout, stderr := runTemplateCommand(t, tc.bunko, args...)
			if code != 0 {
				t.Fatalf("exit %d\nstdout=%q\nstderr=%q", code, stdout, stderr)
			}
			for _, want := range tc.want {
				if !strings.Contains(stdout, want) {
					t.Errorf("stdout missing %q: %q", want, stdout)
				}
			}
		})
	}
}

// Every official template must already be formatted by `mio fmt`, so a project created
// from one starts clean and later formatter changes cannot silently desynchronize them.
func TestProjectTemplatesAreFormatted(t *testing.T) {
	entries, err := os.ReadDir(filepath.Join("..", "..", "templates"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		checked++
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			dir := copyTemplate(t, name)
			var stdout, stderr bytes.Buffer
			if code := runCLI([]string{"fmt", "--check", dir}, &stdout, &stderr); code != 0 {
				t.Fatalf("template %s is not formatted (exit %d)\nstdout: %s\nstderr: %s", name, code, stdout.String(), stderr.String())
			}
		})
	}
	if checked == 0 {
		t.Fatal("no templates found")
	}
}
