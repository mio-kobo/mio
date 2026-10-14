package typecheck

import (
	"strings"
	"testing"
)

// 测试显式数值转换的静态类型规则
func TestNumericConversionTypes(t *testing.T) {
	valid := `
kaku i = int64(3.9);
kaku f = float64(i);
kaku nested = int64(float64(i));
waza convert(value: float64) -> int64 { kaesu int64(value); }
convert(f);
`
	if err := checkMioSource(valid); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		source string
		want   string
	}{
		{`int64();`, `expects 1 argument, got 0`},
		{`float64(1, 2);`, `expects 1 argument, got 2`},
		{`int64(1);`, `requires float64, got int64`},
		{`float64(1.0);`, `requires int64, got float64`},
		{`int64(hai);`, `requires float64, got bool`},
	}
	for _, test := range tests {
		err := checkMioSource(test.source)
		if err == nil || !strings.Contains(err.Error(), test.want) {
			t.Fatalf("source %q error=%v, want containing %q", test.source, err, test.want)
		}
	}
}
