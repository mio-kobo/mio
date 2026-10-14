package typecheck

import (
	"strings"
	"testing"
)

// 测试核心类型系统可表达标准库候选 API
func TestCoreTypesStandardLibraryShapes(t *testing.T) {
	source := `
sentaku Result<T, E> { Ok(T); Err(E); }
kata Record<T> { key: string; value: T; }
waza first<T>(values: []T) -> T? { moshi (len(values) == 0) { kaesu mu; } kaesu values[0]; }
waza find<T>(values: []T, predicate: (T) -> bool) -> T? {
    kaku index = 0;
    aida (index < len(values)) {
        moshi (predicate(values[index])) { kaesu values[index]; }
        index = index + 1;
    }
    kaesu mu;
}
waza mapOne<T, U>(value: T, transform: (T) -> U) -> U { kaesu transform(value); }
waza recursiveFirst<T>(values: []T, depth: int64) -> T {
    moshi (depth == 0) { kaesu values[0]; }
    kaesu recursiveFirst(values, depth - 1);
}
waza maybe<T>(value: T?) -> T? { kaesu value; }
waza genericLength<T>(values: []T) -> int64 { kaesu len(values); }
waza mapGet<K, V>(values: Map<K, V>, key: K) -> V? { kaesu values[key]; }
waza positive(value: int64) -> bool { kaesu value > 0; }
kaku found = find([1, 2], positive);
kaku recursed = recursiveFirst([1, 2], 2);
kaku missing = maybe[int64](mu);
kaku emptyLength = genericLength[int64]([]);
kaku mapped = mapOne[int64, string](1, stringValue);
waza stringValue(value: int64) -> string { kaesu "value"; }
kaku result: Result<int64, string> = Result.Ok(42);
kaku record = Record { key: "answer", value: 42 };
kaku table: Map<string, int64> = Map();
table[record.key] = record.value;
kaku fetched = mapGet(table, "answer");
`
	if err := checkMioSource(source); err != nil {
		t.Fatal(err)
	}
}

// 测试 Map 键约束和枚举穷尽性
func TestCoreTypeErrors(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{`kaku values: Map<[]int64, string> = Map();`, "not hashable"},
		{`waza create<K, V>() -> Map<K, V> { kaku value: Map<K, V> = Map(); kaesu value; } kaku invalid = create[[]int64, string]();`, "must be hashable"},
		{`sentaku State { Ready; Failed(string); } waza code(value: State) -> int64 { awase (value) { baai Ready { kaesu 1; } } }`, "non-exhaustive"},
		{`kata Pair { left: int64; right: int64; } kaku p = Pair { left: 1 };`, "requires 2 fields"},
	}
	for _, test := range tests {
		err := checkMioSource(test.source)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("error=%v want=%q", err, test.want)
		}
	}
}
