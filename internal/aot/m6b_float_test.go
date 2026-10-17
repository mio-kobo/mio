package aot

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"math"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

// 浮点格式化只在目标 ARM64 程序内执行，不能把常量在 Go 编译阶段转为文本。
func TestM6bDirectFloatOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		v    value.Value
	}{
		{"float64-pi", value.NewFloat(math.Pi)},
		{"float64-min-subnormal", value.NewFloat(math.SmallestNonzeroFloat64)},
		{"float64-max", value.NewFloat(math.MaxFloat64)},
		{"float64-minus-zero", value.NewFloat(math.Copysign(0, -1))},
		{"float64-nan", value.NewFloat(math.NaN())},
		{"float64-infinity", value.NewFloat(math.Inf(1))},
		{"float32-pi", value.NewFloat32(float32(math.Pi))},
		{"float32-min-subnormal", value.NewFloat32(math.SmallestNonzeroFloat32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := newM3TestAsm()
			a.emit(code.OpConstant, 0)
			a.emit(code.OpChant)
			p := &compiler.Bytecode{Constants: []value.Value{tc.v}, Instructions: a.build()}
			image, err := CompileDarwinARM64(p)
			if err != nil {
				t.Fatal(err)
			}
			f, err := macho.NewFile(bytes.NewReader(image))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			text := f.Section("__text")
			if text == nil || text.Size < 1700 {
				t.Fatalf("missing native formatter section: %v", text)
			}
		})
	}
}

func TestM6bFloatOutputFromFunction(t *testing.T) {
	main := newM3TestAsm()
	main.emit(code.OpConstant, 0)
	main.emit(code.OpCall, 0)
	main.emit(code.OpChant)
	function := newM3TestAsm()
	function.emit(code.OpGetLocal, 0)
	function.emit(code.OpConstant, 0)
	function.emit(code.OpAdd)
	function.emit(code.OpReturnValue)
	p := &compiler.Bytecode{
		Constants:    []value.Value{value.NewFloat(1.5)},
		Instructions: main.build(),
		Functions: []compiler.CompiledFunction{{
			ID: 0, Name: "float_add", Instructions: function.build(),
			ParameterCount: 1, LocalCount: 1, ReturnsValue: true,
		}},
	}
	if _, err := CompileDarwinARM64(p); err != nil {
		t.Fatal(err)
	}
}

// M6b 保留 OpPop 的按实际执行路径打印语义，整数和 bool 不能误分派到浮点。
func TestM6bMixedTopLevelPop(t *testing.T) {
	p := newM3TestAsm()
	p.emit(code.OpTrue)
	p.jump(code.OpJumpIfFalse, "useInt")
	p.emit(code.OpConstant, 0)
	p.emit(code.OpPop)
	p.jump(code.OpJump, "end")
	p.label("useInt")
	p.emit(code.OpConstant, 1)
	p.emit(code.OpPop)
	p.label("end")
	image, err := CompileDarwinARM64(&compiler.Bytecode{
		Constants:    []value.Value{value.NewFloat32(1.25), value.NewInt(42)},
		Instructions: p.build(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// CBNZ X21 跳到布尔 Pop 分支，CBZ 的条件反转会错误输出整数位模式。
	found := false
	for offset := 0x300; offset+4 <= len(image); offset += 4 {
		word := binary.LittleEndian.Uint32(image[offset : offset+4])
		if word&0xFF00001F == 0xB5000015 {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("missing bool Pop CBNZ branch")
	}
}

func TestM6bNoFloatRuntimeWhenNotUsed(t *testing.T) {
	a := newM3TestAsm()
	a.emit(code.OpConstant, 0)
	a.emit(code.OpChant)
	image, err := CompileDarwinARM64(&compiler.Bytecode{Constants: []value.Value{value.NewInt(42)}, Instructions: a.build()})
	if err != nil {
		t.Fatal(err)
	}
	if len(image) != 16384 {
		t.Fatalf("M1 int program changed image size: %d", len(image))
	}
}
