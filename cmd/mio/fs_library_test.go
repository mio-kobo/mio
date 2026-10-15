package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func runFilesystemSource(t testing.TB, source string) (string, string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	exit := runCLI([]string{"run", path}, &stdout, &stderr)
	return stdout.String(), stderr.String(), exit
}

// 测试大文件通过 kiso/io 流式复制
func TestKisoFilesystemLargeStreamCopy(t *testing.T) {
	if testing.Short() {
		t.Skip("跳过 1 MiB 文件流验证")
	}
	root := t.TempDir()
	sourcePath := filepath.Join(root, "source.bin")
	targetPath := filepath.Join(root, "target.bin")
	data := make([]byte, 1<<20)
	for index := range data {
		data[index] = byte(index)
	}
	if err := os.WriteFile(sourcePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`
yobu "kiso/fs";
yobu "kiso/io";
waza copy_files(input: fs.File?, output: fs.File?) -> unit {
    moshi (input == mu) { kaesu; }
    moshi (output == mu) { kaesu; }
    kaku copied = io.copy_stream(fs.reader(input), fs.writer(output));
    kaku copy_error = copied.error;
    chant(copied.count); chant(copy_error == mu);
    kaku input_closed = fs.close(input); kaku output_closed = fs.close(output);
}
kaku input_result = fs.open_read(%s);
kaku output_result = fs.open_write(%s);
kaku input = input_result.file;
kaku output = output_result.file;
copy_files(input, output);
`, strconv.Quote(filepath.ToSlash(sourcePath)), strconv.Quote(filepath.ToSlash(targetPath)))
	stdout, stderr, exit := runFilesystemSource(t, source)
	if exit != 0 || stdout != "1048576\nhai\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%s", exit, stdout, stderr)
	}
	written, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(written, data) {
		t.Fatal("流式复制结果不一致")
	}
}

// 测试权限错误保留为可恢复文件错误
func TestKisoFilesystemPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("当前平台无法稳定构造权限拒绝")
	}
	target := filepath.Join(t.TempDir(), "private.bin")
	if err := os.WriteFile(target, []byte("secret"), 0); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`
yobu "kiso/fs";
kaku opened = fs.open_read(%s);
kaku failure = opened.error;
chant(failure != mu);
`, strconv.Quote(filepath.ToSlash(target)))
	stdout, stderr, exit := runFilesystemSource(t, source)
	if exit != 0 || stdout != "hai\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试 VM 退出清理未显式关闭的文件
func TestKisoFilesystemRuntimeCleanup(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("文件描述符计数仅在 Linux 验证")
	}
	target := filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(target, []byte("Mio"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skip("无法读取进程文件描述符")
	}
	source := fmt.Sprintf(`
yobu "kiso/fs";
kaku index = 0;
aida (index < 1000) {
    kaku opened = fs.open_read(%s);
    index = index + 1;
}
`, strconv.Quote(filepath.ToSlash(target)))
	stdout, stderr, exit := runFilesystemSource(t, source)
	if exit != 0 || stdout != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%s", exit, stdout, stderr)
	}
	after, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) > len(before)+4 {
		t.Fatalf("文件描述符疑似泄漏: before=%d after=%d", len(before), len(after))
	}
}

// 测试文件写入、读取和二进制往返
func TestKisoFilesystemBinaryRoundTrip(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "data.bin")
	source := fmt.Sprintf(`
yobu "kiso/array";
yobu "kiso/encoding/binary";
yobu "kiso/fs";
yobu "kiso/path";
kaku target = path.join(%s, "data.bin");
kaku data = array.make[uint8](8, uint8(0));
kaku encode_first = binary.write_u32_le(data, 0, uint32(2309737967));
kaku encode_second = binary.write_i32_be(data, 4, int32(-1234567));
kaku written = fs.write_all(target, data);
kaku write_error = written.error;
chant(write_error == mu);
kaku loaded = fs.read_all(target);
kaku read_error = loaded.error;
chant(read_error == mu);
chant(binary.read_u32_le(loaded.data, 0).value);
chant(binary.read_i32_be(loaded.data, 4).value);
`, strconv.Quote(filepath.ToSlash(root)))
	stdout, stderr, exit := runFilesystemSource(t, source)
	if exit != 0 || stdout != "hai\nhai\n2309737967\n-1234567\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%s", exit, stdout, stderr)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal(err)
	}
}

