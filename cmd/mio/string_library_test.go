package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	stdutf8 "unicode/utf8"

	"Mio/internal/mochi"
	"Mio/internal/vm"
)

func runStringSource(t testing.TB, source string) (string, string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	exit := runCLI([]string{"run", path}, &stdout, &stderr)
	return stdout.String(), stderr.String(), exit
}

// 测试字符串包名与内建 string 类型共存
func TestCLIKisoStringPackageAndTypeName(t *testing.T) {
	stdout, stderr, exit := runStringSource(t, `
yobu "kiso/string";
kaku value: string = "Mio中文日本語🍡";
chant(string.byte_len(value));
chant(string.rune_count(value));
`)
	want := fmt.Sprintf("%d\n%d\n", len("Mio中文日本語🍡"), stdutf8.RuneCountInString("Mio中文日本語🍡"))
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s want=%q", exit, stdout, stderr, want)
	}
}

// 测试字符串与字节数组转换的独立存储
func TestCLIKisoStringByteConversions(t *testing.T) {
	stdout, stderr, exit := runStringSource(t, `
yobu "kiso/string";
kaku original = "A\n你日🍡";
kaku data = string.to_bytes(original);
chant(len(data)); chant(data[0]); chant(data[1]);
data[0] = uint8(90);
chant(original);
kaku source: []uint8 = [65, 239, 191, 189, 240, 159, 141, 161];
kaku decoded = string.from_utf8(source);
source[0] = uint8(90);
moshi (decoded != mu) {
    chant(decoded);
}
kaku empty_data = string.to_bytes("");
chant(len(empty_data)); chant(string.from_utf8([]) != mu); chant(string.from_utf8([0]) != mu);
`)
	want := fmt.Sprintf("%d\n65\n10\nA\n你日🍡\nA�🍡\n0\nhai\nhai\n", len("A\n你日🍡"))
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s want=%q", exit, stdout, stderr, want)
	}
}

