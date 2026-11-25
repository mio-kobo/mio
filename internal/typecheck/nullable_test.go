package typecheck

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/diagnostic"
)

// 测试可空类型的赋值规则
func TestNullableAssignments(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{name: "mu 初始化", source: `kaku value: int64? = mu; value = 18;`},
		{name: "非空值提升", source: `kaku value: int64? = 18;`},
		{name: "禁止推断 mu", source: `kaku value = mu;`, wantErr: "cannot infer type"},
		{name: "禁止可空值赋给非空变量", source: `kaku optional: int64? = mu; kaku value: int64 = optional;`, wantErr: "初始化类型不正确"},
		{name: "禁止数值隐式转换", source: `kaku value: float64? = 18;`, wantErr: "初始化类型不正确"},
		{name: "禁止 unit 可空", source: `waza f() -> unit? { kaesu; }`, wantErr: "cannot be nullable"},
		{name: "禁止嵌套可空", source: `kaku value: int64?? = mu;`, wantErr: "cannot be nullable"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := checkMioSource(test.source)
			if test.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error=%v, want %q", err, test.wantErr)
			}
		})
	}
}

// 测试可空局部变量的控制流收窄
func TestNullableControlFlowNarrowing(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		wantErr string
	}{
		{
			name: "非空分支",
			source: `kaku value: int64? = 10;
moshi (value != mu) { chant(value + 1); }`,
		},
		{
			name: "相反分支",
			source: `kaku value: int64? = mu;
moshi (value == mu) { chant("empty"); } sore { chant(value + 1); }`,
		},
		{
			name: "嵌套分支",
			source: `kaku value: int64? = 10;
moshi ((value) != (mu)) { moshi (hai) { chant(value + 1); } }`,
		},
		{
			name: "离开分支失效",
			source: `kaku value: int64? = 10;
moshi (value != mu) { chant(value); }
chant(value + 1);`,
			wantErr: "requires int64",
		},
		{
			name: "赋值为空后失效",
			source: `kaku value: int64? = 10;
moshi (value != mu) { value = mu; chant(value + 1); }`,
			wantErr: "requires int64",
		},
		{
			name:   "明确赋值更新事实",
			source: `kaku value: int64? = mu; value = 10; chant(value + 1);`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := checkMioSource(test.source)
			if test.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("error=%v, want %q", err, test.wantErr)
			}
		})
	}
}

// 测试函数、提前返回和循环中的可空收窄
func TestNullableFunctionAndLoopNarrowing(t *testing.T) {
	valid := []string{
		`waza next(value: int64?) -> int64? {
    moshi (value == mu) { kaesu mu; }
    kaesu value + 1;
}
kaku result = next(10);`,
		`waza use(value: int64?) -> int64 {
    moshi (value == mu) { kaesu 0; }
    kaesu value + 1;
}`,
		`waza drain(value: int64?) -> unit {
	    kaku current: int64? = value;
	    aida (current != mu) { chant(current); current = mu; }
}`,
	}
	for _, source := range valid {
		if err := checkMioSource(source); err != nil {
			t.Fatalf("source=%q error=%v", source, err)
		}
	}

	source := `kaku value: int64? = 10;
waza clear() -> unit { value = mu; }
moshi (value != mu) { clear(); chant(value + 1); }`
	if err := checkMioSource(source); err == nil || !strings.Contains(err.Error(), "requires int64") {
		t.Fatalf("error=%v", err)
	}
}

// 测试提前返回后的事实不会越过重新赋值
func TestNullableEarlyReturnMutationInvalidatesFact(t *testing.T) {
	source := `waza use(value: int64?) -> int64 {
    kaku current: int64? = value;
    moshi (current != mu) { current = mu; } sore { kaesu 0; }
    kaesu current + 1;
}`
	if err := checkMioSource(source); err == nil || !strings.Contains(err.Error(), "requires int64") {
		t.Fatalf("error=%v", err)
	}
}

func TestNullableLoopBackedge(t *testing.T) {
	for _, suffix := range []string{"fallthrough", "continue", "callback"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join("..", "..", "tests", "native-differential", "cases", "compile-errors", "nullable_loop_"+suffix)
			source, err := os.ReadFile(path + ".mio")
			if err != nil {
				t.Fatal(err)
			}
			fixture, err := os.ReadFile(path + ".diagnostic")
			if err != nil {
				t.Fatal(err)
			}
			var expected []any
			if err := json.Unmarshal(fixture, &expected); err != nil || len(expected) != 3 {
				t.Fatalf("invalid diagnostic fixture %s: %v", fixture, err)
			}
			err = checkMioSource(string(source))
			var diag *diagnostic.Diagnostic
			if !errors.As(err, &diag) || len(diag.Labels) != 1 || !diag.Labels[0].Primary {
				t.Fatalf("expected a located diagnostic, got %v", err)
			}
			start := diag.Labels[0].Span.Start
			if diag.Code != expected[0] || float64(start.Line) != expected[1] || float64(start.Column) != expected[2] {
				t.Fatalf("expected %v, got %s at %d:%d", expected, diag.Code, start.Line, start.Column)
			}
		})
	}
}

func TestNullableLoopConditionUsesBackedgeFacts(t *testing.T) {
	err := checkMioSource(`waza run(input: int64?) -> unit {
    kaku value: int64? = input;
    moshi (value == mu) { kaesu; }
    aida (value > 0) { value = mu; }
}`)
	var diag *diagnostic.Diagnostic
	if !errors.As(err, &diag) || diag.Code != "E0302" || len(diag.Labels) != 1 ||
		diag.Labels[0].Span.Start.Line != 4 || diag.Labels[0].Span.Start.Column != 11 {
		t.Fatalf("expected E0302 at the loop condition (4:11), got %+v", err)
	}
}

// 测试可空数组与可空数组元素严格区分
func TestNullableArrayTypes(t *testing.T) {
	valid := []string{
		`kaku values: []int64? = [10, mu, 30]; values[0] = mu; values[1] = 20;`,
		`kaku values: ([]int64)? = mu; values = [10, 20]; moshi (values != mu) { chant(len(values)); }`,
		`waza first(values: []int64) -> int64? { moshi (len(values) == 0) { kaesu mu; } kaesu values[0]; }`,
	}
	for _, source := range valid {
		if err := checkMioSource(source); err != nil {
			t.Fatalf("source=%q error=%v", source, err)
		}
	}

	invalid := []string{
		`kaku values: []int64? = [10, mu]; chant(values[0] + 1);`,
		`kaku values: []int64 = [10]; values[0] = mu;`,
		`kaku values: ([]int64)? = [10]; chant(len(values));`,
		`kaku values: []int64 = [10]; kaku optional: []int64? = values;`,
	}
	for _, source := range invalid {
		if err := checkMioSource(source); err == nil {
			t.Fatalf("source=%q unexpectedly passed", source)
		}
	}
}
