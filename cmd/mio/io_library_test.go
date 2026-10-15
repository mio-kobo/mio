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

	"Mio/internal/mochi"
	"Mio/internal/vm"
)

func runIOSource(t testing.TB, source string, instructionLimit uint64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	program, resolver, err := parseAndCheckFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := vm.NewWithOutput(bytecode, &output).RunWithOptions(vm.ExecutionOptions{InstructionLimit: instructionLimit}); err != nil {
		t.Fatal(err)
	}
	return output.String()
}

// 测试内存流覆盖空数据和大型数据
func TestCLIKisoIOMemorySizes(t *testing.T) {
	for _, size := range []int{0, 1, 1 << 10, 64 << 10, 1 << 20} {
		size := size
		t.Run(fmt.Sprintf("%d", size), func(t *testing.T) {
			if size == 1<<20 {
				skipInShortMode(t, "跳过 1 MiB I/O 验证")
			}
			source := fmt.Sprintf(`
yobu "kiso/array";
yobu "kiso/bytes";
yobu "kiso/io";
kaku input = array.make[uint8](%d, uint8(7));
kaku buffer = bytes.buffer_with_capacity(%d);
kaku result = io.copy_stream(io.reader_from_bytes(input), io.writer_to_buffer(buffer));
kaku stream_error = result.error;
chant(result.count); chant(stream_error == mu); chant(bytes.buffer_len(buffer));
`, size, size)
			want := fmt.Sprintf("%d\nhai\n%d\n", size, size)
			if output := runIOSource(t, source, 250_000_000); output != want {
				t.Fatalf("output=%q want=%q", output, want)
			}
		})
	}
}

// 测试部分读写、EOF 与错误进度契约
func TestCLIKisoIOPartialContracts(t *testing.T) {
	output := runIOSource(t, `
yobu "kiso/bytes";
yobu "kiso/io";
kaku source: []uint8 = [1, 2, 3, 4, 5];
kaku reader = io.reader_from_bytes(source);
kaku destination: []uint8 = [0, 0, 0, 0, 0, 0];
kaku first = io.read(reader, destination, 1, 2);
kaku full = io.read_full(reader, destination, 3, 3);
kaku full_error = full.error;
chant(first.count); chant(first.eof); chant(full.count); chant(full.eof); chant(full_error == mu);
chant(destination[1]); chant(destination[2]); chant(destination[3]); chant(destination[4]); chant(destination[5]);
kaku empty = io.read(reader, destination, 0, 0); chant(empty.count); chant(empty.eof);
`, 1_000_000)
	want := "2\niie\n3\nhai\nhai\n1\n2\n3\n4\n5\n0\nhai\n"
	if output != want {
		t.Fatalf("output=%q want=%q", output, want)
	}
}

// 测试 io 包的 run、build、exec、disasm 和 Mochi 兼容
func TestCLIKisoIOMochiCommands(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	writeInfrastructureSource(t, sourcePath, `yobu "kiso/bytes"; yobu "kiso/io"; kaku b = bytes.new_buffer(); kaku r = io.copy_stream(io.reader_from_bytes([1, 2, 3]), io.writer_to_buffer(b)); chant(r.count);`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"run", sourcePath}, &stdout, &stderr); exit != 0 || stdout.String() != "3\n" {
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
	version := binary.BigEndian.Uint16(artifact[5:7])
	if version > mochi.VersionV14 {
		t.Fatalf("Mochi version=%d", version)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "3\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 ||
		!strings.Contains(stdout.String(), "kiso/io::copy_stream") ||
		!strings.Contains(stdout.String(), "OpClosure") {
		t.Fatalf("disasm exit=%d output=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
}

func benchmarkIOProgram(b *testing.B, source string, processedBytes int64) {
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
	for range b.N {
		if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: 250_000_000}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkKisoIO 测量纯 Mio 内存流和缓冲区复用
func BenchmarkKisoIO(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		b.Run(fmt.Sprintf("memory-read/%d", size), func(b *testing.B) {
			benchmarkIOProgram(b, fmt.Sprintf(`yobu "kiso/array"; yobu "kiso/io"; kaku input = array.make[uint8](%d, uint8(7)); kaku output = array.make[uint8](%d, uint8(0)); kaku result = io.read(io.reader_from_bytes(input), output, 0, len(output));`, size, size), int64(size))
		})
		b.Run(fmt.Sprintf("memory-write/%d", size), func(b *testing.B) {
			benchmarkIOProgram(b, fmt.Sprintf(`yobu "kiso/array"; yobu "kiso/bytes"; yobu "kiso/io"; kaku input = array.make[uint8](%d, uint8(7)); kaku buffer = bytes.buffer_with_capacity(%d); kaku result = io.write(io.writer_to_buffer(buffer), input, 0, len(input));`, size, size), int64(size))
		})
		b.Run(fmt.Sprintf("copy-stream/%d", size), func(b *testing.B) {
			benchmarkIOProgram(b, fmt.Sprintf(`yobu "kiso/array"; yobu "kiso/bytes"; yobu "kiso/io"; kaku input = array.make[uint8](%d, uint8(7)); kaku buffer = bytes.buffer_with_capacity(%d); kaku result = io.copy_stream(io.reader_from_bytes(input), io.writer_to_buffer(buffer));`, size, size), int64(size))
		})
		b.Run(fmt.Sprintf("read-full/%d", size), func(b *testing.B) {
			benchmarkIOProgram(b, fmt.Sprintf(`yobu "kiso/array"; yobu "kiso/io"; kaku input = array.make[uint8](%d, uint8(7)); kaku output = array.make[uint8](%d, uint8(0)); kaku result = io.read_full(io.reader_from_bytes(input), output, 0, len(output));`, size, size), int64(size))
		})
		b.Run(fmt.Sprintf("write-all/%d", size), func(b *testing.B) {
			benchmarkIOProgram(b, fmt.Sprintf(`yobu "kiso/array"; yobu "kiso/bytes"; yobu "kiso/io"; kaku input = array.make[uint8](%d, uint8(7)); kaku buffer = bytes.buffer_with_capacity(%d); kaku result = io.write_all(io.writer_to_buffer(buffer), input, 0, len(input)); bytes.reset(buffer);`, size, size), int64(size))
		})
		b.Run(fmt.Sprintf("buffer-reuse/%d", size), func(b *testing.B) {
			benchmarkIOProgram(b, fmt.Sprintf(`yobu "kiso/array"; yobu "kiso/bytes"; yobu "kiso/io"; kaku input = array.make[uint8](%d, uint8(7)); kaku buffer = bytes.buffer_with_capacity(%d); kaku writer = io.writer_to_buffer(buffer); io.write_all(writer, input, 0, len(input)); bytes.reset(buffer); io.write_all(writer, input, 0, len(input));`, size, size), int64(size*2))
		})
	}
}
