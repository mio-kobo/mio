package bootstrapaudit

import (
	"strings"
	"testing"

	"Mio/internal/code"
	"Mio/internal/compiler"
	"Mio/internal/value"
)

func appendOp(t *testing.T, dst code.Instructions, op code.Opcode, args ...int) code.Instructions {
	t.Helper()
	encoded, err := code.Make(op, args...)
	if err != nil {
		t.Fatalf("make opcode %d: %v", op, err)
	}
	return append(dst, encoded...)
}

func TestAnalyzeCountsMissingAndRestrictedSites(t *testing.T) {
	var main code.Instructions
	main = appendOp(t, main, code.OpConstant, 0)
	main = appendOp(t, main, code.OpSetGlobal, 0)
	main = appendOp(t, main, code.OpConstant, 0)
	main = appendOp(t, main, code.OpCall, 0)
	main = appendOp(t, main, code.OpPop)
	main = appendOp(t, main, code.OpNil)
	main = appendOp(t, main, code.OpConstant, 1)
	main = appendOp(t, main, code.OpChant)
	main = appendOp(t, main, code.OpCall, 1)

	var ordinary code.Instructions
	ordinary = appendOp(t, ordinary, code.OpGetLocal, 0)
	ordinary = appendOp(t, ordinary, code.OpReturnValue)
	var global code.Instructions
	global = appendOp(t, global, code.OpGetGlobal, 0)
	global = appendOp(t, global, code.OpReturnValue)

	p := &compiler.Bytecode{
		Instructions: main,
		Constants:    []value.Value{value.NewInt(42), value.NewString("Mio"), value.NewFloat(1.5)},
		GlobalsCount: 1,
		Functions: []compiler.CompiledFunction{
			{ID: 0, Name: "identity", ParameterCount: 1, LocalCount: 1, ReturnsValue: true, Instructions: ordinary},
			{ID: 1, Name: "$mma::fs::read", ReturnsValue: true},
			{ID: 2, Name: "fn_global", ReturnsValue: true, Instructions: global},
		},
	}
	result, err := Analyze(p)
	if err != nil {
		t.Fatal(err)
	}
	if result.FunctionCount != 3 || result.GlobalsCount != 1 || result.MaxLocalSlots != 1 || result.TotalInstructions != 13 {
		t.Fatalf("unexpected totals: %+v", result)
	}
	if result.MissingInstructions != 2 {
		t.Fatalf("missing=%d, want 2", result.MissingInstructions)
	}
	for _, entry := range result.Opcodes {
		if entry.Name == "OpCall" && (entry.Missing != 1 || entry.Limited != 1) {
			t.Fatalf("OpCall %+v", entry)
		}
		if entry.Name == "OpGetGlobal" && entry.Missing != 1 {
			t.Fatalf("global access %+v", entry)
		}
		if entry.Name == "OpNil" && entry.Limited != 1 {
			t.Fatalf("nil %+v", entry)
		}
	}
	if len(result.NativeCandidates) != 1 || result.NativeCandidates[0].FunctionID != 1 {
		t.Fatalf("native candidates: %+v", result.NativeCandidates)
	}
	if len(result.Constants) != 3 || result.Constants[1].Referenced != 1 {
		t.Fatalf("constants: %+v", result.Constants)
	}
}

func TestAnalyzeRejectsMalformedAndUnknownBytes(t *testing.T) {
	_, err := Analyze(nil)
	if err == nil {
		t.Fatal("nil accepted")
	}
	_, err = Analyze(&compiler.Bytecode{Instructions: code.Instructions{byte(code.OpConstant), 0}})
	if err == nil {
		t.Fatal("truncated operand accepted")
	}
	_, err = Analyze(&compiler.Bytecode{Instructions: code.Instructions{byte(code.OpConstant), 0, 7}})
	if err == nil || !strings.Contains(err.Error(), "越界") {
		t.Fatalf("constant index: %v", err)
	}
	_, err = Analyze(&compiler.Bytecode{Instructions: code.Instructions{byte(code.OpJump), 0, 1}})
	if err == nil || !strings.Contains(err.Error(), "指令边界") {
		t.Fatalf("jump target: %v", err)
	}
}

func TestAnalyzeZeroProgramAndDeterminism(t *testing.T) {
	p := &compiler.Bytecode{}
	a, err := Analyze(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Analyze(p)
	if err != nil {
		t.Fatal(err)
	}
	if a.TotalInstructions != 0 || a.MissingInstructions != 0 || len(a.Functions) != 1 {
		t.Fatalf("empty program: %+v", a)
	}
	if a.CompilerBaseline != b.CompilerBaseline || a.DistinctOpcodes != b.DistinctOpcodes {
		t.Fatal("unstable analysis")
	}
}

func TestClassifyKnownButUnavailable(t *testing.T) {
	for _, name := range []string{"OpMap"} {
		st, _, _, _ := classify(name, nil, -1, nil, &compiler.Bytecode{})
		if st != StatusMissing {
			t.Errorf("%s: %s", name, st)
		}
	}
	for _, name := range []string{"OpDataConvert", "OpMath", "OpIntegerConvert"} {
		st, _, _, _ := classify(name, nil, -1, nil, &compiler.Bytecode{})
		if st != StatusLowered {
			t.Errorf("%s: %s", name, st)
		}
	}
	for _, name := range []string{"OpAdd", "OpChant", "OpGetLocal", "OpConstant", "OpIntToFloat", "OpArray", "OpStruct", "OpArrayPush", "OpArrayClone", "OpArrayTruncate", "OpArrayAppendAll", "OpEnum", "OpEnumTag", "OpGetEnumValue", "OpFunction", "OpClosure", "OpCallValue", "OpGetUpvalue", "OpSetUpvalue", "OpNil", "OpIsNil", "OpSetNullableIndex"} {
		// 对 OpConstant 构造一个可识别的 int64 常量，确保按受限而不是完整支持统计。
		p := &compiler.Bytecode{Constants: []value.Value{value.NewInt(3)}}
		st, _, _, _ := classify(name, []int{0}, -1, nil, p)
		if st != StatusLimited {
			t.Errorf("%s: %s", name, st)
		}
	}
}

func TestAnalyzeStructuralLimits(t *testing.T) {
	p := &compiler.Bytecode{GlobalsCount: 2049, Functions: []compiler.CompiledFunction{{ID: 0, Name: "heavy", ParameterCount: 257, LocalCount: 300, Captures: []compiler.Capture{{Local: true, Index: 0}}}}}
	r, err := Analyze(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.StructuralIssues) != 3 {
		t.Fatalf("structural issues: %+v", r.StructuralIssues)
	}
	if r.FunctionsWithCaptures != 1 || r.FunctionsWithoutBody != 1 {
		t.Fatalf("function metrics: %+v", r)
	}
}
