package main

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mathBunkoRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func runMathSource(t *testing.T, source string) (string, string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	exit := runCLI([]string{"run", path}, &stdout, &stderr)
	return stdout.String(), stderr.String(), exit
}

// 测试基础数学 API、特殊值和跨文件调用
func TestCLIKisoMathAPI(t *testing.T) {
	stdout, stderr, exit := runMathSource(t, `
yobu "kiso/math";
chant(math.min[int64](7, -2));
chant(math.max[float32](float32(1.5), float32(2.5)));
chant(math.clamp[float64](8.0, 1.0, 4.0));
chant(math.abs[int64](-9));
kaku checked_absolute = math.checked_abs[int64](-12);
moshi (checked_absolute != mu) { chant(checked_absolute); }
chant(math.gcd(-54, 24));
chant(math.lcm(-21, 6));
kaku checked_common = math.checked_gcd(0, 0);
moshi (checked_common != mu) { chant(checked_common); }
kaku checked_multiple = math.checked_lcm(0, -9);
moshi (checked_multiple != mu) { chant(checked_multiple); }
chant(math.sqrt[float32](float32(9.0)));
chant(math.sqrt[float64](2.25));
chant(math.floor[float32](float32(1.9)));
chant(math.ceil[float64](-1.9));
chant(math.round[float64](2.5));
chant(math.round[float64](3.5));
chant(math.trunc[float32](float32(-1.9)));
kaku nan: float64 = 0.0 / 0.0;
kaku inf: float32 = float32(1.0) / float32(0.0);
chant(math.is_nan(math.min(nan, 1.0)));
chant(math.is_nan(math.max(1.0, nan)));
chant(math.is_inf(inf));
chant(math.is_finite(math.pi64));
chant(math.min[float64](-0.0, 0.0));
chant(math.max[float64](-0.0, 0.0));
chant(math.pi32);
chant(math.pi64);
chant(math.tau32);
chant(math.tau64);
`)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	want := "-2\n2.5\n4\n9\n12\n6\n42\n0\n0\n3\n1.5\n1\n-1\n2\n4\n-1\nhai\nhai\nhai\nhai\n-0\n0\n3.1415927\n3.141592653589793\n6.2831855\n6.283185307179586\n"
	if stdout != want {
		t.Fatalf("output=%q, want %q", stdout, want)
	}
}

// 测试数学溢出、非法区间和静态约束
func TestCLIKisoMathErrors(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "abs", source: `yobu "kiso/math"; math.abs(-9223372036854775807 - 1);`, want: "kiso/math.abs: int64 overflow"},
		{name: "gcd", source: `yobu "kiso/math"; math.gcd(-9223372036854775807 - 1, 0);`, want: "kiso/math.gcd: result does not fit int64"},
		{name: "lcm", source: `yobu "kiso/math"; math.lcm(9223372036854775807, 2);`, want: "kiso/math.lcm: result does not fit int64"},
		{name: "clamp", source: `yobu "kiso/math"; math.clamp(1, 3, 2);`, want: "kiso/math.clamp: lower bound is greater than upper bound"},
		{name: "float constraint", source: `yobu "kiso/math"; math.sqrt(4);`, want: "must satisfy Float"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, stderr, exit := runMathSource(t, test.source)
			if exit == 0 || !strings.Contains(stderr, test.want) {
				t.Fatalf("exit=%d stderr=%s", exit, stderr)
			}
		})
	}
}

