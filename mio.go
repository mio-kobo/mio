// Package mio 提供可嵌入的 Mio 编译与执行环境。
package mio

import (
	"context"
	"fmt"
	"io"

	"Mio/internal/bunko"
	"Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/mochi"
	"Mio/internal/resolver"
	"Mio/internal/stdlib"
	"Mio/internal/typecheck"
	"Mio/internal/vm"
	"Mio/mma"
)

// Environment 隔离一次宿主编译与执行所用的模块注册表
type Environment struct {
	registry  *mma.ModuleRegistry
	resources *mma.ResourceManager
}

// Program 保存经过静态检查的可执行字节码
type Program struct {
	bytecode *compiler.Bytecode
}

// ExecutionOptions 配置单次程序执行
type ExecutionOptions struct {
	Context          context.Context
	InstructionLimit uint64
	InstructionCount *uint64
	ProgressInterval uint64
	Progress         func(ExecutionProgress)
}

// ExecutionProgress 保存 VM 的执行进度
type ExecutionProgress = vm.ExecutionProgress

// NewEnvironment 创建独立的 Mio 宿主环境
func NewEnvironment(registry *mma.ModuleRegistry) *Environment {
	if registry == nil {
		registry = mma.NewModuleRegistry()
	}
	return &Environment{registry: registry, resources: mma.NewResourceManager()}
}

// NewStandardEnvironment 创建包含随附原生标准库的环境
func NewStandardEnvironment() (*Environment, error) {
	registry, err := stdlib.NewRegistry()
	if err != nil {
		return nil, err
	}
	return NewEnvironment(registry), nil
}

// CompileFile 编译源码及其按需依赖
func (e *Environment) CompileFile(path, bunkoRoot string) (*Program, error) {
	if e == nil {
		return nil, fmt.Errorf("mio: nil environment")
	}
	loaded, err := bunko.LoadProgramWithRegistry(path, bunkoRoot, e.registry)
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
	checker := typecheck.NewWithResolver(r)
	if err := checker.CheckProgram(loaded.AST); err != nil {
		return nil, err
	}
	compiled := compiler.NewWithResolver(r)
	if err := compiled.Compile(loaded.AST); err != nil {
		return nil, err
	}
	return &Program{bytecode: compiled.Bytecode()}, nil
}

// DecodeProgram 解码并验证 Mochi 程序
func DecodeProgram(reader io.Reader) (*Program, error) {
	bytecode, err := mochi.Decode(reader)
	if err != nil {
		return nil, err
	}
	return &Program{bytecode: bytecode}, nil
}

// Encode 写入 Mochi 格式
func (p *Program) Encode(writer io.Writer) error {
	if p == nil || p.bytecode == nil {
		return fmt.Errorf("mio: nil program")
	}
	return mochi.Encode(writer, p.bytecode)
}

// Run 在当前环境中同步执行程序
func (e *Environment) Run(program *Program, output io.Writer) error {
	return e.RunWithOptions(program, output, ExecutionOptions{})
}

// RunWithOptions 使用指定资源限制执行程序
func (e *Environment) RunWithOptions(program *Program, output io.Writer, options ExecutionOptions) error {
	if e == nil {
		return fmt.Errorf("mio: nil environment")
	}
	if program == nil || program.bytecode == nil {
		return fmt.Errorf("mio: nil program")
	}
	return vm.NewWithRuntime(program.bytecode, e.registry, output, e.resources).RunWithOptions(vm.ExecutionOptions{
		Context: options.Context, InstructionLimit: options.InstructionLimit, InstructionCount: options.InstructionCount,
		ProgressInterval: options.ProgressInterval, Progress: options.Progress,
	})
}

// Close 关闭 Environment 中尚未释放的资源
func (e *Environment) Close() error {
	if e == nil || e.resources == nil {
		return nil
	}
	return e.resources.Shutdown()
}
