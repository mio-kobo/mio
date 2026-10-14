package value

// Kind 表示运行时值的种类
type Kind uint8

const (
	KindInvalid Kind = iota
	KindInt
	KindFloat
	KindFloat32
	KindBool
	KindString
	KindNil
	KindArray
	KindStruct
	KindEnum
	KindMap
	KindFunction
	KindInt8
	KindInt16
	KindInt32
	KindUint8
	KindUint16
	KindUint32
)

// String 返回运行时类型名称
func (k Kind) String() string {
	switch k {
	case KindInt:
		return "int64"
	case KindInt8:
		return "int8"
	case KindInt16:
		return "int16"
	case KindInt32:
		return "int32"
	case KindUint8:
		return "uint8"
	case KindUint16:
		return "uint16"
	case KindUint32:
		return "uint32"
	case KindFloat:
		return "float64"
	case KindFloat32:
		return "float32"
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
	kind       Kind
	integer    int64
	floating   float64
	floating32 float32
	boolean    bool
	text       string
	array      *Array
	structure  *Struct
	enum       *Enum
	mapping    *Map
	function   int
	closure    *Closure
}

// Cell 保存闭包共享的可变绑定
type Cell struct {
	Value Value
}

// Closure 保存函数及其捕获的 Cell
type Closure struct {
	Function int
	Cells    []*Cell
}

// Array 保存数组的共享后备存储
type Array struct {
	elements     []Value
	byteElements []byte
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

// NewInt8 创建 int8 值
func NewInt8(n int8) Value { return Value{kind: KindInt8, integer: int64(n)} }

// NewInt16 创建 int16 值
func NewInt16(n int16) Value { return Value{kind: KindInt16, integer: int64(n)} }

// NewInt32 创建 int32 值
func NewInt32(n int32) Value { return Value{kind: KindInt32, integer: int64(n)} }

// NewUint8 创建 uint8 值
func NewUint8(n uint8) Value { return Value{kind: KindUint8, integer: int64(n)} }

// NewUint16 创建 uint16 值
func NewUint16(n uint16) Value { return Value{kind: KindUint16, integer: int64(n)} }

// NewUint32 创建 uint32 值
func NewUint32(n uint32) Value { return Value{kind: KindUint32, integer: int64(n)} }

// NewIntegerBits 按指定整数类型创建值
func NewIntegerBits(kind Kind, bits uint64) (Value, bool) {
	switch kind {
	case KindInt:
		return NewInt(int64(bits)), true
	case KindInt8:
		return NewInt8(int8(bits)), true
	case KindInt16:
		return NewInt16(int16(bits)), true
	case KindInt32:
		return NewInt32(int32(bits)), true
	case KindUint8:
		return NewUint8(uint8(bits)), true
	case KindUint16:
		return NewUint16(uint16(bits)), true
	case KindUint32:
		return NewUint32(uint32(bits)), true
	default:
		return Value{}, false
	}
}

// NewFloat 创建浮点值
func NewFloat(n float64) Value {
	return Value{kind: KindFloat, floating: n}
}

// NewFloat32 创建 binary32 浮点值
func NewFloat32(n float32) Value {
	return Value{kind: KindFloat32, floating32: n}
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
	array := &Array{}
	if isUint8Array(elements) {
		array.byteElements = make([]byte, len(elements))
		for index, element := range elements {
			array.byteElements[index] = uint8(element.integer)
		}
	} else {
		array.elements = append([]Value(nil), elements...)
	}
	return Value{
		kind:  KindArray,
		array: array,
	}
}

func isUint8Array(elements []Value) bool {
	if len(elements) == 0 {
		return false
	}
	for _, element := range elements {
		if element.kind != KindUint8 {
			return false
		}
	}
	return true
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

// NewClosure 创建带捕获环境的函数值
func NewClosure(id int, cells []*Cell) Value {
	return Value{kind: KindFunction, function: id, closure: &Closure{Function: id, Cells: append([]*Cell(nil), cells...)}}
}

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

// Int8 读取 int8 值
func (v Value) Int8() (int8, bool) {
	return int8(v.integer), v.kind == KindInt8
}

// Int16 读取 int16 值
func (v Value) Int16() (int16, bool) {
	return int16(v.integer), v.kind == KindInt16
}

// Int32 读取 int32 值
func (v Value) Int32() (int32, bool) {
	return int32(v.integer), v.kind == KindInt32
}

// Uint8 读取 uint8 值
func (v Value) Uint8() (uint8, bool) {
	return uint8(v.integer), v.kind == KindUint8
}

// Uint16 读取 uint16 值
func (v Value) Uint16() (uint16, bool) {
	return uint16(v.integer), v.kind == KindUint16
}

// Uint32 读取 uint32 值
func (v Value) Uint32() (uint32, bool) {
	return uint32(v.integer), v.kind == KindUint32
}

// IntegerBits 返回整数的无符号位模式
func (v Value) IntegerBits() (uint64, bool) {
	if !v.kind.IsInteger() {
		return 0, false
	}
	return uint64(v.integer), true
}

// SignedInteger 返回有符号整数值
func (v Value) SignedInteger() (int64, bool) {
	if !v.kind.IsSignedInteger() {
		return 0, false
	}
	return v.integer, true
}

// UnsignedInteger 返回无符号整数值
func (v Value) UnsignedInteger() (uint64, bool) {
	if !v.kind.IsUnsignedInteger() {
		return 0, false
	}
	return uint64(v.integer), true
}

// IsInteger 判断运行时类型是否为整数
func (k Kind) IsInteger() bool {
	return k.IsSignedInteger() || k.IsUnsignedInteger()
}

// IsSignedInteger 判断运行时类型是否为有符号整数
func (k Kind) IsSignedInteger() bool {
	switch k {
	case KindInt, KindInt8, KindInt16, KindInt32:
		return true
	default:
		return false
	}
}

// IsUnsignedInteger 判断运行时类型是否为无符号整数
func (k Kind) IsUnsignedInteger() bool {
	switch k {
	case KindUint8, KindUint16, KindUint32:
		return true
	default:
		return false
	}
}

// Float64 读取浮点值
func (v Value) Float64() (float64, bool) {
	if v.kind != KindFloat {
		return 0, false
	}
	return v.floating, true
}

// Float32 读取 binary32 浮点值
func (v Value) Float32() (float32, bool) {
	if v.kind != KindFloat32 {
		return 0, false
	}
	return v.floating32, true
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
	if v.array.byteElements != nil {
		return len(v.array.byteElements), true
	}
	return len(v.array.elements), true
}

// ArrayElement 读取数组元素
func (v Value) ArrayElement(index int64) (Value, bool) {
	if v.kind != KindArray || v.array == nil || index < 0 {
		return Value{}, false
	}
	if v.array.byteElements != nil {
		if index >= int64(len(v.array.byteElements)) {
			return Value{}, false
		}
		return NewUint8(v.array.byteElements[index]), true
	}
	if index >= int64(len(v.array.elements)) {
		return Value{}, false
	}
	return v.array.elements[index], true
}

// SetArrayElement 修改数组元素
func (v Value) SetArrayElement(index int64, element Value) bool {
	if v.kind != KindArray || v.array == nil || index < 0 {
		return false
	}
	if v.array.byteElements != nil {
		if index >= int64(len(v.array.byteElements)) || element.kind != KindUint8 {
			return false
		}
		v.array.byteElements[index] = uint8(element.integer)
		return true
	}
	if index >= int64(len(v.array.elements)) {
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
	if v.kind != KindArray || v.array == nil || index < 0 {
		return false
	}
	if v.array.byteElements != nil {
		if index >= int64(len(v.array.byteElements)) {
			return false
		}
		if element.kind == KindUint8 {
			v.array.byteElements[index] = uint8(element.integer)
			return true
		}
		if element.kind != KindNil {
			return false
		}
		v.array.elements = make([]Value, len(v.array.byteElements))
		for itemIndex, item := range v.array.byteElements {
			v.array.elements[itemIndex] = NewUint8(item)
		}
		v.array.byteElements = nil
		v.array.elements[index] = element
		return true
	}
	if index >= int64(len(v.array.elements)) {
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

// ClosureCells 返回闭包捕获的 Cell
func (v Value) ClosureCells() ([]*Cell, bool) {
	if v.kind != KindFunction || v.closure == nil {
		return nil, false
	}
	return v.closure.Cells, true
}

func makeMapKey(v Value) (mapKey, bool) {
	switch v.kind {
	case KindInt, KindInt8, KindInt16, KindInt32, KindUint8, KindUint16, KindUint32:
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
