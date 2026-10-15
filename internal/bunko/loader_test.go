package bunko

import (
	"Mio/internal/resolver"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 测试公开路径映射到 Bunko 内的包目录
func TestLoaderMapsPublicImportPath(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "kiso", "math")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "integer.mio"), []byte("integer"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "basic.mio"), []byte("basic"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "README.md"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "basic_test.mio"), []byte("ignored test"), 0644); err != nil {
		t.Fatal(err)
	}

	loaded, err := New(root).Load("kiso/math")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ImportPath != "kiso/math" || loaded.Directory != directory {
		t.Fatalf("unexpected package: %#v", loaded)
	}
	names := make([]string, len(loaded.Sources))
	contents := make([]string, len(loaded.Sources))
	for index, source := range loaded.Sources {
		names[index] = source.Name
		contents[index] = string(source.Content)
	}
	if !reflect.DeepEqual(names, []string{"basic.mio", "integer.mio"}) {
		t.Fatalf("source order: got %v", names)
	}
	if !reflect.DeepEqual(contents, []string{"basic", "integer"}) {
		t.Fatalf("source contents: got %v", contents)
	}
}

// 测试物理根目录不会成为公开导入路径
func TestLoaderRejectsPhysicalRootAndInvalidPaths(t *testing.T) {
	loader := New(t.TempDir())
	for _, importPath := range []string{"", "bunko/kiso/math", "../math", "kiso/../math", "/kiso/math", `kiso\math`} {
		if _, err := loader.Load(importPath); err == nil {
			t.Fatalf("Load(%q) succeeded", importPath)
		}
	}
}

// 测试符号链接不能越过 Bunko 根目录
func TestLoaderRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "source.mio"), []byte("outside"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "kiso"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "kiso", "math")); err != nil {
		t.Skipf("当前文件系统不支持符号链接: %v", err)
	}

	_, err := New(root).Load("kiso/math")
	if err == nil || !strings.Contains(err.Error(), "escapes Bunko root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 测试没有 Mio 文件的目录不会被视为包
func TestLoaderRejectsPackageWithoutSources(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "kiso", "math"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "kiso", "math", "README.md"), []byte("docs"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := New(root).Load("kiso/math")
	if err == nil || !strings.Contains(err.Error(), "has no .mio source files") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// 测试只读取明确请求的包
func TestLoaderReadsOnlyRequestedPackage(t *testing.T) {
	root := t.TempDir()
	mathDirectory := filepath.Join(root, "kiso", "math")
	if err := os.MkdirAll(mathDirectory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mathDirectory, "basic.mio"), []byte("math"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "missing"), filepath.Join(root, "kiso", "unused")); err != nil {
		t.Skipf("当前文件系统不支持符号链接: %v", err)
	}

	loaded, err := New(root).Load("kiso/math")
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 1 || string(loaded.Sources[0].Content) != "math" {
		t.Fatalf("unexpected sources: %#v", loaded.Sources)
	}
}

// 测试 Bunko 多文件包的读取开销
func BenchmarkLoaderLoadPackage(b *testing.B) {
	root := b.TempDir()
	directory := filepath.Join(root, "kiso", "math")
	if err := os.MkdirAll(directory, 0755); err != nil {
		b.Fatal(err)
	}
	for _, name := range []string{"basic.mio", "integer.mio", "float.mio", "constants.mio"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("waza sample() -> int64 { kaesu 1; }"), 0644); err != nil {
			b.Fatal(err)
		}
	}
	loader := New(root)
	b.ReportAllocs()
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		if _, err := loader.Load("kiso/math"); err != nil {
			b.Fatal(err)
		}
	}
}

// 测试无导入程序不加载 Bunko
func TestLoadProgramWithoutImportsHasZeroPackageCost(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "main.mio")
	if err := os.WriteFile(source, []byte(`chant(1);`), 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProgram(source, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PackagesCompiled != 0 || loaded.SourcesParsed != 0 {
		t.Fatalf("packages=%d sources=%d", loaded.PackagesCompiled, loaded.SourcesParsed)
	}
}

// 测试菱形依赖中的包只编译一次
func TestLoadProgramDeduplicatesDiamondDependency(t *testing.T) {
	root := t.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	files := map[string]string{
		"shared/shared.mio": `koukai waza value() -> int64 { kaesu 1; }`,
		"left/left.mio":     `yobu "shared"; koukai waza left() -> int64 { kaesu shared.value(); }`,
		"right/right.mio":   `yobu "shared"; koukai waza right() -> int64 { kaesu shared.value(); }`,
	}
	for name, source := range files {
		path := filepath.Join(bunkoRoot, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mainPath := filepath.Join(root, "main.mio")
	if err := os.WriteFile(mainPath, []byte(`yobu "left"; yobu "right"; chant(left.left() + right.right());`), 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProgram(mainPath, bunkoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PackagesCompiled != 3 || loaded.SourcesParsed != 3 {
		t.Fatalf("packages=%d sources=%d", loaded.PackagesCompiled, loaded.SourcesParsed)
	}
}

// 测试普通 Bunko 包不能访问数组内部原语
func TestLoadProgramRejectsArrayInternalOutsideKisoArray(t *testing.T) {
	root := t.TempDir()
	bunkoRoot := filepath.Join(root, "bunko")
	directory := filepath.Join(bunkoRoot, "sample")
	if err := os.MkdirAll(directory, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "sample.mio"), []byte(`koukai waza bad<T>(capacity: int64) -> []T { kaesu __array_with_capacity(capacity); }`), 0644); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(root, "main.mio")
	if err := os.WriteFile(mainPath, []byte(`yobu "sample"; sample.bad[int64](1);`), 0644); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadProgram(mainPath, bunkoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := resolver.New().ResolveProgram(loaded.AST); err == nil || !strings.Contains(err.Error(), "__array_with_capacity") {
		t.Fatalf("unexpected error: %v", err)
	}
}
