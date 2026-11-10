// Package playground is the Web reference VM host used by the browser playground.
//
// It runs the Go reference front end (lexer, parser, resolver, type checker, compiler),
// the Mochi codec and the Go reference VM, compiled to js/wasm by cmd/mio-wasm. It is
// not a native AOT backend and never produces machine code: the pure-Mio native
// backends (darwin-arm64, linux-amd64, windows-amd64) are untouched by this package.
//
// Every request builds fresh compiler, registry and VM state. The capability set is
// closed: sources come only from an allowlisted, read-only Bunko file system, every host
// MMA function (file system, sockets) is bound to a stub that only fails (see
// DeniedRegistry), and stdout is the only output channel.
package playground

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"time"

	"Mio/internal/bunko"
	"Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/diagnostic"
	"Mio/internal/i18n"
	"Mio/internal/lexer"
	"Mio/internal/mochi"
	"Mio/internal/resolver"
	"Mio/internal/typecheck"
	"Mio/internal/value"
	"Mio/internal/vm"
)

const (
	// MaxSourceBytes limits one source program.
	MaxSourceBytes = 256 * 1024
	// MaxMochiBytes limits one imported Mochi file.
	MaxMochiBytes = 4 * 1024 * 1024
	// MaxOutputBytes limits the stdout a single run may produce.
	MaxOutputBytes = 1024 * 1024
	// DefaultInstructionLimit is the VM instruction budget when a request names none.
	DefaultInstructionLimit uint64 = 50_000_000
	// MaxInstructionLimit is the largest budget a request may ask for.
	MaxInstructionLimit uint64 = 2_000_000_000
	// yieldInterval is how many VM instructions run between scheduler yields.
	yieldInterval = 1 << 16
	// SourceName is the file name used for the editor buffer in diagnostics.
	SourceName = "main.mio"
)

// Operations understood by Execute.
const (
	OpRun     = "run"     // compile the source and run it
	OpCheck   = "check"   // static checks only
	OpCompile = "compile" // compile to Mochi bytes without running
	OpExec    = "exec"    // decode, validate and run Mochi bytes
)

// Statuses reported in Result.Status.
const (
	StatusOK         = "ok"
	StatusChecked    = "checked"
	StatusCompiled   = "compiled"
	StatusDiagnostic = "diagnostic"    // compile-time error in the source
	StatusRuntime    = "runtime_error" // the VM stopped with an error
	StatusRejected   = "rejected"      // invalid request or invalid Mochi bytes
	StatusInternal   = "internal_error"
)

// Request is one playground operation.
type Request struct {
	Operation        string `json:"operation"`
	Source           string `json:"source,omitempty"`
	Mochi            []byte `json:"mochi,omitempty"`
	InstructionLimit uint64 `json:"instructionLimit,omitempty"`
}

// Diagnostic is a compile-time or run-time diagnostic in structured and rendered form.
type Diagnostic struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
	Rendered string `json:"rendered"`
}

// Result is the outcome of one operation. ExitCode follows the reference CLI: 0 on
// success, 1 for diagnostics, runtime errors and rejected input.
type Result struct {
	Status       string       `json:"status"`
	ExitCode     int          `json:"exitCode"`
	Stdout       string       `json:"stdout"`
	Diagnostics  []Diagnostic `json:"diagnostics"`
	Error        string       `json:"error,omitempty"`
	Mochi        []byte       `json:"mochi,omitempty"`
	MochiVersion int          `json:"mochiVersion,omitempty"`
	CompileMS    float64      `json:"compileMs"`
	RunMS        float64      `json:"runMs"`
}

// cappedOutput accepts at most MaxOutputBytes and then fails the write, which stops the VM.
type cappedOutput struct{ buffer bytes.Buffer }

// ErrOutputLimit reports that the program exceeded MaxOutputBytes of stdout.
var ErrOutputLimit = fmt.Errorf("output limit exceeded (%d bytes)", MaxOutputBytes)

func (w *cappedOutput) Write(p []byte) (int, error) {
	remaining := MaxOutputBytes - w.buffer.Len()
	if len(p) > remaining {
		w.buffer.Write(p[:remaining])
		return remaining, ErrOutputLimit
	}
	return w.buffer.Write(p)
}

