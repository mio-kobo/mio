package value

// Kind 表示运行时值的种类
type Kind uint8

const (
	KindInvalid Kind = iota
	KindInt
	KindFloat
	KindBool
	KindString
	KindNil
	KindArray
	KindStruct
	KindEnum
	KindMap
	KindFunction
)

// String 返回运行时类型名称
func (k Kind) String() string {
	switch k {
	case KindInt:
		return "int64"
	case KindFloat:
		return "float64"
	case KindBool:
		return "bool"
	case KindString:
		return "string"
	case KindNil:
		return "nil"
	case KindArray:
		return "array"
	case KindStruct:
		return "struct"
	case KindEnum:
		return "enum"
	case KindMap:
		return "Map"
	case KindFunction:
		return "function"
	default:
		return "invalid"
	}
}

// Value 表示 Mio 的运行时值
type Value struct {
	kind      Kind
	integer   int64
	floating  float64
	boolean   bool
	text      string
	array     *Array
	structure *Struct
	enum      *Enum
	mapping   *Map
	function  int
}

// Array 保存数组的共享后备存储
type Array struct {
	elements []Value
}

// Struct 保存不可变结构体字段
type Struct struct {
	name   string
	fields []Value
}

// Enum 保存枚举变体及关联数据
type Enum struct {
	name    string
	variant int
	values  []Value
}

// Map 保存共享键值存储
type Map struct {
	values    map[mapKey]Value
	keyKind   Kind
	valueKind Kind
}

type mapKey struct {
	kind    Kind
	integer int64
	boolean bool
	text    string
}

// NewInt 创建整数值
func NewInt(n int64) Value {
	return Value{
		kind:    KindInt,
		integer: n,
	}
}

// NewFloat 创建浮点值
func NewFloat(n float64) Value {
	return Value{kind: KindFloat, floating: n}
}

// NewBool 创建布尔值
func NewBool(b bool) Value {
	return Value{
		kind:    KindBool,
		boolean: b,
	}
}

// NewString 创建字符串值
func NewString(s string) Value {
	return Value{
		kind: KindString,
		text: s,
	}
}

// NewNil 创建空值
func NewNil() Value {
	return Value{
		kind: KindNil,
	}
}

// NewArray 创建数组值
func NewArray(elements []Value) Value {
	return Value{
		kind: KindArray,
		array: &Array{
			elements: append([]Value(nil), elements...),
		},
	}
}

// NewStruct 创建不可变结构体值
func NewStruct(name string, fields []Value) Value {
	return Value{kind: KindStruct, structure: &Struct{name: name, fields: append([]Value(nil), fields...)}}
}

// NewEnum 创建枚举变体值
func NewEnum(name string, variant int, values []Value) Value {
	return Value{kind: KindEnum, enum: &Enum{name: name, variant: variant, values: append([]Value(nil), values...)}}
}

// NewMap 创建共享 Map 值
func NewMap() Value {
	return Value{kind: KindMap, mapping: &Map{values: make(map[mapKey]Value)}}
}

// NewFunction 创建函数值
func NewFunction(id int) Value { return Value{kind: KindFunction, function: id} }

// Kind 返回值的运行时类型
func (v Value) Kind() Kind {
	return v.kind
}

// Int64 读取整数值
func (v Value) Int64() (int64, bool) {
	if v.kind != KindInt {
		return 0, false
	}

	return v.integer, true
}

// Float64 读取浮点值
func (v Value) Float64() (float64, bool) {
	if v.kind != KindFloat {
		return 0, false
	}
	return v.floating, true
}

// Bool 读取布尔值
func (v Value) Bool() (bool, bool) {
	if v.kind != KindBool {
		return false, false
	}

	return v.boolean, true
}

// Text 读取字符串值
func (v Value) Text() (string, bool) {
	if v.kind != KindString {
		return "", false
	}

	return v.text, true
}

