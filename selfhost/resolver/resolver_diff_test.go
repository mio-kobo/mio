package resolver_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	miolang "Mio"
	"Mio/internal/ast"
	"Mio/internal/diagnostic"
	"Mio/internal/lexer"
	"Mio/internal/parser"
	goresolver "Mio/internal/resolver"
	"Mio/internal/token"
)

type normalizedSymbol struct {
	kind string
	name string
}

type normalizedResolve struct {
	symbols     []normalizedSymbol
	bindings    map[string]int
	errorKind   int
	errorOffset int
}

type sourcePackage struct {
	path         string
	directory    string
	dependencies []string
}

func resolverRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func resolverHarness(paths []string) string {
	var source strings.Builder
	source.WriteString(`
yobu "kiso/fs";
yobu "kiso/string";
yobu "selfhost/parser";
yobu "selfhost/resolver";

waza emit_text(value: string) -> unit {
    kaku data = string.to_bytes(value);
    chant(len(data));
    kaku index = 0;
    aida (index < len(data)) { chant(data[index]); index = index + 1; }
}

waza emit_path(path: string) -> unit {
    kaku loaded = fs.read_all(path);
    kaku parsed = parser.parse(loaded.data);
    kaku result = resolver.resolve_program("case", parsed);
    kaku first_error = -1;
    kaku first_offset = -1;
    moshi (len(result.errors) > 0) { first_error = resolver.resolve_error_kind_code(result.errors[0].kind); }
    moshi (len(result.errors) > 0) { first_offset = result.errors[0].span.start.offset; }
    chant(first_error);
    chant(first_offset);
    kaku count = 0;
    kaku index = 0;
    aida (index < len(result.symbols)) {
        moshi (result.symbols[index].package_path == "case" && result.symbols[index].name != "$closure") { count = count + 1; }
        index = index + 1;
    }
    chant(count);
    index = 0;
    aida (index < len(result.symbols)) {
        kaku symbol = result.symbols[index];
        moshi (symbol.package_path == "case" && symbol.name != "$closure") {
            chant(resolver.symbol_kind_code(symbol.kind));
            emit_text(symbol.name);
        }
        index = index + 1;
    }
    count = 0;
    index = 0;
    aida (index < len(result.bindings)) {
        kaku binding = result.bindings[index];
        kaku node = result.files[binding.reference.file_id].ast.nodes[binding.reference.node_id];
        moshi (parser.node_kind_code(node.kind) == 4) { count = count + 1; }
        index = index + 1;
    }
    chant(count);
    index = 0;
    aida (index < len(result.bindings)) {
        kaku binding = result.bindings[index];
        kaku node = result.files[binding.reference.file_id].ast.nodes[binding.reference.node_id];
        moshi (parser.node_kind_code(node.kind) == 4) { emit_text(result.symbols[binding.symbol_id].name); }
        index = index + 1;
    }
}
`)
	for _, path := range paths {
		fmt.Fprintf(&source, "emit_path(%s);\n", strconv.Quote(path))
	}
	return source.String()
}

func runMioResolver(t testing.TB, sources []string) []normalizedResolve {
	t.Helper()
	root := resolverRoot(t)
	directory := t.TempDir()
	paths := make([]string, len(sources))
	for index, source := range sources {
		paths[index] = filepath.Join(directory, fmt.Sprintf("case_%02d.mio", index))
		if err := os.WriteFile(paths[index], []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	harnessPath := filepath.Join(directory, "harness.mio")
	if err := os.WriteFile(harnessPath, []byte(resolverHarness(paths)), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = environment.Close() })
	program, err := environment.CompileFile(harnessPath, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 20_000_000_000}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(&output)
	readInt := func() int {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("读取 Mio Resolver 输出: %v", readErr)
		}
		value, convertErr := strconv.Atoi(strings.TrimSpace(line))
		if convertErr != nil {
			t.Fatalf("解析 Mio Resolver 输出 %q: %v", line, convertErr)
		}
		return value
	}
	readText := func() string {
		data := make([]byte, readInt())
		for index := range data {
			data[index] = byte(readInt())
		}
		return string(data)
	}
	results := make([]normalizedResolve, len(sources))
	for resultIndex := range results {
		result := normalizedResolve{errorKind: readInt(), errorOffset: readInt(), bindings: make(map[string]int)}
		for count := readInt(); count > 0; count-- {
			kindCode := readInt()
			kind := map[int]string{0: "variable", 1: "parameter", 2: "variable", 4: "function", 5: "function", 6: "type", 7: "type"}[kindCode]
			result.symbols = append(result.symbols, normalizedSymbol{kind: kind, name: readText()})
		}
		for count := readInt(); count > 0; count-- {
			result.bindings[readText()]++
		}
		sort.Slice(result.symbols, func(i, j int) bool {
			if result.symbols[i].kind != result.symbols[j].kind {
				return result.symbols[i].kind < result.symbols[j].kind
			}
			return result.symbols[i].name < result.symbols[j].name
		})
		results[resultIndex] = result
	}
	return results
}

