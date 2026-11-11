package main

import (
	"Mio/internal/bunkoscaffold"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBunkoScaffoldGeneratedPackage(t *testing.T) {
	root, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	stdlib, e := filepath.Abs("../../bunko")
	if e != nil {
		t.Fatal(e)
	}
	defer os.Chdir(root)
	work := t.TempDir()
	if e = os.Chdir(work); e != nil {
		t.Fatal(e)
	}
	t.Setenv("MIO_BUNKO", stdlib)
	t.Setenv("PATH", t.TempDir())
	var out, stderr bytes.Buffer
	args := []string{"bunko", "create", "--non-interactive", "--namespace", "alice", "--name", "json", "--description", "Fixture source package", "--version", "1.0.0", "--license", "MIT", "--platform", "linux/x86_64"}
	if status := runCLI(args, &out, &stderr); status != 0 {
		t.Fatal(status, &out, &stderr)
	}
	for _, command := range [][]string{{"check", filepath.Join(work, "json")}, {"test", filepath.Join(work, "json"), "--format=json"}} {
		out.Reset()
		stderr.Reset()
		if status := runCLI(command, &out, &stderr); status != 0 {
			t.Fatal(command, status, &out, &stderr)
		}
		if command[0] == "test" && !strings.Contains(out.String(), `"passed":1`) {
			t.Fatal("generated test did not execute", &out)
		}
	}
}
func TestBunkoScaffoldLocalizedHelp(t *testing.T) {
	for _, lang := range []string{"en-US", "ja-JP", "zh-CN"} {
		for _, command := range []string{"create", "connect"} {
			var out, stderr bytes.Buffer
			if status := runCLI([]string{"--lang", lang, "--color=always", "bunko", command, "--help"}, &out, &stderr); status != 0 || !strings.Contains(out.String(), "--non-interactive") {
				t.Fatal(status, &out, &stderr)
			}
		}
	}
}

func TestBunkoScaffoldLiteralOptionValues(t *testing.T) {
	root, e := os.Getwd()
	if e != nil {
		t.Fatal(e)
	}
	defer os.Chdir(root)
	work := t.TempDir()
	os.Chdir(work)
	t.Setenv("PATH", t.TempDir())
	for at, description := range []string{"--lang", "--help", "--theme=dark"} {
		var out, stderr bytes.Buffer
		name := fmt.Sprintf("literal%d", at)
		args := []string{"bunko", "create", "--non-interactive", "--namespace", "alice", "--name", name, "--description", description, "--platform", "linux/x86_64"}
		if code := runCLI(args, &out, &stderr); code != 0 {
			t.Fatal(code, &out, &stderr)
		}
		info, e := bunkoscaffold.ReadInfo(filepath.Join(work, name))
		if e != nil || info.Package.Description != description {
			t.Fatal(info, e)
		}
	}
}
