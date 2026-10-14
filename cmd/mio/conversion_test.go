package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/mochi"
)

// 测试显式数值转换的完整运行语义
func TestCLINumericConversions(t *testing.T) {
	source := `
kaku positive = int64(3.9);
kaku negative = int64(-3.9);
kaku precise = float64(42);
kaku rounded = float64(9007199254740993);
kaku nested = int64(float64(9007199254740993));
kaku positiveZero = int64(0.0);
kaku negativeZero = int64(-0.0);
chant(positive);
chant(negative);
chant(precise);
chant(rounded);
chant(nested);
chant(positiveZero);
chant(negativeZero);
`
	exit, stdout, stderr := runSourceCLI(t, "run", source)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	want := "3\n-3\n42\n9.007199254740992e+15\n9007199254740992\n0\n0\n"
	if stdout != want {
		t.Fatalf("stdout=%q, want %q", stdout, want)
	}
}

// 测试函数、递归与循环中的数值转换
func TestCLIConversionsInFunctionsRecursionAndLoops(t *testing.T) {
	source := `
waza sum(n: int64) -> float64 {
	moshi (n <= 0) { kaesu 0.0; }
	kaesu float64(n) + sum(n - 1);
}
waza truncate(value: float64) -> int64 {
	kaesu int64(value);
}
kaku index = 0;
kaku total = 0.0;
aida (index < 3) {
	index = index + 1;
	total = total + float64(index);
}
chant(sum(4));
chant(truncate(-8.75));
chant(total);
`
	exit, stdout, stderr := runSourceCLI(t, "run", source)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if stdout != "10\n-8\n6\n" {
		t.Fatalf("stdout=%q", stdout)
	}
}

// 测试转换的静态错误与运行时范围错误
func TestCLIConversionErrors(t *testing.T) {
	staticTests := []struct {
		source string
		want   string
	}{
		{`int64();`, "expects 1 argument"},
		{`float64(1, 2);`, "expects 1 argument"},
		{`int64(1);`, "requires float64"},
		{`float64(1.0);`, "requires int64"},
		{`1 + float64(1);`, "matching float64"},
	}
	for _, test := range staticTests {
		exit, _, stderr := runSourceCLI(t, "check", test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%q", test.source, exit, stderr)
		}
	}

	runtimeTests := []string{
		`int64(0.0 / 0.0);`,
		`int64(1.0 / 0.0);`,
		`int64(-1.0 / 0.0);`,
		`int64(9223372036854775808.0);`,
		`int64(-9223372036854777856.0);`,
	}
	for _, source := range runtimeTests {
		exit, _, stderr := runSourceCLI(t, "run", source)
		if exit == 0 || !strings.Contains(stderr, "outside int64 range") {
			t.Fatalf("source=%q exit=%d stderr=%q", source, exit, stderr)
		}
	}
}

// 测试转换程序的 run、build、exec 与 disasm
func TestCLIConversionMochiV4(t *testing.T) {
	sourcePath := writeFloatSource(t, `chant(int64(float64(9007199254740993)));`)
	mochiPath := filepath.Join(t.TempDir(), "conversion.mochi")

	var runOut, runErr bytes.Buffer
	if exit := runCLI([]string{"run", sourcePath}, &runOut, &runErr); exit != 0 {
		t.Fatalf("run exit=%d stderr=%q", exit, runErr.String())
	}
	var buildOut, buildErr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &buildOut, &buildErr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%q", exit, buildErr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV4 {
		t.Fatalf("version=%d, want %d", version, mochi.VersionV4)
	}

	var execOut, execErr bytes.Buffer
	if exit := runCLI([]string{"exec", mochiPath}, &execOut, &execErr); exit != 0 {
		t.Fatalf("exec exit=%d stderr=%q", exit, execErr.String())
	}
	if execOut.String() != runOut.String() || execOut.String() != "9007199254740992\n" {
		t.Fatalf("run=%q exec=%q", runOut.String(), execOut.String())
	}

	var disasmOut, disasmErr bytes.Buffer
	if exit := runCLI([]string{"disasm", mochiPath}, &disasmOut, &disasmErr); exit != 0 {
		t.Fatalf("disasm exit=%d stderr=%q", exit, disasmErr.String())
	}
	for _, name := range []string{"OpIntToFloat", "OpFloatToInt"} {
		if !strings.Contains(disasmOut.String(), name) {
			t.Fatalf("disassembly does not contain %s: %q", name, disasmOut.String())
		}
	}
}