func walkIdentifiers(value reflect.Value, visit func(*ast.Identifier)) {
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return
		}
		walkIdentifiers(value.Elem(), visit)
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		if identifier, ok := value.Interface().(*ast.Identifier); ok {
			visit(identifier)
			return
		}
		walkIdentifiers(value.Elem(), visit)
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			if value.Type().Field(index).PkgPath == "" {
				walkIdentifiers(value.Field(index), visit)
			}
		}
	case reflect.Slice:
		for index := 0; index < value.Len(); index++ {
			walkIdentifiers(value.Index(index), visit)
		}
	}
}

func runGoResolver(source string) normalizedResolve {
	result := normalizedResolve{errorKind: -1, errorOffset: -1, bindings: make(map[string]int)}
	checked, err := lexer.NewChecked(source, token.V05)
	if err != nil {
		return result
	}
	program, err := parser.New(checked).ParseProgram()
	if err != nil {
		return result
	}
	resolver := goresolver.New()
	if err := resolver.ResolveProgram(program); err != nil {
		if strings.Contains(err.Error(), "E0201") {
			result.errorKind = 0
		} else if strings.Contains(err.Error(), "E0202") {
			result.errorKind = 1
		}
		var structured *diagnostic.Diagnostic
		if errors.As(err, &structured) {
			if label, ok := structured.PrimaryLabel(); ok {
				result.errorOffset = label.Span.Start.Offset
			}
		}
		return result
	}
	seen := make(map[uint64]bool)
	declarations := make(map[string]int)
	walkIdentifiers(reflect.ValueOf(program), func(identifier *ast.Identifier) {
		symbol, ok := resolver.BindingOf(identifier)
		if !ok {
			return
		}
		if !seen[symbol.ID] && symbol.ID != 0 {
			seen[symbol.ID] = true
			result.symbols = append(result.symbols, normalizedSymbol{kind: string(symbol.Kind), name: symbol.Name})
			declarations[symbol.Name]++
		}
		result.bindings[symbol.Name]++
	})
	for name, count := range declarations {
		result.bindings[name] -= count
		if result.bindings[name] == 0 {
			delete(result.bindings, name)
		}
	}
	sort.Slice(result.symbols, func(i, j int) bool {
		if result.symbols[i].kind != result.symbols[j].kind {
			return result.symbols[i].kind < result.symbols[j].kind
		}
		return result.symbols[i].name < result.symbols[j].name
	})
	return result
}

// 测试纯 Mio Resolver 与 Go Resolver 的值名称解析结果一致
func TestMioResolverDifferential(t *testing.T) {
	sources := []string{
		"kaku global = 1; waza add(value: int64) -> int64 { kaku local = value; kaesu local + global; }",
		"waza left() -> int64 { kaesu right(); } waza right() -> int64 { kaesu left(); }",
		"kata Box { value: int64; } waza make(value: int64) -> int64 { moshi (value > 0) { kaku value = 2; kaesu value; } kaesu value; }",
	}
	actual := runMioResolver(t, sources)
	for index, source := range sources {
		expected := runGoResolver(source)
		if !reflect.DeepEqual(actual[index].symbols, expected.symbols) {
			t.Fatalf("case %d symbols\nMio=%#v\nGo=%#v", index, actual[index].symbols, expected.symbols)
		}
		if !reflect.DeepEqual(actual[index].bindings, expected.bindings) {
			t.Fatalf("case %d bindings\nMio=%#v\nGo=%#v", index, actual[index].bindings, expected.bindings)
		}
	}
}

