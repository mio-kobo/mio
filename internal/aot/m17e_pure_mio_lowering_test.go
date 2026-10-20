package aot

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
	"Mio/internal/value"
)

func m17eEncodeV14(t *testing.T, path string, program *compiler.Bytecode) {
	t.Helper()
	var output bytes.Buffer
	output.WriteString("MOCHI")
	write := func(value any) {
		t.Helper()
		if err := binary.Write(&output, binary.BigEndian, value); err != nil {
			t.Fatal(err)
		}
	}
	write(uint16(14))
	write(uint32(len(program.Constants)))
	for i, constant := range program.Constants {
		if text, ok := constant.Text(); ok {
			output.WriteByte(2)
			write(uint32(len(text)))
			output.WriteString(text)
			continue
		}
		integer, ok := constant.Int64()
		if !ok {
			bits, bitsOK := constant.IntegerBits()
			if !bitsOK || constant.Kind() != value.KindUint8 {
				t.Fatalf("stage-one constant %d is not supported", i)
			}
			output.WriteByte(8)
			output.WriteByte(byte(bits))
			continue
		}
		output.WriteByte(1)
		write(uint64(integer))
	}
	write(uint32(program.GlobalsCount))
	write(uint32(len(program.Instructions)))
	output.Write(program.Instructions)
	write(uint32(len(program.Functions)))
	for i, function := range program.Functions {
		write(uint16(function.ID))
		write(uint16(len(function.Name)))
		output.WriteString(function.Name)
		write(uint32(function.ParameterCount))
		write(uint32(function.LocalCount))
		if function.ReturnsValue {
			output.WriteByte(1)
		} else {
			output.WriteByte(0)
		}
		write(uint32(len(function.Captures)))
		for captureIndex, capture := range function.Captures {
			if capture.Local {
				output.WriteByte(1)
			} else {
				output.WriteByte(0)
			}
			if capture.Index < 0 {
				t.Fatalf("function %d capture %d has negative index", i, captureIndex)
			}
			write(uint32(capture.Index))
		}
		write(uint32(len(function.Instructions)))
		output.Write(function.Instructions)
	}
	if err := os.WriteFile(path, output.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestM17ePureMioStructAllocationAndFieldRead(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	program := &compiler.Bytecode{
		Constants:    []value.Value{value.NewInt(42), value.NewInt(7), value.NewInt(0)},
		GlobalsCount: 5,
		Instructions: code.Instructions{
			byte(code.OpConstant), 0, 0,
			byte(code.OpConstant), 0, 1,
			byte(code.OpStruct), 0, 2,
			byte(code.OpSetGlobal), 0, 0,
			byte(code.OpGetGlobal), 0, 0,
			byte(code.OpGetField), 0, 1,
			byte(code.OpChant),
			byte(code.OpGetGlobal), 0, 0,
			byte(code.OpGetField), 0, 0,
			byte(code.OpChant),
		},
	}
	program.Instructions = append(program.Instructions,
		byte(code.OpConstant), 0, 0,
		byte(code.OpStruct), 0, 1,
		byte(code.OpSetGlobal), 0, 1,
	)
	for range 64 {
		program.Instructions = append(program.Instructions, byte(code.OpConstant), 0, 1)
	}
	program.Instructions = append(program.Instructions,
		byte(code.OpGetGlobal), 0, 1,
		byte(code.OpStruct), 0, 65,
		byte(code.OpSetGlobal), 0, 2,
	)
	for range 400 {
		program.Instructions = append(program.Instructions,
			byte(code.OpConstant), 0, 1,
			byte(code.OpStruct), 0, 1,
			byte(code.OpSetGlobal), 0, 3,
		)
	}
	program.Instructions = append(program.Instructions,
		byte(code.OpGetGlobal), 0, 0,
		byte(code.OpGetField), 0, 1,
		byte(code.OpChant),
		byte(code.OpGetGlobal), 0, 2,
		byte(code.OpGetField), 0, 64,
		byte(code.OpGetField), 0, 0,
		byte(code.OpChant),
	)
	allocatorBody := make(code.Instructions, 0, 400*7+1)
	for range 400 {
		allocatorBody = append(allocatorBody,
			byte(code.OpConstant), 0, 1,
			byte(code.OpStruct), 0, 1,
			byte(code.OpPop),
		)
	}
	allocatorBody = append(allocatorBody, byte(code.OpReturn))
	program.Functions = []compiler.CompiledFunction{
		{ID: 0, Name: "keep", ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Instructions: code.Instructions{
			byte(code.OpGetLocal), 0, 0,
			byte(code.OpCall), 0, 1,
			byte(code.OpGetField), 0, 0,
			byte(code.OpReturnValue),
		}},
		{ID: 1, Name: "allocate", ParameterCount: 0, LocalCount: 0, ReturnsValue: false, Instructions: allocatorBody},
	}
	program.Instructions = append(program.Instructions,
		byte(code.OpConstant), 0, 0,
		byte(code.OpStruct), 0, 1,
		byte(code.OpCall), 0, 0,
		byte(code.OpChant),
		byte(code.OpNil),
		byte(code.OpIsNil),
		byte(code.OpChant),
		byte(code.OpConstant), 0, 2,
		byte(code.OpIsNil),
		byte(code.OpChant),
		byte(code.OpFalse),
		byte(code.OpIsNil),
		byte(code.OpChant),
		byte(code.OpConstant), 0, 2,
		byte(code.OpSetGlobal), 0, 4,
		byte(code.OpCall), 0, 1,
		byte(code.OpGetGlobal), 0, 4,
		byte(code.OpIsNil),
		byte(code.OpChant),
		byte(code.OpNil),
		byte(code.OpSetGlobal), 0, 4,
		byte(code.OpCall), 0, 1,
		byte(code.OpGetGlobal), 0, 4,
		byte(code.OpIsNil),
		byte(code.OpChant),
	)
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "struct.mochi")
	outputPath := filepath.Join(directory, "struct-native")
	m17eEncodeV14(t, inputPath, program)
	builderSource := fmt.Sprintf(`yobu "kiso/aot/lower";
yobu "kiso/aot/macho";
yobu "kiso/encoding/mochi";
yobu "kiso/fs";
kaku decoded = mochi.decode_file(%q);
kaku file = decoded.value;
moshi (decoded.error != mu) { chant(-1); }
sore {
  moshi (file == mu) { chant(-1); }
  sore {
    kaku lowered = lower.compile(file);
    kaku problem = lowered.error;
    moshi (problem != mu) { chant(lower.error_detail(problem)); }
    sore {
      kaku image = macho.executable(macho.ImageSpec { words: lowered.words, constants: lowered.constants, globals: lowered.globals, patches: lowered.patches });
      moshi (image.error != mu) { chant(-3); }
      sore { kaku result = fs.write_all(%q, image.bytes); moshi (result.error != mu) { chant(-4); } }
    }
  }
}
`, inputPath, outputPath)
	builder := m17CompileMio(t, root, builderSource)
	if output := m17RunVM(t, builder, 500_000_000); len(output) != 0 {
		t.Fatalf("pure builder output=%q", output)
	}
	vmBuilderImage, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if output, err := runM12NativeCompile(t, builder, func(input *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(input, DarwinARM64Options{HeapCapacity: 128 << 20})
	}); err != nil {
		t.Fatalf("native builder: %v output=%q", err, output)
	} else if len(output) != 0 {
		t.Fatalf("native builder output=%q", output)
	}
	nativeBuilderImage, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(nativeBuilderImage, vmBuilderImage) {
		t.Fatal("struct/GC image differs between VM and Native builder")
	}
	if err := os.Chmod(outputPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", outputPath).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, output)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--verbose=2", outputPath).CombinedOutput(); err != nil {
		t.Fatalf("codesign verify: %v: %s", err, output)
	}
	want := m17RunVM(t, program, 10_000)
	goAOTOutput, err := runM12NativeCompile(t, program, func(input *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(input, DarwinARM64Options{HeapCapacity: 1 << 20, GCStress: true})
	})
	if err != nil {
		t.Fatalf("Go AOT reference: %v output=%q", err, goAOTOutput)
	}
	if !bytes.Equal(goAOTOutput, want) {
		t.Fatalf("Go AOT output=%q VM=%q", goAOTOutput, want)
	}
	got, err := exec.Command(outputPath).CombinedOutput()
	if err != nil {
		t.Fatalf("execute: %v output=%q", err, got)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("native output=%q VM=%q", got, want)
	}
}

func TestM17ePureMioLoweringVMNativeAndExecution(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	stageSource, err := os.ReadFile(filepath.Join(root, "examples", "m17e_aot", "stage1.mio"))
	if err != nil {
		t.Fatal(err)
	}
	input := m17CompileMio(t, root, string(stageSource))
	wantOutput := m17RunVM(t, input, 10_000_000)
	if !bytes.Equal(wantOutput, []byte("15\n55\n")) {
		t.Fatalf("stage-one VM output=%q", wantOutput)
	}

	directory := t.TempDir()
	inputPath := filepath.Join(directory, "stage1.mochi")
	outputPath := filepath.Join(directory, "stage1-native")
	m17eEncodeV14(t, inputPath, input)

	builderSource := fmt.Sprintf(`yobu "kiso/aot/lower";
yobu "kiso/aot/macho";
yobu "kiso/encoding/mochi";
yobu "kiso/fs";
kaku decoded = mochi.decode_file(%q);
kaku file = decoded.value;
moshi (decoded.error != mu) { chant(-1); }
sore {
  moshi (file == mu) { chant(-1); }
  sore {
    kaku lowered = lower.compile(file);
    moshi (lowered.error != mu) { chant(-2); }
    sore {
      kaku image = macho.executable(macho.ImageSpec { words: lowered.words, constants: lowered.constants, globals: lowered.globals, patches: lowered.patches });
      moshi (image.error != mu) { chant(-3); }
      sore { kaku result = fs.write_all(%q, image.bytes); moshi (result.error != mu) { chant(-4); } }
    }
  }
}

`, inputPath, outputPath)
	builder := m17CompileMio(t, root, builderSource)
	if output := m17RunVM(t, builder, 100_000_000); len(output) != 0 {
		t.Fatalf("VM builder output=%q", output)
	}
	vmImage, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}

	if output, err := runM12NativeCompile(t, builder, func(program *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(program, DarwinARM64Options{HeapCapacity: 128 << 20})
	}); err != nil {
		t.Fatalf("native builder: %v output=%q", err, output)
	} else if len(output) != 0 {
		t.Fatalf("native builder output=%q", output)
	}
	nativeImage, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(vmImage, nativeImage) {
		t.Fatal("pure Mio lowering differs between VM and Native builder")
	}
	if err := os.Chmod(outputPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", outputPath).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, output)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--verbose=2", outputPath).CombinedOutput(); err != nil {
		t.Fatalf("codesign verify: %v: %s", err, output)
	}
	actualOutput, err := exec.Command(outputPath).CombinedOutput()
	if err != nil {
		t.Fatalf("execute: %v output=%q", err, actualOutput)
	}
	if !bytes.Equal(actualOutput, wantOutput) {
		t.Fatalf("native output=%q VM=%q", actualOutput, wantOutput)
	}
	t.Logf("pure Mio stage-one image=%d bytes, VM/native builder cmp identical, output=%q", len(nativeImage), actualOutput)
}

