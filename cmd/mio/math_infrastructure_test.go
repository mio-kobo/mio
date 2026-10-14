package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/compiler"
)

func writeInfrastructureSource(t testing.TB, path, source string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
}

// 测试目录包、多文件、导出、常量、泛型结构体和闭包
func TestCLIMathInfrastructurePackageIntegration(t *testing.T) {
	root := t.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	writeInfrastructureSource(t, filepath.Join(bunkoRoot, "kiso", "sample", "a.mio"), `
teisu hidden = 21;
koukai teisu answer: int64 = hidden * 2;
koukai kata Box<T> { value: T, }
koukai sentaku Choice<T> { Some(T); None; }
koukai waza min<T: Number>(a: T, b: T) -> T {
    moshi (a < b) { kaesu a; }
    kaesu b;
}
koukai waza make_adder(base: int64) -> (int64) -> int64 {
    kaesu waza(value: int64) -> int64 { kaesu base + value; };
}
`)
	writeInfrastructureSource(t, filepath.Join(bunkoRoot, "kiso", "sample", "b.mio"), `
koukai waza twice(value: int64) -> int64 { kaesu add(value, value); }
waza add(left: int64, right: int64) -> int64 { kaesu left + right; }
waza private_value() -> int64 { kaesu answer; }
`)
	app := filepath.Join(root, "app")
	writeInfrastructureSource(t, filepath.Join(app, "a.mio"), `yobu "kiso/sample";`)
	writeInfrastructureSource(t, filepath.Join(app, "b.mio"), `
kaku box: sample.Box<int64> = sample.Box[int64]{ value: sample.answer, };
chant(box.value);
chant(sample.twice(5));
chant(sample.min[float32](float32(2.5), float32(3.0)));
kaku fbox: sample.Box<float32> = sample.Box[float32]{ value: 1.25, };
chant(fbox.value);
kaku values: []float32 = [1.5, 2.5];
chant(values[1]);
kaku choice: sample.Choice<int64> = sample.Choice[int64].Some(9);
awase (choice) {
    baai Some(item) { chant(item); }
    baai None { chant(0); }
}
kaku add = sample.make_adder(7);
chant(add(8));
`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", app}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if got, want := stdout.String(), "42\n10\n2.5\n1.25\n2.5\n9\n15\n"; got != want {
		t.Fatalf("output=%q, want %q", got, want)
	}
	mochiPath := filepath.Join(root, "app.mochi")
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"build", "-o", mochiPath, app}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	stdout.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "42\n10\n2.5\n1.25\n2.5\n9\n15\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "kiso/sample::min") {
		t.Fatalf("disasm exit=%d output=%s stderr=%s", exit, stdout.String(), stderr.String())
	}

	privatePath := filepath.Join(root, "private.mio")
	writeInfrastructureSource(t, privatePath, `yobu "kiso/sample"; sample.private_value();`)
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"check", privatePath}, &stdout, &stderr); exit == 0 || !strings.Contains(stderr.String(), `does not export "private_value"`) {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
}

// 测试循环导入诊断
func TestCLIPackageCycleDiagnostic(t *testing.T) {
	root := t.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	writeInfrastructureSource(t, filepath.Join(bunkoRoot, "a", "a.mio"), `yobu "b"; koukai waza a() -> int64 { kaesu 1; }`)
	writeInfrastructureSource(t, filepath.Join(bunkoRoot, "b", "b.mio"), `yobu "a"; koukai waza b() -> int64 { kaesu 2; }`)
	mainPath := filepath.Join(root, "main.mio")
	writeInfrastructureSource(t, mainPath, `yobu "a"; a.a();`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"check", mainPath}, &stdout, &stderr); exit == 0 || !strings.Contains(stderr.String(), "cyclic package import") {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
}

// 测试 float32 运算、分类、符号零和舍入
func TestCLIFloat32AndMathPrimitives(t *testing.T) {
	root := t.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	writeInfrastructureSource(t, filepath.Join(bunkoRoot, "kiso", "primitives", "primitives.mio"), `
koukai waza sqrt32(x: float32) -> float32 { kaesu __sqrt(x); }
koukai waza sqrt64(x: float64) -> float64 { kaesu __sqrt(x); }
koukai waza floor32(x: float32) -> float32 { kaesu __floor(x); }
koukai waza ceil32(x: float32) -> float32 { kaesu __ceil(x); }
koukai waza round32(x: float32) -> float32 { kaesu __round_even(x); }
koukai waza trunc32(x: float32) -> float32 { kaesu __trunc(x); }
koukai waza is_nan32(x: float32) -> bool { kaesu __is_nan(x); }
koukai waza is_inf32(x: float32) -> bool { kaesu __is_inf(x); }
koukai waza is_finite32(x: float32) -> bool { kaesu __is_finite(x); }
koukai waza is_negative_zero32(x: float32) -> bool { kaesu __is_negative_zero(x); }
koukai waza add_overflows(a: int64, b: int64) -> bool { kaesu __add_overflows(a, b); }
`)
	path := filepath.Join(root, "float32.mio")
	writeInfrastructureSource(t, path, `
yobu "kiso/primitives";
kaku rounded: float32 = 16777216.0 + 1.0;
chant(float64(rounded) == 16777216.0);
chant(primitives.round32(float32(2.5)) == float32(2.0));
chant(primitives.round32(float32(3.5)) == float32(4.0));
chant(primitives.is_negative_zero32(float32(-0.0)));
chant(primitives.is_negative_zero32(primitives.sqrt32(float32(-0.0))));
chant(primitives.floor32(float32(1.9)) == float32(1.0));
chant(primitives.ceil32(float32(-1.9)) == float32(-1.0));
chant(primitives.trunc32(float32(-1.9)) == float32(-1.0));
kaku nan: float32 = float32(0.0) / float32(0.0);
kaku inf: float32 = float32(1.0) / float32(0.0);
chant(primitives.is_nan32(nan));
chant(primitives.is_inf32(inf));
chant(primitives.is_finite32(float32(1.0)));
chant(primitives.sqrt32(float32(9.0)));
chant(primitives.sqrt64(2.25));
chant(primitives.add_overflows(9223372036854775807, 1));
`)
	t.Setenv("MIO_BUNKO", bunkoRoot)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", path}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if got, want := stdout.String(), "hai\nhai\nhai\nhai\nhai\nhai\nhai\nhai\nhai\nhai\nhai\n3\n1.5\nhai\n"; got != want {
		t.Fatalf("output=%q, want %q", got, want)
	}
}

