package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
)

// runFileCLI 执行使用已有文件的 CLI 命令
func runFileCLI(args ...string) (int, string, string) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	exitCode := runCLI(args, &stdout, &stderr)
	return exitCode, stdout.String(), stderr.String()
}

// writeSourceFile 写入测试使用的 Mio 源码
func writeSourceFile(t *testing.T, directory, name, source string) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// 测试源码构建后可重新加载执行
func TestCLIBuildAndExec(t *testing.T) {
	tests := []struct {
		name   string
		source string
	}{
		{name: "整数", source: `1 + 2 * 3;`},
		{name: "字符串", source: `"Hello, Mio!";`},
		{name: "全局变量", source: `kaku n = 40; n = n + 2; n;`},
		{name: "条件跳转", source: `kaku n = 1; moshi (n < 2) { n = 7; } sore { n = 9; } n;`},
		{name: "chant", source: `chant("Hello, Mio!");`},
		{name: "代码块局部变量", source: `kaku value = 10; moshi (hai) { kaku value = 20; chant(value); } chant(value);`},
		{name: "递归与相互递归", source: `
waza factorial(n: int64) -> int64 {
    moshi (n <= 1) { kaesu 1; } sore { kaesu n * factorial(n - 1); }
}
waza even(n: int64) -> bool {
    moshi (n == 0) { kaesu hai; } sore { kaesu odd(n - 1); }
}
waza odd(n: int64) -> bool {
    moshi (n == 0) { kaesu iie; } sore { kaesu even(n - 1); }
}
chant(factorial(6)); chant(even(10));
`},
		{name: "词法闭包", source: `
waza make_counter() -> () -> int64 {
    kaku count = 0;
    kaesu waza () -> int64 { count = count + 1; kaesu count; };
}
kaku counter = make_counter();
chant(counter()); chant(counter()); chant(counter());
`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			directory := t.TempDir()
			sourcePath := writeSourceFile(t, directory, "program.mio", tt.source)

			runCode, runOutput, runError := runFileCLI("run", sourcePath)
			if runCode != 0 || runError != "" {
				t.Fatalf("run: exit=%d stdout=%q stderr=%q", runCode, runOutput, runError)
			}

			buildCode, buildOutput, buildError := runFileCLI("build", sourcePath)
			if buildCode != 0 || buildOutput != "" || buildError != "" {
				t.Fatalf("build: exit=%d stdout=%q stderr=%q", buildCode, buildOutput, buildError)
			}

			mochiPath := filepath.Join(directory, "program"+mochi.Extension)
			if _, err := os.Stat(mochiPath); err != nil {
				t.Fatalf("stat output: %v", err)
			}

			execCode, execOutput, execError := runFileCLI("exec", mochiPath)
			if execCode != 0 || execError != "" || execOutput != runOutput {
				t.Fatalf("exec: exit=%d stdout=%q stderr=%q; want stdout=%q", execCode, execOutput, execError, runOutput)
			}
		})
	}
}

// 测试 build 的自定义输出路径
func TestCLIBuildOutputPath(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "input.mio", `42;`)
	outputPath := filepath.Join(directory, "custom.mochi")

	exitCode, stdout, stderr := runFileCLI("build", "-o", outputPath, sourcePath)
	if exitCode != 0 || stdout != "" || stderr != "" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("stat output: %v", err)
	}
}

// 测试 disasm 输出常量、全局槽位和指令
func TestCLIDisasm(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "program.mio", "kaku text = \"line\\n\\t\\\"\\\\\"; chant(text); 42;")
	mochiPath := filepath.Join(directory, "program.mochi")

	if exitCode, _, stderr := runFileCLI("build", sourcePath); exitCode != 0 {
		t.Fatalf("build: exit=%d stderr=%q", exitCode, stderr)
	}
	exitCode, stdout, stderr := runFileCLI("disasm", mochiPath)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("disasm: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	for _, want := range []string{
		"Globals: 1\n",
		"Constants (2):\n",
		`[0] string "line\n\t\"\\"`,
		"[1] int64 42\n",
		"Instructions:\n",
		"0000 OpConstant 0\n",
		"0003 OpSetGlobal 0\n",
		"0009 OpChant\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "line\n\t\"\\\n") {
		t.Fatalf("disasm executed program or emitted an unescaped string: %q", stdout)
	}
}

