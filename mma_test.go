package mio_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	mio "Mio"
	"Mio/mma"
)

// 测试资源句柄隔离、类型检查、关闭共享和槽位代次
func TestMMAResourceHandleLifecycle(t *testing.T) {
	first := mma.NewResourceManager()
	second := mma.NewResourceManager()
	var closed atomic.Int32
	handle, err := first.Create("test.file", "payload", func(any) error {
		closed.Add(1)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	alias := handle
	if value, err := first.Use(handle, "test.file"); err != nil || value != "payload" {
		t.Fatalf("value=%v error=%v", value, err)
	}
	if _, err := first.Use(handle, "test.socket"); err == nil || !strings.Contains(err.Error(), "resource_type") {
		t.Fatalf("type error=%v", err)
	}
	if _, err := second.Use(handle, "test.file"); err == nil || !strings.Contains(err.Error(), "invalid_resource") {
		t.Fatalf("isolation error=%v", err)
	}
	if err := first.Close(alias, "test.file"); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 1 {
		t.Fatalf("close count=%d", closed.Load())
	}
	if _, err := first.Use(handle, "test.file"); err == nil || !strings.Contains(err.Error(), "closed_resource") {
		t.Fatalf("closed error=%v", err)
	}
	replacement, err := first.Create("test.file", "new", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Use(handle, "test.file"); err == nil {
		t.Fatal("旧句柄在槽位复用后复活")
	}
	if value, err := first.Use(replacement, "test.file"); err != nil || value != "new" {
		t.Fatalf("replacement=%v error=%v", value, err)
	}
}

// 测试 CallContext 只能通过当前 Runtime 创建和使用资源
func TestMMAResourceCallContext(t *testing.T) {
	manager := mma.NewResourceManager()
	createSignature := mma.FunctionSignature{Return: mma.TypeResource}
	returned, ok, err := mma.InvokeWithResources(func(call *mma.CallContext) error {
		resource, err := call.CreateResource("test.value", int64(42), nil)
		if err != nil {
			return err
		}
		return call.Return(resource)
	}, nil, createSignature, manager)
	if err != nil || !ok {
		t.Fatalf("create ok=%v error=%v", ok, err)
	}
	useSignature := mma.FunctionSignature{Parameters: []mma.Type{mma.TypeResource}, Return: mma.TypeInt64}
	result, ok, err := mma.InvokeWithResources(func(call *mma.CallContext) error {
		value, err := call.UseResource(0, "test.value")
		if err != nil {
			return err
		}
		return call.Return(mma.Values.Int64(value.(int64)))
	}, []mma.Value{returned}, useSignature, manager)
	if err != nil || !ok {
		t.Fatalf("use ok=%v error=%v", ok, err)
	}
	value, err := mma.Values.AsInt64(result)
	if err != nil || value != 42 {
		t.Fatalf("value=%d error=%v", value, err)
	}
	if _, _, err := mma.Invoke(func(call *mma.CallContext) error {
		_, err := call.CreateResource("test.value", 1, nil)
		return err
	}, nil, createSignature); err == nil {
		t.Fatal("缺少 Runtime 资源表时仍能创建资源")
	}
}

// 测试 Runtime 清理未显式关闭的资源
func TestMMAResourceCleanup(t *testing.T) {
	manager := mma.NewResourceManager()
	var closed atomic.Int32
	for range 100 {
		if _, err := manager.Create("test.file", struct{}{}, func(any) error {
			closed.Add(1)
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := manager.Cleanup(); err != nil {
		t.Fatal(err)
	}
	if closed.Load() != 100 {
		t.Fatalf("close count=%d", closed.Load())
	}
}

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
				Name: "byte_sum", Public: true,
				Signature: mma.FunctionSignature{Parameters: []mma.Type{mma.TypeBytes}, Return: mma.TypeInt64},
				Function: func(context *mma.CallContext) error {
					data, err := mma.Values.AsBytes(mustMMAArgument(context, 0))
					if err != nil {
						return err
					}
					var sum int64
					for _, item := range data {
						sum += int64(item)
					}
					return context.Return(mma.Values.Int64(sum))
				},
			},
			{
				Name: "byte_make", Public: true,
				Signature: mma.FunctionSignature{Parameters: []mma.Type{mma.TypeInt64}, Return: mma.TypeBytes},
				Function: func(context *mma.CallContext) error {
					count, err := mma.Values.AsInt64(mustMMAArgument(context, 0))
					if err != nil {
						return err
					}
					data := make([]byte, count)
					for index := range data {
						data[index] = byte(index + 1)
					}
					if err := context.Return(mma.Values.Bytes(data)); err != nil {
						return err
					}
					clear(data)
					return nil
				},
			},
			{
				Name: "snapshot_then_write", Public: true,
				Signature: mma.FunctionSignature{Parameters: []mma.Type{mma.TypeBytes}, Return: mma.TypeBytes},
				Function: func(context *mma.CallContext) error {
					snapshot, err := context.ReadBytes(0, 0, 1)
					if err != nil {
						return err
					}
					if err := context.WriteBytes(0, 0, []byte{99}); err != nil {
						return err
					}
					return context.Return(mma.Values.Bytes(snapshot))
				},
			},
			{
				Name: "write_then_fail", Public: true,
				Signature: mma.FunctionSignature{Parameters: []mma.Type{mma.TypeBytes}, Return: mma.TypeUnit},
				Function: func(context *mma.CallContext) error {
					if err := context.WriteBytes(0, 0, []byte{77}); err != nil {
						return err
					}
					return mma.Errors.New("after_write", "写入后失败", nil)
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

func mustMMAArgument(context *mma.CallContext, index int) mma.Value {
	argument, err := context.Argument(index)
	if err != nil {
		panic(err)
	}
	return argument
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

// 测试 MMA 字节快照、显式写回、别名和独立返回值
func TestMMAByteArrayBoundary(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	program := compileSource(t, environment, `
yobu "test/native";
kaku data: []uint8 = [1, 2, 3];
kaku alias = data;
chant(native.byte_sum(data));
kaku snapshot = native.snapshot_then_write(data);
chant(data[0]); chant(alias[0]); chant(snapshot[0]);
kaku made = native.byte_make(3);
chant(made[0]); chant(made[1]); chant(made[2]);
`)
	var output bytes.Buffer
	if err := environment.Run(program, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "6\n99\n99\n1\n1\n2\n3\n" {
		t.Fatalf("output=%q", output.String())
	}
}

// 测试写入成功后发生错误时保留已完成修改
func TestMMAByteArrayWritePersistsOnError(t *testing.T) {
	registry := testRegistry(t)
	functions, _ := registry.Module("test/native")
	var function mma.RegisteredFunction
	for _, candidate := range functions {
		if candidate.ID.Name == "write_then_fail" {
			function = candidate
		}
	}
	active := true
	data := []byte{1}
	argument := mma.Values.BorrowBytes(int64(len(data)), func(offset, length int64) ([]byte, error) {
		return append([]byte(nil), data[offset:offset+length]...), nil
	}, func(offset int64, source []byte) error {
		copy(data[offset:], source)
		return nil
	}, &active)
	_, _, err := mma.Invoke(function.Function, []mma.Value{argument}, function.Signature)
	active = false
	if err == nil || !strings.Contains(err.Error(), "after_write") || data[0] != 77 {
		t.Fatalf("error=%v data=%v", err, data)
	}
}

// 测试字节范围检查拒绝负数、越界和加法溢出
func TestMMAByteArrayRangeValidation(t *testing.T) {
	signature := mma.FunctionSignature{Parameters: []mma.Type{mma.TypeBytes}, Return: mma.TypeUnit}
	for _, test := range []struct{ offset, length int64 }{{-1, 0}, {0, -1}, {2, 0}, {1, 1}, {1<<63 - 1, 1}} {
		_, _, err := mma.Invoke(func(context *mma.CallContext) error {
			_, readErr := context.ReadBytes(0, test.offset, test.length)
			return readErr
		}, []mma.Value{mma.Values.Bytes([]byte{1})}, signature)
		if err == nil {
			t.Fatalf("offset=%d length=%d unexpectedly succeeded", test.offset, test.length)
		}
	}
	active := true
	called := false
	borrowed := mma.Values.BorrowBytes(1, nil, func(offset int64, source []byte) error {
		called = true
		return nil
	}, &active)
	_, _, err := mma.Invoke(func(context *mma.CallContext) error {
		return context.WriteBytes(0, 1<<63-1, []byte{1})
	}, []mma.Value{borrowed}, signature)
	if err == nil || called {
		t.Fatalf("write overflow error=%v called=%v", err, called)
	}
}

// 测试原生调用结束后上下文和借用数组失效
func TestMMAByteArrayCallLifetime(t *testing.T) {
	var savedContext *mma.CallContext
	var savedValue mma.Value
	active := true
	argument := mma.Values.BorrowBytes(1, func(offset, length int64) ([]byte, error) {
		return []byte{1}, nil
	}, nil, &active)
	_, _, err := mma.Invoke(func(context *mma.CallContext) error {
		savedContext = context
		savedValue = mustMMAArgument(context, 0)
		return nil
	}, []mma.Value{argument}, mma.FunctionSignature{Parameters: []mma.Type{mma.TypeBytes}, Return: mma.TypeUnit})
	active = false
	if err != nil {
		t.Fatal(err)
	}
	if _, err := savedContext.Argument(0); err == nil {
		t.Fatal("调用结束后上下文仍可访问")
	}
	if _, err := mma.Values.AsBytes(savedValue); err == nil {
		t.Fatal("调用结束后借用数组仍可访问")
	}
}

// 测试字节数组签名在编译期检查
func TestMMAByteArrayStaticChecks(t *testing.T) {
	environment := mio.NewEnvironment(testRegistry(t))
	for _, source := range []string{
		`yobu "test/native"; native.byte_sum("bytes");`,
		`yobu "test/native"; kaku result: int64 = native.byte_make(2);`,
	} {
		if err := compileSourceError(t, environment, source); err == nil {
			t.Fatal("expected static type error")
		}
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

// 测试资源句柄创建和关闭成本
func BenchmarkMMAResourceHandle(b *testing.B) {
	manager := mma.NewResourceManager()
	b.ReportAllocs()
	for range b.N {
		handle, err := manager.Create("bench.resource", struct{}{}, nil)
		if err != nil {
			b.Fatal(err)
		}
		if err := manager.Close(handle, "bench.resource"); err != nil {
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

var benchmarkMMABytes []byte

// BenchmarkMMAByteArraySnapshot 测量字节数组完整快照成本
func BenchmarkMMAByteArraySnapshot(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			data := make([]byte, size)
			value := mma.Values.Bytes(data)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for range b.N {
				result, err := mma.Values.AsBytes(value)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkMMABytes = result
			}
		})
	}
}

// BenchmarkMMAByteArrayRange 测量范围快照和写回成本
func BenchmarkMMAByteArrayRange(b *testing.B) {
	data := make([]byte, 1<<20)
	for _, size := range []int{1, 1 << 10, 64 << 10} {
		b.Run("read/"+strconv.Itoa(size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for range b.N {
				active := true
				value := mma.Values.BorrowBytes(int64(len(data)), func(offset, length int64) ([]byte, error) {
					return append([]byte(nil), data[offset:offset+length]...), nil
				}, nil, &active)
				_, _, err := mma.Invoke(func(context *mma.CallContext) error {
					result, readErr := context.ReadBytes(0, 0, int64(size))
					benchmarkMMABytes = result
					return readErr
				}, []mma.Value{value}, mma.FunctionSignature{Parameters: []mma.Type{mma.TypeBytes}, Return: mma.TypeUnit})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run("write/"+strconv.Itoa(size), func(b *testing.B) {
			source := make([]byte, size)
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for range b.N {
				active := true
				value := mma.Values.BorrowBytes(int64(len(data)), nil, func(offset int64, input []byte) error {
					copy(data[offset:], input)
					return nil
				}, &active)
				_, _, err := mma.Invoke(func(context *mma.CallContext) error {
					return context.WriteBytes(0, 0, source)
				}, []mma.Value{value}, mma.FunctionSignature{Parameters: []mma.Type{mma.TypeBytes}, Return: mma.TypeUnit})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
