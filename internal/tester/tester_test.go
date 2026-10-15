package tester

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/vm"
	"Mio/mma"
)

func writeTestPackage(t testing.TB, normal, tests string) (string, string) {
	t.Helper()
	root := t.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	testingDirectory := filepath.Join(bunkoRoot, "kiso", "testing")
	if err := os.MkdirAll(testingDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	testingSource := `yobu "mio/testing/runtime";
koukai waza expect(condition: bool, message: string) -> unit { runtime.report(condition, message); }
koukai waza fail(message: string) -> unit { runtime.report(iie, message); }`
	if err := os.WriteFile(filepath.Join(testingDirectory, "testing.mio"), []byte(testingSource), 0644); err != nil {
		t.Fatal(err)
	}
	packagePath := filepath.Join(root, "sample")
	if err := os.MkdirAll(packagePath, 0755); err != nil {
		t.Fatal(err)
	}
	if normal != "" {
		if err := os.WriteFile(filepath.Join(packagePath, "sample.mio"), []byte(normal), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if tests != "" {
		if err := os.WriteFile(filepath.Join(packagePath, "sample_test.mio"), []byte(tests), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return packagePath, bunkoRoot
}

// 测试断言失败不会阻止后续测试
func TestRunPackageRecordsFailuresAndContinues(t *testing.T) {
	packagePath, bunkoRoot := writeTestPackage(t, `waza hidden() -> int64 { kaesu 42; }`, `
yobu "kiso/testing";
waza test_fail() -> unit {
    testing.fail("first");
    testing.expect(iie, "second");
}
waza test_pass() -> unit { testing.expect(hidden() == 42, "private helper"); }
`)
	result, err := RunPackage(packagePath, bunkoRoot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tests) != 2 || result.Tests[0].Name != "test_fail" || result.Tests[1].Name != "test_pass" {
		t.Fatalf("unexpected order: %#v", result.Tests)
	}
	if result.Tests[0].Passed || len(result.Tests[0].Failures) != 2 || !result.Tests[1].Passed {
		t.Fatalf("unexpected results: %#v", result.Tests)
	}
}

// 测试每个测试拥有独立 Globals 和可变对象
func TestRunPackageIsolatesRuntimeState(t *testing.T) {
	packagePath, bunkoRoot := writeTestPackage(t, `
kaku count = 0;
kaku values = [0];
`, `
yobu "kiso/testing";
waza test_first() -> unit {
    count = count + 1;
    values[0] = values[0] + 1;
    testing.expect(count == 1 && values[0] == 1, "first isolated state");
}
waza test_second() -> unit {
    count = count + 1;
    values[0] = values[0] + 1;
    testing.expect(count == 1 && values[0] == 1, "second isolated state");
}
`)
	result, err := RunPackage(packagePath, bunkoRoot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range result.Tests {
		if !test.Passed {
			t.Fatalf("%s failed: %#v", test.Name, test)
		}
	}
}

// 测试运行时错误和资源限制只终止当前测试
func TestRunPackageRuntimeFailures(t *testing.T) {
	packagePath, bunkoRoot := writeTestPackage(t, "", `
yobu "kiso/testing";
waza test_bounds() -> unit { kaku values = [1]; values[2]; }
waza test_limit() -> unit { aida (hai) {} }
waza test_after() -> unit { testing.expect(hai, "still runs"); }
`)
	result, err := RunPackage(packagePath, bunkoRoot, Options{InstructionLimit: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Tests) != 3 || result.Tests[0].RuntimeErr == nil || !result.Tests[1].Resource || !result.Tests[2].Passed {
		t.Fatalf("unexpected results: %#v", result.Tests)
	}
}

// 测试无效测试签名产生稳定诊断
func TestRunPackageRejectsInvalidSignature(t *testing.T) {
	for name, source := range map[string]string{
		"parameter": `waza test_bad(value: int64) -> unit {}`,
		"return":    `waza test_bad() -> int64 { kaesu 1; }`,
		"generic":   `waza test_bad<T>() -> unit {}`,
	} {
		t.Run(name, func(t *testing.T) {
			packagePath, bunkoRoot := writeTestPackage(t, "", source)
			_, err := RunPackage(packagePath, bunkoRoot, Options{})
			if err == nil || !strings.Contains(err.Error(), "E0601") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// 测试多个测试文件按文件名稳定加载
func TestRunPackageUsesStableFileOrder(t *testing.T) {
	packagePath, bunkoRoot := writeTestPackage(t, "", `waza test_middle() -> unit {}`)
	if err := os.WriteFile(filepath.Join(packagePath, "a_test.mio"), []byte(`waza test_first() -> unit {}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packagePath, "z_test.mio"), []byte(`waza test_last() -> unit {}`), 0644); err != nil {
		t.Fatal(err)
	}
	result, err := RunPackage(packagePath, bunkoRoot, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"test_first", "test_middle", "test_last"}
	for index, name := range want {
		if result.Tests[index].Name != name {
			t.Fatalf("order=%#v", result.Tests)
		}
	}
}

// 测试名称筛选只影响执行并保留完整编译检查
func TestRunPackageFiltersAfterCompilation(t *testing.T) {
	packagePath, bunkoRoot := writeTestPackage(t, "", `
waza test_alpha() -> unit {}
waza test_beta() -> unit {}
`)
	result, err := RunPackage(packagePath, bunkoRoot, Options{RunPattern: "^test_beta$"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Discovered != 2 || len(result.Tests) != 1 || result.Tests[0].Name != "test_beta" {
		t.Fatalf("result=%#v", result)
	}

	invalidPath, invalidBunkoRoot := writeTestPackage(t, "", `
waza test_selected() -> unit {}
waza test_invalid(value: int64) -> unit {}
`)
	if _, err := RunPackage(invalidPath, invalidBunkoRoot, Options{RunPattern: "^test_selected$"}); err == nil || !strings.Contains(err.Error(), "E0601") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 测试非法名称筛选返回明确错误
func TestRunPackageRejectsInvalidRunPattern(t *testing.T) {
	packagePath, bunkoRoot := writeTestPackage(t, "", `waza test_ok() -> unit {}`)
	if _, err := RunPackage(packagePath, bunkoRoot, Options{RunPattern: "["}); err == nil || !strings.Contains(err.Error(), "invalid --run pattern") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 测试 MMA 报告器绑定到单个记录器
func TestTestRegistryReporter(t *testing.T) {
	recorder := &Recorder{}
	registry, err := NewRegistry(recorder)
	if err != nil {
		t.Fatal(err)
	}
	function, ok := registry.Resolve(mma.FunctionID{Module: runtimeModule, Name: "report"})
	if !ok {
		t.Fatal("report function not registered")
	}
	_, _, err = mma.Invoke(function.Function, []mma.Value{mma.Values.Bool(false), mma.Values.String("kept")}, function.Signature)
	if err != nil || len(recorder.failures) != 1 || recorder.failures[0].Message != "kept" {
		t.Fatalf("failures=%#v err=%v", recorder.failures, err)
	}
}

func benchmarkHundredTestPackage(b *testing.B) (string, string) {
	b.Helper()
	var source strings.Builder
	source.WriteString("yobu \"kiso/testing\";\n")
	for index := 0; index < 100; index++ {
		fmt.Fprintf(&source, "waza test_%03d() -> unit { testing.expect(hai, \"ok\"); }\n", index)
	}
	return writeTestPackage(b, "", source.String())
}

// 测试测试模块编译开销
func BenchmarkCompileTestPackage(b *testing.B) {
	packagePath, bunkoRoot := benchmarkHundredTestPackage(b)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, _, err := compilePackage(packagePath, bunkoRoot); err != nil {
			b.Fatal(err)
		}
	}
}

// 测试一百个轻量测试的执行开销
func BenchmarkExecuteHundredTests(b *testing.B) {
	packagePath, bunkoRoot := benchmarkHundredTestPackage(b)
	base, tests, err := compilePackage(packagePath, bunkoRoot)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		for _, test := range tests {
			if result := runTest(base, test, Options{Output: io.Discard}); !result.Passed {
				b.Fatal(result.RuntimeErr)
			}
		}
	}
}

// 测试单个测试 VM 的初始化开销
func BenchmarkTestVMInitialization(b *testing.B) {
	packagePath, bunkoRoot := benchmarkHundredTestPackage(b)
	base, tests, err := compilePackage(packagePath, bunkoRoot)
	if err != nil {
		b.Fatal(err)
	}
	bytecode, err := entryBytecode(base, tests[0].Function.Name.Name)
	if err != nil {
		b.Fatal(err)
	}
	recorder := &Recorder{}
	registry, err := NewRegistry(recorder)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		_ = vm.NewWithRegistryAndOutput(bytecode, registry, io.Discard)
	}
}

// 测试报告器分配成本
func BenchmarkRecorder(b *testing.B) {
	b.ReportAllocs()
	for index := 0; index < b.N; index++ {
		recorder := &Recorder{}
		recorder.failures = append(recorder.failures, Failure{Message: "failure"})
	}
}
