package codegen_test

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	miolang "Mio"
	"Mio/internal/stdlib"
	"Mio/internal/tester"
)

func codegenRoot(t testing.TB) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func pureCodegenHarness(source string) string {
	return fmt.Sprintf(`
yobu "kiso/string";
yobu "selfhost/parser";
yobu "selfhost/resolver";
yobu "selfhost/typecheck";
yobu "selfhost/codegen";

kaku parsed = parser.parse(string.to_bytes(%s));
kaku resolved = resolver.resolve_program("case", parsed);
kaku checked = typecheck.check_program(resolved);
kaku compiled = codegen.compile_program(resolved, checked);
moshi (compiled.success) { chant(1); } sore { chant(0); }
chant(len(compiled.bytes));
kaku index = 0;
aida (index < len(compiled.bytes)) { chant(int64(compiled.bytes[index])); index = index + 1; }
chant(len(compiled.errors));
index = 0;
aida (index < len(compiled.errors)) { chant(compiled.errors[index].detail); index = index + 1; }
`, strconv.Quote(source))
}

func compileWithPureMio(t testing.TB, source string) []byte {
	t.Helper()
	return compilePureHarness(t, pureCodegenHarness(source))
}

func compilePureHarness(t testing.TB, harnessSource string) []byte {
	t.Helper()
	root := codegenRoot(t)
	harness := filepath.Join(t.TempDir(), "harness.mio")
	if err := os.WriteFile(harness, []byte(harnessSource), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = environment.Close() })
	program, err := environment.CompileFile(harness, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 100_000_000_000}); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(&output)
	read := func() int {
		if !scanner.Scan() {
			t.Fatal("纯 Mio Codegen 输出提前结束")
		}
		value, convertErr := strconv.Atoi(strings.TrimSpace(scanner.Text()))
		if convertErr != nil {
			t.Fatal(convertErr)
		}
		return value
	}
	success := read()
	result := make([]byte, read())
	for index := range result {
		result[index] = byte(read())
	}
	if !scanner.Scan() {
		t.Fatal("纯 Mio Codegen 缺少错误计数")
	}
	errorCount, convertErr := strconv.Atoi(strings.TrimSpace(scanner.Text()))
	if convertErr != nil {
		t.Fatal(convertErr)
	}
	if success != 1 {
		details := make([]string, 0, errorCount)
		for scanner.Scan() {
			details = append(details, scanner.Text())
		}
		t.Fatalf("纯 Mio Codegen 编译失败: %s", strings.Join(details, "; "))
	}
	return result
}

func runPureAndGo(t *testing.T, source string) (string, string) {
	t.Helper()
	root := codegenRoot(t)
	directory := t.TempDir()
	path := filepath.Join(directory, "case.mio")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	reference, err := environment.CompileFile(path, filepath.Join(root, "bunko"))
	if err != nil {
		t.Fatal(err)
	}
	var referenceOutput bytes.Buffer
	if err := environment.Run(reference, &referenceOutput); err != nil {
		t.Fatal(err)
	}
	pure, err := miolang.DecodeProgram(bytes.NewReader(compileWithPureMio(t, source)))
	if err != nil {
		t.Fatalf("纯 Mio Mochi 解码失败: %v", err)
	}
	var pureOutput bytes.Buffer
	if err := environment.Run(pure, &pureOutput); err != nil {
		t.Fatalf("纯 Mio Mochi 执行失败: %v", err)
	}
	return referenceOutput.String(), pureOutput.String()
}

