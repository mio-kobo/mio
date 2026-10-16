package bootstrap_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	miolang "Mio"
	"Mio/internal/bunko"
	"Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/mochi"
	"Mio/internal/resolver"
	"Mio/internal/stdlib"
	"Mio/internal/typecheck"
)

const bootstrapInstructionLimit = 500_000_000_000

type artifact struct {
	name      string
	bytes     []byte
	functions int
	constants int
}

// 测试纯 Mio 编译器连续自编译
func TestBootstrapSelfCompilation(t *testing.T) {
	if testing.Short() {
		t.Skip("自编译需要较大的执行预算")
	}
	if deadline, ok := t.Deadline(); ok && time.Until(deadline) < 30*time.Minute {
		t.Skip("完整自编译需要至少 30 分钟测试期限；请使用 go test ./selfhost/bootstrap -timeout=60m")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	previousDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previousDirectory) })

	configPath := filepath.Join(root, "selfhost", "bootstrap", "bootstrap.config")
	originalConfig, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(configPath, originalConfig, 0o644) })

	directory := os.Getenv("MIO_BOOTSTRAP_ARTIFACT_DIR")
	if directory == "" {
		directory = t.TempDir()
	} else if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Logf("bootstrap: artifact directory=%s", directory)
	compilerA := filepath.Join(directory, "compiler-A.mochi")
	compilerB := filepath.Join(directory, "compiler-B.mochi")
	compilerC := filepath.Join(directory, "compiler-C.mochi")
	compilerD := filepath.Join(directory, "compiler-D.mochi")

	artifactA := compileGoBootstrap(t, root, compilerA)
	artifactB := runCompiler(t, configPath, compilerA, compilerB, "Compiler A")
	artifactC := runCompiler(t, configPath, compilerB, compilerC, "Compiler B")
	artifactD := runCompiler(t, configPath, compilerC, compilerD, "Compiler C")

	if !bytes.Equal(artifactB.bytes, artifactC.bytes) {
		t.Fatal("Compiler B 与 Compiler C 不一致")
	}
	if !bytes.Equal(artifactC.bytes, artifactD.bytes) {
		t.Fatal("Compiler C 与 Compiler D 不一致")
	}
	for _, item := range []artifact{artifactA, artifactB, artifactC, artifactD} {
		t.Logf("bootstrap: %s size=%d sha256=%x functions=%d constants=%d", item.name, len(item.bytes), sha256.Sum256(item.bytes), item.functions, item.constants)
	}
}

// compileGoBootstrap 使用 Go 编译器生成并验证 Compiler A
func compileGoBootstrap(t *testing.T, root, outputPath string) artifact {
	t.Helper()
	registry, err := stdlib.NewRegistry()
	if err != nil {
		t.Fatal(err)
	}

	started := time.Now()
	loaded, err := bunko.LoadProgramWithRegistry(filepath.Join(root, "selfhost", "bootstrap"), filepath.Join(root, "bunko"), registry)
	if err != nil {
		t.Fatal(err)
	}
	logStage(t, "Go Compiler 源码发现、Lexer、Parser", started, "packages=%d sources=%d", loaded.PackagesCompiled, loaded.SourcesParsed)

	started = time.Now()
	if err := constant.Fold(loaded.AST); err != nil {
		t.Fatal(err)
	}
	logStage(t, "Go Compiler Constant Evaluator", started, "")

	started = time.Now()
	resolved := resolver.New()
	if err := resolved.ResolveProgram(loaded.AST); err != nil {
		t.Fatal(err)
	}
	logStage(t, "Go Compiler Resolver", started, "")

	started = time.Now()
	checker := typecheck.NewWithResolver(resolved)
	if err := checker.CheckProgram(loaded.AST); err != nil {
		t.Fatal(err)
	}
	logStage(t, "Go Compiler TypeChecker", started, "")

	started = time.Now()
	compiled := compiler.NewWithResolver(resolved)
	if err := compiled.Compile(loaded.AST); err != nil {
		t.Fatal(err)
	}
	logStage(t, "Go Compiler Codegen", started, "")

	started = time.Now()
	file, err := os.Create(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := mochi.Encode(file, compiled.Bytecode()); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	logStage(t, "Go Compiler Encoder 与 Mochi 写入", started, "size=%d", len(data))
	return verifyArtifact(t, "Compiler A", data)
}

// runCompiler 使用纯 Mio 编译器生成并验证下一轮产物
func runCompiler(t *testing.T, configPath, compilerPath, outputPath, name string) artifact {
	t.Helper()
	if err := os.WriteFile(configPath, []byte(outputPath), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(compilerPath)
	if err != nil {
		t.Fatal(err)
	}
	program, err := miolang.DecodeProgram(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bootstrap: %s Mochi 解码和结构验证完成", name)

	environment, err := miolang.NewStandardEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	defer environment.Close()
	var executed uint64
	var output bytes.Buffer
	started := time.Now()
	if err := environment.RunWithOptions(program, &output, miolang.ExecutionOptions{
		InstructionLimit: bootstrapInstructionLimit,
		InstructionCount: &executed,
		ProgressInterval: 100_000_000,
		Progress: func(progress miolang.ExecutionProgress) {
			logStage(t, fmt.Sprintf("%s 纯 Mio 编译进度", name), started, "instructions=%d function=%s offset=%d", progress.Instructions, progress.Function, progress.Offset)
		},
	}); err != nil {
		t.Fatal(err)
	}
	logStage(t, fmt.Sprintf("%s 纯 Mio 编译执行", name), started, "instructions=%d", executed)
	t.Logf("bootstrap: %s 输出=%q", name, output.String())

	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("%s 未写入 Mochi 文件: %v; 输出: %s", name, err, output.String())
	}
	return verifyArtifact(t, filepath.Base(outputPath), data)
}

// verifyArtifact 解码并验证 Mochi 产物结构
func verifyArtifact(t *testing.T, name string, data []byte) artifact {
	t.Helper()
	decoded, err := mochi.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bootstrap: %s Mochi 文件写入、解码和结构验证完成", name)
	return artifact{name: name, bytes: data, functions: len(decoded.Functions), constants: len(decoded.Constants)}
}

// logStage 记录自举测试的阶段耗时和内存
func logStage(t *testing.T, stage string, started time.Time, format string, arguments ...any) {
	t.Helper()
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	detail := ""
	if format != "" {
		detail = " " + fmt.Sprintf(format, arguments...)
	}
	t.Logf("bootstrap: %s elapsed=%s alloc=%d heap=%d sys=%d gc=%d%s", stage, time.Since(started).Round(time.Millisecond), memory.Alloc, memory.HeapAlloc, memory.Sys, memory.NumGC, detail)
}
