// Package tester 编译并隔离执行 Mio 测试
package tester

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"Mio/internal/bunko"
	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/diagnostic"
	"Mio/internal/nativefs"
	"Mio/internal/resolver"
	"Mio/internal/typecheck"
	"Mio/internal/vm"
	"Mio/mma"
)

const runtimeModule = "mio/testing/runtime"

// Failure 保存一条用户断言失败
type Failure struct {
	Message string `json:"message"`
}

// Recorder 保存单个测试的断言结果
type Recorder struct {
	failures []Failure
}

// Failures 返回断言失败快照
func (r *Recorder) Failures() []Failure {
	return append([]Failure(nil), r.failures...)
}

// Result 保存单个测试的执行结果
type Result struct {
	Name       string
	Path       string
	Passed     bool
	Failures   []Failure
	RuntimeErr error
	Resource   bool
	Duration   time.Duration
}

// PackageResult 保存单个包的测试结果
type PackageResult struct {
	Path       string
	Discovered int
	Tests      []Result
	Duration   time.Duration
}

// CompileError 保存与单个测试声明关联的编译错误
type CompileError struct {
	TestName string
	Path     string
	Err      error
}

// Error 返回测试编译错误
func (e *CompileError) Error() string { return e.Err.Error() }

// Unwrap 返回原始编译错误
func (e *CompileError) Unwrap() error { return e.Err }

// Options 配置单个包的测试执行
type Options struct {
	Output           io.Writer
	InstructionLimit uint64
	RunPattern       string
}

// NewRegistry 创建绑定到单个记录器的测试原生模块
func NewRegistry(recorder *Recorder) (*mma.ModuleRegistry, error) {
	if recorder == nil {
		return nil, fmt.Errorf("test recorder is nil")
	}
	registry := mma.NewModuleRegistry()
	if err := nativefs.Register(registry); err != nil {
		return nil, err
	}
	err := registry.RegisterModule(mma.ModuleDefinition{
		Path: runtimeModule,
		Functions: []mma.FunctionDefinition{{
			Name: "report",
			Signature: mma.FunctionSignature{
				Parameters: []mma.Type{mma.TypeBool, mma.TypeString},
				Return:     mma.TypeUnit,
			},
			Public: true,
			Function: func(call *mma.CallContext) error {
				conditionValue, err := call.Argument(0)
				if err != nil {
					return err
				}
				condition, err := mma.Values.AsBool(conditionValue)
				if err != nil {
					return err
				}
				messageValue, err := call.Argument(1)
				if err != nil {
					return err
				}
				message, err := mma.Values.AsString(messageValue)
				if err != nil {
					return err
				}
				if !condition {
					recorder.failures = append(recorder.failures, Failure{Message: message})
				}
				return nil
			},
		}},
	})
	return registry, err
}

// RunPackage 编译一次测试包并使用独立 VM 执行每个测试
func RunPackage(packagePath, bunkoRoot string, options Options) (*PackageResult, error) {
	started := time.Now()
	if options.Output == nil {
		options.Output = io.Discard
	}
	base, tests, err := compilePackage(packagePath, bunkoRoot)
	if err != nil {
		return nil, err
	}
	result := &PackageResult{Path: packagePath, Discovered: len(tests)}
	var matcher *regexp.Regexp
	if options.RunPattern != "" {
		matcher, err = regexp.Compile(options.RunPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid --run pattern %q: %w", options.RunPattern, err)
		}
	}
	for _, test := range tests {
		if matcher != nil && !matcher.MatchString(test.Name) {
			continue
		}
		result.Tests = append(result.Tests, runTest(base, test, options))
	}
	result.Duration = time.Since(started)
	return result, nil
}

func compilePackage(packagePath, bunkoRoot string) (*compiler.Bytecode, []bunko.TestFunction, error) {
	compileRecorder := &Recorder{}
	registry, err := NewRegistry(compileRecorder)
	if err != nil {
		return nil, nil, err
	}
	loaded, err := bunko.LoadTestProgramWithRegistry(packagePath, bunkoRoot, registry)
	if err != nil {
		return nil, nil, err
	}
	if len(loaded.Tests) == 0 {
		return nil, nil, nil
	}
	for _, test := range loaded.Tests {
		if err := validateTest(test); err != nil {
			return nil, nil, err
		}
	}
	if err := constant.Fold(loaded.AST); err != nil {
		return nil, nil, err
	}
	r := resolver.New()
	if err := r.ResolveProgram(loaded.AST); err != nil {
		return nil, nil, err
	}
	if err := typecheck.NewWithResolver(r).CheckProgram(loaded.AST); err != nil {
		return nil, nil, err
	}
	c := compiler.NewWithResolver(r)
	if err := c.Compile(loaded.AST); err != nil {
		return nil, nil, err
	}
	return c.Bytecode(), loaded.Tests, nil
}

func validateTest(test bunko.TestFunction) error {
	function := test.Function
	validReturn := function.ReturnType == nil || function.ReturnType.Name == "unit"
	if len(function.Parameters) == 0 && len(function.TypeParams) == 0 && validReturn {
		return nil
	}
	d := diagnostic.NewError("E0601", "invalid test function signature", diagnostic.FromPositions(function.Name.Span().Start, function.Name.Span().End))
	d.Path = test.Path
	d.WithNote("test functions must have no parameters, return unit, and not be generic")
	return &CompileError{TestName: test.Name, Path: test.Path, Err: d}
}

func runTest(base *compiler.Bytecode, test bunko.TestFunction, options Options) Result {
	started := time.Now()
	result := Result{Name: test.Name, Path: test.Path}
	testBytecode, err := entryBytecode(base, test.Function.Name.Name)
	if err != nil {
		result.RuntimeErr = err
		result.Duration = time.Since(started)
		return result
	}
	recorder := &Recorder{}
	registry, err := NewRegistry(recorder)
	if err != nil {
		result.RuntimeErr = err
		result.Duration = time.Since(started)
		return result
	}
	machine := vm.NewWithRegistryAndOutput(testBytecode, registry, options.Output)
	err = machine.RunWithOptions(vm.ExecutionOptions{InstructionLimit: options.InstructionLimit})
	result.Failures = recorder.Failures()
	result.RuntimeErr = err
	if err != nil {
		message := err.Error()
		result.Resource = strings.Contains(message, "limit") || strings.Contains(message, "interrupted")
	}
	result.Passed = err == nil && len(result.Failures) == 0
	result.Duration = time.Since(started)
	return result
}

func entryBytecode(base *compiler.Bytecode, name string) (*compiler.Bytecode, error) {
	functionID := -1
	for _, function := range base.Functions {
		if function.Name == name {
			functionID = function.ID
			break
		}
	}
	if functionID < 0 {
		return nil, fmt.Errorf("compiled test function %q was not found", name)
	}
	call, err := code.Make(code.OpCall, functionID)
	if err != nil {
		return nil, err
	}
	bytecode := &compiler.Bytecode{
		Instructions: append(append(code.Instructions(nil), base.Instructions...), call...),
		Constants:    base.Constants,
		GlobalsCount: base.GlobalsCount,
		Functions:    base.Functions,
	}
	return bytecode, nil
}