// ArrayLen 返回数组长度
func (v Value) ArrayLen() (int, bool) {
	if v.kind != KindArray || v.array == nil {
		return 0, false
	}
	return len(v.array.elements), true
}

// ArrayElement 读取数组元素
func (v Value) ArrayElement(index int64) (Value, bool) {
	if v.kind != KindArray || v.array == nil || index < 0 || index >= int64(len(v.array.elements)) {
		return Value{}, false
	}
	return v.array.elements[index], true
}

// SetArrayElement 修改数组元素
func (v Value) SetArrayElement(index int64, element Value) bool {
	if v.kind != KindArray || v.array == nil || index < 0 || index >= int64(len(v.array.elements)) {
		return false
	}
	if v.array.elements[index].kind != element.kind {
		return false
	}
	v.array.elements[index] = element
	return true
}

// SetNullableArrayElement 修改可空数组元素
func (v Value) SetNullableArrayElement(index int64, element Value) bool {
	if v.kind != KindArray || v.array == nil || index < 0 || index >= int64(len(v.array.elements)) {
		return false
	}
	current := v.array.elements[index]
	if current.kind != KindNil && element.kind != KindNil && current.kind != element.kind {
		return false
	}
	v.array.elements[index] = element
	return true
}

// StructField 读取结构体字段
func (v Value) StructField(index int) (Value, bool) {
	if v.kind != KindStruct || v.structure == nil || index < 0 || index >= len(v.structure.fields) {
		return Value{}, false
	}
	return v.structure.fields[index], true
}

// EnumTag 读取枚举变体编号
func (v Value) EnumTag() (int, bool) {
	if v.kind != KindEnum || v.enum == nil {
		return 0, false
	}
	return v.enum.variant, true
}

// EnumValue 读取枚举关联数据
func (v Value) EnumValue(index int) (Value, bool) {
	if v.kind != KindEnum || v.enum == nil || index < 0 || index >= len(v.enum.values) {
		return Value{}, false
	}
	return v.enum.values[index], true
}

// MapGet 安全读取 Map 值
func (v Value) MapGet(key Value) (Value, bool, bool) {
	if v.kind != KindMap || v.mapping == nil {
		return Value{}, false, false
	}
	encoded, ok := makeMapKey(key)
	if !ok {
		return Value{}, false, false
	}
	if v.mapping.keyKind != KindInvalid && v.mapping.keyKind != key.kind {
		return Value{}, false, false
	}
	result, found := v.mapping.values[encoded]
	return result, found, true
}

// MapSet 插入或更新 Map 值
func (v Value) MapSet(key, element Value) bool {
	if v.kind != KindMap || v.mapping == nil {
		return false
	}
	encoded, ok := makeMapKey(key)
	if !ok {
		return false
	}
	if v.mapping.keyKind != KindInvalid && v.mapping.keyKind != key.kind {
		return false
	}
	if element.kind != KindNil && v.mapping.valueKind != KindInvalid && v.mapping.valueKind != element.kind {
		return false
	}
	if v.mapping.keyKind == KindInvalid {
		v.mapping.keyKind = key.kind
	}
	if element.kind != KindNil && v.mapping.valueKind == KindInvalid {
		v.mapping.valueKind = element.kind
	}
	v.mapping.values[encoded] = element
	return true
}

// FunctionID 返回函数值的 FunctionID
func (v Value) FunctionID() (int, bool) {
	return v.function, v.kind == KindFunction
}

func makeMapKey(v Value) (mapKey, bool) {
	switch v.kind {
	case KindInt:
		return mapKey{kind: v.kind, integer: v.integer}, true
	case KindBool:
		return mapKey{kind: v.kind, boolean: v.boolean}, true
	case KindString:
		return mapKey{kind: v.kind, text: v.text}, true
	default:
		return mapKey{}, false
	}
}

// IsNil 判断是否为空值
func (v Value) IsNil() bool {
	return v.kind == KindNil
}
