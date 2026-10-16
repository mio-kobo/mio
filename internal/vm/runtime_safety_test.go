package vm

import (
	"context"
	"math"
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 测试 int64 算术保持二进制补码回绕语义
func TestVMIntegerOverflowSemantics(t *testing.T) {
	tests := []struct {
		name      string
		op        code.Opcode
		constants []value.Value
		want      int64
	}{
		{name: "加法上溢", op: code.OpAdd, constants: []value.Value{value.NewInt(math.MaxInt64), value.NewInt(1)}, want: math.MinInt64},
		{name: "减法下溢", op: code.OpSub, constants: []value.Value{value.NewInt(math.MinInt64), value.NewInt(1)}, want: math.MaxInt64},
		{name: "乘法上溢", op: code.OpMul, constants: []value.Value{value.NewInt(math.MaxInt64), value.NewInt(2)}, want: -2},
		{name: "除法上溢", op: code.OpDiv, constants: []value.Value{value.NewInt(math.MinInt64), value.NewInt(-1)}, want: math.MinInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			machine := New(&compiler.Bytecode{
				Constants: tt.constants,
				Instructions: code.Instructions{
					byte(code.OpConstant), 0, 0,
					byte(code.OpConstant), 0, 1,
					byte(tt.op),
					byte(code.OpPop),
				},
			})
			if err := machine.Run(); err != nil {
				t.Fatal(err)
			}
			got, ok := machine.LastPoppedStackElem()
			if !ok || got != tt.want {
				t.Fatalf("result = %d, present=%v, want %d", got, ok, tt.want)
			}
		})
	}

	machine := New(&compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(math.MinInt64)},
		Instructions: code.Instructions{byte(code.OpConstant), 0, 0, byte(code.OpNeg), byte(code.OpPop)},
	})
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	if got, ok := machine.LastPoppedStackElem(); !ok || got != math.MinInt64 {
		t.Fatalf("negation result = %d, present=%v", got, ok)
	}
}

// 测试执行预算和取消信号终止恶意字节码
func TestVMExecutionCanBeStopped(t *testing.T) {
	loop := &compiler.Bytecode{Instructions: code.Instructions{byte(code.OpJump), 0, 0}}
	machine := New(loop)
	var executed uint64
	var reports []uint64
	err := machine.RunWithOptions(ExecutionOptions{
		InstructionLimit: 3,
		InstructionCount: &executed,
		ProgressInterval: 1,
		Progress: func(progress ExecutionProgress) {
			reports = append(reports, progress.Instructions)
		},
	})
	if err == nil || !strings.Contains(err.Error(), "instruction limit 3 exceeded") {
		t.Fatalf("RunWithOptions() error = %v", err)
	}
	if executed != 3 {
		t.Fatalf("executed = %d, want 3", executed)
	}
	if len(reports) != 3 || reports[0] != 1 || reports[2] != 3 {
		t.Fatalf("reports = %v, want [1 2 3]", reports)
	}
	if len(machine.stack) != 0 || len(machine.frames) != 0 || len(machine.globals) != 0 {
		t.Fatal("instruction limit error left runtime state")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = machine.RunWithOptions(ExecutionOptions{Context: ctx})
	if err == nil || !strings.Contains(err.Error(), "execution interrupted") {
		t.Fatalf("RunWithOptions() error = %v", err)
	}
}

// 测试运行时错误后可安全重复执行
func TestVMRuntimeErrorResetsState(t *testing.T) {
	machine := New(&compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(1), value.NewInt(0)},
		GlobalsCount: 1,
		Instructions: code.Instructions{
			byte(code.OpConstant), 0, 0,
			byte(code.OpSetGlobal), 0, 0,
			byte(code.OpConstant), 0, 0,
			byte(code.OpConstant), 0, 1,
			byte(code.OpDiv),
		},
	})
	if err := machine.Run(); err == nil || !strings.Contains(err.Error(), "division by zero") {
		t.Fatalf("Run() error = %v", err)
	}
	if len(machine.stack) != 0 || len(machine.frames) != 0 || len(machine.globals) != 0 {
		t.Fatal("runtime error left execution state")
	}
	if _, ok := machine.LastPoppedValue(); ok {
		t.Fatal("runtime error left previous result")
	}

	machine.bytecode = compileSource(t, "40 + 2;")
	if err := machine.Run(); err != nil {
		t.Fatal(err)
	}
	if got, ok := machine.LastPoppedStackElem(); !ok || got != 42 {
		t.Fatalf("second run result = %d, present=%v", got, ok)
	}
}

