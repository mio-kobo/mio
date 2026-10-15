package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	stdutf8 "unicode/utf8"

	"Mio/internal/mochi"
	"Mio/internal/vm"
)

func runUTF8Source(t testing.TB, source string) (string, string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	exit := runCLI([]string{"run", path}, &stdout, &stderr)
	return stdout.String(), stderr.String(), exit
}

func mioBytes(data []byte) string {
	items := make([]string, len(data))
	for index, value := range data {
		items[index] = strconv.Itoa(int(value))
	}
	return "[" + strings.Join(items, ",") + "]"
}

// 测试全部 UTF-8 宽度、边界及常用文字
func TestCLIKisoUTF8DecodeReferenceData(t *testing.T) {
	runes := []rune{
		0, 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xe000, 0xffff,
		0x10000, stdutf8.MaxRune, '你', '日', '🍡', stdutf8.RuneError,
	}
	data := []byte(string(runes))
	if !stdutf8.Valid(data) || stdutf8.RuneCount(data) != len(runes) {
		t.Fatal("Go UTF-8 参考数据无效")
	}
	var source strings.Builder
	fmt.Fprintf(&source, "yobu \"kiso/utf8\"; kaku data: []uint8 = %s; kaku offset = 0;\n", mioBytes(data))
	source.WriteString("aida (offset < len(data)) { kaku decoded = utf8.decode_one(data, offset); chant(decoded.value); chant(decoded.width); chant(decoded.valid); offset = offset + decoded.width; }\n")
	source.WriteString("chant(utf8.valid(data)); kaku count = utf8.rune_count(data); moshi (count != mu) { chant(count); }\n")

	var want strings.Builder
	for _, value := range runes {
		want.WriteString(strconv.FormatInt(int64(value), 10))
		want.WriteByte('\n')
		want.WriteString(strconv.Itoa(stdutf8.RuneLen(value)))
		want.WriteString("\nhai\n")
	}
	want.WriteString("hai\n14\n")
	stdout, stderr, exit := runUTF8Source(t, source.String())
	if exit != 0 || stderr != "" || stdout != want.String() {
		t.Fatalf("exit=%d output=%q stderr=%s want=%q", exit, stdout, stderr, want.String())
	}
}

// 测试编码结果与 Go 标准库参考数据一致
func TestCLIKisoUTF8EncodeReferenceData(t *testing.T) {
	runes := []rune{0, 0x7f, 0x80, 0x7ff, 0x800, 0xd7ff, 0xe000, 0xffff, 0x10000, stdutf8.MaxRune, '你', '日', '🍡', stdutf8.RuneError}
	var source strings.Builder
	source.WriteString("yobu \"kiso/utf8\";\n")
	for index, value := range runes {
		fmt.Fprintf(&source, "kaku encoded%d = utf8.encode_rune(rune(%d)); chant(utf8.rune_width(rune(%d))); chant(len(encoded%d)); ", index, value, value, index)
		for byteIndex := range []byte(string(value)) {
			fmt.Fprintf(&source, "chant(encoded%d[%d]); ", index, byteIndex)
		}
		source.WriteByte('\n')
	}

	var want strings.Builder
	for _, value := range runes {
		encoded := []byte(string(value))
		fmt.Fprintf(&want, "%d\n%d\n", stdutf8.RuneLen(value), len(encoded))
		for _, octet := range encoded {
			fmt.Fprintf(&want, "%d\n", octet)
		}
	}
	stdout, stderr, exit := runUTF8Source(t, source.String())
	if exit != 0 || stderr != "" || stdout != want.String() {
		t.Fatalf("exit=%d output=%q stderr=%s want=%q", exit, stdout, stderr, want.String())
	}
}

