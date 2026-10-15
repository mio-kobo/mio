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

func runArraySource(t *testing.T, source string) (string, string, int) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "main.mio")
	writeInfrastructureSource(t, path, source)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	exit := runCLI([]string{"run", path}, &stdout, &stderr)
	return stdout.String(), stderr.String(), exit
}

// 测试动态数组公开 API 和共享引用语义
func TestCLIKisoArrayAPI(t *testing.T) {
	stdout, stderr, exit := runArraySource(t, `
yobu "kiso/array";
kaku bytes = array.make[uint8](3, uint8(7));
kaku data = array.with_capacity[uint8](4);
kaku alias = data;
array.push(data, uint8(10));
array.push(alias, uint8(20));
array.reserve(data, 100);
array.push(alias, uint8(30));
chant(len(bytes)); chant(bytes[2]);
chant(len(data)); chant(data[2]);
chant(array.capacity(data) >= 103);
kaku copy = array.clone(data);
copy[0] = uint8(99);
chant(data[0]); chant(copy[0]);
`)
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if stdout != "3\n7\n3\n30\nhai\n10\n99\n" {
		t.Fatalf("output=%q", stdout)
	}
}

// 测试普通元素类型、泛型和零长度数组
func TestCLIKisoArrayGenericTypes(t *testing.T) {
	stdout, stderr, exit := runArraySource(t, `
yobu "kiso/array";
waza append_one<T>(values: []T, value: T) -> []T { array.push(values, value); kaesu values; }
waza generic_capacity<T>(capacity: int64) -> []T { kaesu array.with_capacity[T](capacity); }
waza generic_make<T>(value: T) -> []T { kaesu array.make[T](2, value); }
kaku integers = array.with_capacity[int64](0);
kaku floats = array.make[float32](2, float32(1.5));
kaku strings = array.with_capacity[string](2);
kaku nullable = array.with_capacity[int64?](2);
kaku returned = append_one(integers, 42);
array.push(strings, "Mio");
array.push(nullable, mu);
array.push(nullable, 7);
kaku empty = array.make[int64](0, 9);
kaku generic_bytes = generic_capacity[uint8](4);
array.push(generic_bytes, uint8(255));
kaku generic_strings = generic_make[string]("x");
chant(integers[0]); chant(floats[1]); chant(strings[0]);
chant(len(empty)); chant(array.capacity(empty));
chant(nullable[0] == mu); chant(nullable[1] != mu);
chant(array.capacity(generic_bytes)); chant(generic_bytes[0]); chant(generic_strings[1]);
`)
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if stdout != "42\n1.5\nMio\n0\n0\nhai\nhai\n4\n255\nx\n" {
		t.Fatalf("output=%q", stdout)
	}
}

// 测试数组经过字段、函数和闭包后仍共享同一对象
func TestCLIKisoArrayReferencePaths(t *testing.T) {
	stdout, stderr, exit := runArraySource(t, `
yobu "kiso/array";
kata Box { values: []int64; }
waza relay(values: []int64) -> []int64 { array.push(values, 2); kaesu values; }
waza capture(values: []int64) -> () -> unit {
    kaesu waza () -> unit { array.push(values, 3); kaesu; };
}
kaku values: []int64 = [];
kaku box = Box { values: values };
array.push(box.values, 1);
kaku returned = relay(values);
kaku append_captured = capture(returned);
append_captured();
chant(len(values)); chant(box.values[0]); chant(returned[1]); chant(values[2]);
`)
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if stdout != "3\n1\n2\n3\n" {
		t.Fatalf("output=%q", stdout)
	}
}

// 测试嵌套数组 clone 采用浅复制
func TestCLIKisoArrayNestedClone(t *testing.T) {
	stdout, stderr, exit := runArraySource(t, `
yobu "kiso/array";
kaku inner = [1];
kaku outer = [inner];
kaku copy = array.clone(outer);
array.push(copy[0], 2);
chant(len(outer[0])); chant(outer[0][1]);
copy[0] = [9];
chant(outer[0][0]); chant(copy[0][0]);
`)
	if exit != 0 || stderr != "" || stdout != "2\n2\n1\n9\n" {
		t.Fatalf("exit=%d output=%q stderr=%s", exit, stdout, stderr)
	}
}

// 测试数组截断、重新追加和批量自追加
func TestCLIKisoArrayBulkOperations(t *testing.T) {
	stdout, stderr, exit := runArraySource(t, `
yobu "kiso/array";
kaku values = array.with_capacity[int64](8);
array.push(values, 1); array.push(values, 2); array.push(values, 3);
kaku alias = values;
array.truncate(values, 1);
chant(len(alias)); chant(array.capacity(alias));
array.push(alias, 4);
array.append_all(values, values);
chant(len(values)); chant(values[0]); chant(values[1]); chant(values[2]); chant(values[3]);
kaku empty: []int64 = [];
array.append_all(values, empty);
chant(len(values));
`)
	if exit != 0 || stderr != "" {
		t.Fatalf("exit=%d stderr=%s", exit, stderr)
	}
	if stdout != "1\n8\n4\n1\n4\n1\n4\n4\n" {
		t.Fatalf("output=%q", stdout)
	}
}