// Execute runs one request against the closed library file system.
func Execute(request Request, library fs.FS) (result Result) {
	result = Result{Status: StatusInternal, ExitCode: 1, Diagnostics: []Diagnostic{}}
	defer func() {
		if p := recover(); p != nil {
			result.Status, result.ExitCode = StatusInternal, 1
			result.Error = fmt.Sprintf("internal error: %v", p)
		}
	}()
	limit := request.InstructionLimit
	if limit == 0 {
		limit = DefaultInstructionLimit
	}
	if limit > MaxInstructionLimit {
		return rejected(result, fmt.Sprintf("instruction limit %d exceeds maximum %d", limit, MaxInstructionLimit))
	}
	switch request.Operation {
	case OpRun, OpCheck, OpCompile:
		return compileAndRun(request, library, limit, result)
	case OpExec:
		if len(request.Mochi) > MaxMochiBytes {
			return rejected(result, fmt.Sprintf("Mochi file limit exceeded (%d bytes)", MaxMochiBytes))
		}
		bytecode, version, err := DecodeMochi(request.Mochi)
		if err != nil {
			return rejected(result, err.Error())
		}
		result.MochiVersion = int(version)
		return run(bytecode, limit, result)
	default:
		return rejected(result, fmt.Sprintf("unsupported operation %q", request.Operation))
	}
}

func rejected(result Result, message string) Result {
	result.Status, result.ExitCode, result.Error = StatusRejected, 1, message
	return result
}

// DecodeMochi decodes and validates Mochi bytes with the reference codec and reports the
// header version. It accepts exactly what the reference `mio exec` accepts.
func DecodeMochi(data []byte) (*compiler.Bytecode, uint16, error) {
	bytecode, err := mochi.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	return bytecode, uint16(data[5])<<8 | uint16(data[6]), nil
}

// Compile runs the reference front end over source with dependencies from library.
func Compile(source string, library fs.FS) (*compiler.Bytecode, error) {
	loaded, err := bunko.LoadSourceFS(source, library)
	if err != nil {
		return nil, err
	}
	if err := constant.Fold(loaded.AST); err != nil {
		return nil, err
	}
	r := resolver.New()
	if err := r.ResolveProgram(loaded.AST); err != nil {
		return nil, err
	}
	if err := typecheck.NewWithResolver(r).CheckProgram(loaded.AST); err != nil {
		return nil, err
	}
	c := compiler.NewWithResolver(r)
	if err := c.Compile(loaded.AST); err != nil {
		return nil, err
	}
	return c.Bytecode(), nil
}

func staticCheck(source string, library fs.FS) error {
	loaded, err := bunko.LoadSourceFS(source, library)
	if err != nil {
		return err
	}
	if err := constant.Fold(loaded.AST); err != nil {
		return err
	}
	r := resolver.New()
	if err := r.ResolveProgram(loaded.AST); err != nil {
		return err
	}
	return typecheck.NewWithResolver(r).CheckProgram(loaded.AST)
}

func compileAndRun(request Request, library fs.FS, limit uint64, result Result) Result {
	if len(request.Source) > MaxSourceBytes {
		return rejected(result, fmt.Sprintf("source limit exceeded (%d bytes)", MaxSourceBytes))
	}
	start := time.Now()
	if request.Operation == OpCheck {
		err := staticCheck(request.Source, library)
		result.CompileMS = elapsed(start)
		if err != nil {
			return sourceDiagnostic(result, request.Source, err)
		}
		result.Status, result.ExitCode = StatusChecked, 0
		return result
	}
	bytecode, err := Compile(request.Source, library)
	result.CompileMS = elapsed(start)
	if err != nil {
		return sourceDiagnostic(result, request.Source, err)
	}
	// Always go through the real Mochi encoder and decoder, as `mio build` + `mio exec` do.
	if err := mochi.Validate(bytecode); err != nil {
		result.Error = "validate bytecode: " + err.Error()
		return result
	}
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, bytecode); err != nil {
		result.Error = "encode: " + err.Error()
		return result
	}
	data := encoded.Bytes()
	decoded, version, err := DecodeMochi(data)
	if err != nil {
		result.Error = "decode: " + err.Error()
		return result
	}
	result.MochiVersion = int(version)
	if request.Operation == OpCompile {
		result.Mochi = append([]byte(nil), data...)
		result.Status, result.ExitCode = StatusCompiled, 0
		return result
	}
	return run(decoded, limit, result)
}

