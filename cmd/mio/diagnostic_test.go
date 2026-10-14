package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 测试 CLI 诊断格式与退出码
func TestCLIDiagnosticFormats(t *testing.T) {
	path := filepath.Join(t.TempDir(), "错误.mio")
	if err := os.WriteFile(path, []byte("kaku 値 = 1;\r\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runCLI([]string{"check", path, "--format=short", "--color=never"}, &stdout, &stderr)
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "error[E0001]") || strings.Contains(stderr.String(), "\x1b[") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}

	stderr.Reset()
	code = runCLI([]string{"check", "--format=json", path}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	var value map[string]any
	if err := json.Unmarshal(stderr.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value["code"] == "" || value["path"] != path {
		t.Fatalf("JSON 字段错误: %s", stderr.String())
	}
}

// 测试语法错误使用统一诊断且默认不带颜色
func TestCLISyntaxDiagnostic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "syntax.mio")
	if err := os.WriteFile(path, []byte("kaku n = 1"), 0644); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runCLI([]string{"check", path}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stderr.String(), "error[E0101]") || !strings.Contains(stderr.String(), path+":1:11") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if strings.Contains(stderr.String(), "\x1b[") {
		t.Fatal("重定向输出不应包含 ANSI 颜色")
	}
}
