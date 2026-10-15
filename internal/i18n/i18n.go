// Package i18n 提供 Mio 命令行共享的轻量消息本地化
package i18n

import (
	"os"
	"strings"
)

// Language 表示受支持的输出语言
type Language string

const (
	Chinese  Language = "zh-CN"
	Japanese Language = "ja-JP"
	English  Language = "en-US"
)

// Key 标识稳定的本地化消息
type Key string

const (
	TestStarting         Key = "test.starting"
	TestPassed           Key = "test.passed"
	TestFailed           Key = "test.failed"
	AssertionFailed      Key = "test.assertion_failed"
	NoTests              Key = "test.no_tests"
	NoMatchingTests      Key = "test.no_matching_tests"
	InvalidTestSignature Key = "test.invalid_signature"
	CompileFailed        Key = "test.compile_failed"
	RuntimeFailed        Key = "test.runtime_failed"
	ResourceLimit        Key = "test.resource_limit"
	PassedCount          Key = "test.passed_count"
	FailedCount          Key = "test.failed_count"
	TotalDuration        Key = "test.total_duration"
	Summary              Key = "test.summary"
)

var messages = map[Language]map[Key]string{
	Chinese: {
		TestStarting:         "开始测试 %s",
		TestPassed:           "通过",
		TestFailed:           "失败",
		AssertionFailed:      "断言失败",
		NoTests:              "没有发现测试",
		NoMatchingTests:      "没有匹配 --run=%s 的测试",
		InvalidTestSignature: "测试函数签名错误",
		CompileFailed:        "编译失败",
		RuntimeFailed:        "运行时错误",
		ResourceLimit:        "超出资源限制",
		PassedCount:          "成功数量",
		FailedCount:          "失败数量",
		TotalDuration:        "总执行时间",
		Summary:              "测试完成：%d 个通过，%d 个失败",
	},
	Japanese: {
		TestStarting:         "テストを開始 %s",
		TestPassed:           "成功",
		TestFailed:           "失敗",
		AssertionFailed:      "アサーション失敗",
		NoTests:              "テストが見つかりません",
		NoMatchingTests:      "--run=%s に一致するテストがありません",
		InvalidTestSignature: "テスト関数のシグネチャが無効です",
		CompileFailed:        "コンパイル失敗",
		RuntimeFailed:        "実行時エラー",
		ResourceLimit:        "リソース制限を超えました",
		PassedCount:          "成功数",
		FailedCount:          "失敗数",
		TotalDuration:        "合計実行時間",
		Summary:              "テスト完了：%d 件成功、%d 件失敗",
	},
	English: {
		TestStarting:         "Starting tests %s",
		TestPassed:           "PASS",
		TestFailed:           "FAIL",
		AssertionFailed:      "assertion failed",
		NoTests:              "no tests found",
		NoMatchingTests:      "no tests matched --run=%s",
		InvalidTestSignature: "invalid test function signature",
		CompileFailed:        "compilation failed",
		RuntimeFailed:        "runtime error",
		ResourceLimit:        "resource limit exceeded",
		PassedCount:          "passed",
		FailedCount:          "failed",
		TotalDuration:        "total duration",
		Summary:              "Tests complete: %d passed, %d failed",
	},
}

// Normalize 将常见 Locale 形式归一化为受支持语言
func Normalize(value string) Language {
	value = strings.TrimSpace(strings.ToLower(value))
	if dot := strings.IndexByte(value, '.'); dot >= 0 {
		value = value[:dot]
	}
	if modifier := strings.IndexByte(value, '@'); modifier >= 0 {
		value = value[:modifier]
	}
	value = strings.ReplaceAll(value, "_", "-")
	switch {
	case value == "zh", strings.HasPrefix(value, "zh-"):
		return Chinese
	case value == "ja", strings.HasPrefix(value, "ja-"):
		return Japanese
	case value == "en", strings.HasPrefix(value, "en-"):
		return English
	default:
		return English
	}
}

// Select 按 CLI、环境变量和系统 Locale 的优先级选择语言
func Select(explicit string, getenv func(string) string) Language {
	if explicit != "" {
		return Normalize(explicit)
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	if configured := getenv("MIO_LANG"); configured != "" {
		return Normalize(configured)
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale := getenv(name); locale != "" {
			return Normalize(locale)
		}
	}
	return English
}

// Text 返回指定语言的静态消息模板
func Text(language Language, key Key) string {
	if translated := messages[language][key]; translated != "" {
		return translated
	}
	return messages[English][key]
}
