package i18n

import (
	"strings"
	"testing"
)

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

func TestNormalizeFallbackVectors(t *testing.T) {
	tests := map[string]Language{
		"en-us": "en-US", "zh_cn": "zh-CN", "pt_br": "pt-BR",
		"zh_CN.UTF-8": "zh-CN", "ja_JP.UTF-8": "ja-JP",
		"fr-CA": "fr-FR", "de-AT": "de-DE", "es-AR": "es-ES",
		"zh-HK": "zh-HK", "zh-MO": "zh-HK", "zh-Hant": "zh-TW",
		"zh-Hans": "zh-CN", "pt": "pt-BR", "no": "nb-NO", "iw": "he-IL",
		"in": "id-ID", "tl": "fil-PH", "en-AU": "en-US",
		"C": "en-US", "POSIX": "en-US", "unknown": "en-US",
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Errorf("Normalize(%q)=%q want %q", input, got, want)
		}
	}
}

func TestSupportedLocales(t *testing.T) {
	locales := Supported()
	if len(locales) != 39 {
		t.Fatalf("supported locales = %d, want 39", len(locales))
	}
	for _, language := range locales {
		for _, key := range []Key{KeyHelpTitle, KeyCmdBuild, KeyOptLang, KeyErrUnknownCommand, KeyTestSummary} {
			text := Text(language, key)
			if text == "" || text == string(key) {
				t.Fatalf("%s is missing %s", language, key)
			}
			if strings.ContainsRune(text, '\x1b') {
				t.Fatalf("%s.%s contains an ANSI escape", language, key)
			}
		}
	}
}

func TestFormatSubstitutesPlaceholders(t *testing.T) {
	got := Format(Japanese, KeyTestSummary, 2, 1)
	if got != "テスト完了：2 件成功、1 件失敗" {
		t.Fatalf("Format = %q", got)
	}
	got = Format(Chinese, KeyErrUnknownCommand, "build!")
	if got != "未知命令：build!" {
		t.Fatalf("Format = %q", got)
	}
	if got := Format(English, KeyTestSummary, 0, 0); got != "Tests complete: 0 passed, 0 failed" {
		t.Fatalf("Format = %q", got)
	}
}

func TestFormatPreservesMalformedPlaceholder(t *testing.T) {
	// The template has no placeholder; arguments are ignored rather than corrupting the text.
	if got := Format(English, KeyCheckPassed, "ignored"); got != "check passed" {
		t.Fatalf("Format = %q", got)
	}
}

func TestDirection(t *testing.T) {
	for _, language := range []Language{"ar-SA", "he-IL", "fa-IR"} {
		if got := Direction(language); got != "rtl" {
			t.Fatalf("Direction(%s)=%q", language, got)
		}
	}
	for _, language := range []Language{"en-US", "ja-JP", "zh-CN"} {
		if got := Direction(language); got != "ltr" {
			t.Fatalf("Direction(%s)=%q", language, got)
		}
	}
}

func TestSelectLanguageEnvironmentOrder(t *testing.T) {
	environment := map[string]string{
		"MIO_LANG":    "ja_JP.UTF-8",
		"LANGUAGE":    "fr_FR:en",
		"LC_ALL":      "zh_CN.UTF-8",
		"LC_MESSAGES": "de_DE.UTF-8",
		"LANG":        "es_ES.UTF-8",
	}
	getenv := func(name string) string { return environment[name] }
	if got := Select("pt-BR", getenv); got != "pt-BR" {
		t.Fatalf("explicit = %q", got)
	}
	if got := Select("", getenv); got != "ja-JP" {
		t.Fatalf("MIO_LANG = %q", got)
	}
	delete(environment, "MIO_LANG")
	if got := Select("", getenv); got != "fr-FR" {
		t.Fatalf("LANGUAGE = %q", got)
	}
	delete(environment, "LANGUAGE")
	if got := Select("", getenv); got != "zh-CN" {
		t.Fatalf("LC_ALL = %q", got)
	}
	delete(environment, "LC_ALL")
	if got := Select("", getenv); got != "de-DE" {
		t.Fatalf("LC_MESSAGES = %q", got)
	}
	delete(environment, "LC_MESSAGES")
	if got := Select("", getenv); got != "es-ES" {
		t.Fatalf("LANG = %q", got)
	}
	delete(environment, "LANG")
	if got := Select("", getenv); got != "en-US" {
		t.Fatalf("default = %q", got)
	}
}

func TestSelectUsesLanguagePreferenceList(t *testing.T) {
	environment := map[string]string{"LANGUAGE": "xx:ja_JP", "LC_ALL": "de_DE.UTF-8"}
	getenv := func(name string) string { return environment[name] }
	if got := Select("", getenv); got != "ja-JP" {
		t.Fatalf("LANGUAGE list = %q, want ja-JP", got)
	}
	environment["LANGUAGE"] = "xx:en_GB"
	if got := Select("", getenv); got != "en-GB" {
		t.Fatalf("LANGUAGE list = %q, want en-GB", got)
	}
	environment["LANGUAGE"] = "xx:yy"
	if got := Select("", getenv); got != "de-DE" {
		t.Fatalf("unsupported LANGUAGE list should fall through to LC_ALL, got %q", got)
	}
	environment["LANGUAGE"] = "en_US:fr_FR"
	if got := Select("", getenv); got != "en-US" {
		t.Fatalf("LANGUAGE list = %q, want en-US", got)
	}
	environment["LANGUAGE"] = "fr_CA"
	if got := Select("", getenv); got != "fr-FR" {
		t.Fatalf("LANGUAGE list = %q, want fr-FR", got)
	}
	delete(environment, "LC_ALL")
	environment["LANGUAGE"] = "xx"
	if got := Select("", getenv); got != "en-US" {
		t.Fatalf("unsupported list default = %q", got)
	}
}