// 测试 checked 数学函数返回 mu
func TestCLIKisoMathCheckedOverflow(t *testing.T) {
	stdout, stderr, exit := runMathSource(t, `
yobu "kiso/math";
kaku minimum = -9223372036854775807 - 1;
kaku absolute = math.checked_abs(minimum);
kaku common = math.checked_gcd(minimum, 0);
kaku multiple = math.checked_lcm(9223372036854775807, 2);
moshi (absolute == mu) { chant("mu"); }
moshi (common == mu) { chant("mu"); }
moshi (multiple == mu) { chant("mu"); }
`)
	if exit != 0 || stdout != "mu\nmu\nmu\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试整数算法和平方根的独立参考边界
func TestCLIKisoMathReferenceEdges(t *testing.T) {
	stdout, stderr, exit := runMathSource(t, `
yobu "kiso/math";
kaku minimum = -9223372036854775807 - 1;
chant(math.gcd(minimum, 3));
chant(math.gcd(minimum, -1));
chant(math.lcm(9223372036854775807, 1));
chant(math.sqrt[float64](2.0) == 1.4142135623730951);
chant(math.sqrt[float32](float32(2.0)) == float32(1.4142135623730951));
chant(math.is_nan(math.sqrt[float64](-1.0)));
kaku infinity: float64 = 1.0 / 0.0;
chant(math.is_inf(math.sqrt[float64](infinity)));
chant(math.round[float32](float32(2.5)) == float32(2.0));
chant(math.round[float32](float32(3.5)) == float32(4.0));
chant(math.min[float32](float32(-0.0), float32(0.0)));
chant(math.max[float32](float32(-0.0), float32(0.0)));
`)
	want := "1\n1\n9223372036854775807\nhai\nhai\nhai\nhai\nhai\nhai\n-0\n0\n"
	if exit != 0 || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试数学函数在循环和闭包中调用
func TestCLIKisoMathLoopAndClosure(t *testing.T) {
	stdout, stderr, exit := runMathSource(t, `
yobu "kiso/math";
kaku total = 0;
kaku current = 1;
aida (current <= 4) {
    total = total + math.max(current, 2);
    current = current + 1;
}
kaku magnitude = waza(value: int64) -> int64 { kaesu math.abs(value); };
chant(total);
chant(magnitude(-7));
`)
	if exit != 0 || stdout != "11\n7\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试数学包的 run、build、exec 和 disasm 一致性
func TestCLIKisoMathMochiCommands(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(t, sourcePath, `yobu "kiso/math"; chant(math.gcd(84, 30)); chant(math.sqrt[float32](float32(16.0)));`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", sourcePath}, &stdout, &stderr); exit != 0 || stdout.String() != "6\n4\n" {
		t.Fatalf("run exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "6\n4\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "kiso/math::checked_gcd") || !strings.Contains(stdout.String(), "OpMath") || strings.Contains(stdout.String(), "kiso/math::lcm") || strings.Contains(stdout.String(), "kiso/math::min") {
		t.Fatalf("disasm exit=%d output=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

// 测试泛型数学函数共享字节码且未使用函数不进入 Mochi
func TestCLIKisoMathPayForUse(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(t, sourcePath, `
yobu "kiso/math";
chant(math.min[int64](1, 2));
chant(math.min[float32](float32(1.0), float32(2.0)));
chant(math.min[float64](1.0, 2.0));
`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	bytecode, err := decodeMochiFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(bytecode.Functions) != 1 || bytecode.Functions[0].Name != "kiso/math::min" {
		t.Fatalf("functions=%#v", bytecode.Functions)
	}
}

// 测试数学常量按目标 IEEE 754 格式舍入
func TestCLIKisoMathConstantRounding(t *testing.T) {
	stdout, stderr, exit := runMathSource(t, `
yobu "kiso/math";
chant(float64(math.pi32) == 3.1415927410125732421875);
chant(math.pi64 == 3.141592653589793115997963468544185161590576171875);
chant(float64(math.tau32) == 6.283185482025146484375);
chant(math.tau64 == 6.28318530717958623199592693708837032318115234375);
`)
	if exit != 0 || stdout != "hai\nhai\nhai\nhai\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
	if math.Float32bits(float32(math.Pi)) != 0x40490fdb || math.Float64bits(math.Pi) != 0x400921fb54442d18 {
		t.Fatal("Go reference constants do not match IEEE 754 expectations")
	}
}

// 测量 kiso/math 编译成本和 Mochi 体积
func BenchmarkKisoMathCompile(b *testing.B) {
	root := b.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(b, sourcePath, `yobu "kiso/math"; chant(math.gcd(84, 30)); chant(math.sqrt[float32](float32(16.0)));`)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		b.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	info, err := os.Stat(mochiPath)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		program, resolver, err := parseAndCheckFile(sourcePath)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := compileProgram(program, resolver); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(info.Size()), "mochi-B")
}

// 测量 kiso/math 执行成本和字节码体积
func BenchmarkKisoMathExecute(b *testing.B) {
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, `yobu "kiso/math"; math.gcd(84, 30); math.sqrt[float32](float32(16.0)); math.round[float64](2.5);`)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
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
	for i := 0; i < b.N; i++ {
		if runtimeError, err := executeBytecode(bytecode, bytes.NewBuffer(nil)); err != nil || runtimeError {
			b.Fatalf("runtime=%v error=%v", runtimeError, err)
		}
	}
	b.ReportMetric(float64(instructionBytes(bytecode)), "bytecode-B")
}