// 测试 float32 Mochi 往返和反汇编
func TestCLIFloat32MochiRoundTrip(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(t, sourcePath, `teisu x: float32 = 1.25; chant(x);`)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != 10 {
		t.Fatalf("version=%d, want 10", version)
	}
	bytecode, err := decodeMochiFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytecode.GlobalsCount != 0 {
		t.Fatalf("constant produced %d globals", bytecode.GlobalsCount)
	}
	floating, ok := bytecode.Constants[0].Float32()
	if !ok || math.Float32bits(floating) != math.Float32bits(1.25) {
		t.Fatalf("constant=%v", bytecode.Constants[0])
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "1.25\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "float32 1.25") {
		t.Fatalf("disasm exit=%d output=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

// 测试 float32 与 float64 禁止隐式混用
func TestCLIFloat32StrictTyping(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mixed.mio")
	writeInfrastructureSource(t, path, `kaku x: float32 = 1.0; x + 1.0;`)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"check", path}, &stdout, &stderr); exit == 0 || !strings.Contains(stderr.String(), "不允许隐式类型转换") {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
}

// 测试普通程序不能直接访问 Bunko 底层原语
func TestCLINoImplicitMathPrimitives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, `__sqrt(1.0);`)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"check", path}, &stdout, &stderr); exit == 0 || !strings.Contains(stderr.String(), `未定义的变量 "__sqrt"`) {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
}

// 测试 teisu 拒绝副作用、循环依赖和修改
func TestCLIConstantDiagnostics(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "副作用", source: `teisu bad = chant(1);`, want: "not a pure compile-time expression"},
		{name: "循环", source: `teisu a = b; teisu b = a;`, want: "constant dependency cycle"},
		{name: "复合值", source: `teisu bad = [1, 2];`, want: "not a pure compile-time expression"},
		{name: "修改", source: `teisu answer = 42; answer = 1;`, want: "未定义的赋值目标"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "main.mio")
			writeInfrastructureSource(t, path, test.source)
			var stdout, stderr bytes.Buffer
			if exit := runCLI([]string{"check", path}, &stdout, &stderr); exit == 0 || !strings.Contains(stderr.String(), test.want) {
				t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
			}
		})
	}
}

// 测试局部名称可以遮蔽包级常量
func TestCLIConstantLocalShadowing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, `
teisu value = 1;
waza identity(value: int64) -> int64 { kaesu value; }
chant(identity(2));
`)
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", path}, &stdout, &stderr); exit != 0 || stdout.String() != "2\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
}

// 测量数学基础设施编译时间、分配和字节码体积
func BenchmarkMathInfrastructureCompile(b *testing.B) {
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, `
waza min<T: Number>(a: T, b: T) -> T { moshi (a < b) { kaesu a; } kaesu b; }
teisu pi: float32 = 3.14159265358979323846;
chant(min[float32](pi, float32(4.0)));
`)
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		b.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(instructionBytes(bytecode)), "bytecode-B")
	for i := 0; i < b.N; i++ {
		program, resolver, err := parseAndCheckFile(path)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := compileProgram(program, resolver); err != nil {
			b.Fatal(err)
		}
	}
}

// 测量数学原语执行时间、分配和字节码体积
func BenchmarkMathInfrastructureExecute(b *testing.B) {
	root := b.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	writeInfrastructureSource(b, filepath.Join(bunkoRoot, "benchmath", "math.mio"), `
koukai waza sqrt(x: float32) -> float32 { kaesu __sqrt(x); }
koukai waza round(x: float32) -> float32 { kaesu __round_even(x); }
koukai waza finite(x: float32) -> bool { kaesu __is_finite(x); }
`)
	path := filepath.Join(root, "main.mio")
	writeInfrastructureSource(b, path, `
yobu "benchmath";
kaku value: float32 = 9.0;
benchmath.sqrt(value);
benchmath.round(float32(2.5));
benchmath.finite(value);
`)
	b.Setenv("MIO_BUNKO", bunkoRoot)
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		b.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	b.ReportMetric(float64(instructionBytes(bytecode)), "bytecode-B")
	for i := 0; i < b.N; i++ {
		if runtimeError, err := executeBytecode(bytecode, bytes.NewBuffer(nil)); err != nil || runtimeError {
			b.Fatalf("runtime=%v error=%v", runtimeError, err)
		}
	}
}

func instructionBytes(bytecode *compiler.Bytecode) int {
	size := len(bytecode.Instructions)
	for _, function := range bytecode.Functions {
		size += len(function.Instructions)
	}
	return size
}
