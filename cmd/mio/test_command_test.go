package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeCLITestPackage(t *testing.T, testSource string) (string, string) {
	t.Helper()
	root := t.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	testingDirectory := filepath.Join(bunkoRoot, "kiso", "testing")
	if err := os.MkdirAll(testingDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	module := `yobu "mio/testing/runtime";
koukai waza expect(condition: bool, message: string) -> unit { runtime.report(condition, message); }
koukai waza fail(message: string) -> unit { runtime.report(iie, message); }`
	if err := os.WriteFile(filepath.Join(testingDirectory, "testing.mio"), []byte(module), 0644); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(root, "sample")
	if err := os.MkdirAll(packagePath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packagePath, "sample.mio"), []byte(`waza hidden() -> int64 { kaesu 42; }`), 0644); err != nil {
		t.Fatal(err)
	}
	if testSource != "" {
		if err := os.WriteFile(filepath.Join(packagePath, "sample_test.mio"), []byte(testSource), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return packagePath, bunkoRoot
}

// 测试三种语言和语言选择优先级
func TestCLITestLanguages(t *testing.T) {
	packagePath, bunkoRoot := writeCLITestPackage(t, `
yobu "kiso/testing";
waza test_ok() -> unit { testing.expect(hidden() == 42, "ok"); }
`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want string
	}{
		{name: "中文", args: []string{"test", packagePath, "--lang=zh-CN"}, want: "测试完成：1 个通过，0 个失败"},
		{name: "日文", args: []string{"test", packagePath, "--lang=ja-JP"}, want: "テスト完了：1 件成功、0 件失敗"},
		{name: "英文", args: []string{"test", packagePath, "--lang=en-US"}, want: "Tests complete: 1 passed, 0 failed"},
		{name: "环境变量", args: []string{"test", packagePath}, env: map[string]string{"MIO_LANG": "ja_JP.UTF-8", "LC_ALL": "zh_CN.UTF-8"}, want: "テスト完了"},
		{name: "系统 Locale", args: []string{"test", packagePath}, env: map[string]string{"MIO_LANG": "", "LC_ALL": "zh_CN.UTF-8"}, want: "测试完成"},
		{name: "显式优先", args: []string{"test", packagePath, "--lang=en-US"}, env: map[string]string{"MIO_LANG": "ja-JP", "LC_ALL": "zh_CN.UTF-8"}, want: "Tests complete"},
		{name: "未知语言", args: []string{"test", packagePath, "--lang=unknown"}, want: "Tests complete"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("MIO_LANG", "")
			t.Setenv("LC_ALL", "")
			t.Setenv("LC_MESSAGES", "")
			t.Setenv("LANG", "")
			for name, value := range test.env {
				t.Setenv(name, value)
			}
			var stdout, stderr bytes.Buffer
			if exit := runCLI(test.args, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), test.want) {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
			}
		})
	}
}

// 测试失败续跑和 JSON 稳定字段
func TestCLITestJSONFailureReport(t *testing.T) {
	packagePath, bunkoRoot := writeCLITestPackage(t, `
yobu "kiso/testing";
waza test_fail() -> unit { testing.fail("custom message"); }
waza test_after() -> unit { testing.expect(hai, "runs"); }
`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	exit := runCLI([]string{"test", packagePath, "--format=json", "--lang=ja-JP"}, &stdout, &stderr)
	if exit != 1 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["schema_version"] != float64(1) {
		t.Fatalf("report=%s", stdout.String())
	}
	packages := report["packages"].([]any)
	tests := packages[0].(map[string]any)["tests"].([]any)
	if len(tests) != 2 || tests[0].(map[string]any)["status"] != "failed" || tests[1].(map[string]any)["status"] != "passed" {
		t.Fatalf("report=%s", stdout.String())
	}
	failures := tests[0].(map[string]any)["failures"].([]any)
	if failures[0].(map[string]any)["code"] != "E0602" || failures[0].(map[string]any)["message"] != "custom message" {
		t.Fatalf("report=%s", stdout.String())
	}
}

// 测试普通命令忽略测试文件并拒绝直接运行测试文件
func TestCLIRegularCommandsIgnoreTestSources(t *testing.T) {
	packagePath, _ := writeCLITestPackage(t, `waza test_bad() -> unit { missing(); }`)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"check", packagePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("check exit=%d stderr=%q", exit, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	testPath := filepath.Join(packagePath, "sample_test.mio")
	if exit := runCLI([]string{"run", testPath}, &stdout, &stderr); exit == 0 || !strings.Contains(stderr.String(), "mio test") {
		t.Fatalf("run exit=%d stderr=%q", exit, stderr.String())
	}
	mochiPath := filepath.Join(t.TempDir(), "sample.mochi")
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"build", "-o", mochiPath, packagePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%q", exit, stderr.String())
	}
	bytecode, err := decodeMochiFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, function := range bytecode.Functions {
		if strings.HasPrefix(function.Name, "test_") {
			t.Fatalf("test function entered Mochi: %s", function.Name)
		}
	}
}

// 测试无测试包的明确提示
func TestCLITestNoTests(t *testing.T) {
	packagePath, bunkoRoot := writeCLITestPackage(t, "")
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"test", packagePath, "--lang=zh-CN"}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "没有发现测试") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

