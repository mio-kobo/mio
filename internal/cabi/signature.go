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

// IsReservedLoaderIntrinsic reports whether symbol is one of the internal
// Darwin dylib loader intrinsics owned by kiso/ffi/darwin.
func IsReservedLoaderIntrinsic(symbol string) bool {
	switch symbol {
	case "mio_ffi_store_byte",
		"mio_ffi_load_byte",
		"mio_ffi_call_g",
		"mio_ffi_call_gg",
		"mio_ffi_call_ggg",
		"mio_ffi_call_jj",
		"mio_ffi_call_jgg",
		"mio_ffi_call_lg",
		"mio_ffi_load_pointer",
		"mio_ffi_pointer_offset",
		"mio_ffi_load_word":
		return true
	default:
		return false
	}
}

// IsReservedSymbol reports whether declaring an external C symbol from
// packagePath is forbidden because it collides with compiler-owned dylib
// markers or private loader intrinsics.
func IsReservedSymbol(symbol string, packagePath string) bool {
	if strings.HasPrefix(symbol, "mio_ffi_v1_") {
		return true
	}
	if packagePath == "kiso/ffi/darwin" {
		return false
	}
	return IsReservedLoaderIntrinsic(symbol)
}
