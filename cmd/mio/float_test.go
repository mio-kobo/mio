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

// 测试浮点程序的运行与函数调用
func TestCLIFloatPrograms(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{
			name: "变量推断与算术",
			source: `
kaku pi = 3.1415926;
kaku radius = 5.0;
kaku area = pi * radius * radius;
chant(area);
`,
			want: "78.539815\n",
		},
		{
			name: "函数参数与返回值",
			source: `
waza square(x: float64) -> float64 {
	kaesu x * x;
}
chant(square(2.5));
`,
			want: "6.25\n",
		},
		{
			name: "递归与比较",
			source: `
waza sum(x: float64) -> float64 {
	moshi (x <= 0.0) {
		kaesu 0.0;
	}
	kaesu x + sum(x - 1.0);
}
chant(sum(3.0));
`,
			want: "6\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeFloatSource(t, tt.source)
			var stdout, stderr bytes.Buffer
			if code := runCLI([]string{"run", path}, &stdout, &stderr); code != 0 {
				t.Fatalf("exit=%d stderr=%q", code, stderr.String())
			}
			if stdout.String() != tt.want {
				t.Fatalf("stdout=%q, want %q", stdout.String(), tt.want)
			}
		})
	}
}

// 测试 IEEE 754 特殊值与比较规则
func TestCLIFloatIEEE754(t *testing.T) {
	source := `
kaku zero = 0.0;
kaku nan = zero / zero;
kaku infinity = 1.0 / zero;
chant(nan);
chant(infinity);
chant(-1.0 / zero);
chant(zero / -1.0);
chant(nan == nan);
chant(nan != nan);
chant(nan < 1.0);
chant(zero == -0.0);
chant(1.7976931348623157e308);
chant(5e-324);
`
	want := "NaN\n+Inf\n-Inf\n-0\niie\nhai\niie\nhai\n1.7976931348623157e+308\n5e-324\n"
	path := writeFloatSource(t, source)
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"run", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if stdout.String() != want {
		t.Fatalf("stdout=%q, want %q", stdout.String(), want)
	}
}

// 测试浮点算术与全部比较运算符
func TestCLIFloatOperators(t *testing.T) {
	source := `
chant(1.5 + 2.25);
chant(5.5 - 2.0);
chant(2.5 * 4.0);
chant(7.5 / 2.5);
chant(-2.5);
chant(2.0 == 2.0);
chant(2.0 != 3.0);
chant(2.0 < 3.0);
chant(2.0 <= 2.0);
chant(3.0 > 2.0);
chant(3.0 >= 3.0);
`
	want := "3.75\n3.5\n10\n3\n-2.5\nhai\nhai\nhai\nhai\nhai\nhai\n"
	path := writeFloatSource(t, source)
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"run", path}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
	if stdout.String() != want {
		t.Fatalf("stdout=%q, want %q", stdout.String(), want)
	}
}

// 测试整数与浮点数不会隐式转换
func TestCLIFloatMixedTypesRejected(t *testing.T) {
	for _, source := range []string{
		`1 + 1.0;`,
		`1.0 < 2;`,
		`1 == 1.0;`,
		`waza f(x: float64) -> float64 { kaesu x; } f(1);`,
	} {
		path := writeFloatSource(t, source)
		var stdout, stderr bytes.Buffer
		if code := runCLI([]string{"check", path}, &stdout, &stderr); code == 0 {
			t.Fatalf("source %q unexpectedly passed", source)
		}
		if !strings.Contains(stderr.String(), "got") && !strings.Contains(stderr.String(), "matching types") {
			t.Fatalf("unexpected error: %q", stderr.String())
		}
	}
}

// 测试 run 与 build、exec 的浮点输出一致
func TestCLIFloatBuildExecMatchesRun(t *testing.T) {
	path := writeFloatSource(t, `chant((2.5 + 1.5) / 2.0);`)
	mochiPath := filepath.Join(t.TempDir(), "float.mochi")

	var runOut, runErr bytes.Buffer
	if code := runCLI([]string{"run", path}, &runOut, &runErr); code != 0 {
		t.Fatalf("run exit=%d stderr=%q", code, runErr.String())
	}
	var buildOut, buildErr bytes.Buffer
	if code := runCLI([]string{"build", "-o", mochiPath, path}, &buildOut, &buildErr); code != 0 {
		t.Fatalf("build exit=%d stderr=%q", code, buildErr.String())
	}
	data, err := os.ReadFile(mochiPath)
	if err != nil {
		t.Fatal(err)
	}
	if version := binary.BigEndian.Uint16(data[5:7]); version != mochi.VersionV3 {
		t.Fatalf("version=%d, want %d", version, mochi.VersionV3)
	}
	var execOut, execErr bytes.Buffer
	if code := runCLI([]string{"exec", mochiPath}, &execOut, &execErr); code != 0 {
		t.Fatalf("exec exit=%d stderr=%q", code, execErr.String())
	}
	if execOut.String() != runOut.String() || execOut.String() != "2\n" {
		t.Fatalf("run=%q exec=%q", runOut.String(), execOut.String())
	}
}

// writeFloatSource 写入浮点测试源码
func writeFloatSource(t *testing.T, source string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "float.mio")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
