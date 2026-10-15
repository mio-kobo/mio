package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/vm"
)

var benchmarkBytesVM *vm.VM

func runBytesSource(t *testing.T, source string) (string, string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	exit := runCLI([]string{"run", path}, &stdout, &stderr)
	return stdout.String(), stderr.String(), exit
}

// 测试字节比较、查找、前后缀和边界值
func TestCLIKisoBytesReadOnlyAPI(t *testing.T) {
	stdout, stderr, exit := runBytesSource(t, `
yobu "kiso/bytes";
kaku empty: []uint8 = [];
kaku one: []uint8 = [0];
kaku values: []uint8 = [0, 1, 255, 1];
chant(bytes.equal(empty, []));
chant(bytes.equal(one, [0]));
chant(bytes.equal(values, [0, 1, 255, 1]));
chant(bytes.equal(values, [0, 1, 255]));
chant(bytes.equal(values, [0, 2, 255, 1]));
chant(bytes.compare(empty, []));
chant(bytes.compare(empty, one));
chant(bytes.compare(one, empty));
chant(bytes.compare([0, 255], [1, 0]));
chant(bytes.compare([255], [254, 255]));
chant(bytes.compare([0, 1], [0, 1, 0]));
chant(bytes.index(values, 0));
chant(bytes.index(values, 255));
chant(bytes.index(values, 2));
chant(bytes.contains(values, 1));
chant(bytes.contains(values, 2));
chant(bytes.has_prefix(values, empty));
chant(bytes.has_prefix(values, [0, 1]));
chant(bytes.has_prefix(values, [0, 2]));
chant(bytes.has_prefix(one, values));
chant(bytes.has_suffix(values, empty));
chant(bytes.has_suffix(values, [255, 1]));
chant(bytes.has_suffix(values, [0, 1]));
chant(bytes.has_suffix(one, values));
`)
	if exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	want := "hai\nhai\nhai\niie\niie\n0\n-1\n1\n-1\n1\n-1\n0\n2\n-1\nhai\niie\nhai\nhai\niie\niie\nhai\nhai\niie\niie\n"
	if stdout != want {
		t.Fatalf("output=%q, want %q", stdout, want)
	}
}

// 测试 copy 和 fill 保留数组共享引用语义
func TestCLIKisoBytesMutationAndAliasing(t *testing.T) {
	stdout, stderr, exit := runBytesSource(t, `
yobu "kiso/bytes";
kaku destination: []uint8 = [0, 0, 0, 0];
kaku alias = destination;
kaku source: []uint8 = [255, 1];
chant(bytes.copy(destination, source));
chant(alias[0]);
chant(alias[1]);
chant(alias[2]);
bytes.fill(alias, 255);
chant(destination[0]);
chant(destination[3]);
kaku short: []uint8 = [0];
chant(bytes.copy(short, [1, 2, 3]));
chant(short[0]);
kaku empty: []uint8 = [];
chant(bytes.copy(empty, source));
bytes.fill(empty, 0);
chant(len(empty));
`)
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	want := "2\n255\n1\n0\n255\n255\n1\n1\n0\n0\n"
	if stdout != want {
		t.Fatalf("output=%q, want %q", stdout, want)
	}
}

