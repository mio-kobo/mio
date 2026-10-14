package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Mio/internal/mochi"
)

// 测试可空程序在 run 与 Mochi 执行中结果一致
func TestCLINullableRunAndMochi(t *testing.T) {
	source := `
waza find_first(items: []int64) -> int64? {
    moshi (len(items) == 0) { kaesu mu; }
    kaesu items[0];
}
kaku result = find_first([10, 20]);
moshi (result != mu) { chant(result + 1); }
kaku values: []int64? = [10, mu, 30];
kaku alias = values;
alias[0] = mu;
moshi (values[0] == mu) { chant("empty"); }
`
	exit, runOutput, runError := runSourceCLI(t, "run", source)
	if exit != 0 || runError != "" || runOutput != "11\nempty\n" {
		t.Fatalf("run exit=%d stdout=%q stderr=%q", exit, runOutput, runError)
	}

	sourcePath := writeFloatSource(t, source)
	mochiPath := filepath.Join(t.TempDir(), "nullable.mochi")
	var stdout, stderr bytes.Buffer
	if exit := runCLI([]string{"build", "-o", mochiPath, sourcePath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("build exit=%d stderr=%q", exit, stderr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV10 {
		t.Fatalf("version=%d, want %d", version, mochi.VersionV10)
	}
	stdout.Reset()
	stderr.Reset()
	if exit := runCLI([]string{"exec", mochiPath}, &stdout, &stderr); exit != 0 {
		t.Fatalf("exec exit=%d stderr=%q", exit, stderr.String())
	}
	if stdout.String() != runOutput {
		t.Fatalf("run=%q exec=%q", runOutput, stdout.String())
	}
}

// 测试 CLI 报告不安全的可空使用
func TestCLINullableDiagnostics(t *testing.T) {
	for _, source := range []string{
		`kaku value: int64? = mu; chant(value + 1);`,
		`kaku value = mu;`,
		`kaku values: ([]int64)? = mu; chant(len(values));`,
	} {
		exit, _, stderr := runSourceCLI(t, "check", source)
		if exit == 0 || !strings.Contains(stderr, "error[") {
			t.Fatalf("source=%q exit=%d stderr=%q", source, exit, stderr)
		}
	}
}
