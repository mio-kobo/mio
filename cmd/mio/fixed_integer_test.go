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

// 测试固定宽度整数贯穿复合类型、函数和泛型
func TestCLIFixedWidthIntegers(t *testing.T) {
	source := `
teisu BYTE_MIN: int8 = -128;
teisu BYTE_WRAP: uint8 = 255 + 1;
kata Box<T> { value: T; }
waza twice<T: Integer>(value: T) -> T { kaesu value + value; }
waza pass(value: uint32) -> uint32 { kaesu value; }
kaku a: int8 = 127;
kaku b: int16 = -32768;
kaku c: int32 = 2147483647;
kaku d: uint8 = 255;
kaku e: uint16 = 65535;
kaku f: uint32 = 4294967295;
a = a + 1;
b = b / -1;
c = c + 1;
d = d + 1;
e = e * 2;
f = f + 1;
kaku values: []uint8 = [254, 255];
values[0] = values[0] + 2;
kaku box = Box[int16] { value: -7 };
chant(a); chant(b); chant(c); chant(d); chant(e); chant(f);
chant(values[0]); chant(box.value); chant(twice[int8](12)); chant(pass(42));
chant(a < int8(0));
chant(BYTE_MIN); chant(BYTE_WRAP);
`
	exit, out, stderr := runSourceCLI(t, "run", source)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	want := "-128\n-32768\n-2147483648\n0\n65534\n0\n0\n-7\n24\n42\nhai\n-128\n0\n"
	if out != want {
		t.Fatalf("output=%q, want %q", out, want)
	}
}

// 测试所有固定宽度整数的字面量边界
func TestCLIFixedWidthIntegerLiteralBoundaries(t *testing.T) {
	tests := []struct {
		typeName string
		minimum  string
		maximum  string
		below    string
		above    string
	}{
		{"int8", "-128", "127", "-129", "128"},
		{"int16", "-32768", "32767", "-32769", "32768"},
		{"int32", "-2147483648", "2147483647", "-2147483649", "2147483648"},
		{"uint8", "0", "255", "-1", "256"},
		{"uint16", "0", "65535", "-1", "65536"},
		{"uint32", "0", "4294967295", "-1", "4294967296"},
	}
	for _, test := range tests {
		valid := "kaku low: " + test.typeName + " = " + test.minimum + "; kaku high: " + test.typeName + " = " + test.maximum + ";"
		if exit, _, stderr := runSourceCLI(t, "check", valid); exit != 0 {
			t.Fatalf("type=%s valid boundary stderr=%q", test.typeName, stderr)
		}
		for _, invalid := range []string{test.below, test.above} {
			source := "kaku value: " + test.typeName + " = " + invalid + ";"
			if exit, _, stderr := runSourceCLI(t, "check", source); exit == 0 || !strings.Contains(stderr, "outside "+test.typeName+" range") {
				t.Fatalf("source=%q exit=%d stderr=%q", source, exit, stderr)
			}
		}
	}
}

// 测试固定宽度整数的静态边界和强类型规则
func TestCLIFixedWidthIntegerTypeErrors(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`kaku value: int8 = 128;`, "outside int8 range"},
		{`kaku value: int8 = -129;`, "outside int8 range"},
		{`kaku value: uint8 = -1;`, "outside uint8 range"},
		{`kaku value: uint16 = 65536;`, "outside uint16 range"},
		{`kaku a: int8 = 1; kaku b: int16 = 1; a + b;`, "不允许隐式类型转换"},
		{`waza signed<T: Signed>(value: T) -> T { kaesu -value; } signed[uint8](1);`, "must satisfy Signed"},
	}
	for _, test := range tests {
		exit, _, stderr := runSourceCLI(t, "check", test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%q, want %q", test.source, exit, stderr, test.want)
		}
	}
}

// 测试固定宽度整数转换越界与除零
func TestCLIFixedWidthIntegerRuntimeErrors(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`kaku value: uint16 = 256; uint8(value);`, "outside uint8 range"},
		{`kaku value: int8 = -1; uint8(value);`, "outside uint8 range"},
		{`kaku value: uint32 = 4294967295; int32(value);`, "outside int32 range"},
		{`int8(128.0);`, "outside int8 range"},
		{`uint8(-1.0);`, "outside uint8 range"},
		{`kaku zero: uint8 = 0; kaku one: uint8 = 1; one / zero;`, "division by zero"},
	}
	for _, test := range tests {
		exit, _, stderr := runSourceCLI(t, "run", test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%q, want %q", test.source, exit, stderr, test.want)
		}
	}
}

// 测试固定宽度整数的 run、build、exec 与 disasm 一致
func TestCLIFixedWidthIntegerMochi(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "fixed.mio")
	mochiPath := filepath.Join(directory, "fixed.mochi")
	if err := os.WriteFile(sourcePath, []byte(`kaku value: uint8 = 255; chant(value + 1); chant(int16(value));`), 0644); err != nil {
		t.Fatal(err)
	}
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
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.Version {
		t.Fatalf("version=%d, want %d", version, mochi.Version)
	}
	var execOut, execErr bytes.Buffer
	if exit := runCLI([]string{"exec", mochiPath}, &execOut, &execErr); exit != 0 {
		t.Fatalf("exec exit=%d stderr=%q", exit, execErr.String())
	}
	if execOut.String() != runOut.String() || execOut.String() != "0\n255\n" {
		t.Fatalf("run=%q exec=%q", runOut.String(), execOut.String())
	}
	var disasmOut, disasmErr bytes.Buffer
	if exit := runCLI([]string{"disasm", mochiPath}, &disasmOut, &disasmErr); exit != 0 {
		t.Fatalf("disasm exit=%d stderr=%q", exit, disasmErr.String())
	}
	for _, want := range []string{"uint8 255", "OpIntegerConvert"} {
		if !strings.Contains(disasmOut.String(), want) {
			t.Fatalf("disassembly missing %q: %s", want, disasmOut.String())
		}
	}
}

// 测试无上下文整数字面量仍推断为 int64
func TestCLIDefaultIntegerRemainsInt64(t *testing.T) {
	exit, _, stderr := runSourceCLI(t, "check", `kaku inferred = 1; kaku narrow: int8 = 1; inferred = narrow;`)
	if exit == 0 || !strings.Contains(stderr, "declared int64") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
}

// 测试数学库约束区分无符号与有符号整数
func TestCLIFixedWidthIntegerMathConstraints(t *testing.T) {
	out, stderr, exit := runMathSource(t, `
yobu "kiso/math";
chant(math.min[uint8](255, 1));
chant(math.abs[int8](-12));
`)
	if exit != 0 || out != "1\n12\n" {
		t.Fatalf("exit=%d out=%q stderr=%q", exit, out, stderr)
	}

	path := filepath.Join(t.TempDir(), "invalid.mio")
	if err := os.WriteFile(path, []byte(`yobu "kiso/math"; math.abs[uint8](1);`), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, invalidErr bytes.Buffer
	if exit := runCLI([]string{"check", path}, &stdout, &invalidErr); exit == 0 || !strings.Contains(invalidErr.String(), "must satisfy Signed") {
		t.Fatalf("exit=%d stderr=%q", exit, invalidErr.String())
	}
}