// 测试 bytes.clone 返回独立的紧凑字节数组
func TestCLIKisoBytesClone(t *testing.T) {
	stdout, stderr, exit := runBytesSource(t, `
yobu "kiso/bytes";
kaku original: []uint8 = [0, 255];
kaku copy = bytes.clone(original);
copy[0] = uint8(7);
chant(original[0]); chant(copy[0]); chant(len(copy));
`)
	if exit != 0 || stderr != "" || stdout != "0\n7\n2\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试 ByteBuffer 写入、共享、重置、克隆和独立导出
func TestCLIKisoByteBuffer(t *testing.T) {
	stdout, stderr, exit := runBytesSource(t, `
yobu "kiso/bytes";
kata Holder { buffer: bytes.ByteBuffer; }
waza relay(buffer: bytes.ByteBuffer) -> bytes.ByteBuffer { kaesu buffer; }
waza resetter(buffer: bytes.ByteBuffer) -> () -> unit {
    kaesu waza () -> unit { bytes.reset(buffer); kaesu; };
}
kaku empty = bytes.new_buffer();
chant(bytes.buffer_len(empty)); chant(bytes.buffer_capacity(empty));
kaku buffer = bytes.buffer_with_capacity(2);
kaku alias = buffer;
kaku holder = Holder { buffer: buffer };
bytes.write_byte(buffer, uint8(0)); bytes.write_byte(buffer, uint8(255));
chant(bytes.write(alias, [1, 2, 3]));
chant(bytes.buffer_len(holder.buffer)); chant(bytes.buffer_capacity(buffer) >= 5);
kaku returned = relay(buffer);
kaku cloned = bytes.buffer_clone(buffer);
kaku exported = bytes.buffer_bytes(buffer);
bytes.write_byte(cloned, uint8(9));
bytes.write_byte(buffer, uint8(8));
chant(bytes.buffer_len(cloned)); chant(bytes.buffer_len(returned)); chant(len(exported));
kaku clear = resetter(alias); clear();
chant(bytes.buffer_len(buffer)); chant(bytes.buffer_capacity(buffer) >= 5);
bytes.write_byte(holder.buffer, uint8(255));
chant(bytes.buffer_len(alias)); chant(bytes.buffer_bytes(alias)[0]);
chant(bytes.buffer_len(cloned)); chant(exported[0]); chant(exported[1]);
`)
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	want := "0\n0\n3\n5\nhai\n6\n6\n5\n0\nhai\n1\n255\n6\n0\n255\n"
	if stdout != want {
		t.Fatalf("output=%q, want %q", stdout, want)
	}
}

// 测试 ByteBuffer 多次扩容和大量批量写入
func TestCLIKisoByteBufferGrowth(t *testing.T) {
	const size = 4096
	items := make([]string, size)
	for index := range items {
		items[index] = fmt.Sprintf("%d", index&255)
	}
	literal := "[" + strings.Join(items, ",") + "]"
	stdout, stderr, exit := runBytesSource(t, fmt.Sprintf(`
yobu "kiso/bytes";
kaku buffer = bytes.new_buffer();
kaku index = 0;
aida (index < 2048) { bytes.write_byte(buffer, uint8(7)); index = index + 1; }
chant(bytes.write(buffer, %s));
chant(bytes.buffer_len(buffer)); chant(bytes.buffer_capacity(buffer) >= 6144);
`, literal))
	if exit != 0 || stderr != "" || stdout != "4096\n6144\nhai\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试 bytes API 拒绝错误数组和元素类型
func TestCLIKisoBytesStaticTypes(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{source: `yobu "kiso/bytes"; kaku values: []int64 = [1]; bytes.equal(values, values);`, want: "expected []uint8"},
		{source: `yobu "kiso/bytes"; kaku values: []uint8 = [0]; bytes.index(values, 256);`, want: "outside uint8 range"},
		{source: `yobu "kiso/bytes"; kaku values: []uint8? = [0, mu]; bytes.fill(values, 0);`, want: "expected []uint8"},
	}
	for _, test := range tests {
		path := filepath.Join(t.TempDir(), "main.mio")
		writeInfrastructureSource(t, path, test.source)
		t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
		var stdout, stderr bytes.Buffer
		if exit := runCLI([]string{"check", path}, &stdout, &stderr); exit == 0 || !strings.Contains(stderr.String(), test.want) {
			t.Fatalf("source=%q exit=%d stderr=%s", test.source, exit, stderr.String())
		}
	}
}

// 测试大型数组顺序遍历、比较和查找
func TestCLIKisoBytesLargeArray(t *testing.T) {
	const size = 4096
	items := make([]string, size)
	for index := range items {
		items[index] = fmt.Sprintf("%d", index&255)
	}
	literal := "[" + strings.Join(items, ",") + "]"
	stdout, stderr, exit := runBytesSource(t, fmt.Sprintf(`
yobu "kiso/bytes";
kaku left: []uint8 = %s;
kaku right: []uint8 = %s;
chant(bytes.equal(left, right));
chant(bytes.compare(left, right));
chant(bytes.index(left, 255));
right[4095] = 0;
chant(bytes.equal(left, right));
chant(bytes.compare(left, right));
`, literal, literal))
	if exit != 0 || stdout != "hai\n0\n255\niie\n1\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试 bytes 包的 run、build、exec 和 disasm 一致性
func TestCLIKisoBytesMochiCommands(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(t, sourcePath, `yobu "kiso/bytes"; kaku values: []uint8 = [0, 255]; chant(bytes.contains(values, 255));`)
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
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "hai\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "kiso/bytes::contains") || !strings.Contains(stdout.String(), "kiso/bytes::index") || strings.Contains(stdout.String(), "kiso/bytes::equal") || strings.Contains(stdout.String(), "kiso/bytes::fill") {
		t.Fatalf("disasm exit=%d output=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

// 测试未导入 bytes 时不增加 Mochi 体积
func TestCLIKisoBytesPayForUse(t *testing.T) {
	root := t.TempDir()
	plainPath := filepath.Join(root, "plain.mio")
	importedPath := filepath.Join(root, "imported.mio")
	plainMochi := filepath.Join(root, "plain.mochi")
	importedMochi := filepath.Join(root, "imported.mochi")
	writeInfrastructureSource(t, plainPath, `chant(42);`)
	writeInfrastructureSource(t, importedPath, `yobu "kiso/bytes"; chant(42);`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	for _, pair := range [][2]string{{plainPath, plainMochi}, {importedPath, importedMochi}} {
		var stdout, stderr bytes.Buffer
		if exit := runCLI([]string{"build", "-o", pair[1], pair[0]}, &stdout, &stderr); exit != 0 {
			t.Fatalf("build %s exit=%d stderr=%s", pair[0], exit, stderr.String())
		}
	}
	plainInfo, err := os.Stat(plainMochi)
	if err != nil {
		t.Fatal(err)
	}
	importedInfo, err := os.Stat(importedMochi)
	if err != nil {
		t.Fatal(err)
	}
	if importedInfo.Size() != plainInfo.Size() {
		t.Fatalf("plain=%d imported=%d", plainInfo.Size(), importedInfo.Size())
	}
	t.Logf("plain=%d bytes imported-unused=%d bytes", plainInfo.Size(), importedInfo.Size())
}

// 测量 1 KiB bytes 算法的 VM 复用执行成本
func BenchmarkKisoBytesExecute1024(b *testing.B) {
	const size = 1024
	items := make([]string, size)
	for index := range items {
		items[index] = fmt.Sprintf("%d", index&255)
	}
	literal := "[" + strings.Join(items, ",") + "]"
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, fmt.Sprintf(`
yobu "kiso/bytes";
kaku left: []uint8 = %s;
kaku right: []uint8 = %s;
kaku same = bytes.equal(left, right);
kaku position = bytes.index(left, 254);
`, literal, literal))
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
	b.SetBytes(size * 3)
	b.ResetTimer()
	b.ReportMetric(float64(instructionBytes(bytecode)), "bytecode-B")
	for iteration := 0; iteration < b.N; iteration++ {
		if err := machine.Run(); err != nil {
			b.Fatal(err)
		}
	}
}

// 测量 bytes 字节码对应的 VM 创建成本
func BenchmarkKisoBytesVMCreation(b *testing.B) {
	path := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, path, `yobu "kiso/bytes"; kaku values: []uint8 = [0, 255]; bytes.contains(values, 255);`)
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
	for iteration := 0; iteration < b.N; iteration++ {
		benchmarkBytesVM = vm.NewWithOutput(bytecode, io.Discard)
	}
}

// BenchmarkKisoByteBufferExecute 测量端到端缓冲区写入开销
func BenchmarkKisoByteBufferExecute(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		for _, mode := range []string{"byte", "batch", "reserved", "reset"} {
			b.Run(fmt.Sprintf("%s-%d", mode, size), func(b *testing.B) {
				if mode == "byte" && size == 1<<20 {
					b.Skip("逐字节端到端写入超过当前 VM 指令上限")
				}
				capacity := 0
				operation := fmt.Sprintf(`
kaku index = 0;
aida (index < %d) { bytes.write_byte(buffer, uint8(1)); index = index + 1; }
`, size)
				if mode == "batch" || mode == "reserved" || mode == "reset" {
					operation = "bytes.write(buffer, source);"
				}
				if mode == "reserved" || mode == "reset" {
					capacity = size
				}
				if mode == "reset" {
					operation = "bytes.write(buffer, source); bytes.reset(buffer); bytes.write(buffer, source);"
				}
				path := filepath.Join(b.TempDir(), "main.mio")
				writeInfrastructureSource(b, path, fmt.Sprintf(`
yobu "kiso/array";
yobu "kiso/bytes";
kaku source = array.make[uint8](%d, uint8(1));
kaku buffer = bytes.buffer_with_capacity(%d);
%s
`, size, capacity, operation))
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
				b.SetBytes(int64(size))
				b.ReportMetric(float64(instructionBytes(bytecode)), "bytecode-B")
				b.ResetTimer()
				for iteration := 0; iteration < b.N; iteration++ {
					if err := machine.Run(); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