// 测试纯 Mio Resolver 与 Go Resolver 的结构化错误类别一致
func TestMioResolverErrorDifferential(t *testing.T) {
	sources := []string{
		"kaku value = missing;",
		"waza bad(value: int64, value: int64) -> int64 { kaesu value; }",
	}
	actual := runMioResolver(t, sources)
	for index, source := range sources {
		expected := runGoResolver(source)
		if actual[index].errorKind != expected.errorKind || actual[index].errorOffset != expected.errorOffset {
			t.Fatalf("case %d error Mio=(%d,%d) Go=(%d,%d)", index, actual[index].errorKind, actual[index].errorOffset, expected.errorKind, expected.errorOffset)
		}
	}
}

// 测试固定种子的有效与非法程序差分
func TestMioResolverRandomDifferential(t *testing.T) {
	random := rand.New(rand.NewSource(20261016))
	var sources []string
	for index := 0; index < 32; index++ {
		left := random.Intn(1000)
		right := random.Intn(1000)
		sources = append(sources, fmt.Sprintf("kaku global = %d; waza calculate(value: int64) -> int64 { kaku local = %d; kaesu value + local + global; }", left, right))
	}
	for index := 0; index < 16; index++ {
		sources = append(sources, fmt.Sprintf("waza invalid%d() -> int64 { kaesu missing%d; }", index, random.Intn(1000)))
	}
	actual := runMioResolver(t, sources)
	for index, source := range sources {
		expected := runGoResolver(source)
		if actual[index].errorKind != expected.errorKind || actual[index].errorOffset != expected.errorOffset {
			t.Fatalf("case %d error Mio=(%d,%d) Go=(%d,%d)", index, actual[index].errorKind, actual[index].errorOffset, expected.errorKind, expected.errorOffset)
		}
		if expected.errorKind < 0 && (!reflect.DeepEqual(actual[index].symbols, expected.symbols) || !reflect.DeepEqual(actual[index].bindings, expected.bindings)) {
			t.Fatalf("case %d\nMio=%#v %#v\nGo=%#v %#v", index, actual[index].symbols, actual[index].bindings, expected.symbols, expected.bindings)
		}
	}
}

// 测试纯 Mio Resolver 的 Mochi 往返执行
func TestMioResolverMochiRoundTrip(t *testing.T) {
	root := resolverRoot(t)
	directory := t.TempDir()
	input := filepath.Join(directory, "input.mio")
	if err := os.WriteFile(input, []byte("waza identity(value: int64) -> int64 { kaesu value; }"), 0o644); err != nil {
		t.Fatal(err)
	}
	harness := filepath.Join(directory, "harness.mio")
	if err := os.WriteFile(harness, []byte(resolverHarness([]string{input})), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		t.Fatal(err)
	}
	decoded, err := miolang.DecodeProgram(bytes.NewReader(artifact.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(decoded, &output, miolang.ExecutionOptions{InstructionLimit: 1_000_000_000}); err != nil {
		t.Fatal(err)
	}
	if output.Len() == 0 {
		t.Fatal("Mochi 执行没有输出")
	}
}

func selfResolveHarness(t testing.TB, packages []sourcePackage) string {
	t.Helper()
	root := resolverRoot(t)
	var source strings.Builder
	source.WriteString("yobu \"kiso/array\";\nyobu \"kiso/fs\";\nyobu \"selfhost/parser\";\nyobu \"selfhost/resolver\";\n")
	indexes := make(map[string]int)
	for packageIndex, item := range packages {
		indexes[item.path] = packageIndex
		entries, err := os.ReadDir(filepath.Join(root, item.directory))
		if err != nil {
			t.Fatal(err)
		}
		var paths []string
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".mio") && !strings.HasSuffix(entry.Name(), "_test.mio") {
				paths = append(paths, filepath.Join(root, item.directory, entry.Name()))
			}
		}
		sort.Strings(paths)
		fmt.Fprintf(&source, "kaku files%d: []resolver.SourceFile = [];\n", packageIndex)
		for fileIndex, path := range paths {
			fmt.Fprintf(&source, "kaku loaded%d_%d = fs.read_all(%s);\n", packageIndex, fileIndex, strconv.Quote(path))
			fmt.Fprintf(&source, "kaku parsed%d_%d = parser.parse(loaded%d_%d.data);\n", packageIndex, fileIndex, packageIndex, fileIndex)
			fmt.Fprintf(&source, "chant(len(parsed%d_%d.errors));\n", packageIndex, fileIndex)
			fmt.Fprintf(&source, "array.push(files%d, resolver.SourceFile { file_id: %d, path: %s, ast: parsed%d_%d.ast, root: parsed%d_%d.root });\n", packageIndex, fileIndex, strconv.Quote(path), packageIndex, fileIndex, packageIndex, fileIndex)
		}
		fmt.Fprintf(&source, "kaku dependencies%d: []resolver.ResolvedPackage = [];\n", packageIndex)
		for _, dependency := range item.dependencies {
			dependencyIndex, ok := indexes[dependency]
			if !ok {
				t.Fatalf("package %s dependency %s is not ordered", item.path, dependency)
			}
			fmt.Fprintf(&source, "array.push(dependencies%d, package%d);\n", packageIndex, dependencyIndex)
		}
		fmt.Fprintf(&source, "kaku package%d = resolver.resolve_package(%s, files%d, dependencies%d);\n", packageIndex, strconv.Quote(item.path), packageIndex, packageIndex)
		fmt.Fprintf(&source, "chant(len(package%d.errors)); chant(len(package%d.symbols)); chant(len(package%d.scopes)); chant(len(package%d.bindings));\n", packageIndex, packageIndex, packageIndex, packageIndex)
	}
	return source.String()
}

