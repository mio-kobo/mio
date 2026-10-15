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

// 测试数组推断、动态元素与索引读取
func TestCLIArrayBasics(t *testing.T) {
	source := `
kaku base = 10;
kaku numbers = [base, base + 10, base * 3];
kaku names = ["Mio", "Mochi"];
chant(numbers[0]);
chant(numbers[1] + numbers[2]);
chant(names[1]);
`
	exit, stdout, stderr := runSourceCLI(t, "run", source)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if stdout != "10\n50\nMochi\n" {
		t.Fatalf("stdout=%q", stdout)
	}
}

// 测试数组函数参数与返回值
func TestCLIArrayFunctions(t *testing.T) {
	source := `
waza make(a: int64, b: int64) -> []int64 {
	kaesu [a, b, a + b];
}
waza second(values: []int64) -> int64 {
	kaesu values[1];
}
kaku values = make(7, 8);
chant(second(values));
chant(values[2]);
`
	exit, stdout, stderr := runSourceCLI(t, "run", source)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if stdout != "8\n15\n" {
		t.Fatalf("stdout=%q", stdout)
	}
}

// 测试数组静态类型错误
func TestCLIArrayTypeErrors(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`kaku values = [1, "Mio"];`, "array element 2"},
		{`kaku values = [];`, "cannot infer element type"},
		{`kaku values = [1, 2]; values[1.0];`, "array index must be int64"},
		{`kaku value = 1; value[0];`, "index target must be an array"},
		{`kaku left = [1]; kaku right = [1]; left == right;`, "does not support []int64"},
		{`waza take(values: []string) -> string { kaesu values[0]; } take([1]);`, "expected []string"},
	}
	for _, test := range tests {
		exit, _, stderr := runSourceCLI(t, "check", test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%q", test.source, exit, stderr)
		}
	}
}

// 测试数组索引运行时错误及后续独立执行
func TestCLIArrayIndexErrorsAndRecovery(t *testing.T) {
	for _, test := range []struct {
		source string
		want   string
	}{
		{`kaku values = [1, 2]; chant(values[-1]);`, "index -1 out of bounds"},
		{`kaku values = [1, 2]; chant(values[2]);`, "index 2 out of bounds"},
	} {
		exit, _, stderr := runSourceCLI(t, "run", test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) || !strings.Contains(stderr, "mio: runtime:") {
			t.Fatalf("source=%q exit=%d stderr=%q", test.source, exit, stderr)
		}
	}

	exit, stdout, stderr := runSourceCLI(t, "run", `chant([40, 42][1]);`)
	if exit != 0 || stdout != "42\n" || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
}

// 测试数组元素赋值拒绝负索引和越界索引
func TestCLIArrayMutationIndexErrors(t *testing.T) {
	for _, test := range []struct {
		source string
		want   string
	}{
		{`kaku values = [1, 2]; values[-1] = 3;`, "index -1 out of bounds"},
		{`kaku values = [1, 2]; values[2] = 3;`, "index 2 out of bounds"},
	} {
		exit, _, stderr := runSourceCLI(t, "run", test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) || !strings.Contains(stderr, "mio: runtime:") {
			t.Fatalf("source=%q exit=%d stderr=%q", test.source, exit, stderr)
		}
	}
}

// 测试数组程序使用 Mochi v5 往返执行
func TestCLIArrayMochiV5(t *testing.T) {
	sourcePath := writeFloatSource(t, `kaku values = [20, 22]; chant(values[0] + values[1]);`)
	mochiPath := filepath.Join(t.TempDir(), "array.mochi")
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%q", exit, stderr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV5 {
		t.Fatalf("version=%d, want %d", version, mochi.VersionV5)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exec exit=%d stderr=%q", exit, stderr.String())
	}
	if stdout.String() != "42\n" {
		t.Fatalf("stdout=%q", stdout.String())
	}
	stdout.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("disasm exit=%d stderr=%q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OpArray 2") || !strings.Contains(stdout.String(), "OpIndex") {
		t.Fatalf("disassembly=%q", stdout.String())
	}
}

// 测试数组长度、共享修改、函数参数修改与循环遍历
func TestCLIArrayLengthAndMutation(t *testing.T) {
	source := `
waza replace(values: []int64, index: int64, value: int64) -> unit {
	values[index] = value;
	kaesu;
}
kaku left = [1, 2, 3, 4];
kaku right = left;
right[1] = 20;
replace(left, 2, 30);
kaku index = 0;
kaku total = 0;
aida (index < len(right)) {
	total = total + right[index];
	index = index + 1;
}
chant(len(left));
chant(left[1]);
chant(right[2]);
chant(total);
`
	exit, stdout, stderr := runSourceCLI(t, "run", source)
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
	if stdout != "4\n20\n30\n55\n" {
		t.Fatalf("stdout=%q", stdout)
	}
}

// 测试 len 可以被同名用户函数遮蔽
func TestCLILenBuiltinShadowing(t *testing.T) {
	source := `
waza len(value: int64) -> int64 {
	kaesu value + 1;
}
chant(len(41));
`
	exit, stdout, stderr := runSourceCLI(t, "run", source)
	if exit != 0 || stdout != "42\n" || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
}

// 测试数组元素赋值的静态类型约束
func TestCLIArrayMutationTypeErrors(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`kaku values = [1, 2]; values[0] = "Mio";`, "array element assignment requires int64"},
		{`kaku values = [1, 2]; values[1.0] = 3;`, "array index must be int64"},
		{`kaku text = "Mio"; text[0] = "m";`, "index assignment target must be an array"},
		{`kaku value = 1; len(value);`, `builtin "len" requires array`},
	}
	for _, test := range tests {
		exit, _, stderr := runSourceCLI(t, "check", test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%q", test.source, exit, stderr)
		}
	}
}

// 测试数组修改在 run、build 和 exec 中保持一致
func TestCLIArrayMutationMochiV6(t *testing.T) {
	source := `kaku values = [10, 20, 30]; values[1] = 32; chant(len(values)); chant(values[1]);`
	exit, runOutput, runError := runSourceCLI(t, "run", source)
	if exit != 0 || runError != "" {
		t.Fatalf("run exit=%d stderr=%q", exit, runError)
	}

	sourcePath := writeFloatSource(t, source)
	mochiPath := filepath.Join(t.TempDir(), "array-mutation.mochi")
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%q", exit, stderr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV6 {
		t.Fatalf("version=%d, want %d", version, mochi.VersionV6)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exec exit=%d stderr=%q", exit, stderr.String())
	}
	if stdout.String() != runOutput || stdout.String() != "3\n32\n" {
		t.Fatalf("run=%q exec=%q", runOutput, stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("disasm exit=%d stderr=%q", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), "OpSetIndex") || !strings.Contains(stdout.String(), "OpLen") {
		t.Fatalf("disassembly=%q", stdout.String())
	}
}
