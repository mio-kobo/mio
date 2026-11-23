// Package cabi describes the C prototypes that the Reference VM can check.
package cabi

const Prefix = "$cabi::"

func ValidSymbol(name string) bool {
	if name == "main" || len(name) == 0 || len(name) > 1024 {
		return false
	}
	for index, b := range []byte(name) {
		if !(b == '_' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || index > 0 && b >= '0' && b <= '9') {
			return false
		}
	}
	return true
}

func TypeCode(name string) string {
	switch name {
	case "unit":
		return "v"
	case "int8":
		return "a"
	case "uint8":
		return "b"
	case "int16":
		return "c"
	case "uint16":
		return "d"
	case "int32", "rune":
		return "e"
	case "uint32":
		return "f"
	case "int64":
		return "g"
	case "uint64":
		return "h"
	case "float32":
		return "i"
	case "float64":
		return "j"
	case "cptr":
		return "k"
	case "bool":
		return "l"
	default:
		return ""
	}
}
