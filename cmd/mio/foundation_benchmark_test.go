package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"testing"

	"Mio/internal/stdlib"
	"Mio/internal/vm"
)

func benchmarkFoundationProgram(b *testing.B, source string, processedBytes int64, limit uint64) {
	b.Helper()
	sourcePath := filepath.Join(b.TempDir(), "main.mio")
	writeInfrastructureSource(b, sourcePath, source)
	b.Setenv("MIO_BUNKO", mathBunkoRoot(b))
	program, resolver, err := parseAndCheckFile(sourcePath)
	if err != nil {
		b.Fatal(err)
	}
	bytecode, err := compileProgram(program, resolver)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	if processedBytes != 0 {
		b.SetBytes(processedBytes)
	}
	b.ResetTimer()
	for range b.N {
		registry, err := stdlib.NewRegistry()
		if err != nil {
			b.Fatal(err)
		}
		machine := vm.NewWithRegistryAndOutput(bytecode, registry, io.Discard)
		if err := machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: limit}); err != nil {
			b.Fatal(err)
		}
	}
}

// 测试文件读写吞吐量
func BenchmarkKisoFilesystemReadWrite(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			target := filepath.ToSlash(filepath.Join(b.TempDir(), "data.bin"))
			source := fmt.Sprintf(`
yobu "kiso/array";
yobu "kiso/fs";
kaku data = array.make[uint8](%d, uint8(7));
kaku written = fs.write_all(%s, data);
kaku loaded = fs.read_all(%s);
`, size, strconv.Quote(target), strconv.Quote(target))
			benchmarkFoundationProgram(b, source, int64(size*2), 250_000_000)
		})
	}
}

// 测试文件打开关闭成本
func BenchmarkKisoFilesystemOpenClose(b *testing.B) {
	target := filepath.ToSlash(filepath.Join(b.TempDir(), "data.bin"))
	source := fmt.Sprintf(`
yobu "kiso/fs";
kaku opened = fs.open_write(%s);
kaku file = opened.file;
moshi (file != mu) { kaku closed = fs.close(file); }
`, strconv.Quote(target))
	benchmarkFoundationProgram(b, source, 0, 1_000_000)
}

// 测试 1 MiB 文件流式复制
func BenchmarkKisoFilesystemStreamCopy(b *testing.B) {
	root := b.TempDir()
	sourcePath := filepath.ToSlash(filepath.Join(root, "source.bin"))
	targetPath := filepath.ToSlash(filepath.Join(root, "target.bin"))
	if err := os.WriteFile(sourcePath, make([]byte, 1<<20), 0o600); err != nil {
		b.Fatal(err)
	}
	source := fmt.Sprintf(`
yobu "kiso/fs";
yobu "kiso/io";
waza copy(input: fs.File?, output: fs.File?) -> unit {
    moshi (input == mu) { kaesu; }
    moshi (output == mu) { kaesu; }
    kaku copied = io.copy_stream(fs.reader(input), fs.writer(output));
    kaku input_closed = fs.close(input); kaku output_closed = fs.close(output);
}
kaku input = fs.open_read(%s);
kaku output = fs.open_write(%s);
copy(input.file, output.file);
`, strconv.Quote(sourcePath), strconv.Quote(targetPath))
	benchmarkFoundationProgram(b, source, 1<<20, 250_000_000)
}

// 测试逻辑路径规范化和拼接
func BenchmarkKisoPathCleanJoin(b *testing.B) {
	source := `
yobu "kiso/path";
kaku index = 0;
kaku result = "";
aida (index < 1000) {
    result = path.join("/workspace/project/src", "../test/./case.mio");
    index = index + 1;
}
`
	benchmarkFoundationProgram(b, source, int64(1000*45), 100_000_000)
}

// 测试 1 MiB 固定宽度二进制往返
func BenchmarkKisoBinaryOneMiB(b *testing.B) {
	source := `
yobu "kiso/array";
yobu "kiso/encoding/binary";
kaku data = array.make[uint8](1048576, uint8(0));
kaku offset = 0;
aida (offset < len(data)) {
    kaku written = binary.write_u32_le(data, offset, uint32(2309737967));
    offset = offset + 4;
}
offset = 0;
kaku total = 0;
aida (offset < len(data)) {
    total = total + int64(binary.read_u32_le(data, offset).value);
    offset = offset + 4;
}
`
	benchmarkFoundationProgram(b, source, 2<<20, 250_000_000)
}

// 测试 Varint 编解码
func BenchmarkKisoBinaryVarint(b *testing.B) {
	source := `
yobu "kiso/encoding/binary";
kaku index = 0;
kaku total = 0;
aida (index < 10000) {
    kaku encoded = binary.write_var_i64(-123456789);
    total = total + binary.read_var_i64(encoded.data, 0).value;
    index = index + 1;
}
`
	benchmarkFoundationProgram(b, source, 100000, 100_000_000)
}

// 测试宿主固定宽度二进制往返
func BenchmarkHostBinaryOneMiB(b *testing.B) {
	data := make([]byte, 1<<20)
	b.ReportAllocs()
	b.SetBytes(2 << 20)
	for range b.N {
		for offset := 0; offset < len(data); offset += 4 {
			binary.LittleEndian.PutUint32(data[offset:], 2309737967)
		}
		var total uint64
		for offset := 0; offset < len(data); offset += 4 {
			total += uint64(binary.LittleEndian.Uint32(data[offset:]))
		}
		if total == 0 {
			b.Fatal("unexpected zero")
		}
	}
}

// 测试宿主路径规范化和拼接
func BenchmarkHostPathCleanJoin(b *testing.B) {
	b.ReportAllocs()
	b.SetBytes(1000 * 45)
	for range b.N {
		var result string
		for range 1000 {
			result = path.Join("/workspace/project/src", "../test/./case.mio")
		}
		if result == "" {
			b.Fatal("unexpected empty path")
		}
	}
}

// 测试宿主文件读写吞吐量
func BenchmarkHostFilesystemReadWrite(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		b.Run(strconv.Itoa(size), func(b *testing.B) {
			target := filepath.Join(b.TempDir(), "data.bin")
			data := make([]byte, size)
			b.ReportAllocs()
			b.SetBytes(int64(size * 2))
			for range b.N {
				if err := os.WriteFile(target, data, 0o600); err != nil {
					b.Fatal(err)
				}
				if _, err := os.ReadFile(target); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