// 测试编码、追加与解码完整往返
func TestCLIKisoUTF8RoundTrip(t *testing.T) {
	stdout, stderr, exit := runUTF8Source(t, `
yobu "kiso/array";
yobu "kiso/utf8";
kaku values: []rune = [rune(0), rune(127), rune(128), rune(2047), rune(2048), '你', '日', '🍡', rune(1114111)];
kaku joined = array.with_capacity[uint8](32);
kaku index = 0;
aida (index < len(values)) {
    kaku encoded = utf8.encode_rune(values[index]);
    kaku decoded = utf8.decode_one(encoded, 0);
    chant(decoded.value == values[index]); chant(decoded.valid); chant(decoded.width == len(encoded));
    utf8.append_rune(joined, values[index]);
    index = index + 1;
}
chant(utf8.valid(joined));
kaku count = utf8.rune_count(joined); moshi (count != mu) { chant(count); }
`)
	want := strings.Repeat("hai\nhai\nhai\n", 9) + "hai\n9\n"
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试非法 UTF-8 返回替换字符并拒绝整段输入
func TestCLIKisoUTF8RejectsInvalidSequences(t *testing.T) {
	invalid := [][]byte{
		{0x80}, {0xc0, 0x80}, {0xc1, 0xbf}, {0xf5, 0x80, 0x80, 0x80}, {0xfe}, {0xff},
		{0xe0, 0x80, 0x80}, {0xf0, 0x80, 0x80, 0x80}, {0xed, 0xa0, 0x80}, {0xf4, 0x90, 0x80, 0x80},
		{0xc2}, {0xe2, 0x82}, {0xf0, 0x9f, 0x8d}, {0xe2, 0x28, 0xa1}, {0xf0, 0x28, 0x8c, 0xbc},
	}
	var source strings.Builder
	source.WriteString("yobu \"kiso/utf8\";\n")
	for index, data := range invalid {
		if stdutf8.Valid(data) {
			t.Fatalf("case %d unexpectedly valid in Go", index)
		}
		fmt.Fprintf(&source, "kaku data%d: []uint8 = %s; kaku decoded%d = utf8.decode_one(data%d, 0); chant(decoded%d.value); chant(decoded%d.width); chant(decoded%d.valid); chant(utf8.valid(data%d)); chant(utf8.rune_count(data%d) == mu);\n", index, mioBytes(data), index, index, index, index, index, index, index)
	}
	want := strings.Repeat("65533\n1\niie\niie\nhai\n", len(invalid))
	stdout, stderr, exit := runUTF8Source(t, source.String())
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试空输入、末尾位置和非法字节后的继续解码
func TestCLIKisoUTF8EndAndRecovery(t *testing.T) {
	stdout, stderr, exit := runUTF8Source(t, `
yobu "kiso/utf8";
kaku empty: []uint8 = [];
kaku end = utf8.decode_one(empty, 0);
chant(end.value); chant(end.width); chant(end.valid);
chant(utf8.valid(empty));
kaku empty_count = utf8.rune_count(empty); moshi (empty_count != mu) { chant(empty_count); }
kaku data: []uint8 = [255, 65];
kaku bad = utf8.decode_one(data, 0);
kaku next = utf8.decode_one(data, bad.width);
chant(bad.value); chant(bad.width); chant(bad.valid);
chant(next.value); chant(next.width); chant(next.valid);
`)
	want := "65533\n0\niie\nhai\n0\n65533\n1\niie\n65\n1\nhai\n"
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试非法序列恢复与 Go 参考解码状态一致
func TestCLIKisoUTF8DecodeRecoveryReference(t *testing.T) {
	samples := [][]byte{
		[]byte("A你日🍡"),
		{0xef, 0xbf, 0xbd, 0x80, 0x42},
		{0xe2, 0x28, 0xa1, 0x43},
		{0xf0, 0x9f, 0x8d, 0x44},
		{0xed, 0xa0, 0x80, 0x45},
	}
	var source, want strings.Builder
	source.WriteString("yobu \"kiso/utf8\";\n")
	for index, data := range samples {
		fmt.Fprintf(&source, "kaku data%d: []uint8 = %s; kaku offset%d = 0;\n", index, mioBytes(data), index)
		fmt.Fprintf(&source, "aida (offset%d < len(data%d)) { kaku decoded = utf8.decode_one(data%d, offset%d); chant(decoded.value); chant(decoded.width); chant(decoded.valid); offset%d = offset%d + decoded.width; }\n", index, index, index, index, index, index)
		fmt.Fprintf(&source, "kaku end%d = utf8.decode_one(data%d, len(data%d)); chant(end%d.value); chant(end%d.width); chant(end%d.valid); chant(utf8.valid(data%d)); chant(utf8.rune_count(data%d) == mu);\n", index, index, index, index, index, index, index, index)
		for byteIndex := range data {
			fmt.Fprintf(&source, "chant(data%d[%d]);", index, byteIndex)
		}
		source.WriteByte('\n')

		for offset := 0; offset < len(data); {
			value, width := stdutf8.DecodeRune(data[offset:])
			valid := value != stdutf8.RuneError || width != 1
			fmt.Fprintf(&want, "%d\n%d\n%s\n", value, width, map[bool]string{true: "hai", false: "iie"}[valid])
			offset += width
		}
		want.WriteString("65533\n0\niie\n")
		fmt.Fprintf(&want, "%s\n%s\n", map[bool]string{true: "hai", false: "iie"}[stdutf8.Valid(data)], map[bool]string{true: "hai", false: "iie"}[!stdutf8.Valid(data)])
		for _, octet := range data {
			fmt.Fprintf(&want, "%d\n", octet)
		}
	}
	stdout, stderr, exit := runUTF8Source(t, source.String())
	if exit != 0 || stderr != "" || stdout != want.String() {
		t.Fatalf("exit=%d output=%q stderr=%s want=%q", exit, stdout, stderr, want.String())
	}
}

// 测试动态数组和 ByteBuffer 共享追加结果
func TestCLIKisoUTF8AppendAndByteBufferAliasing(t *testing.T) {
	stdout, stderr, exit := runUTF8Source(t, `
yobu "kiso/array";
yobu "kiso/bytes";
yobu "kiso/utf8";
kaku destination = array.with_capacity[uint8](1);
kaku alias = destination;
utf8.append_rune(destination, 'A');
utf8.append_rune(alias, '你');
chant(len(destination)); chant(array.capacity(destination) >= 4);
chant(destination[0]); chant(destination[1]); chant(destination[2]); chant(destination[3]);
kaku buffer = bytes.buffer_with_capacity(4);
kaku buffer_alias = buffer;
utf8.append_rune(buffer.data, '🍡');
chant(bytes.buffer_len(buffer_alias));
kaku exported = bytes.buffer_bytes(buffer_alias);
chant(exported[0]); chant(exported[1]); chant(exported[2]); chant(exported[3]);
`)
	want := "4\nhai\n65\n228\n189\n160\n4\n240\n159\n141\n161\n"
	if exit != 0 || stderr != "" || stdout != want {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试 Unicode 标量值检查和非法输入运行时契约
func TestCLIKisoUTF8RuneValidationAndErrors(t *testing.T) {
	stdout, stderr, exit := runUTF8Source(t, `
yobu "kiso/utf8";
chant(utf8.is_valid_rune(rune(0)));
chant(utf8.is_valid_rune(rune(55295)));
chant(utf8.is_valid_rune(rune(55296)));
chant(utf8.is_valid_rune(rune(57343)));
chant(utf8.is_valid_rune(rune(57344)));
chant(utf8.is_valid_rune(rune(1114111)));
chant(utf8.is_valid_rune(rune(1114112)));
chant(utf8.is_valid_rune(rune(-1)));
`)
	if exit != 0 || stderr != "" || stdout != "hai\nhai\niie\niie\nhai\nhai\niie\niie\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}

	tests := []struct {
		source string
		want   string
	}{
		{`yobu "kiso/utf8"; utf8.decode_one([], -1);`, "array index -1 out of bounds"},
		{`yobu "kiso/utf8"; utf8.decode_one([], 1);`, "array index 1 out of bounds"},
		{`yobu "kiso/utf8"; utf8.rune_width(rune(55296));`, "invalid Unicode scalar value"},
		{`yobu "kiso/utf8"; utf8.encode_rune(rune(1114112));`, "invalid Unicode scalar value"},
		{`yobu "kiso/utf8"; kaku data: []uint8 = [7]; utf8.append_rune(data, rune(-1));`, "invalid Unicode scalar value"},
	}
	for _, test := range tests {
		_, runtimeError, runtimeExit := runUTF8Source(t, test.source)
		if runtimeExit == 0 || !strings.Contains(runtimeError, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%q", test.source, runtimeExit, runtimeError)
		}
	}

	staticTests := []struct {
		source string
		want   string
	}{
		{`yobu "kiso/utf8"; kaku data: []int64 = [65]; utf8.valid(data);`, "expected []uint8"},
		{`yobu "kiso/utf8"; utf8.decode_one([65], uint8(0));`, "expected int64"},
		{`yobu "kiso/utf8"; kaku value: int64 = 65; utf8.encode_rune(value);`, "expected int32"},
	}
	for _, test := range staticTests {
		path := filepath.Join(t.TempDir(), "main.mio")
		writeInfrastructureSource(t, path, test.source)
		t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
		var checkOut, checkErr bytes.Buffer
		if checkExit := runCLI([]string{"check", path}, &checkOut, &checkErr); checkExit == 0 || !strings.Contains(checkErr.String(), test.want) {
			t.Fatalf("source=%q exit=%d stderr=%q", test.source, checkExit, checkErr.String())
		}
	}
}

// 测试大型混合 UTF-8 数据的校验和计数
func TestCLIKisoUTF8LargeMixedData(t *testing.T) {
	skipInShortMode(t, "跳过大型 UTF-8 数据验证")
	repeats := 2048
	if stressTestEnabled() {
		repeats = 32768
	}
	seed := []byte("Mio中文日本語🍡")
	expectedRunes := stdutf8.RuneCount(seed) * repeats
	source := fmt.Sprintf(`
yobu "kiso/array";
yobu "kiso/utf8";
kaku seed: []uint8 = %s;
kaku data = array.with_capacity[uint8](len(seed) * %d);
kaku index = 0;
aida (index < %d) { array.append_all(data, seed); index = index + 1; }
chant(len(data)); chant(utf8.valid(data));
kaku count = utf8.rune_count(data); moshi (count != mu) { chant(count); }
`, mioBytes(seed), repeats, repeats)
	instructionLimit := uint64(100_000_000)
	if stressTestEnabled() {
		instructionLimit = 500_000_000
	}
	stdout := runSourceVM(t, source, instructionLimit)
	want := fmt.Sprintf("%d\nhai\n%d\n", len(seed)*repeats, expectedRunes)
	if stdout != want {
		t.Fatalf("output=%q want=%q", stdout, want)
	}
}

// 测试 UTF-8 包的命令链、函数裁剪和按需导入
func TestCLIKisoUTF8MochiCommandsAndPayForUse(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(t, sourcePath, `yobu "kiso/utf8"; chant(utf8.valid([240, 159, 141, 161]));`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", sourcePath}, &stdout, &stderr); exit != 0 || stdout.String() != "hai\n" {
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
	if len(artifact) < 7 || binary.BigEndian.Uint16(artifact[5:7]) != mochi.VersionV11 {
		t.Fatalf("UTF-8 Mochi 版本无效")
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "hai\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 ||
		!strings.Contains(stdout.String(), "kiso/utf8::valid") ||
		!strings.Contains(stdout.String(), "kiso/utf8::decode_packed") ||
		strings.Contains(stdout.String(), "kiso/utf8::decode_one") ||
		strings.Contains(stdout.String(), "kiso/utf8::encode_rune") ||
		strings.Contains(stdout.String(), "kiso/utf8::append_rune") {
		t.Fatalf("disasm exit=%d output=%s stderr=%s", exit, stdout.String(), stderr.String())
	}

	plainPath := filepath.Join(root, "plain.mio")
	unusedPath := filepath.Join(root, "unused.mio")
	plainMochi := filepath.Join(root, "plain.mochi")
	unusedMochi := filepath.Join(root, "unused.mochi")
	writeInfrastructureSource(t, plainPath, `chant(42);`)
	writeInfrastructureSource(t, unusedPath, `yobu "kiso/utf8"; chant(42);`)
	for _, pair := range [][2]string{{plainPath, plainMochi}, {unusedPath, unusedMochi}} {
		stdout.Reset()
		stderr.Reset()
		if exit := runCLI([]string{"build", "-o", pair[1], pair[0]}, &stdout, &stderr); exit != 0 {
			t.Fatalf("build %s exit=%d stderr=%s", pair[0], exit, stderr.String())
		}
	}
	plain, err := os.ReadFile(plainMochi)
	if err != nil {
		t.Fatal(err)
	}
	unused, err := os.ReadFile(unusedMochi)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(plain, unused) {
		t.Fatalf("plain=%d unused-import=%d", len(plain), len(unused))
	}
}

func benchmarkUTF8Program(b *testing.B, source string, processedBytes int64, instructionLimit uint64) {
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
	b.ReportAllocs()
	b.SetBytes(processedBytes)
	bytecodeBytes := instructionBytes(bytecode)
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, bytecode); err != nil {
		b.Fatal(err)
	}
	mochiBytes := encoded.Len()
	var instructions uint64
	if os.Getenv("MIO_BENCH_INSTRUCTIONS") != "" {
		instructions = measureUTF8Instructions(b, bytecode, instructionLimit)
	}
	b.ResetTimer()
	b.ReportMetric(float64(bytecodeBytes), "bytecode-B")
	b.ReportMetric(float64(mochiBytes), "mochi-B")
	if instructions != 0 {
		b.ReportMetric(float64(instructions), "instructions/op")
	}
	for iteration := 0; iteration < b.N; iteration++ {
		if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: instructionLimit}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkKisoUTF8ASCII 测量 ASCII 校验和码点统计
func BenchmarkKisoUTF8ASCII(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		for _, operation := range []string{"valid", "rune_count"} {
			b.Run(fmt.Sprintf("%s-%d", operation, size), func(b *testing.B) {
				source := fmt.Sprintf(`yobu "kiso/array"; yobu "kiso/utf8"; kaku data = array.make[uint8](%d, uint8(65)); utf8.%s(data);`, size, operation)
				benchmarkUTF8Program(b, source, int64(size), uint64(size)*256+10000)
			})
		}
	}
}

// BenchmarkKisoUTF8Mixed 测量中日文与 Emoji 混合扫描
func BenchmarkKisoUTF8Mixed(b *testing.B) {
	seed := []byte("Mio中文日本語🍡")
	const repeats = 4096
	source := fmt.Sprintf(`
yobu "kiso/array"; yobu "kiso/utf8";
kaku seed: []uint8 = %s; kaku data = array.with_capacity[uint8](len(seed) * %d); kaku index = 0;
aida (index < %d) { array.append_all(data, seed); index = index + 1; }
utf8.valid(data); utf8.rune_count(data);
`, mioBytes(seed), repeats, repeats)
	benchmarkUTF8Program(b, source, int64(len(seed)*repeats*2), uint64(len(seed)*repeats)*256+100000)
}

// BenchmarkKisoUTF8DecodeOne 测量单个码点解码
func BenchmarkKisoUTF8DecodeOne(b *testing.B) {
	benchmarkUTF8Program(b, `yobu "kiso/utf8"; utf8.decode_one([240, 159, 141, 161], 0);`, 4, 10000)
}

// BenchmarkKisoUTF8Invalid 测量非法 UTF-8 的快速拒绝
func BenchmarkKisoUTF8Invalid(b *testing.B) {
	for _, operation := range []string{"valid", "rune_count"} {
		b.Run(operation, func(b *testing.B) {
			benchmarkUTF8Program(b, fmt.Sprintf(`yobu "kiso/utf8"; kaku data: []uint8 = [65, 228, 189, 160, 255, 240, 159, 141, 161]; utf8.%s(data);`, operation), 5, 10000)
		})
	}
}

// BenchmarkKisoUTF8EncodeContinuous 测量连续编码和容量增长
func BenchmarkKisoUTF8EncodeContinuous(b *testing.B) {
	const count = 4096
	for _, reserved := range []bool{false, true} {
		name := "growth"
		capacity := 0
		if reserved {
			name = "reserved"
			capacity = count * 4
		}
		b.Run(name, func(b *testing.B) {
			source := fmt.Sprintf(`
yobu "kiso/array"; yobu "kiso/utf8";
kaku data = array.with_capacity[uint8](%d); kaku index = 0;
aida (index < %d) { utf8.append_rune(data, '🍡'); index = index + 1; }
`, capacity, count)
			benchmarkUTF8Program(b, source, count*4, count*256)
		})
	}
}

// BenchmarkKisoUTF8FullPipeline 测量加载、检查、编译和执行总成本
func BenchmarkKisoUTF8FullPipeline(b *testing.B) {
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, `yobu "kiso/array"; yobu "kiso/utf8"; kaku data = array.make[uint8](1024, uint8(65)); utf8.valid(data);`)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
	b.ReportAllocs()
	b.SetBytes(1024)
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		program, resolver, err := parseAndCheckFile(path)
		if err != nil {
			b.Fatal(err)
		}
		bytecode, err := compileProgram(program, resolver)
		if err != nil {
			b.Fatal(err)
		}
		if err := vm.NewWithOutput(bytecode, io.Discard).Run(); err != nil {
			b.Fatal(err)
		}
	}
}