// 测试纯 Mio Codegen 的基础执行语义
func TestCodegenExecutionDifferential(t *testing.T) {
	cases := []string{
		`kaku x: int64 = 1; moshi (x < 2) { chant(x + 2); } sore { chant(0); }`,
		`waza sum(n: int64) -> int64 { kaku total: int64 = 0; kaku i: int64 = 0; aida (i < n) { total = total + i; i = i + 1; } kaesu total; } chant(sum(6));`,
		`kaku value: int64 = -9223372036854775808; chant(value); chant(iie && hai); chant(hai || iie);`,
		`teisu base: int64 = 40 + 2; waza answer() -> int64 { kaesu base; } chant(answer());`,
		`kaku values: []int64 = [1, 2, 3]; values[1] = 9; chant(values[1]); chant(len(values));`,
		`kaku values: []int64? = [1, mu]; values[0] = mu; values[1] = 9; chant(values[0] == mu); chant(values[1] != mu);`,
		`kata Pair { left: int64; right: int64; } kaku pair = Pair { right: 4, left: 3 }; chant(pair.left + pair.right);`,
		`sentaku Maybe { None; Some(int64); } kaku value = Maybe.Some(7); awase (value) { baai None { chant(0); } baai Some(item) { chant(item); } }`,
		`kaku values = Map[string, int64](); values["answer"] = 42; kaku found = values["answer"]; chant(found != mu);`,
		`kaku add = waza(value: int64) -> int64 { kaesu value + 1; }; chant(add(4));`,
		`waza closures() -> unit { kaku x: int64 = 0; kaku inc = waza() -> unit { x = x + 1; }; kaku read = waza() -> int64 { kaesu x; }; inc(); inc(); chant(read()); } closures();`,
		`waza nested() -> unit { kaku x: int64 = 3; kaku middle = waza() -> unit { kaku inner = waza() -> int64 { kaesu x; }; chant(inner()); }; middle(); } nested();`,
		`waza identity<T>(value: T) -> T { kaesu value; } chant(identity[int64](7)); chant(identity[string]("mio"));`,
		`kaku a: int8 = 127; kaku b: int8 = 1; chant(a + b); kaku c: uint32 = 4294967295; chant(c + uint32(1));`,
		`kaku precise: float32 = 1.000000059604644775390625; kaku negative: float64 = -0.0; chant(precise); chant(negative); chant(float64(precise));`,
	}
	for index, source := range cases {
		t.Run(strconv.Itoa(index), func(t *testing.T) {
			reference, pure := runPureAndGo(t, source)
			if pure != reference {
				t.Fatalf("执行输出不一致: Go=%q Mio=%q", reference, pure)
			}
		})
	}
}

// 测试纯 Mio Codegen 生成稳定的 MMA 原生函数 stub
func TestCodegenNativeStub(t *testing.T) {
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}
	functions, ok := registry.Module("mio/fs/runtime")
	if !ok {
		t.Fatal("缺少 mio/fs/runtime")
	}
	var binding string
	var parameterCount int
	var returnsValue bool
	for _, function := range functions {
		if function.ID.Name == "stat" {
			binding = function.BindingKey
			parameterCount = len(function.Signature.Parameters)
			returnsValue = string(function.Signature.Return) != "unit"
		}
	}
	if binding == "" {
		t.Fatal("缺少 stat 原生函数")
	}
	source := `yobu "mio/fs/runtime"; kaku status: []uint8 = [uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0), uint8(0)]; chant(runtime.stat(".", status)); chant(status[0]);`
	harness := fmt.Sprintf(`
yobu "kiso/string";
yobu "selfhost/parser";
yobu "selfhost/resolver";
yobu "selfhost/typecheck";
yobu "selfhost/codegen";
kaku parsed = parser.parse(string.to_bytes(%s));
kaku resolved = resolver.resolve_program("case", parsed);
kaku checked = typecheck.check_program(resolved);
kaku packages: []resolver.ResolvedPackage = [resolved];
kaku checks: []typecheck.TypeCheckResult = [checked];
kaku natives: []codegen.NativeFunctionLayout = [codegen.NativeFunctionLayout { package_path: "mio/fs/runtime", name: "stat", binding_key: %s, parameter_count: %d, returns_value: %v }];
kaku compiled = codegen.compile_modules_with_natives(packages, checks, natives);
moshi (compiled.success) { chant(1); } sore { chant(0); }
chant(len(compiled.bytes));
kaku index = 0; aida (index < len(compiled.bytes)) { chant(int64(compiled.bytes[index])); index = index + 1; }
chant(len(compiled.errors)); index = 0; aida (index < len(compiled.errors)) { chant(compiled.errors[index].detail); index = index + 1; }
`, strconv.Quote(source), strconv.Quote(binding), parameterCount, map[bool]string{true: "hai", false: "iie"}[returnsValue])
	pureBytes := compilePureHarness(t, harness)
	program, err := miolang.DecodeProgram(bytes.NewReader(pureBytes))
	if err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	var output bytes.Buffer
	if err := environment.Run(program, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(output.String(), "\n") {
		t.Fatalf("原生调用输出异常: %q", output.String())
	}
}

