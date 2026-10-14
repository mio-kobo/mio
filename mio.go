// Package mio 提供可嵌入的 Mio 编译与执行环境。
package mio

import (
	"fmt"
	"io"

	"Mio/internal/bunko"
	"Mio/internal/compiler"
	"Mio/internal/constant"
	"Mio/internal/mochi"
	"Mio/internal/resolver"
	"Mio/internal/typecheck"
	"Mio/internal/vm"
	"Mio/mma"
)

// Environment 隔离一次宿主编译与执行所用的模块注册表
type Environment struct {
	registry *mma.ModuleRegistry
}

// Program 保存经过静态检查的可执行字节码
type Program struct {
	bytecode *compiler.Bytecode
}

// NewEnvironment 创建独立的 Mio 宿主环境
func NewEnvironment(registry *mma.ModuleRegistry) *Environment {
	if registry == nil {
		registry = mma.NewModuleRegistry()
	}
	return &Environment{registry: registry}
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
	if e == nil {
		return fmt.Errorf("mio: nil environment")
	}
	if program == nil || program.bytecode == nil {
		return fmt.Errorf("mio: nil program")
	}
	return vm.NewWithRegistryAndOutput(program.bytecode, e.registry, output).Run()
}
