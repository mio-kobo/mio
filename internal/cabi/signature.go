// Package cabi describes the C prototypes that the Reference VM can check.
package cabi

import "strings"

const Prefix = "$cabi::"

// IndirectSymbol is the reserved symbol of an indirect C call. The first
// parameter of such a declaration is the cptr of the function to call (usually
// from dlsym) and the remaining parameters are the C arguments.
const IndirectSymbol = "mio_ffi_indirect"

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

// ValidExportSymbol rejects the reserved runtime prefix on C exports.
func ValidExportSymbol(name string) bool {
	return ValidSymbol(name) && !strings.HasPrefix(name, "mio_ffi_v1_")
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

// ParamCode returns the prototype code of a C parameter type. A []uint8 passes
// the address of its data ("m"); the array stays owned by Mio and the pointer is
// only valid for the duration of the call.
func ParamCode(name string, element string) string {
	if name == "[]" {
		if element == "uint8" {
			return "m"
		}
		return ""
	}
	if element != "" {
		return ""
	}
	return TypeCode(name)
}
