package mio_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	mio "Mio"
	"Mio/mma"
)

func testRegistry(t testing.TB) *mma.ModuleRegistry {
	t.Helper()
	registry := mma.NewModuleRegistry()
	err := registry.RegisterModule(mma.ModuleDefinition{
		Path: "test/native",
		Functions: []mma.FunctionDefinition{
			{
				Name: "add", Public: true,
				Signature: mma.FunctionSignature{Parameters: []mma.Type{mma.TypeInt64, mma.TypeInt64}, Return: mma.TypeInt64},
				Function: func(context *mma.CallContext) error {
					left, err := context.Argument(0)
					if err != nil {
						return err
					}
					right, err := context.Argument(1)
					if err != nil {
						return err
					}
					a, err := mma.Values.AsInt64(left)
					if err != nil {
						return err
					}
					b, err := mma.Values.AsInt64(right)
					if err != nil {
						return err
					}
					return context.Return(mma.Values.Int64(a + b))
				},
			},
			{
				Name: "fail", Public: true,
				Signature: mma.FunctionSignature{Return: mma.TypeUnit},
				Function: func(*mma.CallContext) error {
					return mma.Errors.New("test_failure", "测试原生错误", errors.New("cause"))
				},
			},
			{
				Name: "panic_now", Public: true,
				Signature: mma.FunctionSignature{Return: mma.TypeUnit},
				Function: func(*mma.CallContext) error {
					panic("boom")
				},
			},
			{
				Name: "hidden", Public: false,
				Signature: mma.FunctionSignature{Return: mma.TypeUnit},
				Function:  func(*mma.CallContext) error { return nil },
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func compileSource(t testing.TB, environment *mio.Environment, source string) *mio.Program {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "main.mio")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := environment.CompileFile(path, filepath.Join(directory, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	return program
}

func compileSourceError(t *testing.T, environment *mio.Environment, source string) error {
	t.Helper()
	directory := t.TempDir()
	path := filepath.Join(directory, "main.mio")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := environment.CompileFile(path, filepath.Join(directory, "bunko"))
	if err == nil {
		t.Fatal("expected compilation error")
	}
	return err
}

// 测试宿主注册、导入、静态检查和执行完整流程
func TestMMAHostIntegration(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	program := compileSource(t, environment, `
yobu "test/native";
chant(native.add(10, 20));
`)
	var output bytes.Buffer
	if err := environment.Run(program, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "30\n" {
		t.Fatalf("output = %q, want %q", output.String(), "30\\n")
	}
}

// 测试原生模块与现有 kiso/math 按需编译共同工作
func TestMMAWithKisoMath(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	directory := t.TempDir()
	path := filepath.Join(directory, "main.mio")
	source := `
yobu "test/native";
yobu "kiso/math";
chant(native.add(math.gcd(84, 30), 1));
`
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	program, err := environment.CompileFile(path, filepath.Join("bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.Run(program, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "7\n" {
		t.Fatalf("output = %q", output.String())
	}
}

// 测试原生函数参数数量和类型在编译期检查
func TestMMAStaticCallChecks(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"参数不足", `yobu "test/native"; native.add(1);`, "expects 2 arguments"},
		{"参数类型错误", `yobu "test/native"; native.add(1, hai);`, "参数类型不正确"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := compileSourceError(t, environment, test.source)
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %q, want substring %q", err, test.want)
			}
		})
	}
}

// 测试模块缺失和非公开符号诊断
func TestMMAModuleAndVisibilityErrors(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	err := compileSourceError(t, environment, `yobu "missing/native"; native.noop();`)
	if !strings.Contains(err.Error(), `"missing/native"`) {
		t.Fatalf("missing module error = %q", err)
	}
	err = compileSourceError(t, environment, `yobu "test/native"; native.hidden();`)
	if !strings.Contains(err.Error(), `does not export "hidden"`) {
		t.Fatalf("private symbol error = %q", err)
	}
}

// 测试重复模块和函数注册被拒绝
func TestMMARegistrationConflicts(t *testing.T) {
	registry := mma.NewModuleRegistry()
	noop := func(*mma.CallContext) error { return nil }
	definition := mma.ModuleDefinition{Path: "test/conflict", Functions: []mma.FunctionDefinition{
		{Name: "noop", Public: true, Signature: mma.FunctionSignature{Return: mma.TypeUnit}, Function: noop},
	}}
	if err := registry.RegisterModule(definition); err != nil {
		t.Fatal(err)
	}
	if err := registry.RegisterModule(definition); err == nil || !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("duplicate module error = %v", err)
	}
	duplicate := mma.ModuleDefinition{Path: "test/duplicate", Functions: []mma.FunctionDefinition{
		{Name: "noop", Signature: mma.FunctionSignature{Return: mma.TypeUnit}, Function: noop},
		{Name: "noop", Signature: mma.FunctionSignature{Return: mma.TypeUnit}, Function: noop},
	}}
	if err := registry.RegisterModule(duplicate); err == nil || !strings.Contains(err.Error(), "duplicate function") {
		t.Fatalf("duplicate function error = %v", err)
	}
}

// 测试注册表在编译环境之间隔离
func TestMMARegistryIsolation(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	program := compileSource(t, environment, `yobu "test/native"; chant(native.add(2, 3));`)
	var encoded bytes.Buffer
	if err := program.Encode(&encoded); err != nil {
		t.Fatal(err)
	}
	decoded, err := mio.DecodeProgram(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if err := mio.NewEnvironment(nil).Run(decoded, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "missing native binding") {
		t.Fatalf("isolated environment error = %v", err)
	}
	var output bytes.Buffer
	if err := environment.Run(decoded, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "5\n" {
		t.Fatalf("output = %q", output.String())
	}
}

// 测试原生错误和 panic 保持在 VM 错误边界内
func TestMMARuntimeErrorBoundary(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	for _, test := range []struct {
		name string
		call string
		want string
	}{
		{"错误", "fail", "test_failure"},
		{"panic", "panic_now", "native_panic"},
	} {
		t.Run(test.name, func(t *testing.T) {
			program := compileSource(t, environment, fmt.Sprintf(`yobu "test/native"; native.%s();`, test.call))
			err := environment.Run(program, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("runtime error = %v", err)
			}
		})
	}
}

// 测试 Mochi 使用稳定标识重新绑定原生函数
func TestMMAMochiRoundTripAndSignatureBinding(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	program := compileSource(t, environment, `yobu "test/native"; chant(native.add(7, 8));`)
	var encoded bytes.Buffer
	if err := program.Encode(&encoded); err != nil {
		t.Fatal(err)
	}
	decoded, err := mio.DecodeProgram(bytes.NewReader(encoded.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.Run(decoded, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "15\n" {
		t.Fatalf("output = %q", output.String())
	}

	mismatch := mma.NewModuleRegistry()
	if err := mismatch.RegisterModule(mma.ModuleDefinition{Path: "test/native", Functions: []mma.FunctionDefinition{{
		Name: "add", Public: true,
		Signature: mma.FunctionSignature{Parameters: []mma.Type{mma.TypeString, mma.TypeString}, Return: mma.TypeString},
		Function:  func(*mma.CallContext) error { return nil },
	}}}); err != nil {
		t.Fatal(err)
	}
	if err := mio.NewEnvironment(mismatch).Run(decoded, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "signature mismatch") {
		t.Fatalf("signature mismatch error = %v", err)
	}
}

// 测试未调用的原生模块不增加 Mochi 体积或触发执行
func TestMMAUnusedImportHasNoRuntimeCost(t *testing.T) {
	called := 0
	registry := mma.NewModuleRegistry()
	if err := registry.RegisterModule(mma.ModuleDefinition{Path: "test/unused", Functions: []mma.FunctionDefinition{{
		Name: "touch", Public: true, Signature: mma.FunctionSignature{Return: mma.TypeUnit},
		Function: func(*mma.CallContext) error { called++; return nil },
	}}}); err != nil {
		t.Fatal(err)
	}
	environment := mio.NewEnvironment(registry)
	withImport := compileSource(t, environment, `yobu "test/unused"; chant(1);`)
	withoutImport := compileSource(t, environment, `chant(1);`)
	var importedBytes, plainBytes bytes.Buffer
	if err := withImport.Encode(&importedBytes); err != nil {
		t.Fatal(err)
	}
	if err := withoutImport.Encode(&plainBytes); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(importedBytes.Bytes(), plainBytes.Bytes()) {
		t.Fatalf("unused import changed Mochi size: %d != %d", importedBytes.Len(), plainBytes.Len())
	}
	if err := environment.Run(withImport, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("unused native function called %d times", called)
	}
}

// 测试原生调用基础开销
func BenchmarkMMANativeCall(b *testing.B) {
	environment := mio.NewEnvironment(testRegistry(b))
	program := compileSource(b, environment, `yobu "test/native"; native.add(10, 20);`)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := environment.Run(program, &bytes.Buffer{}); err != nil {
			b.Fatal(err)
		}
	}
}

// 测试模块注册成本
func BenchmarkMMARegistration(b *testing.B) {
	noop := func(*mma.CallContext) error { return nil }
	b.ReportAllocs()
	for index := range b.N {
		registry := mma.NewModuleRegistry()
		if err := registry.RegisterModule(mma.ModuleDefinition{
			Path:      "bench/module" + strconv.Itoa(index),
			Functions: []mma.FunctionDefinition{{Name: "noop", Public: true, Signature: mma.FunctionSignature{Return: mma.TypeUnit}, Function: noop}},
		}); err != nil {
			b.Fatal(err)
		}
	}
}

// 测试原生依赖的 Mochi 体积
func BenchmarkMMANativeBytecodeSize(b *testing.B) {
	environment := mio.NewEnvironment(testRegistry(b))
	program := compileSource(b, environment, `yobu "test/native"; native.add(10, 20);`)
	var encoded bytes.Buffer
	if err := program.Encode(&encoded); err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(encoded.Len()), "Mochi-bytes")
	for range b.N {
		_ = encoded.Len()
	}
}
