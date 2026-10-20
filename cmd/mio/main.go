package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"Mio/internal/ast"
	"Mio/internal/bunko"
	"Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/diagnostic"
	"Mio/internal/lexer"
	"Mio/internal/lsp"
	"Mio/internal/mochi"
	"Mio/internal/resolver"
	"Mio/internal/stdlib"
	"Mio/internal/typecheck"
	"Mio/internal/value"
	"Mio/internal/vm"
)

var version = "1.0.0-beta.1"

// main 是 Mio 命令行程序的入口
func main() {
	os.Exit(runCLI(
		os.Args[1:],
		os.Stdout,
		os.Stderr,
	))
}

// runCLI 解析并执行命令行指令
func runCLI(
	args []string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if len(args) == 1 {
		switch args[0] {
		case "version":
			fmt.Fprintf(stdout, "Mio %s\n", version)
			return 0

		case "help", "-h", "--help":
			printUsage(stdout)
			return 0
		}
	}

	if len(args) == 0 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "lsp":
		return runLSP(args[1:], os.Stdin, stdout, stderr)

	case "test":
		return runTestCommand(args[1:], stdout, stderr)

	case "check", "run":
		path, options, ok := parseDiagnosticArgs(args[1:])
		if !ok {
			printUsage(stderr)
			return 2
		}

		// 读取、解析并检查 Mio 源码
		program, r, err := parseAndCheckFile(path)
		if err != nil {
			renderCLIError(stderr, path, err, options, false)
			return 1
		}

		// 静态检查通过后直接退出
		if args[0] == "check" {
			fmt.Fprintln(stdout, "check passed")
			return 0
		}

		bytecode, err := compileProgram(program, r)
		if err != nil {
			renderCLIError(stderr, path, err, options, false)
			return 1
		}

		runtimeError, err := executeBytecode(bytecode, stdout)
		if err != nil {
			if runtimeError {
				renderCLIError(stderr, "", err, options, true)
			} else {
				fmt.Fprintln(stderr, "mio:", err)
			}
			return 1
		}

		return 0

	case "build":
		sourcePath, outputPath, ok := parseBuildArgs(args[1:])
		if !ok {
			printUsage(stderr)
			return 2
		}

		program, r, err := parseAndCheckFile(sourcePath)
		if err != nil {
			renderCLIError(stderr, sourcePath, err, cliDiagnosticOptions{}, false)
			return 1
		}
		bytecode, err := compileProgram(program, r)
		if err != nil {
			fmt.Fprintln(stderr, "mio: compile:", err)
			return 1
		}
		if err := writeMochiFile(outputPath, bytecode); err != nil {
			fmt.Fprintln(stderr, "mio: build:", err)
			return 1
		}
		return 0

	case "exec", "disasm":
		if len(args) != 2 {
			printUsage(stderr)
			return 2
		}

		bytecode, err := decodeMochiFile(args[1])
		if err != nil {
			fmt.Fprintln(stderr, "mio: decode:", err)
			return 1
		}

		if args[0] == "disasm" {
			if err := printDisassembly(stdout, bytecode); err != nil {
				fmt.Fprintln(stderr, "mio: disasm:", err)
				return 1
			}
			return 0
		}

		runtimeError, err := executeBytecode(bytecode, stdout)
		if err != nil {
			if runtimeError {
				renderCLIError(stderr, "", err, cliDiagnosticOptions{}, true)
			} else {
				fmt.Fprintln(stderr, "mio:", err)
			}
			return 1
		}
		return 0

	default:
		printUsage(stderr)
		return 2
	}
}

type cliDiagnosticOptions struct {
	color   diagnostic.ColorMode
	format  diagnostic.Format
	verbose bool
}

func parseDiagnosticArgs(args []string) (string, cliDiagnosticOptions, bool) {
	options := cliDiagnosticOptions{color: diagnostic.ColorAuto, format: diagnostic.FormatHuman}
	path := ""
	for _, arg := range args {
		switch arg {
		case "--color=auto":
			options.color = diagnostic.ColorAuto
		case "--color=always":
			options.color = diagnostic.ColorAlways
		case "--color=never":
			options.color = diagnostic.ColorNever
		case "--format=human":
			options.format = diagnostic.FormatHuman
		case "--format=short":
			options.format = diagnostic.FormatShort
		case "--format=json":
			options.format = diagnostic.FormatJSON
		case "--verbose":
			options.verbose = true
		default:
			if strings.HasPrefix(arg, "--") || path != "" || arg == "" {
				return "", cliDiagnosticOptions{}, false
			}
			path = arg
		}
	}
	return path, options, path != ""
}

func renderCLIError(out io.Writer, path string, err error, options cliDiagnosticOptions, runtime bool) {
	d := diagnosticFromError(err, runtime)
	source := ""
	if path != "" {
		if content, readErr := os.ReadFile(path); readErr == nil {
			source = string(content)
		}
	}
	if d.Path == "" {
		d.Path = path
	}
	renderer := diagnostic.Renderer{Writer: out, Source: source, Path: path, Color: options.color, Format: options.format, Verbose: options.verbose}
	if renderErr := renderer.Render(d); renderErr != nil {
		fmt.Fprintln(out, "mio:", err)
	}
}