func TestM17ePureMioNativeFSOpenBridge(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	inputFile := filepath.Join(directory, "nativefs-input.bin")
	if err := os.WriteFile(inputFile, []byte{'M', 0, 'i', 'o'}, 0o600); err != nil {
		t.Fatal(err)
	}
	const openBinding = "$mma::mio/fs/runtime::open::1192df94a5bdf48f4df10a0aa4b61147cfc2e6f2c2f8ea1248826e517f716633"
	program := &compiler.Bytecode{GlobalsCount: 1, Constants: []value.Value{value.NewString(inputFile), value.NewInt(0), value.NewUint8(77), value.NewInt(0), value.NewInt(0)}, Instructions: code.Instructions{
		byte(code.OpConstant), 0, 3, byte(code.OpArrayWithCapacity), byte(value.KindUint8), byte(code.OpSetGlobal), 0, 0,
		byte(code.OpGetGlobal), 0, 0, byte(code.OpConstant), 0, 2, byte(code.OpArrayPush),
		byte(code.OpGetGlobal), 0, 0, byte(code.OpConstant), 0, 4, byte(code.OpIndex), byte(code.OpPop),
		byte(code.OpConstant), 0, 0, byte(code.OpConstant), 0, 1, byte(code.OpCall), 0, 0, byte(code.OpChant),
	}, Functions: []compiler.CompiledFunction{{ID: 0, Name: openBinding, ParameterCount: 2, LocalCount: 2, ReturnsValue: true}}}
	inputPath := filepath.Join(directory, "nativefs-open.mochi")
	outputPath := filepath.Join(directory, "nativefs-open")
	m17eEncodeV14(t, inputPath, program)
	builderSource := fmt.Sprintf(`yobu "kiso/aot/lower";
yobu "kiso/aot/macho";
yobu "kiso/encoding/mochi";
yobu "kiso/fs";
kaku decoded = mochi.decode_file(%q);
kaku file = decoded.value;
moshi (decoded.error != mu) { chant(-1); }
sore {
  moshi (file == mu) { chant(-1); }
  sore {
    kaku lowered = lower.compile(file);
    kaku problem = lowered.error;
    moshi (problem != mu) { chant(lower.error_detail(problem)); }
    sore {
      kaku image = macho.executable(macho.ImageSpec { words: lowered.words, constants: lowered.constants, globals: lowered.globals, patches: lowered.patches });
      moshi (image.error != mu) { chant(-3); }
      sore { kaku written = fs.write_all(%q, image.bytes); moshi (written.error != mu) { chant(-4); } }
    }
  }
}
`, inputPath, outputPath)
	builder := m17CompileMio(t, root, builderSource)
	if output := m17RunVM(t, builder, 200_000_000); len(output) != 0 {
		t.Fatalf("pure NativeFS builder output=%q", output)
	}
	if err := os.Chmod(outputPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", outputPath).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, output)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--verify", "--strict", "--verbose=2", outputPath).CombinedOutput(); err != nil {
		t.Fatalf("codesign verify: %v: %s", err, output)
	}
	want, err := runM12NativeCompile(t, program, func(input *compiler.Bytecode) ([]byte, error) {
		return CompileDarwinARM64WithOptions(input, DarwinARM64Options{HeapCapacity: 1 << 20, GCStress: true})
	})
	if err != nil {
		t.Fatalf("Go AOT reference: %v output=%q", err, want)
	}
	got, err := exec.Command(outputPath).CombinedOutput()
	if err != nil {
		t.Fatalf("pure NativeFS execute: %v output=%q", err, got)
	}
	if !bytes.Equal(got, want) || !bytes.Equal(got, []byte("257\n77\n")) {
		t.Fatalf("pure NativeFS output=%q Go AOT=%q", got, want)
	}
}