// run executes bytecode like the reference `mio exec`: stdout, then the last popped value.
func run(bytecode *compiler.Bytecode, limit uint64, result Result) Result {
	registry, err := DeniedRegistry()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	output := &cappedOutput{}
	start := time.Now()
	machine := vm.NewWithRegistryAndOutput(bytecode, registry, output)
	err = machine.RunWithOptions(vm.ExecutionOptions{
		InstructionLimit: limit,
		// js/wasm has a single thread and no asynchronous preemption: unless the VM goroutine
		// yields, the garbage collector's background workers may never finish a cycle and an
		// allocation-heavy program exhausts the 4 GiB address space. Yielding here keeps the
		// VM itself unchanged.
		ProgressInterval: yieldInterval,
		Progress:         func(vm.ExecutionProgress) { runtime.Gosched() },
	})
	if err == nil {
		if last, ok := machine.LastPoppedValue(); ok {
			err = value.WriteLine(output, last)
		}
	}
	result.RunMS = elapsed(start)
	result.Stdout = output.buffer.String()
	if err != nil {
		result.Status, result.ExitCode = StatusRuntime, 1
		result.Error = err.Error()
		result.Diagnostics = append(result.Diagnostics, render(runtimeDiagnostic(err), ""))
		return result
	}
	result.Status, result.ExitCode = StatusOK, 0
	return result
}

func elapsed(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}

func sourceDiagnostic(result Result, source string, err error) Result {
	result.Status, result.ExitCode = StatusDiagnostic, 1
	result.Error = err.Error()
	result.Diagnostics = append(result.Diagnostics, render(compileDiagnostic(err), source))
	return result
}

// compileDiagnostic mirrors the reference CLI (cmd/mio diagnosticFromError, compile branch).
func compileDiagnostic(err error) *diagnostic.Diagnostic {
	var existing *diagnostic.Diagnostic
	if errors.As(err, &existing) {
		return existing
	}
	var lexical *lexer.LexicalError
	if errors.As(err, &lexical) {
		end := lexical.Position
		end.Offset++
		end.Column++
		return diagnostic.NewError("E0001", "无法识别此处的源码", diagnostic.FromPositions(lexical.Position, end)).WithCause(err).WithDisplayMessage(i18n.KeyDiagLexical)
	}
	return (&diagnostic.Diagnostic{Code: "E0401", Severity: diagnostic.SeverityError, Message: "编译失败", Cause: err}).
		WithNote(err.Error()).WithDisplayMessage(i18n.KeyTestCompileFailed)
}

// runtimeDiagnostic mirrors the reference CLI (cmd/mio diagnosticFromError, runtime branch).
func runtimeDiagnostic(err error) *diagnostic.Diagnostic {
	d := &diagnostic.Diagnostic{Code: "E0501", Severity: diagnostic.SeverityError, Message: "运行时错误: mio: runtime: " + err.Error(), Cause: err}
	d.WithRuntimeDisplayMessage(err)
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

func render(d *diagnostic.Diagnostic, source string) Diagnostic {
	path := ""
	if source != "" || d.Code != "E0501" {
		path = SourceName
	}
	if d.Path == "" {
		d.Path = path
	}
	var out bytes.Buffer
	renderer := diagnostic.Renderer{Writer: &out, Source: source, Path: path, Color: diagnostic.ColorNever, Format: diagnostic.FormatHuman}
	if err := renderer.Render(d); err != nil {
		out.Reset()
		fmt.Fprintln(&out, "mio:", d.Error())
	}
	result := Diagnostic{Code: d.Code, Message: d.Message, Rendered: out.String()}
	for _, label := range d.Labels {
		if label.Primary || result.Line == 0 {
			result.Line, result.Column = label.Span.Start.Line, label.Span.Start.Column
		}
	}
	return result
}
