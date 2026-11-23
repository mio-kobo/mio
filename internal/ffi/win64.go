// Package ffi 定义可序列化的 Windows AMD64 C 原型，不参与运行时调用。
package ffi

import (
	"fmt"
	"strings"
)

const Prefix = "$win64c|"

// Type 是固定原型的传输类型；cptr 与 uint64 保持独立的源码类型。
func Type(name string, result bool) bool {
	switch name {
	case "int8", "uint8", "int16", "uint16", "int32", "uint32", "int64", "uint64", "float32", "float64", "cptr":
		return true
	case "unit":
		return result
	}
	return false
}

func ValidName(name string, dll bool) bool {
	if len(name) == 0 || len(name) > 255 {
		return false
	}
	for _, c := range []byte(name) {
		if c < 33 || c > 126 || strings.ContainsRune("|/\\:", rune(c)) {
			return false
		}
	}
	if dll && (!strings.HasSuffix(strings.ToLower(name), ".dll") || name == ".dll") {
		return false
	}
	return true
}

// Key 复用 Mochi 空指令 native stub 的名称槽，格式显式携带 DLL、导出名和完整原型。
// 分隔符不允许出现在名称中，因此没有转义或歧义，也不改变 Runtime ABI。
func Key(dll, symbol, result string, params []string) string {
	return Prefix + strings.Join(append([]string{dll, symbol, result}, params...), "|")
}

func Validate(key string, count int, returns bool) error {
	parts := strings.Split(key, "|")
	if len(parts) != 4+count || count < 0 || count > 256 || parts[0] != "$win64c" {
		return fmt.Errorf("E_FFI_PROTOTYPE invalid Windows AMD64 C prototype")
	}
	if !ValidName(parts[1], true) || !ValidName(parts[2], false) {
		return fmt.Errorf("E_FFI_NAME invalid DLL or export name")
	}
	if !Type(parts[3], true) || (parts[3] != "unit") != returns {
		return fmt.Errorf("E_FFI_TYPE unsupported C return type")
	}
	for _, p := range parts[4:] {
		if !Type(p, false) {
			return fmt.Errorf("E_FFI_TYPE unsupported C parameter %q", p)
		}
	}
	return nil
}