// 测试 VM 拒绝破坏表达式栈和指令边界的字节码
func TestVMRejectsInvalidStackAndControlFlow(t *testing.T) {
	tests := []struct {
		name     string
		bytecode *compiler.Bytecode
		want     string
	}{
		{
			name:     "主指令流残留值",
			bytecode: &compiler.Bytecode{Constants: []value.Value{value.NewInt(1)}, Instructions: code.Instructions{byte(code.OpConstant), 0, 0}},
			want:     "main exited with 1 value stack entries",
		},
		{
			name: "unit 返回残留值",
			bytecode: &compiler.Bytecode{
				Constants:    []value.Value{value.NewInt(1)},
				Instructions: code.Instructions{byte(code.OpCall), 0, 0},
				Functions: []compiler.CompiledFunction{{ID: 0, Name: "bad", Instructions: code.Instructions{
					byte(code.OpConstant), 0, 0, byte(code.OpReturn),
				}}},
			},
			want: "returned with 1 value stack entries",
		},
		{
			name: "返回多个值",
			bytecode: &compiler.Bytecode{
				Constants:    []value.Value{value.NewInt(1)},
				Instructions: code.Instructions{byte(code.OpCall), 0, 0, byte(code.OpPop)},
				Functions: []compiler.CompiledFunction{{ID: 0, Name: "bad", ReturnsValue: true, Instructions: code.Instructions{
					byte(code.OpConstant), 0, 0, byte(code.OpConstant), 0, 0, byte(code.OpReturnValue),
				}}},
			},
			want: "returned with 2 result values",
		},
		{
			name:     "跳入操作数",
			bytecode: &compiler.Bytecode{Instructions: code.Instructions{byte(code.OpJump), 0, 1}},
			want:     "not an instruction boundary",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(tt.bytecode).Run()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

// 测试局部槽位索引、初始化状态和参数数量检查
func TestVMLocalSlotSafety(t *testing.T) {
	tests := []struct {
		name         string
		function     compiler.CompiledFunction
		instructions code.Instructions
		want         string
	}{
		{
			name: "读取越界槽位",
			function: compiler.CompiledFunction{ID: 0, Name: "bad", LocalCount: 1, ReturnsValue: true,
				Instructions: code.Instructions{byte(code.OpGetLocal), 0, 1, byte(code.OpReturnValue)}},
			instructions: code.Instructions{byte(code.OpCall), 0, 0, byte(code.OpPop)},
			want:         "local slot 1 out of range",
		},
		{
			name: "读取未初始化槽位",
			function: compiler.CompiledFunction{ID: 0, Name: "bad", LocalCount: 1, ReturnsValue: true,
				Instructions: code.Instructions{byte(code.OpGetLocal), 0, 0, byte(code.OpReturnValue)}},
			instructions: code.Instructions{byte(code.OpCall), 0, 0, byte(code.OpPop)},
			want:         "local slot 0 is uninitialized",
		},
		{
			name: "写入时缺少值",
			function: compiler.CompiledFunction{ID: 0, Name: "bad", LocalCount: 1,
				Instructions: code.Instructions{byte(code.OpSetLocal), 0, 0, byte(code.OpReturn)}},
			instructions: code.Instructions{byte(code.OpCall), 0, 0},
			want:         "stack underflow for OpSetLocal",
		},
		{
			name: "调用参数不足",
			function: compiler.CompiledFunction{ID: 0, Name: "bad", ParameterCount: 1, LocalCount: 1,
				Instructions: code.Instructions{byte(code.OpReturn)}},
			instructions: code.Instructions{byte(code.OpCall), 0, 0},
			want:         "stack underflow for OpCall",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bytecode := &compiler.Bytecode{Instructions: tt.instructions, Functions: []compiler.CompiledFunction{tt.function}}
			err := New(bytecode).Run()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}