func TestM17eCompilerAFirstGetFieldLayout(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "build", "compiler-A.mochi"))
	if os.IsNotExist(err) {
		t.Skip("compiler-A.mochi is not present")
	}
	if err != nil {
		t.Fatal(err)
	}
	program, err := mochi.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := analyzeM6(program)
	if err != nil {
		t.Fatal(err)
	}
	main := analysis.blocks[0]
	instruction := -1
	for index, current := range main.inst {
		if current.offset == 12 {
			instruction = index
			break
		}
	}
	if instruction < 0 || main.inst[instruction].op != code.OpGetField || main.inst[instruction].operands[0] != 1 {
		t.Fatalf("main offset 12 is not OpGetField 1: instruction=%d", instruction)
	}
	if got := len(main.entry[instruction]); got != 1 {
		t.Fatalf("operand stack before main offset 12 has depth %d, want 1", got)
	}
	receiver := main.entry[instruction][0]
	if got := analysis.u.get(receiver); got != m6Struct {
		t.Fatalf("main offset 12 receiver kind=%s, want struct", got)
	}
	if got := analysis.u.structFieldCount(receiver); got != 2 {
		t.Fatalf("ReadAllResult field count=%d, want 2", got)
	}
	dataField, err := analysis.u.structField(receiver, 0)
	if err != nil {
		t.Fatal(err)
	}
	errorField, err := analysis.u.structField(receiver, 1)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.u.get(dataField) != m6Array || !m13ReferenceNode(analysis.u, dataField) {
		t.Fatalf("ReadAllResult.data kind=%s reference=%v, want array reference", analysis.u.get(dataField), m13ReferenceNode(analysis.u, dataField))
	}
	if analysis.u.get(errorField) != m6Enum || !analysis.u.isNullable(errorField) || !m13ReferenceNode(analysis.u, errorField) {
		t.Fatalf("ReadAllResult.error kind=%s nullable=%v reference=%v, want nullable enum reference", analysis.u.get(errorField), analysis.u.isNullable(errorField), m13ReferenceNode(analysis.u, errorField))
	}
	const bitmapWords = 1
	const fieldIndex = 1
	if got := 32 + bitmapWords*8 + fieldIndex*8; got != 48 {
		t.Fatalf("ReadAllResult.error payload offset=%d, want 48", got)
	}
}