// 测试严格 UTF-8 校验拒绝非法序列
func TestCLIKisoStringRejectsInvalidUTF8(t *testing.T) {
	invalid := [][]byte{
		{0x80}, {0xc0, 0x80}, {0xed, 0xa0, 0x80}, {0xf4, 0x90, 0x80, 0x80},
		{0xe2, 0x82}, {0xf0, 0x9f, 0x8d},
	}
	var source strings.Builder
	source.WriteString("yobu \"kiso/string\";\n")
	for index, data := range invalid {
		if stdutf8.Valid(data) {
			t.Fatalf("case %d unexpectedly valid", index)
		}
		fmt.Fprintf(&source, "kaku invalid%d: []uint8 = %s; chant(string.from_utf8(invalid%d) == mu);\n", index, mioBytes(data), index)
	}
	source.WriteString("chant(string.from_utf8([239, 191, 189]) != mu);\n")
	stdout, stderr, exit := runStringSource(t, source.String())
	want := strings.Repeat("hai\n", len(invalid)+1)
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试前后缀、包含、字节偏移和拼接
func TestCLIKisoStringAlgorithms(t *testing.T) {
	stdout, stderr, exit := runStringSource(t, `
yobu "kiso/string";
kaku value = "你Mio日本🍡";
chant(string.has_prefix(value, "")); chant(string.has_prefix(value, "你Mio")); chant(string.has_prefix(value, "Mio"));
chant(string.has_suffix(value, "")); chant(string.has_suffix(value, "日本🍡")); chant(string.has_suffix(value, "日本"));
chant(string.contains(value, "Mio")); chant(string.contains(value, "不存在"));
chant(string.index(value, "")); chant(string.index(value, "Mio")); chant(string.index(value, "日本")); chant(string.index(value, "不存在"));
chant(string.concat("A你", "日🍡")); chant(string.concat("", ""));
`)
	want := "hai\nhai\niie\nhai\nhai\niie\nhai\niie\n0\n3\n6\n-1\nA你日🍡\n\n"
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试 StringBuilder 扩容、共享、重置和独立导出
func TestCLIKisoStringBuilder(t *testing.T) {
	stdout, stderr, exit := runStringSource(t, `
yobu "kiso/string";
kaku builder = string.builder_with_capacity(2);
kaku alias = builder;
string.write_string(builder, "A");
string.write_rune(alias, '你');
string.write_string(builder, "日🍡");
chant(string.builder_len(builder)); chant(string.builder_capacity(builder) >= string.builder_len(builder));
kaku snapshot = string.builder_string(builder);
string.write_string(alias, "!");
chant(snapshot); chant(string.builder_string(builder));
string.builder_reset(alias);
chant(string.builder_len(builder)); chant(string.builder_capacity(builder) >= 11);
string.write_string(builder, "再利用");
chant(string.builder_string(alias));
kaku empty = string.new_builder(); chant(string.builder_len(empty)); chant(string.builder_string(empty));
`)
	want := "11\nhai\nA你日🍡\nA你日🍡!\n0\nhai\n再利用\n0\n\n"
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试 StringBuilder 拒绝非法 rune、UTF-8 和容量
func TestCLIKisoStringBuilderErrors(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{source: `yobu "kiso/string"; kaku builder = string.new_builder(); string.write_string(builder, "ok"); string.write_rune(builder, rune(55296));`, want: "invalid Unicode scalar value"},
		{source: `yobu "kiso/string"; kaku builder = string.new_builder(); string.write_string(builder, "ok"); builder.buffer.data[0] = uint8(255); string.builder_string(builder);`, want: "builder contains invalid UTF-8"},
		{source: `yobu "kiso/string"; string.builder_with_capacity(-1);`, want: "array size cannot be negative"},
	}
	for _, test := range tests {
		_, stderr, exit := runStringSource(t, test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%q", test.source, exit, stderr)
		}
	}
}

// 测试大型文本和 StringBuilder 批量追加
func TestCLIKisoStringLargeText(t *testing.T) {
	skipInShortMode(t, "跳过大型文本验证")
	repeats := 4096
	if stressTestEnabled() {
		repeats = 32768
	}
	seed := "Mio中文日本語🍡\n"
	source := fmt.Sprintf(`
yobu "kiso/string";
kaku builder = string.builder_with_capacity(%d);
kaku index = 0;
aida (index < %d) { string.write_string(builder, %q); index = index + 1; }
kaku result = string.builder_string(builder);
chant(string.byte_len(result)); chant(string.rune_count(result)); chant(string.has_suffix(result, %q));
`, len(seed)*repeats, repeats, seed, seed)
	instructionLimit := uint64(100_000_000)
	if stressTestEnabled() {
		instructionLimit = 500_000_000
	}
	stdout := runSourceVM(t, source, instructionLimit)
	want := fmt.Sprintf("%d\n%d\nhai\n", len(seed)*repeats, stdutf8.RuneCountInString(seed)*repeats)
	if stdout != want {
		t.Fatalf("output=%q want=%q", stdout, want)
	}
}

// 测试 string 包的 run、build、exec、disasm 和 Mochi 版本
func TestCLIKisoStringMochiCommands(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(t, sourcePath, `yobu "kiso/string"; chant(string.concat("Mio", "🍡"));`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", sourcePath}, &stdout, &stderr); exit != 0 || stdout.String() != "Mio🍡\n" {
		t.Fatalf("run exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	artifact, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifact) < 7 || binary.BigEndian.Uint16(artifact[5:7]) != mochi.VersionV14 {
		t.Fatalf("Mochi version=%d", binary.BigEndian.Uint16(artifact[5:7]))
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "Mio🍡\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 ||
		!strings.Contains(stdout.String(), "OpDataConvert") ||
		!strings.Contains(stdout.String(), "kiso/string::concat") ||
		strings.Contains(stdout.String(), "kiso/string::index") {
		t.Fatalf("disasm exit=%d output=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

func benchmarkStringProgram(b *testing.B, source string, processedBytes int64) {
	b.Helper()
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, source)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		b.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		b.Fatal(err)
	}
	machine := vm.NewWithOutput(bytecode, io.Discard)
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, bytecode); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.SetBytes(processedBytes)
	b.ResetTimer()
	b.ReportMetric(float64(encoded.Len()), "mochi-B")
	for iteration := 0; iteration < b.N; iteration++ {
		if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: 100_000_000}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkKisoStringOperations 测量完整 Mio 字符串操作成本
func BenchmarkKisoStringOperations(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		text := strings.Repeat("a", size)
		b.Run(fmt.Sprintf("to-bytes/%d", size), func(b *testing.B) {
			benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku data = string.to_bytes(%q);`, text), int64(size))
		})
		b.Run(fmt.Sprintf("byte-len/%d", size), func(b *testing.B) {
			benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku size = string.byte_len(%q);`, text), int64(size))
		})
	}
	mixed := strings.Repeat("中文日本語🍡", 1024)
	b.Run("from-utf8/mixed", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku data = string.to_bytes(%q); kaku result = string.from_utf8(data);`, mixed), int64(len(mixed)))
	})
	b.Run("rune-count/mixed", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku count = string.rune_count(%q);`, mixed), int64(len(mixed)))
	})
	b.Run("index/mixed", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku position = string.index(%q, "x");`, mixed), int64(len(mixed)))
	})
	b.Run("concat/mixed", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku result = string.concat(%q, %q);`, mixed, mixed), int64(len(mixed)*2))
	})
	b.Run("builder/reserved-write", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku builder = string.builder_with_capacity(%d); string.write_string(builder, %q);`, len(mixed), mixed), int64(len(mixed)))
	})
	b.Run("builder/auto-write", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku builder = string.new_builder(); string.write_string(builder, %q);`, mixed), int64(len(mixed)))
	})
	b.Run("builder/export", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku builder = string.builder_with_capacity(%d); string.write_string(builder, %q); kaku result = string.builder_string(builder);`, len(mixed), mixed), int64(len(mixed)))
	})
	b.Run("builder/auto-growth-export", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku builder = string.new_builder(); string.write_string(builder, %q); kaku result = string.builder_string(builder);`, mixed), int64(len(mixed)))
	})
	b.Run("builder/reset-reuse-export", func(b *testing.B) {
		benchmarkStringProgram(b, fmt.Sprintf(`yobu "kiso/string"; kaku builder = string.builder_with_capacity(%d); string.write_string(builder, %q); string.builder_reset(builder); string.write_string(builder, %q); kaku result = string.builder_string(builder);`, len(mixed), mixed, mixed), int64(len(mixed)*2))
	})
	b.Run("builder/runes", func(b *testing.B) {
		benchmarkStringProgram(b, `yobu "kiso/string"; kaku builder = string.new_builder(); kaku index = 0; aida (index < 1024) { string.write_rune(builder, '你'); index = index + 1; } kaku result = string.builder_string(builder);`, 3<<10)
	})
}
