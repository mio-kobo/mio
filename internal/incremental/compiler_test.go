package incremental

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	bytecodecompiler "Mio/internal/compiler"
	"Mio/internal/diagnostic"
	"Mio/internal/typecheck"
)

const baseProgram = `
waza first(value: int64) -> int64 {
	kaesu value + 1;
}
waza second(value: int64) -> int64 {
	kaesu value + 2;
}

waza combine(value: int64) -> int64 {
	kaesu first(value) + second(value);
}

combine(10);
`

// 测试新核心类型在注解缓存不安全时回退完整编译
func TestCoreTypesUseFullFallback(t *testing.T) {
	source := `kata Box<T> { value: T; } waza read(box: Box<int64>) -> int64 { kaesu box.value; } kaku box = Box { value: 42 }; read(box);`
	c := New()
	for i := 0; i < 2; i++ {
		if _, stats, err := c.Compile("core", source); err != nil {
			t.Fatal(err)
		} else if !stats.FullFallback {
			t.Fatalf("compile %d did not use full fallback", i)
		}
	}
}

// 测试未变化函数复用类型检查和字节码
func TestCompileReusesUnchangedFunctions(t *testing.T) {
	c := New()
	if _, _, err := c.Compile("main", baseProgram); err != nil {
		t.Fatal(err)
	}
	_, stats, err := c.Compile("main", "\n\n"+baseProgram)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TypeCheck.Hits != 3 || stats.TypeCheck.Recomputed != 0 {
		t.Fatalf("type-check stats = %#v", stats.TypeCheck)
	}
	if stats.Codegen.Hits != 3 || stats.Codegen.Recomputed != 0 {
		t.Fatalf("codegen stats = %#v", stats.Codegen)
	}
	if stats.HitRate() != 1 {
		t.Fatalf("hit rate = %f, want 1", stats.HitRate())
	}
}

// 测试函数体变化只使自身缓存失效
func TestCompileInvalidatesOnlyChangedBody(t *testing.T) {
	c := New()
	if _, _, err := c.Compile("main", baseProgram); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(baseProgram, "value + 2", "value + 200", 1)
	got, stats, err := c.Compile("main", changed)
	if err != nil {
		t.Fatal(err)
	}
	want := compileFull(t, changed)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("incremental bytecode differs from full compilation\nincremental: %#v\nfull: %#v", got, want)
	}
	if stats.BodyChanges != 1 {
		t.Fatalf("body changes = %d, want 1", stats.BodyChanges)
	}
	if stats.TypeCheck.Hits != 2 || stats.TypeCheck.Recomputed != 1 {
		t.Fatalf("type-check stats = %#v", stats.TypeCheck)
	}
	if stats.Codegen.Hits != 2 || stats.Codegen.Recomputed != 1 {
		t.Fatalf("codegen stats = %#v", stats.Codegen)
	}
}

// 测试复用函数按当前常量池重定位
func TestCompileRelocatesCachedFunctionConstants(t *testing.T) {
	c := New()
	source := `
waza changed(value: int64) -> int64 { kaesu value + 1; }
waza stable(value: int64) -> int64 { kaesu value + 9; }
stable(changed(1));
`
	if _, _, err := c.Compile("main", source); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(source, "value + 1", "value + 1 + 2", 1)
	got, stats, err := c.Compile("main", changed)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Codegen.Hits != 1 || stats.Codegen.Recomputed != 1 {
		t.Fatalf("codegen stats = %#v", stats.Codegen)
	}
	if want := compileFull(t, changed); !reflect.DeepEqual(got, want) {
		t.Fatal("relocated bytecode differs from full compilation")
	}
}

// 测试签名变化使直接调用者缓存失效
func TestCompileInvalidatesCallerAfterSignatureChange(t *testing.T) {
	c := New()
	source := `
	waza accept(value: int64) -> int64 { kaesu 1; }
waza caller() -> int64 { kaesu accept(1); }
caller();
`
	if _, _, err := c.Compile("main", source); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(source, "value: int64", "value: string", 1)
	_, stats, err := c.Compile("main", changed)
	if err == nil || !strings.Contains(err.Error(), "expected string, got int64") {
		t.Fatalf("error = %v", err)
	}
	if stats.SignatureChanges != 1 || stats.CallerInvalidations != 1 {
		t.Fatalf("signature stats = %#v", stats)
	}
	if stats.TypeCheck.Hits != 0 || stats.TypeCheck.Recomputed != 2 {
		t.Fatalf("type-check stats = %#v", stats.TypeCheck)
	}
}