// 测试纯 Mio Resolver 解析自身及实际 Bunko 源码
func TestMioResolverSelfAndBunkoSources(t *testing.T) {
	packages := []sourcePackage{
		{path: "kiso/array", directory: "bunko/kiso/array"},
		{path: "kiso/bytes", directory: "bunko/kiso/bytes", dependencies: []string{"kiso/array"}},
		{path: "kiso/utf8", directory: "bunko/kiso/utf8", dependencies: []string{"kiso/array", "kiso/bytes"}},
		{path: "kiso/string", directory: "bunko/kiso/string", dependencies: []string{"kiso/bytes", "kiso/utf8"}},
		{path: "kiso/math", directory: "bunko/kiso/math"},
		{path: "kiso/convert", directory: "bunko/kiso/convert", dependencies: []string{"kiso/array", "kiso/bytes", "kiso/math", "kiso/string"}},
		{path: "kiso/io", directory: "bunko/kiso/io", dependencies: []string{"kiso/array", "kiso/bytes"}},
		{path: "kiso/fs", directory: "bunko/kiso/fs", dependencies: []string{"kiso/array", "kiso/io"}},
		{path: "kiso/path", directory: "bunko/kiso/path", dependencies: []string{"kiso/array", "kiso/string"}},
		{path: "kiso/encoding/binary", directory: "bunko/kiso/encoding/binary", dependencies: []string{"kiso/array"}},
		{path: "kiso/testing", directory: "bunko/kiso/testing"},
		{path: "selfhost/lexer", directory: "selfhost/lexer", dependencies: []string{"kiso/array", "kiso/string", "kiso/utf8"}},
		{path: "selfhost/parser", directory: "selfhost/parser", dependencies: []string{"kiso/array", "kiso/convert", "kiso/string", "kiso/utf8", "selfhost/lexer"}},
		{path: "selfhost/resolver", directory: "selfhost/resolver", dependencies: []string{"kiso/array", "kiso/string", "selfhost/parser"}},
	}
	root := resolverRoot(t)
	directory := t.TempDir()
	harness := filepath.Join(directory, "self_resolve.mio")
	if err := os.WriteFile(harness, []byte(selfResolveHarness(t, packages)), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewScanner(&output)
	read := func() int {
		if !reader.Scan() {
			t.Fatal("纯 Mio Resolver 输出提前结束")
		}
		value, convertErr := strconv.Atoi(reader.Text())
		if convertErr != nil {
			t.Fatal(convertErr)
		}
		return value
	}
	for _, item := range packages {
		entries, readErr := os.ReadDir(filepath.Join(root, item.directory))
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, entry := range entries {
			if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), ".mio") && !strings.HasSuffix(entry.Name(), "_test.mio") && read() != 0 {
				t.Fatalf("%s/%s 纯 Mio Parser 失败", item.directory, entry.Name())
			}
		}
		errors, symbols, scopes, bindings := read(), read(), read(), read()
		if errors != 0 {
			t.Fatalf("package %s resolve errors=%d symbols=%d scopes=%d bindings=%d", item.path, errors, symbols, scopes, bindings)
		}
		if symbols == 0 || scopes < 2 {
			t.Fatalf("package %s result symbols=%d scopes=%d", item.path, symbols, scopes)
		}
	}
}

