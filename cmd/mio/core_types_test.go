package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 测试结构体、泛型、函数值和 Map 的端到端行为
func TestCLICoreTypesRun(t *testing.T) {
	source := `
kata Pair<T> { first: T; second: T; }
waza addPair(pair: Pair<int64>) -> int64 {
    kaesu pair.first + pair.second;
}
waza apply<T, U>(value: T, transform: (T) -> U) -> U {
    kaesu transform(value);
}
waza double(value: int64) -> int64 { kaesu value * 2; }
waza identity<T>(value: T) -> T { kaesu value; }
waza choose() -> (int64) -> int64 { kaesu double; }
waza first<T>(values: []T) -> T? {
    moshi (len(values) == 0) { kaesu mu; }
    kaesu values[0];
}
kaku pair = Pair[int64] { second: 22, first: 20 };
chant(addPair(pair));
chant(apply[int64, int64](21, double));
chant(choose()(20));
kaku callbacks = [double];
chant(callbacks[0](21));
kaku item = first([7, 8]);
moshi (item != mu) { chant(item); }
kaku entries = Map[string, int64]();
entries["answer"] = 42;
kaku sameEntries = identity[Map<string, int64>](entries);
kaku answer = sameEntries["answer"];
moshi (answer != mu) { chant(answer); }
`
	exit, stdout, stderr := runSourceCLI(t, "run", source)
	if exit != 0 || stderr != "" || stdout != "42\n42\n40\n42\n7\n42\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
}

// 测试泛型枚举与穷尽分支
func TestCLIEnumMatchRun(t *testing.T) {
	source := `
sentaku Result<T, E> { Ok(T); Err(E); }
waza valueOrZero(result: Result<int64, string>) -> int64 {
    awase (result) {
        baai Ok(value) { kaesu value; }
        baai Err(message) { chant(message); kaesu 0; }
    }
}
waza relay(result: Result<int64, string>) -> Result<int64, string> {
    awase (result) {
        baai Ok(value) { kaesu Result.Ok(value); }
        baai Err(message) { kaesu Result.Err(message); }
    }
}
kaku success = Result[int64, string].Ok(42);
kaku failure: Result<int64, string> = Result.Err("failed");
chant(valueOrZero(relay(success)));
chant(valueOrZero(relay(failure)));
`
	exit, stdout, stderr := runSourceCLI(t, "run", source)
	if exit != 0 || stderr != "" || stdout != "42\nfailed\n0\n" {
		t.Fatalf("exit=%d stdout=%q stderr=%q", exit, stdout, stderr)
	}
}

// 测试新增类型字节码在 build、exec 和 disasm 中一致
func TestCLICoreTypesMochi(t *testing.T) {
	source := `kata Point { x: int64; y: int64; } kaku p = Point { x: 20, y: 22 }; chant(p.x + p.y);`
	sourcePath := writeFloatSource(t, source)
	mochiPath := filepath.Join(t.TempDir(), "types.mochi")
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%q", exit, stderr.String())
	}
	if _, err := os.Stat(mochiPath); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 || stdout.String() != "42\n" {
		t.Fatalf("exec exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"disasm", mochiPath}, &stdout, &stderr); exit != 0 || !strings.Contains(stdout.String(), "OpStruct") || !strings.Contains(stdout.String(), "OpGetField") {
		t.Fatalf("disasm exit=%d stdout=%q stderr=%q", exit, stdout.String(), stderr.String())
	}
}

// 测试枚举分支必须穷尽
func TestCLIEnumMatchExhaustiveness(t *testing.T) {
	source := `sentaku Choice { A; B; } waza pick(value: Choice) -> int64 { awase (value) { baai A { kaesu 1; } } }`
	exit, _, stderr := runSourceCLI(t, "check", source)
	if exit == 0 || !strings.Contains(stderr, "non-exhaustive") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr)
	}
}