func diagnosticFromError(err error, runtime bool) *diagnostic.Diagnostic {
	var existing *diagnostic.Diagnostic
	if errors.As(err, &existing) {
		return existing
	}
	var lexical *lexer.LexicalError
	if errors.As(err, &lexical) {
		end := lexical.Position
		end.Offset++
		end.Column++
		return diagnostic.NewError("E0001", "无法识别此处的源码", diagnostic.FromPositions(lexical.Position, end)).WithCause(err)
	}
	if runtime {
		d := &diagnostic.Diagnostic{Code: "E0501", Severity: diagnostic.SeverityError, Message: "运行时错误: mio: runtime: " + err.Error(), Cause: err}
		var runtimeErr *vm.RuntimeError
		if errors.As(err, &runtimeErr) {
			for index, frame := range runtimeErr.Stack {
				prefix := "发生于"
				if index > 0 {
					prefix = "调用自"
				}
				d.Notes = append(d.Notes, diagnostic.Note{Message: fmt.Sprintf("%s %s，字节码偏移 %d", prefix, frame.Function, frame.Offset)})
			}
		}
		return d
	}
	return (&diagnostic.Diagnostic{Code: "E0401", Severity: diagnostic.SeverityError, Message: "编译失败", Cause: err}).WithNote(err.Error())
}

// compileProgram 将经过检查的 AST 编译为字节码
func compileProgram(program *ast.Program, r *resolver.Resolver) (*compiler.Bytecode, error) {
	c := compiler.NewWithResolver(r)
	if err := c.Compile(program); err != nil {
		return nil, err
	}
	return c.Bytecode(), nil
}

// executeBytecode 执行字节码并显示最后一条普通表达式
func executeBytecode(bytecode *compiler.Bytecode, stdout io.Writer) (bool, error) {
	registry, err := stdlib.NewRegistry()
	if err != nil {
		return false, err
	}
	machine := vm.NewWithRegistryAndOutput(bytecode, registry, stdout)
	if err := machine.Run(); err != nil {
		return true, err
	}

	result, ok := machine.LastPoppedValue()
	if !ok {
		return false, nil
	}
	return false, printValue(stdout, result)
}

// parseBuildArgs 解析 build 命令参数
func parseBuildArgs(args []string) (string, string, bool) {
	if len(args) == 1 {
		sourcePath := args[0]
		extension := filepath.Ext(sourcePath)
		return sourcePath, strings.TrimSuffix(sourcePath, extension) + mochi.Extension, true
	}
	if len(args) == 3 && args[0] == "-o" && args[1] != "" {
		return args[2], args[1], true
	}
	if len(args) == 3 && args[1] == "-o" && args[2] != "" {
		return args[0], args[2], true
	}
	return "", "", false
}