// 测试纯 Mio Codegen 链接跨包函数
func TestCodegenModuleExecution(t *testing.T) {
	librarySource := `koukai waza twice(value: int64) -> int64 { kaesu value * 2; }`
	appSource := `yobu "sample/lib"; chant(lib.twice(6));`
	harness := fmt.Sprintf(`
yobu "kiso/string";
yobu "selfhost/parser";
yobu "selfhost/resolver";
yobu "selfhost/typecheck";
yobu "selfhost/codegen";
kaku library_parsed = parser.parse(string.to_bytes(%s));
kaku library = resolver.resolve_program("sample/lib", library_parsed);
kaku library_types = typecheck.check_program(library);
kaku app_parsed = parser.parse(string.to_bytes(%s));
kaku app_files: []resolver.SourceFile = [resolver.SourceFile { file_id: 0, path: "app", ast: app_parsed.ast, root: app_parsed.root }];
kaku resolved_dependencies: []resolver.ResolvedPackage = [library];
kaku app = resolver.resolve_package("sample/app", app_files, resolved_dependencies);
kaku type_dependencies: []typecheck.TypeCheckResult = [library_types];
kaku app_types = typecheck.check_package(app, type_dependencies);
kaku packages: []resolver.ResolvedPackage = [library, app];
kaku checks: []typecheck.TypeCheckResult = [library_types, app_types];
kaku compiled = codegen.compile_modules(packages, checks);
moshi (compiled.success) { chant(1); } sore { chant(0); }
chant(len(compiled.bytes)); kaku index = 0; aida (index < len(compiled.bytes)) { chant(int64(compiled.bytes[index])); index = index + 1; }
chant(len(compiled.errors)); index = 0; aida (index < len(compiled.errors)) { chant(compiled.errors[index].detail); index = index + 1; }
`, strconv.Quote(librarySource), strconv.Quote(appSource))
	pure, err := miolang.DecodeProgram(bytes.NewReader(compilePureHarness(t, harness)))
	if err != nil {
		t.Fatal(err)
	}
	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	var output bytes.Buffer
	if err := environment.Run(pure, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "12\n" {
		t.Fatalf("跨包执行输出=%q", output.String())
	}
}

type codegenSourcePackage struct {
	path         string
	directory    string
	dependencies []string
}

func sourceCodegenHarness(t testing.TB, packages []codegenSourcePackage, artifact string) string {
	t.Helper()
	root := codegenRoot(t)
	var source strings.Builder
	source.WriteString("yobu \"kiso/array\";\nyobu \"kiso/fs\";\nyobu \"kiso/string\";\nyobu \"selfhost/parser\";\nyobu \"selfhost/resolver\";\nyobu \"selfhost/typecheck\";\nyobu \"selfhost/codegen\";\n")
	indexes := make(map[string]int)
	for packageIndex, item := range packages {
		indexes[item.path] = packageIndex
		entries, err := os.ReadDir(filepath.Join(root, item.directory))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&source, "kaku files%d: []resolver.SourceFile = [];\n", packageIndex)
		fileIndex := 0
		for _, entry := range entries {
			if !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".mio") || strings.HasSuffix(entry.Name(), "_test.mio") {
				continue
			}
			path := filepath.Join(root, item.directory, entry.Name())
			fmt.Fprintf(&source, "kaku loaded%d_%d = fs.read_all(%s);\n", packageIndex, fileIndex, strconv.Quote(path))
			fmt.Fprintf(&source, "kaku parsed%d_%d = parser.parse(loaded%d_%d.data);\n", packageIndex, fileIndex, packageIndex, fileIndex)
			fmt.Fprintf(&source, "array.push(files%d, resolver.SourceFile { file_id: %d, path: %s, ast: parsed%d_%d.ast, root: parsed%d_%d.root });\n", packageIndex, fileIndex, strconv.Quote(path), packageIndex, fileIndex, packageIndex, fileIndex)
			fileIndex++
		}
		fmt.Fprintf(&source, "kaku resolved_dependencies%d: []resolver.ResolvedPackage = [];\n", packageIndex)
		fmt.Fprintf(&source, "kaku type_dependencies%d: []typecheck.TypeCheckResult = [];\n", packageIndex)
		for _, dependency := range item.dependencies {
			dependencyIndex, ok := indexes[dependency]
			if !ok {
				t.Fatalf("package %s dependency %s is not ordered", item.path, dependency)
			}
			fmt.Fprintf(&source, "array.push(resolved_dependencies%d, package%d); array.push(type_dependencies%d, checked%d);\n", packageIndex, dependencyIndex, packageIndex, dependencyIndex)
		}
		fmt.Fprintf(&source, "kaku package%d = resolver.resolve_package(%s, files%d, resolved_dependencies%d);\n", packageIndex, strconv.Quote(item.path), packageIndex, packageIndex)
		fmt.Fprintf(&source, "kaku checked%d = typecheck.check_package(package%d, type_dependencies%d);\n", packageIndex, packageIndex, packageIndex)
	}
	appIndex := len(packages)
	verificationSource := fmt.Sprintf(`
yobu "kiso/math";
yobu "kiso/array";
yobu "kiso/bytes";
yobu "kiso/utf8";
yobu "kiso/string";
yobu "kiso/convert";
yobu "kiso/io";
yobu "kiso/fs";
yobu "kiso/path";
yobu "kiso/encoding/binary";
yobu "kiso/testing";
chant(math.gcd(42, 30));
kaku values = array.clone[int64]([1, 2]); array.push(values, 3); chant(len(values));
chant(bytes.equal([uint8(1)], [uint8(1)]));
chant(utf8.valid(string.to_bytes("Mio")));
chant(string.concat("Mi", "o"));
chant(convert.format_int(-42));
kaku destination: []uint8 = [uint8(0), uint8(0)];
kaku reading = io.read(io.reader_from_bytes([uint8(7), uint8(8)]), destination, 0, 2); chant(reading.count);
kaku loaded = fs.read_all(%s); chant(len(loaded.data) > 0);
chant(path.join("a/b", "../c"));
kaku binary_data: []uint8 = [uint8(0), uint8(0)]; binary.write_u16_be(binary_data, 0, uint16(4660)); chant(binary.read_u16_be(binary_data, 0).value);
testing.expect(hai, "pure codegen testing runtime");
`, strconv.Quote(filepath.Join(root, "selfhost", "codegen", "compiler.mio")))
	fmt.Fprintf(&source, "kaku parsed%d = parser.parse(string.to_bytes(%s));\n", appIndex, strconv.Quote(verificationSource))
	fmt.Fprintf(&source, "kaku files%d: []resolver.SourceFile = [resolver.SourceFile { file_id: 0, path: \"stage5/app.mio\", ast: parsed%d.ast, root: parsed%d.root }];\n", appIndex, appIndex, appIndex)
	fmt.Fprintf(&source, "kaku resolved_dependencies%d: []resolver.ResolvedPackage = []; kaku type_dependencies%d: []typecheck.TypeCheckResult = [];\n", appIndex, appIndex)
	for dependencyIndex := 0; dependencyIndex < 11; dependencyIndex++ {
		fmt.Fprintf(&source, "array.push(resolved_dependencies%d, package%d); array.push(type_dependencies%d, checked%d);\n", appIndex, dependencyIndex, appIndex, dependencyIndex)
	}
	fmt.Fprintf(&source, "kaku package%d = resolver.resolve_package(\"stage5/app\", files%d, resolved_dependencies%d); kaku checked%d = typecheck.check_package(package%d, type_dependencies%d);\n", appIndex, appIndex, appIndex, appIndex, appIndex, appIndex)
	source.WriteString("kaku packages: []resolver.ResolvedPackage = []; kaku checks: []typecheck.TypeCheckResult = [];\n")
	for packageIndex := range packages {
		fmt.Fprintf(&source, "array.push(packages, package%d); array.push(checks, checked%d);\n", packageIndex, packageIndex)
	}
	fmt.Fprintf(&source, "array.push(packages, package%d); array.push(checks, checked%d);\n", appIndex, appIndex)
	source.WriteString("kaku natives: []codegen.NativeFunctionLayout = [];\n")
	registry, err := tester.NewRegistry(&tester.Recorder{})
	if err != nil {
		t.Fatal(err)
	}
	for _, module := range []string{"mio/fs/runtime", "mio/testing/runtime"} {
		functions, ok := registry.Module(module)
		if !ok {
			t.Fatalf("缺少原生模块 %s", module)
		}
		for _, function := range functions {
			returnsValue := string(function.Signature.Return) != "unit"
			fmt.Fprintf(&source, "array.push(natives, codegen.NativeFunctionLayout { package_path: %s, name: %s, binding_key: %s, parameter_count: %d, returns_value: %v });\n", strconv.Quote(module), strconv.Quote(function.ID.Name), strconv.Quote(function.BindingKey), len(function.Signature.Parameters), map[bool]string{true: "hai", false: "iie"}[returnsValue])
		}
	}
	fmt.Fprintf(&source, "kaku compiled = codegen.compile_modules_with_natives(packages, checks, natives); chant(compiled.success); chant(len(compiled.errors)); chant(len(compiled.bytes)); chant(len(compiled.layout.constants)); chant(len(compiled.layout.functions)); chant(compiled.layout.global_count); kaku error_index = 0; aida (error_index < len(compiled.errors)) { chant(compiled.errors[error_index].detail); error_index = error_index + 1; } fs.write_all(%s, compiled.bytes);\n", strconv.Quote(artifact))
	return source.String()
}

