package typecheck

import (
	"strings"
	"testing"
)

// 测试标准库所需的泛型高阶函数签名
func TestClosureTypesExpressStandardLibraryAPIs(t *testing.T) {
	source := `
waza map<T, U>(items: []T, transform: (T) -> U) -> []U { kaesu [transform(items[0])]; }
waza filter<T>(items: []T, predicate: (T) -> bool) -> []T {
    predicate(items[0]);
    kaesu items;
}
waza find<T>(items: []T, predicate: (T) -> bool) -> T? {
    predicate(items[0]);
    kaesu items[0];
}
kaku threshold = 2;
kaku selected = filter([1, 2, 3], waza (value: int64) -> bool { kaesu value > threshold; });
kaku mapped = map(selected, waza (value: int64) -> string { kaesu "item"; });
kaku found = find(selected, waza (value: int64) -> bool { kaesu value == threshold; });
`
	if err := checkMioSource(source); err != nil {
		t.Fatal(err)
	}
}

// 测试函数签名严格匹配且函数值不可比较
func TestClosureTypeSafety(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "参数类型不匹配",
			src:  `kaku f: (int64) -> int64 = waza (value: string) -> int64 { kaesu 1; };`,
			want: "初始化类型不正确",
		},
		{
			name: "返回类型不匹配",
			src:  `kaku f: (int64) -> string = waza (value: int64) -> int64 { kaesu value; };`,
			want: "初始化类型不正确",
		},
		{
			name: "函数值不可比较",
			src:  `kaku f = waza () -> int64 { kaesu 1; }; kaku same = f == f;`,
			want: "does not support",
		},
		{
			name: "unit 不能作为值",
			src:  `kaku f = waza () -> unit { kaesu; }; kaku result = f();`,
			want: "unit",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkMioSource(tt.src)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error=%v, want containing %q", err, tt.want)
			}
		})
	}
}

// 测试调用闭包后清除捕获变量的类型收窄
func TestClosureCallInvalidatesCapturedNarrowing(t *testing.T) {
	err := checkMioSource(`
waza unsafe(value: int64?) -> int64 {
    kaku current: int64? = value;
    kaku clear = waza () -> unit { current = mu; kaesu; };
    moshi (current != mu) {
        clear();
        kaesu current + 1;
    }
    kaesu 0;
}
`)
	if err == nil {
		t.Fatal("expected nullable access after closure call to fail")
	}
}
