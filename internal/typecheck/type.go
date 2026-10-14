package typecheck

import "strings"

// Kind 保存完整的 Mio 静态类型描述
type Kind string

const (
	KindInvalid Kind = ""
	KindInt     Kind = "int64"
	KindFloat   Kind = "float64"
	KindBool    Kind = "bool"
	KindString  Kind = "string"
	KindNil     Kind = "nil"
	KindUnit    Kind = "unit"
	KindArray   Kind = "array"
)

// ArrayOf 创建数组类型
func ArrayOf(element Kind) Kind {
	if element == KindInvalid || element == KindNil || element == KindUnit || element.IsArray() {
		return KindInvalid
	}
	return Kind("array<" + string(element) + ">")
}

// NullableOf 创建可空类型
func NullableOf(kind Kind) Kind {
	if kind == KindInvalid || kind == KindNil || kind == KindUnit || kind.IsNullable() {
		return KindInvalid
	}
	return Kind("nullable<" + string(kind) + ">")
}

// NamedOf 创建带类型参数的具名类型
func NamedOf(name string, args ...Kind) Kind {
	if name == "" {
		return KindInvalid
	}
	parts := make([]string, len(args))
	for i, arg := range args {
		if arg == KindInvalid {
			return KindInvalid
		}
		parts[i] = string(arg)
	}
	if len(parts) == 0 {
		return Kind("named<" + name + ">")
	}
	return Kind("named<" + name + ";" + strings.Join(parts, ";") + ">")
}

// TypeParameter 创建类型参数引用
func TypeParameter(name string) Kind { return Kind("param<" + name + ">") }

// FunctionOf 创建函数类型
func FunctionOf(params []Kind, result Kind) Kind {
	parts := make([]string, len(params))
	for i, param := range params {
		parts[i] = string(param)
	}
	return Kind("fn<" + strings.Join(parts, ";") + "->" + string(result) + ">")
}

// MapOf 创建 Map 类型
func MapOf(key, value Kind) Kind { return NamedOf("Map", key, value) }

// IsNullable 判断类型是否允许 mu
func (k Kind) IsNullable() bool { return strings.HasPrefix(string(k), "nullable<") }

// NonNullable 返回可空类型的基础类型
func (k Kind) NonNullable() Kind {
	if !k.IsNullable() {
		return k
	}
	return Kind(string(k)[len("nullable<") : len(k)-1])
}

// IsArray 判断是否为数组类型
func (k Kind) IsArray() bool { return strings.HasPrefix(string(k), "array<") }

// ElementKind 返回数组元素类型
func (k Kind) ElementKind() Kind {
	if !k.IsArray() {
		return KindInvalid
	}
	return Kind(string(k)[len("array<") : len(k)-1])
}

// IsNamed 判断是否为具名类型
func (k Kind) IsNamed() bool { return strings.HasPrefix(string(k), "named<") }

// NamedParts 返回具名类型名称及参数
func (k Kind) NamedParts() (string, []Kind, bool) {
	if !k.IsNamed() {
		return "", nil, false
	}
	content := string(k)[len("named<") : len(k)-1]
	parts := splitTypeParts(content, ';')
	args := make([]Kind, 0, len(parts)-1)
	for _, part := range parts[1:] {
		args = append(args, Kind(part))
	}
	return parts[0], args, true
}

// IsTypeParameter 判断是否为类型参数
func (k Kind) IsTypeParameter() bool { return strings.HasPrefix(string(k), "param<") }

// TypeParameterName 返回类型参数名称
func (k Kind) TypeParameterName() string {
	if !k.IsTypeParameter() {
		return ""
	}
	return string(k)[len("param<") : len(k)-1]
}

// IsFunction 判断是否为函数类型
func (k Kind) IsFunction() bool { return strings.HasPrefix(string(k), "fn<") }

// FunctionParts 返回函数参数与结果类型
func (k Kind) FunctionParts() ([]Kind, Kind, bool) {
	if !k.IsFunction() {
		return nil, KindInvalid, false
	}
	content := string(k)[len("fn<") : len(k)-1]
	arrow := topLevelArrow(content)
	if arrow < 0 {
		return nil, KindInvalid, false
	}
	params := make([]Kind, 0)
	if content[:arrow] != "" {
		for _, part := range splitTypeParts(content[:arrow], ';') {
			params = append(params, Kind(part))
		}
	}
	return params, Kind(content[arrow+2:]), true
}

// IsMap 判断是否为 Map 类型
func (k Kind) IsMap() bool {
	name, args, ok := k.NamedParts()
	return ok && name == "Map" && len(args) == 2
}

// String 返回类型名称
func (k Kind) String() string {
	if k == KindInvalid {
		return "invalid"
	}
	if k.IsNullable() {
		base := k.NonNullable()
		if base.IsArray() || base.IsFunction() {
			return "(" + base.String() + ")?"
		}
		return base.String() + "?"
	}
	if k.IsArray() {
		return "[]" + k.ElementKind().String()
	}
	if name, args, ok := k.NamedParts(); ok {
		if len(args) == 0 {
			return name
		}
		parts := make([]string, len(args))
		for i, arg := range args {
			parts[i] = arg.String()
		}
		return name + "<" + strings.Join(parts, ", ") + ">"
	}
	if k.IsTypeParameter() {
		return k.TypeParameterName()
	}
	if params, result, ok := k.FunctionParts(); ok {
		parts := make([]string, len(params))
		for i, param := range params {
			parts[i] = param.String()
		}
		return "(" + strings.Join(parts, ", ") + ") -> " + result.String()
	}
	return string(k)
}

func splitTypeParts(input string, separator byte) []string {
	parts := make([]string, 0)
	depth := 0
	start := 0
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case '<':
			depth++
		case '>':
			depth--
		default:
			if input[i] == separator && depth == 0 {
				parts = append(parts, input[start:i])
				start = i + 1
			}
		}
	}
	return append(parts, input[start:])
}

func topLevelArrow(input string) int {
	depth := 0
	for i := 0; i+1 < len(input); i++ {
		switch input[i] {
		case '<':
			depth++
		case '>':
			depth--
		}
		if depth == 0 && input[i] == '-' && input[i+1] == '>' {
			return i
		}
	}
	return -1
}