// 测试局部变量字节码的构建、解码和反汇编
func TestCLILocalVariableMochiAndDisassembly(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "local.mio", `
kaku value = 10;
moshi (hai) {
    kaku value = 20;
    chant(value);
}
chant(value);
`)
	mochiPath := filepath.Join(directory, "local.mochi")

	if exitCode, stdout, stderr := runFileCLI("build", sourcePath); exitCode != 0 || stdout != "" || stderr != "" {
		t.Fatalf("build: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if exitCode, stdout, stderr := runFileCLI("exec", mochiPath); exitCode != 0 || stdout != "20\n10\n" || stderr != "" {
		t.Fatalf("exec: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}

	exitCode, stdout, stderr := runFileCLI("disasm", mochiPath)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("disasm: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	for _, want := range []string{
		"Globals: 2\n",
		"OpSetGlobal 0",
		"OpSetGlobal 1",
		"OpGetGlobal 1",
		"OpGetGlobal 0",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// 测试用户函数的 build、exec 和 disasm 链路
func TestCLIFunctionBuildExecAndDisasm(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "function.mio", `
waza add(a: int64, b: int64) -> int64 {
    kaesu a + b;
}
chant(add(10, 20));
`)
	mochiPath := filepath.Join(directory, "function.mochi")
	if exitCode, stdout, stderr := runFileCLI("build", sourcePath); exitCode != 0 || stdout != "" || stderr != "" {
		t.Fatalf("build: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if exitCode, stdout, stderr := runFileCLI("exec", mochiPath); exitCode != 0 || stdout != "30\n" || stderr != "" {
		t.Fatalf("exec: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	exitCode, stdout, stderr := runFileCLI("disasm", mochiPath)
	if exitCode != 0 || stderr != "" {
		t.Fatalf("disasm: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	for _, want := range []string{
		"Functions (1):",
		"Function 0 add params=2 locals=2 returns=value:",
		"OpCall 0",
		"OpGetLocal 0",
		"OpGetLocal 1",
		"OpReturnValue",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("stdout missing %q:\n%s", want, stdout)
		}
	}
}

// 测试静态检查失败时不创建或破坏输出文件
func TestCLIBuildStaticFailurePreservesOutput(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "invalid.mio", `kaku n = 1; n = "bad";`)
	outputPath := filepath.Join(directory, "invalid.mochi")

	exitCode, _, stderr := runFileCLI("build", sourcePath)
	if exitCode != 1 || !strings.Contains(stderr, "cannot assign string") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr)
	}
	if _, err := os.Stat(outputPath); !os.IsNotExist(err) {
		t.Fatalf("unexpected output file: %v", err)
	}

	original := []byte("existing output")
	if err := os.WriteFile(outputPath, original, 0644); err != nil {
		t.Fatal(err)
	}
	exitCode, _, stderr = runFileCLI("build", "-o", outputPath, sourcePath)
	if exitCode != 1 || !strings.Contains(stderr, "cannot assign string") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, original) {
		t.Fatalf("output changed: got %q, want %q", got, original)
	}
}

// 测试 build 报告输出文件写入失败
func TestCLIBuildWriteFailure(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "program.mio", `42;`)
	outputPath := filepath.Join(directory, "missing", "program.mochi")

	exitCode, stdout, stderr := runFileCLI("build", "-o", outputPath, sourcePath)
	if exitCode != 1 || stdout != "" || !strings.Contains(stderr, "mio: build: create temporary file") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

// 测试 exec 和 disasm 拒绝非法 Mochi 文件
func TestCLIMochiDecodeErrors(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "program.mio", `42;`)
	if exitCode, _, stderr := runFileCLI("build", sourcePath); exitCode != 0 {
		t.Fatalf("build: exit=%d stderr=%q", exitCode, stderr)
	}
	valid, err := os.ReadFile(filepath.Join(directory, "program.mochi"))
	if err != nil {
		t.Fatal(err)
	}

	invalidSignature := append([]byte(nil), valid...)
	invalidSignature[0] = 'X'
	invalidVersion := append([]byte(nil), valid...)
	binary.BigEndian.PutUint16(invalidVersion[5:7], mochi.Version+1)
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{name: "非法签名", data: invalidSignature, want: "invalid file signature"},
		{name: "非法版本", data: invalidVersion, want: "unsupported format version"},
		{name: "截断文件", data: valid[:len(valid)-1], want: "unexpected EOF"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(directory, tt.name+".mochi")
			if err := os.WriteFile(path, tt.data, 0644); err != nil {
				t.Fatal(err)
			}
			for _, command := range []string{"exec", "disasm"} {
				exitCode, stdout, stderr := runFileCLI(command, path)
				if exitCode != 1 || stdout != "" || !strings.Contains(stderr, tt.want) {
					t.Fatalf("%s: exit=%d stdout=%q stderr=%q", command, exitCode, stdout, stderr)
				}
			}
		})
	}
}

// 测试 exec 报告字节码运行时错误
func TestCLIExecRuntimeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.mochi")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	bytecode := &compiler.Bytecode{Instructions: code.Instructions{byte(code.OpPop)}}
	if err := mochi.Encode(file, bytecode); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	exitCode, stdout, stderr := runFileCLI("exec", path)
	if exitCode != 1 || stdout != "" || !strings.Contains(stderr, "mio: runtime: stack underflow for OpPop") {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}

// 测试新增命令不会改变原有命令行为
func TestCLIMochiCommandsRegression(t *testing.T) {
	directory := t.TempDir()
	sourcePath := writeSourceFile(t, directory, "program.mio", `1 + 2;`)

	if exitCode, stdout, stderr := runFileCLI("check", sourcePath); exitCode != 0 || stdout != "check passed\n" || stderr != "" {
		t.Fatalf("check: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if exitCode, stdout, stderr := runFileCLI("run", sourcePath); exitCode != 0 || stdout != "3\n" || stderr != "" {
		t.Fatalf("run: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
	if exitCode, stdout, stderr := runFileCLI("version"); exitCode != 0 || !strings.Contains(stdout, version) || stderr != "" {
		t.Fatalf("version: exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
	}
}