// writeMochiFile 原子写入经过验证的 Mochi 文件
func writeMochiFile(path string, bytecode *compiler.Bytecode) error {
	if err := mochi.Validate(bytecode); err != nil {
		return fmt.Errorf("validate bytecode: %w", err)
	}

	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".mio-build-*")
	if err != nil {
		return fmt.Errorf("create temporary file for %q: %w", path, err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	if err := mochi.Encode(temporary, bytecode); err != nil {
		temporary.Close()
		return fmt.Errorf("encode %q: %w", path, err)
	}
	if err := temporary.Chmod(0644); err != nil {
		temporary.Close()
		return fmt.Errorf("set permissions for %q: %w", path, err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close temporary file for %q: %w", path, err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("replace %q: %w", path, err)
	}
	return nil
}

// decodeMochiFile 读取并解码 Mochi 文件
func decodeMochiFile(path string) (*compiler.Bytecode, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer file.Close()

	bytecode, err := mochi.Decode(file)
	if err != nil {
		return nil, err
	}
	return bytecode, nil
}

// printDisassembly 输出 Mochi 字节码内容
func printDisassembly(out io.Writer, bytecode *compiler.Bytecode) error {
	if err := mochi.Validate(bytecode); err != nil {
		return fmt.Errorf("validate bytecode: %w", err)
	}

	if _, err := fmt.Fprintf(out, "Globals: %d\nConstants (%d):\n", bytecode.GlobalsCount, len(bytecode.Constants)); err != nil {
		return err
	}
	for index, constant := range bytecode.Constants {
		var display string
		switch constant.Kind() {
		case value.KindInt:
			integer, _ := constant.Int64()
			display = strconv.FormatInt(integer, 10)
		case value.KindInt8, value.KindInt16, value.KindInt32:
			integer, _ := constant.SignedInteger()
			display = strconv.FormatInt(integer, 10)
		case value.KindUint8, value.KindUint16, value.KindUint32:
			integer, _ := constant.UnsignedInteger()
			display = strconv.FormatUint(integer, 10)
		case value.KindFloat:
			floating, _ := constant.Float64()
			display = strconv.FormatFloat(floating, 'g', -1, 64)
		case value.KindFloat32:
			floating, _ := constant.Float32()
			display = strconv.FormatFloat(float64(floating), 'g', -1, 32)
		case value.KindString:
			text, _ := constant.Text()
			display = strconv.Quote(text)
		default:
			return fmt.Errorf("constant %d has unsupported kind %s", index, constant.Kind())
		}
		if _, err := fmt.Fprintf(out, "  [%d] %s %s\n", index, constant.Kind(), display); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(out, "Instructions:"); err != nil {
		return err
	}
	if _, err := io.WriteString(out, bytecode.Instructions.String()); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(out, "Functions (%d):\n", len(bytecode.Functions)); err != nil {
		return err
	}
	for _, function := range bytecode.Functions {
		returnKind := "unit"
		if function.ReturnsValue {
			returnKind = "value"
		}
		if _, err := fmt.Fprintf(
			out,
			"  Function %d %s params=%d locals=%d returns=%s:\n",
			function.ID,
			function.Name,
			function.ParameterCount,
			function.LocalCount,
			returnKind,
		); err != nil {
			return err
		}
		for index, capture := range function.Captures {
			source := "upvalue"
			if capture.Local {
				source = "local"
			}
			if _, err := fmt.Fprintf(out, "    Capture %d %s=%d\n", index, source, capture.Index); err != nil {
				return err
			}
		}
		if _, err := io.WriteString(out, function.Instructions.String()); err != nil {
			return err
		}
	}
	return nil
}

// parseAndCheckFile 完成源码读取与静态语义检查
func parseAndCheckFile(
	path string,
) (*ast.Program, *resolver.Resolver, error) {
	registry, err := stdlib.NewRegistry()
	if err != nil {
		return nil, nil, err
	}
	loaded, err := bunko.LoadProgramWithRegistry(path, bunkoRoot(path), registry)
	if err != nil {
		return nil, nil, err
	}
	program := loaded.AST
	if err := constant.Fold(program); err != nil {
		return nil, nil, err
	}

	// 名称解析
	r := resolver.New()

	if err := r.ResolveProgram(program); err != nil {
		return nil, nil, err
	}

	// 静态类型检查
	checker := typecheck.NewWithResolver(r)

	if err := checker.CheckProgram(program); err != nil {
		return nil, nil, err
	}

	return program, r, nil
}

// bunkoRoot 查找标准库物理目录
func bunkoRoot(sourcePath string) string {
	if configured := os.Getenv("MIO_BUNKO"); configured != "" {
		return configured
	}
	absolute, err := filepath.Abs(sourcePath)
	if err == nil {
		current := filepath.Dir(absolute)
		for {
			candidate := filepath.Join(current, "bunko")
			if info, statErr := os.Stat(candidate); statErr == nil && info.IsDir() {
				return candidate
			}
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}
	return "bunko"
}

// printValue 显示 Mio 运行时值
func printValue(out io.Writer, v value.Value) error {
	return value.WriteLine(out, v)
}

// runLSP 启动 MioCore 内置语言服务器；stdout 只用于 JSON-RPC 帧
func runLSP(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	for _, arg := range args {
		switch arg {
		case "--stdio":
		case "help", "-h", "--help":
			fmt.Fprintln(stdout, "Usage: mio lsp [--stdio]")
			return 0
		default:
			fmt.Fprintf(stderr, "mio lsp: unknown argument %q\n", arg)
			fmt.Fprintln(stderr, "Usage: mio lsp [--stdio]")
			return 2
		}
	}

	if err := lsp.Serve(stdin, stdout, stderr, lsp.Options{Version: version}); err != nil {
		if errors.Is(err, lsp.ErrExitWithoutShutdown) {
			return 1
		}
		fmt.Fprintln(stderr, "mio lsp:", err)
		return 1
	}
	return 0
}

// printUsage 显示命令行用法
func printUsage(out io.Writer) {
	fmt.Fprintf(out, "Mio %s\n", version)
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  mio check <file.mio>")
	fmt.Fprintln(out, "  mio run <file.mio>")
	fmt.Fprintln(out, "  options: --color=auto|always|never --format=human|short|json --verbose")
	fmt.Fprintln(out, "  mio test <directory|./...> [--run=<regexp>] [--lang=zh-CN|ja-JP|en-US]")
	fmt.Fprintln(out, "  mio build [-o <file.mochi>] <file.mio>")
	fmt.Fprintln(out, "  mio exec <file.mochi>")
	fmt.Fprintln(out, "  mio disasm <file.mochi>")
	fmt.Fprintln(out, "  mio lsp [--stdio]")
	fmt.Fprintln(out, "  mio version")
	fmt.Fprintln(out, "  mio help")
}
