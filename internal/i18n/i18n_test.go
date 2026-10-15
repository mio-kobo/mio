package i18n

import "testing"

// 测试常见 Locale 的归一化
func TestNormalizeLocale(t *testing.T) {
	tests := map[string]Language{
		"zh_CN.UTF-8": Chinese,
		"ja_JP.UTF-8": Japanese,
		"en_US.UTF-8": English,
		"unknown":     English,
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q)=%q want %q", input, got, want)
		}
	}
}

// 测试语言选择优先级
func TestSelectPriority(t *testing.T) {
	environment := map[string]string{
		"MIO_LANG":    "ja_JP.UTF-8",
		"LC_ALL":      "zh_CN.UTF-8",
		"LC_MESSAGES": "en_US.UTF-8",
		"LANG":        "en_US.UTF-8",
	}
	getenv := func(name string) string { return environment[name] }
	if got := Select("en-US", getenv); got != English {
		t.Fatalf("explicit language=%q", got)
	}
	if got := Select("", getenv); got != Japanese {
		t.Fatalf("MIO_LANG=%q", got)
	}
	delete(environment, "MIO_LANG")
	if got := Select("", getenv); got != Chinese {
		t.Fatalf("OS locale=%q", got)
	}
}

// 测试消息查找不进行动态初始化
func BenchmarkText(b *testing.B) {
	for index := 0; index < b.N; index++ {
		_ = Text(Chinese, Summary)
	}
}