// 测试顶层推断依赖的签名变化会使函数缓存失效
func TestCompileInvalidatesFunctionsAfterGlobalTypeChange(t *testing.T) {
	c := New()
	source := `
waza make() -> int64 { kaesu 1; }
kaku global = make();
waza use() -> int64 { kaesu global + 1; }
use();
`
	if _, _, err := c.Compile("main", source); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(source, "make() -> int64 { kaesu 1; }", `make() -> string { kaesu "value"; }`, 1)
	_, stats, err := c.Compile("main", changed)
	if err == nil || !strings.Contains(err.Error(), "requires int64 operands") {
		t.Fatalf("error = %v", err)
	}
	if stats.TypeCheck.Hits != 0 {
		t.Fatalf("type-check stats = %#v", stats.TypeCheck)
	}
}

// 测试顶层环境变化安全回退完整函数检查
func TestCompileFallsBackAfterTopLevelChange(t *testing.T) {
	c := New()
	source := `
kaku offset = 1;
waza add(value: int64) -> int64 { kaesu value + offset; }
waza twice(value: int64) -> int64 { kaesu add(value) + add(value); }
twice(2);
`
	if _, _, err := c.Compile("main", source); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(source, "offset = 1", "offset = 2", 1)
	got, stats, err := c.Compile("main", changed)
	if err != nil {
		t.Fatal(err)
	}
	if !stats.FullFallback || stats.TypeCheck.Hits != 0 || stats.Codegen.Hits != 0 {
		t.Fatalf("fallback stats = %#v", stats)
	}
	if want := compileFull(t, changed); !reflect.DeepEqual(got, want) {
		t.Fatal("fallback bytecode differs from full compilation")
	}
}

// 测试失败诊断不会复用旧源码位置
func TestCompileDoesNotCacheDiagnostics(t *testing.T) {
	c := New()
	source := `waza broken() -> int64 {
	kaesu "bad";
}
broken();`
	_, _, firstErr := c.Compile("main", source)
	if firstErr == nil {
		t.Fatal("expected first type error")
	}
	_, _, secondErr := c.Compile("main", "\n\n\n"+source)
	if secondErr == nil {
		t.Fatal("expected second type error")
	}
	var firstDiagnostic, secondDiagnostic *diagnostic.Diagnostic
	if !errors.As(firstErr, &firstDiagnostic) || !errors.As(secondErr, &secondDiagnostic) {
		t.Fatalf("errors are not diagnostics: %v, %v", firstErr, secondErr)
	}
	firstLabel, firstOK := firstDiagnostic.PrimaryLabel()
	secondLabel, secondOK := secondDiagnostic.PrimaryLabel()
	if !firstOK || !secondOK {
		t.Fatal("diagnostics do not have primary labels")
	}
	if secondLabel.Span.Start.Line != firstLabel.Span.Start.Line+3 {
		t.Fatalf("diagnostic lines = %d, %d", firstLabel.Span.Start.Line, secondLabel.Span.Start.Line)
	}
}

// 测试依赖图使用模块和函数名作为稳定身份
func TestDependencyGraphUsesStableFunctionIdentity(t *testing.T) {
	c := New()
	if _, _, err := c.Compile("example/main", baseProgram); err != nil {
		t.Fatal(err)
	}
	graph := c.Graph()
	id := FunctionID{Module: "example/main", Name: "combine"}
	want := []FunctionID{
		{Module: "example/main", Name: "first"},
		{Module: "example/main", Name: "second"},
	}
	if !reflect.DeepEqual(graph.Calls[id], want) {
		t.Fatalf("dependencies = %#v, want %#v", graph.Calls[id], want)
	}
}

func compileFull(t testing.TB, source string) *bytecodecompiler.Bytecode {
	t.Helper()
	program, r, err := parseAndResolve(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := typecheck.NewWithResolver(r).CheckProgram(program); err != nil {
		t.Fatal(err)
	}
	c := bytecodecompiler.NewWithResolver(r)
	if err := c.Compile(program); err != nil {
		t.Fatal(err)
	}
	return c.Bytecode()
}

func largeProgram(functions, operations int) string {
	var source strings.Builder
	source.WriteString("waza identity(value: int64) -> int64 { kaesu value; }\n")
	for index := 0; index < functions; index++ {
		fmt.Fprintf(&source, "waza fn%d(value: int64) -> int64 {\n", index)
		source.WriteString("kaku result = value;\n")
		for operation := 0; operation < operations; operation++ {
			source.WriteString("result = identity(result);\n")
		}
		source.WriteString("kaesu result;\n}\n")
	}
	fmt.Fprintf(&source, "fn%d(1);\n", functions-1)
	return source.String()
}

func BenchmarkCompileLargeProgram(b *testing.B) {
	source := largeProgram(120, 40)
	b.Run("full", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			compileFull(b, source)
		}
	})
	b.Run("incremental-hit", func(b *testing.B) {
		c := New()
		if _, _, err := c.Compile("benchmark", source); err != nil {
			b.Fatal(err)
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, stats, err := c.Compile("benchmark", source); err != nil {
				b.Fatal(err)
			} else if stats.TypeCheck.Hits != 121 || stats.Codegen.Hits != 121 {
				b.Fatalf("unexpected stats: %#v", stats)
			}
		}
	})
}