func TestM17ePureMioAllConstantsAndStaticStringImage(t *testing.T) {
	if runtime.GOOS != "darwin" || runtime.GOARCH != "arm64" {
		t.Skip("requires macOS ARM64")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	constants := []value.Value{
		value.NewInt(math.MinInt64),
		value.NewString("a\x00中🌟"),
		value.NewFloat(math.Float64frombits(0x7ff8000000000042)),
		value.NewFloat(math.Inf(1)),
		value.NewFloat(math.Copysign(0, -1)),
		value.NewFloat32(math.Float32frombits(0x7fc00004)),
		value.NewFloat32(float32(math.Inf(-1))),
		value.NewFloat32(math.Float32frombits(0x80000000)),
		value.NewInt8(-1), value.NewInt16(-2), value.NewInt32(-3),
		value.NewUint8(0xff), value.NewUint16(0xffff), value.NewUint32(0xffffffff),
		value.NewString("a\x00中🌟"),
	}
	functionInstructions := make(code.Instructions, 0, len(constants)*4+1)
	for index := range constants {
		functionInstructions = append(functionInstructions, byte(code.OpConstant), byte(index>>8), byte(index), byte(code.OpPop))
	}
	functionInstructions = append(functionInstructions, byte(code.OpReturn))
	program := &compiler.Bytecode{
		Constants: constants,
		Functions: []compiler.CompiledFunction{{ID: 0, Name: "constants", LocalCount: 0, Instructions: functionInstructions}},
		Instructions: code.Instructions{
			byte(code.OpCall), 0, 0,
			byte(code.OpConstant), 0, 1,
			byte(code.OpConstant), 0, 14,
			byte(code.OpEqual), byte(code.OpChant),
			byte(code.OpConstant), 0, 4,
			byte(code.OpIsNil), byte(code.OpChant),
		},
	}
	directory := t.TempDir()
	inputPath := filepath.Join(directory, "constants.mochi")
	outputPath := filepath.Join(directory, "constants-native")
	var encoded bytes.Buffer
	if err := mochi.Encode(&encoded, program); err != nil {
		t.Fatal(err)
	}
	data := encoded.Bytes()
	binary.BigEndian.PutUint16(data[5:7], mochi.VersionV14)
	if err := os.WriteFile(inputPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	builderSource := fmt.Sprintf(`yobu "kiso/aot/lower";
yobu "kiso/aot/macho";
yobu "kiso/encoding/mochi";
yobu "kiso/fs";
kaku decoded = mochi.decode_file(%q);
kaku file = decoded.value;
moshi (decoded.error != mu) { chant(-1); }
sore {
  moshi (file == mu) { chant(-1); }
  sore {
    kaku lowered = lower.compile(file);
    moshi (lowered.error != mu) { chant(-2); }
    sore {
      kaku image = macho.executable(macho.ImageSpec { words: lowered.words, constants: lowered.constants, globals: lowered.globals, patches: lowered.patches });
      moshi (image.error != mu) { chant(-3); }
      sore { kaku written = fs.write_all(%q, image.bytes); moshi (written.error != mu) { chant(-4); } }
    }
  }
}

`, inputPath, outputPath)
	builder := m17CompileMio(t, root, builderSource)
	if output := m17RunVM(t, builder, 100_000_000); len(output) != 0 {
		t.Fatalf("VM builder output=%q", output)
	}
	image, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	mach, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	section := mach.Section("__const")
	if section == nil {
		mach.Close()
		t.Fatal("missing __TEXT,__const")
	}
	constantData, err := section.Data()
	mach.Close()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Count(constantData, []byte("a\x00中🌟")) != 1 {
		t.Fatalf("static string was not deduplicated: %x", constantData)
	}
	if err := os.Chmod(outputPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("/usr/bin/codesign", "--force", "--sign", "-", outputPath).CombinedOutput(); err != nil {
		t.Fatalf("codesign: %v: %s", err, output)
	}
	output, err := exec.Command(outputPath).CombinedOutput()
	if err != nil {
		t.Fatalf("execute: %v output=%q", err, output)
	}
	if !bytes.Equal(output, []byte("hai\niie\n")) {
		t.Fatalf("native output=%q", output)
	}
}
