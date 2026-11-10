// Package i18n implements the Mio CLI localization contract.
//
// The locale catalogs under selfhost/i18n/locales are the single source of
// truth; scripts/i18n/catalog.py generates messages_generated.go for this
// package and messages_generated.mio for the native CLI. Both paths implement
// the same BCP 47 normalization, fallback order and {0} placeholders.
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Language is a canonical BCP 47 locale tag with a bundled catalog.
type Language string

// Legacy aliases kept for existing callers and tests.
const (
	Chinese  Language = "zh-CN"
	Japanese Language = "ja-JP"
	English  Language = "en-US"
)

// Key identifies a stable localized message.
type Key string

// Legacy message identifiers. New code should use the generated KeyX names.
const (
	TestStarting         Key = KeyTestStarting
	TestPassed           Key = KeyTestPass
	TestFailed           Key = KeyTestFail
	AssertionFailed      Key = KeyTestAssertion
	NoTests              Key = KeyTestNone
	NoMatchingTests      Key = KeyTestNoMatch
	InvalidTestSignature Key = KeyTestInvalidSignature
	CompileFailed        Key = KeyTestCompileFailed
	RuntimeFailed        Key = KeyTestRuntime
	ResourceLimit        Key = KeyTestResource
	PassedCount          Key = KeyTestSummary
	FailedCount          Key = KeyTestSummary
	TotalDuration        Key = KeyTestDuration
	Summary              Key = KeyTestSummary
)

// Default returns the final fallback language.
func Default() Language { return defaultLanguage }

// Supported returns every locale with a complete catalog.
func Supported() []Language { return supported() }

// Normalize canonicalizes a locale tag and applies the documented fallback
// chain. Unknown input resolves to en-US and never fails.
func Normalize(value string) Language { return resolve(value) }

// Resolve is the documented public spelling of Normalize.
func Resolve(value string) Language { return resolve(value) }

func resolve(value string) Language {
	tag := normalizeTag(value)
	if tag == "" {
		return defaultLanguage
	}
	if isSupported(tag) {
		return Language(tag)
	}
	language := firstSubtag(tag)
	region := lastSubtag(tag)
	if region != language {
		candidate := language + "-" + region
		if isSupported(candidate) {
			return Language(candidate)
		}
		if mapped, ok := regionalTags[candidate]; ok && isSupported(string(mapped)) {
			return mapped
		}
	}
	if script := scriptPrefix(tag); script != "" {
		if mapped, ok := scriptDefaults[script]; ok && isSupported(string(mapped)) {
			return mapped
		}
	}
	if aliased, ok := aliases[language]; ok {
		if primary, ok := primaries[aliased]; ok {
			return primary
		}
	}
	if primary, ok := primaries[language]; ok {
		return primary
	}
	return defaultLanguage
}

func isSupported(tag string) bool {
	for _, language := range supported() {
		if string(language) == tag {
			return true
		}
	}
	return false
}

// Select implements the environment part of the documented priority:
// explicit --lang, MIO_LANG, LANGUAGE, LC_ALL, LC_MESSAGES, LANG, en-US.
// The native CLI additionally probes the operating-system preference.
func Select(explicit string, getenv func(string) string) Language {
	if explicit != "" {
		return resolve(explicit)
	}
	if getenv == nil {
		getenv = os.Getenv
	}
	if configured := getenv("MIO_LANG"); configured != "" {
		return resolve(configured)
	}
	if configured := getenv("LANGUAGE"); configured != "" {
		for _, candidate := range strings.Split(configured, ":") {
			candidate = strings.TrimSpace(candidate)
			if usableLocale(candidate) {
				return resolve(candidate)
			}
		}
	}
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale := getenv(name); locale != "" {
			return resolve(locale)
		}
	}
	return defaultLanguage
}

// Text returns a message template, falling back to en-US and then to the key.
func Text(language Language, key Key) string {
	if translated := messages[language][key]; translated != "" {
		return translated
	}
	if fallback := messages[defaultLanguage][key]; fallback != "" {
		return fallback
	}
	return string(key)
}

// Format substitutes {0}..{n} placeholders in a translated template.
func Format(language Language, key Key, arguments ...any) string {
	template := Text(language, key)
	var output strings.Builder
	for at := 0; at < len(template); {
		if template[at] == '{' {
			end := at + 1
			index := 0
			digits := 0
			for end < len(template) && template[end] != '}' {
				if template[end] < '0' || template[end] > '9' {
					break
				}
				index = index*10 + int(template[end]-'0')
				digits++
				end++
			}
			if digits > 0 && end < len(template) && template[end] == '}' && index < len(arguments) {
				fmt.Fprint(&output, arguments[index])
				at = end + 1
				continue
			}
		}
		output.WriteByte(template[at])
		at++
	}
	return output.String()
}

// Direction reports "rtl" for Arabic, Hebrew and Persian catalogs.
func Direction(language Language) string {
	if direction := directions[language]; direction != "" {
		return direction
	}
	return "ltr"
}

func normalizeTag(value string) string {
	value = strings.TrimSpace(value)
	if index := strings.IndexAny(value, ".@"); index >= 0 {
		value = value[:index]
	}
	value = strings.ReplaceAll(value, "_", "-")
	parts := make([]string, 0, 4)
	for _, part := range strings.Split(value, "-") {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	language := strings.ToLower(parts[0])
	if language == "c" || language == "posix" {
		return ""
	}
	if !validLanguage(language) {
		return ""
	}
	normalized := []string{language}
	for _, part := range parts[1:] {
		switch {
		case len(part) == 4 && isAlpha(part):
			normalized = append(normalized, strings.ToUpper(part[:1])+strings.ToLower(part[1:]))
		case (len(part) == 2 && isAlpha(part)) || (len(part) == 3 && isDigits(part)):
			normalized = append(normalized, strings.ToUpper(part))
		default:
			normalized = append(normalized, strings.ToLower(part))
		}
	}
	return strings.Join(normalized, "-")
}

func validLanguage(value string) bool {
	if len(value) >= 2 && len(value) <= 3 {
		return isAlpha(value)
	}
	return len(value) >= 5 && len(value) <= 8 && isAlpha(value)
}

func isAlpha(value string) bool {
	for index := 0; index < len(value); index++ {
		character := value[index]
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')) {
			return false
		}
	}
	return len(value) > 0
}

func isDigits(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}
	return len(value) > 0
}

func firstSubtag(tag string) string {
	if index := strings.IndexByte(tag, '-'); index >= 0 {
		return tag[:index]
	}
	return tag
}

func lastSubtag(tag string) string {
	if index := strings.LastIndexByte(tag, '-'); index >= 0 {
		return tag[index+1:]
	}
	return tag
}

func scriptPrefix(tag string) string {
	parts := strings.Split(tag, "-")
	if len(parts) < 2 || len(parts[1]) != 4 || !isAlpha(parts[1]) {
		return ""
	}
	return parts[0] + "-" + parts[1]
}

func usableLocale(value string) bool {
	if value == "" {
		return false
	}
	if resolve(value) != defaultLanguage {
		return true
	}
	tag := normalizeTag(value)
	return tag != "" && firstSubtag(tag) == "en"
}

func beforeColon(value string) string {
	if index := strings.IndexByte(value, ':'); index >= 0 {
		return value[:index]
	}
	return value
}