// 测试名称筛选的多语言提示和执行结果
func TestCLITestRunFilter(t *testing.T) {
	packagePath, bunkoRoot := writeCLITestPackage(t, `
yobu "kiso/testing";
waza test_alpha() -> unit { testing.fail("not selected"); }
waza test_gcd() -> unit { testing.expect(hai, "selected"); }
`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"test", packagePath, "--run=^test_gcd$", "--lang=zh-CN"}, &stdout, &stderr); exit != 0 || strings.Contains(stdout.String(), "test_alpha") || !strings.Contains(stdout.String(), "test_gcd") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}

	for _, test := range []struct {
		language string
		want     string
	}{
		{language: "zh-CN", want: "没有匹配 --run=missing 的测试"},
		{language: "ja-JP", want: "--run=missing に一致するテストがありません"},
		{language: "en-US", want: "no tests matched --run=missing"},
	} {
		stdout.Reset()
		stderr.Reset()
		exit := runCLI([]string{"test", packagePath, "--run=missing", "--lang=" + test.language}, &stdout, &stderr)
		if exit != 0 || !strings.Contains(stdout.String(), test.want) {
			t.Fatalf("language=%s exit=%d stdout=%q stderr=%q", test.language, exit, stdout.String(), stderr.String())
		}
	}
}

// 测试名称筛选保持 JSON 字段和空结果状态
func TestCLITestRunFilterJSON(t *testing.T) {
	packagePath, bunkoRoot := writeCLITestPackage(t, `waza test_ok() -> unit {}`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"test", packagePath, "--run=missing", "--format=json"}, &stdout, &stderr); exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	summary := report["summary"].(map[string]any)
	packages := report["packages"].([]any)
	if report["schema_version"] != float64(1) || summary["status"] != "no_tests" || len(packages[0].(map[string]any)["tests"].([]any)) != 0 {
		t.Fatalf("report=%s", stdout.String())
	}
}

// 测试编译失败使用所选语言并返回非零状态
func TestCLITestLocalizedCompileFailure(t *testing.T) {
	packagePath, bunkoRoot := writeCLITestPackage(t, `waza test_bad(value: int64) -> unit {}`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"test", packagePath, "--lang=ja-JP"}, &stdout, &stderr); exit != 1 || !strings.Contains(stderr.String(), "テスト関数のシグネチャが無効です") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

// 测试递归发现排除隐藏目录并保持排序
func TestDiscoverTestPackages(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"b/sample_test.mio", "a/sample_test.mio", ".cache/ignored_test.mio", "build/ignored_test.mio"} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(""), 0644); err != nil {
			t.Fatal(err)
		}
	}
	packages, err := discoverTestPackages(filepath.Join(root, "..."))
	if err != nil {
		t.Fatal(err)
	}
	if len(packages) != 2 || filepath.Base(packages[0]) != "a" || filepath.Base(packages[1]) != "b" {
		t.Fatalf("packages=%v", packages)
	}
}

// 测试单包发现开销
func BenchmarkDiscoverTestPackages(b *testing.B) {
	root := b.TempDir()
	if err := os.WriteFile(filepath.Join(root, "sample_test.mio"), []byte(""), 0644); err != nil {
		b.Fatal(err)
	}
	target := filepath.Join(root, "...")
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := discoverTestPackages(target); err != nil {
			b.Fatal(err)
		}
	}
}