func resolverBenchmarkSource(size int) []byte {
	var source strings.Builder
	for index := 0; source.Len() < size; index++ {
		fmt.Fprintf(&source, "kaku value%d = %d;\n", index, index)
	}
	return []byte(source.String())
}

func benchmarkMioResolver(b *testing.B, size int) {
	root := resolverRoot(b)
	directory := b.TempDir()
	input := filepath.Join(directory, "input.mio")
	if err := os.WriteFile(input, resolverBenchmarkSource(size), 0o644); err != nil {
		b.Fatal(err)
	}
	harness := filepath.Join(directory, "benchmark.mio")
	source := `
yobu "kiso/fs";
yobu "selfhost/parser";
yobu "selfhost/resolver";
kaku loaded = fs.read_all(` + strconv.Quote(input) + `);
kaku parsed = parser.parse(loaded.data);
kaku result = resolver.resolve_program("benchmark", parsed);
chant(len(result.symbols));
chant(len(result.scopes));
chant(len(result.bindings));
`
	if err := os.WriteFile(harness, []byte(source), 0o644); err != nil {
		b.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		b.Fatal(err)
	}
	defer environment.Close()
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		b.Fatal(err)
	}
	var artifact bytes.Buffer
	if err := program.Encode(&artifact); err != nil {
		b.Fatal(err)
	}
	var counts bytes.Buffer
	if err := environment.RunWithOptions(program, &counts, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
		b.Fatal(err)
	}
	scanner := bufio.NewScanner(&counts)
	metrics := make([]int, 0, 3)
	for scanner.Scan() {
		value, convertErr := strconv.Atoi(scanner.Text())
		if convertErr != nil {
			b.Fatal(convertErr)
		}
		metrics = append(metrics, value)
	}
	if len(metrics) != 3 {
		b.Fatalf("metrics=%v", metrics)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := environment.RunWithOptions(program, io.Discard, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(metrics[0]), "symbols/op")
	b.ReportMetric(float64(metrics[1]), "scopes/op")
	b.ReportMetric(float64(metrics[2]), "bindings/op")
	b.ReportMetric(float64(artifact.Len()), "Mochi-bytes")
}

func benchmarkResolverParserBaseline(b *testing.B, size int) {
	root := resolverRoot(b)
	directory := b.TempDir()
	input := filepath.Join(directory, "input.mio")
	if err := os.WriteFile(input, resolverBenchmarkSource(size), 0o644); err != nil {
		b.Fatal(err)
	}
	harness := filepath.Join(directory, "benchmark.mio")
	source := `
yobu "kiso/fs";
yobu "selfhost/parser";
kaku loaded = fs.read_all(` + strconv.Quote(input) + `);
kaku parsed = parser.parse(loaded.data);
chant(len(parsed.ast.nodes));
`
	if err := os.WriteFile(harness, []byte(source), 0o644); err != nil {
		b.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		b.Fatal(err)
	}
	defer environment.Close()
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		if err := environment.RunWithOptions(program, io.Discard, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkMioResolver 测量纯 Mio Lexer、Parser 与 Resolver 完整链路
func BenchmarkMioResolver(b *testing.B) {
	for _, item := range []struct {
		name string
		size int
	}{{"1KiB", 1024}, {"64KiB", 64 * 1024}} {
		b.Run(item.name, func(b *testing.B) { benchmarkMioResolver(b, item.size) })
	}
}

// BenchmarkMioResolverParserBaseline 测量 Resolver 基准同源输入的 Lexer 与 Parser 成本
func BenchmarkMioResolverParserBaseline(b *testing.B) {
	for _, item := range []struct {
		name string
		size int
	}{{"1KiB", 1024}, {"64KiB", 64 * 1024}} {
		b.Run(item.name, func(b *testing.B) { benchmarkResolverParserBaseline(b, item.size) })
	}
}