// 测试纯 Mio Codegen 编译真实 Bunko 和自举源码
func TestCodegenSelfAndBunkoSources(t *testing.T) {
	if testing.Short() {
		t.Skip("大型纯 Mio Codegen 自检由 Full 测试覆盖")
	}
	packages := []codegenSourcePackage{
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
		{path: "selfhost/typecheck", directory: "selfhost/typecheck", dependencies: []string{"kiso/array", "kiso/convert", "kiso/string", "selfhost/parser", "selfhost/resolver"}},
		{path: "selfhost/codegen", directory: "selfhost/codegen", dependencies: []string{"kiso/array", "kiso/convert", "kiso/string", "selfhost/lexer", "selfhost/parser", "selfhost/resolver", "selfhost/typecheck"}},
	}
	root := codegenRoot(t)
	artifact := filepath.Join(t.TempDir(), "selfhost.mochi")
	harness := filepath.Join(t.TempDir(), "self_codegen.mio")
	if err := os.WriteFile(harness, []byte(sourceCodegenHarness(t, packages, artifact)), 0o644); err != nil {
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
	started := time.Now()
	var output bytes.Buffer
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{InstructionLimit: 500_000_000_000}); err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(output.String())
	if len(fields) < 6 {
		t.Fatalf("纯 Mio Codegen 自检输出=%q", output.String())
	}
	if fields[0] != "hai" || fields[1] != "0" {
		lines := strings.Split(strings.TrimSpace(output.String()), "\n")
		details := []string{}
		if len(lines) > 6 {
			details = lines[6:]
		}
		t.Fatalf("纯 Mio Codegen 自检失败: metrics=%q errors=%q", strings.Join(lines[:min(6, len(lines))], ","), details)
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := miolang.DecodeProgram(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("真实源码产物解码失败: %v", err)
	}
	runtimeRegistry, err := tester.NewRegistry(&tester.Recorder{})
	if err != nil {
		t.Fatal(err)
	}
	runtimeEnvironment := miolang.NewEnvironment(runtimeRegistry)
	defer runtimeEnvironment.Close()
	var executionOutput bytes.Buffer
	if err := runtimeEnvironment.Run(decoded, &executionOutput); err != nil {
		t.Fatalf("真实源码产物执行失败: %v", err)
	}
	const expectedOutput = "6\n3\nhai\nhai\nMio\n-42\n2\nhai\na/c\n4660\n"
	if executionOutput.String() != expectedOutput {
		t.Fatalf("真实 Bunko 程序输出=%q，期望=%q", executionOutput.String(), expectedOutput)
	}
	t.Logf("packages=%d bytes=%s constants=%s functions=%s globals=%s duration=%s", len(packages)+1, fields[2], fields[3], fields[4], fields[5], time.Since(started))
}