// 测试文件错误、重复关闭和关闭后读写
func TestKisoFilesystemLifecycleErrors(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "closed.bin")
	missing := filepath.Join(root, "missing.bin")
	source := fmt.Sprintf(`
yobu "kiso/fs";
kaku missing = fs.open_read(%s);
kaku missing_error = missing.error;
chant(missing_error != mu);
kaku opened = fs.open_write(%s);
kaku file = opened.file;
moshi (file != mu) {
    kaku alias = file;
    kaku first = fs.close(file); kaku first_error = first.error; chant(first_error == mu);
    kaku second = fs.close(alias); kaku second_error = second.error; chant(second_error != mu);
    kaku result = fs.write(alias, [1], 0, 1); kaku write_error = result.error; chant(write_error != mu);
}
`, strconv.Quote(filepath.ToSlash(missing)), strconv.Quote(filepath.ToSlash(target)))
	stdout, stderr, exit := runFilesystemSource(t, source)
	if exit != 0 || stdout != "hai\nhai\nhai\nhai\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试追加写入和 kiso/io.read_full 的 EOF 传播
func TestKisoFilesystemAppendAndReadFull(t *testing.T) {
	target := filepath.ToSlash(filepath.Join(t.TempDir(), "append.bin"))
	source := fmt.Sprintf(`
yobu "kiso/fs";
yobu "kiso/io";
waza append_byte(file: fs.File?) -> unit {
    moshi (file == mu) { kaesu; }
    kaku written = fs.write(file, [3], 0, 1);
    kaku closed = fs.close(file);
}
waza read_bytes(file: fs.File?) -> unit {
    moshi (file == mu) { kaesu; }
    kaku destination: []uint8 = [0, 0, 0, 0];
    kaku result = io.read_full(fs.reader(file), destination, 0, 4);
    kaku failure = result.error;
    chant(result.count); chant(result.eof); chant(failure != mu);
    chant(destination[0]); chant(destination[1]); chant(destination[2]);
    kaku closed = fs.close(file);
}
kaku initial = fs.write_all(%s, [1, 2]);
kaku appended = fs.open_append(%s); append_byte(appended.file);
kaku opened = fs.open_read(%s); read_bytes(opened.file);
`, strconv.Quote(target), strconv.Quote(target), strconv.Quote(target))
	stdout, stderr, exit := runFilesystemSource(t, source)
	if exit != 0 || stdout != "3\nhai\nhai\n1\n2\n3\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试目录创建、状态和目录项读取
func TestKisoFilesystemDirectories(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "child")
	file := filepath.Join(root, "entry.txt")
	if err := os.WriteFile(file, []byte("Mio"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`
yobu "kiso/fs";
kaku created = fs.create_dir(%s); kaku create_error = created.error; chant(create_error == mu);
chant(fs.exists(%s)); chant(fs.is_dir(%s)); chant(fs.is_file(%s));
kaku listed = fs.read_dir(%s); kaku list_error = listed.error; chant(list_error == mu); chant(len(listed.entries));
`, strconv.Quote(filepath.ToSlash(directory)), strconv.Quote(filepath.ToSlash(directory)), strconv.Quote(filepath.ToSlash(directory)), strconv.Quote(filepath.ToSlash(file)), strconv.Quote(filepath.ToSlash(root)))
	stdout, stderr, exit := runFilesystemSource(t, source)
	if exit != 0 || !strings.HasPrefix(stdout, "hai\nhai\nhai\nhai\nhai\n2\n") {
		t.Fatalf("exit=%d stdout=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试基础库未使用时不改变 Mochi
func TestKisoFoundationUnusedImports(t *testing.T) {
	root := t.TempDir()
	plainPath := filepath.Join(root, "plain.mio")
	unusedPath := filepath.Join(root, "unused.mio")
	plainMochi := filepath.Join(root, "plain.mochi")
	unusedMochi := filepath.Join(root, "unused.mochi")
	writeInfrastructureSource(t, plainPath, `chant(42);`)
	writeInfrastructureSource(t, unusedPath, `yobu "kiso/fs"; yobu "kiso/path"; yobu "kiso/encoding/binary"; chant(42);`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	for _, pair := range [][2]string{{plainPath, plainMochi}, {unusedPath, unusedMochi}} {
		var stdout, stderr bytes.Buffer
		if exit := runCLI([]string{"build", "-o", pair[1], pair[0]}, &stdout, &stderr); exit != 0 {
			t.Fatalf("build %s: exit=%d stderr=%s", pair[0], exit, stderr.String())
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
		t.Fatalf("未使用导入改变 Mochi: plain=%d unused=%d", len(plain), len(unused))
	}
}

// 测试文件系统原生绑定可由 Mochi 重新绑定
func TestKisoFilesystemMochiExecution(t *testing.T) {
	root := t.TempDir()
	sourcePath := filepath.Join(root, "main.mio")
	mochiPath := filepath.Join(root, "main.mochi")
	target := filepath.ToSlash(filepath.Join(root, "roundtrip.bin"))
	source := fmt.Sprintf(`yobu "kiso/fs"; kaku written = fs.write_all(%s, [1, 2, 3]); kaku loaded = fs.read_all(%s); chant(len(loaded.data));`, strconv.Quote(target), strconv.Quote(target))
	writeInfrastructureSource(t, sourcePath, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "3\n" {
		t.Fatalf("exec exit=%d stdout=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
}