// 测试动态数组静态类型约束
func TestCLIKisoArrayTypeErrors(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`yobu "kiso/array"; kaku values: []int64 = []; array.push(values, "Mio");`, "expected int64"},
		{`yobu "kiso/array"; kaku values: []int64 = []; array.push(values, mu);`, "expected int64"},
		{`yobu "kiso/array"; kaku initial = 1; kaku values = array.make[uint8](1, initial);`, "expected uint8"},
		{`yobu "kiso/array"; kaku values: []int64 = []; array.reserve(values, uint8(1));`, "expected int64"},
	}
	for _, test := range tests {
		_, stderr, exit := runArraySource(t, test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%s", test.source, exit, stderr)
		}
	}
}

// 测试动态数组运行时边界错误
func TestCLIKisoArrayRuntimeErrors(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`yobu "kiso/array"; array.make[int64](-1, 0);`, "cannot be negative"},
		{`yobu "kiso/array"; array.with_capacity[uint8](-1);`, "cannot be negative"},
		{`yobu "kiso/array"; kaku values: []int64 = []; array.reserve(values, -1);`, "cannot be negative"},
		{`yobu "kiso/array"; array.make[uint8](268435457, uint8(0));`, "exceeds 268435456 byte limit"},
		{`yobu "kiso/array"; kaku values = [1, 2]; array.truncate(values, -1);`, "outside range"},
		{`yobu "kiso/array"; kaku values = [1, 2]; array.truncate(values, 3);`, "outside range"},
	}
	for _, test := range tests {
		_, stderr, exit := runArraySource(t, test.source)
		if exit == 0 || !strings.Contains(stderr, test.want) {
			t.Fatalf("source=%q exit=%d stderr=%s", test.source, exit, stderr)
		}
	}
}

// 测试数组批量指令使用 Mochi v13
func TestCLIKisoArrayBulkMochiV13(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "bulk.mio")
	mochiPath := filepath.Join(directory, "bulk.mochi")
	writeInfrastructureSource(t, sourcePath, `yobu "kiso/array"; kaku values = [1]; array.append_all(values, values); array.truncate(values, 1); chant(len(values));`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV13 {
		t.Fatalf("version=%d", version)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "1\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "OpArrayAppendAll") || !strings.Contains(stdout.String(), "OpArrayTruncate") {
		t.Fatalf("disasm exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
}

// 测试动态数组支持 build、exec 和 disasm
func TestCLIKisoArrayMochiCommands(t *testing.T) {
	directory := t.TempDir()
	sourcePath := filepath.Join(directory, "array.mio")
	mochiPath := filepath.Join(directory, "array.mochi")
	writeInfrastructureSource(t, sourcePath, `yobu "kiso/array"; kaku values = array.with_capacity[uint8](2); array.push(values, uint8(42)); chant(values[0]);`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV12 {
		t.Fatalf("version=%d", version)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "42\n" {
		t.Fatalf("exec exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "OpArrayWithCapacity") || !strings.Contains(stdout.String(), "OpArrayPush") {
		t.Fatalf("disasm exit=%d output=%q stderr=%s", exit, stdout.String(), stderr.String())
	}
}

// 测试未使用数组包不增加 Mochi 内容
func TestCLIKisoArrayUnusedPackageHasNoMochiCost(t *testing.T) {
	directory := t.TempDir()
	plainSource := filepath.Join(directory, "plain.mio")
	importSource := filepath.Join(directory, "import.mio")
	plainMochi := filepath.Join(directory, "plain.mochi")
	importMochi := filepath.Join(directory, "import.mochi")
	writeInfrastructureSource(t, plainSource, `chant(42);`)
	writeInfrastructureSource(t, importSource, `yobu "kiso/array"; chant(42);`)
	t.Setenv("MIO_BUNKO", mathBunkoRoot(t))
	for _, pair := range [][2]string{{plainSource, plainMochi}, {importSource, importMochi}} {
		var stdout, stderr bytes.Buffer
		if exit := runCLI([]string{"build", "-o", pair[1], pair[0]}, &stdout, &stderr); exit != 0 {
			t.Fatalf("build exit=%d stderr=%s", exit, stderr.String())
		}
	}
	plain, _ := os.ReadFile(plainMochi)
	imported, _ := os.ReadFile(importMochi)
	if !bytes.Equal(plain, imported) {
		t.Fatalf("plain=%d imported=%d", len(plain), len(imported))
	}
}
