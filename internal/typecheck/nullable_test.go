package typecheck

import (
	"strings"
	"testing"
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
