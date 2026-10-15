package value

import (
	"fmt"
	"unicode/utf8"
)

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
	KindResource
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
	case KindResource:
		return "resource"
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
	resource   any
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
	elementKind  Kind
	nullable     bool
	reserved     int
}

const maxArrayAllocationBytes = 256 << 20

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

// NewResource 创建不透明资源值
func NewResource(resource any) Value { return Value{kind: KindResource, resource: resource} }

// Resource 返回不透明资源载荷
func (v Value) Resource() (any, bool) { return v.resource, v.kind == KindResource && v.resource != nil }

// NewArray 创建数组值
func NewArray(elements []Value) Value {
	array := &Array{}
	if isUint8Array(elements) {
		array.elementKind = KindUint8
		array.byteElements = make([]byte, len(elements))
		for index, element := range elements {
			array.byteElements[index] = uint8(element.integer)
		}
	} else {
		array.elements = append([]Value(nil), elements...)
		if len(elements) != 0 {
			array.elementKind = elements[0].kind
		}
	}
	return Value{
		kind:  KindArray,
		array: array,
	}
}

// NewTypedArray 按元素类型创建数组
func NewTypedArray(elements []Value, elementKind Kind, nullable bool) Value {
	if elementKind == KindInvalid {
		for _, element := range elements {
			if element.kind == KindNil {
				nullable = true
			} else if elementKind == KindInvalid {
				elementKind = element.kind
			}
		}
	}
	array := &Array{elementKind: elementKind, nullable: nullable}
	if elementKind == KindUint8 && !nullable {
		array.byteElements = make([]byte, len(elements))
		for index, element := range elements {
			array.byteElements[index] = uint8(element.integer)
		}
	} else {
		array.elements = append([]Value(nil), elements...)
	}
	return Value{kind: KindArray, array: array}
}

// NewByteArray 创建独立的紧凑 uint8 数组
func NewByteArray(elements []byte) (result Value, err error) {
	if err := checkArrayAllocation(int64(len(elements)), KindUint8); err != nil {
		return Value{}, err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = Value{}
			err = fmt.Errorf("array allocation failed: %v", recovered)
		}
	}()
	return Value{
		kind: KindArray,
		array: &Array{
			byteElements: append([]byte(nil), elements...),
			elementKind:  KindUint8,
		},
	}, nil
}

// NewByteArrayFromString 复制字符串字节到紧凑数组
func NewByteArrayFromString(text string) (result Value, err error) {
	if err := checkArrayAllocation(int64(len(text)), KindUint8); err != nil {
		return Value{}, err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = Value{}
			err = fmt.Errorf("array allocation failed: %v", recovered)
		}
	}()
	elements := make([]byte, len(text))
	copy(elements, text)
	return Value{kind: KindArray, array: &Array{byteElements: elements, elementKind: KindUint8}}, nil
}

// NewFilledArray 创建指定长度的数组
func NewFilledArray(length int64, initial Value, elementKind Kind, nullable bool) (result Value, err error) {
	if elementKind == KindInvalid {
		if initial.kind == KindNil {
			nullable = true
		} else {
			elementKind = initial.kind
		}
	}
	if err := checkArrayAllocation(length, elementKind); err != nil {
		return Value{}, err
	}
	if !nullable && initial.kind == KindNil {
		return Value{}, fmt.Errorf("non-nullable array cannot contain nil")
	}
	if elementKind != KindInvalid && initial.kind != KindNil && initial.kind != elementKind {
		return Value{}, fmt.Errorf("array requires %s value, got %s", elementKind, initial.kind)
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = Value{}
			err = fmt.Errorf("array allocation failed: %v", recovered)
		}
	}()
	array := NewTypedArray(nil, elementKind, nullable)
	if elementKind == KindUint8 && !nullable {
		array.array.byteElements = make([]byte, int(length))
		for index := range array.array.byteElements {
			array.array.byteElements[index] = uint8(initial.integer)
		}
	} else {
		array.array.elements = make([]Value, int(length))
		for index := range array.array.elements {
			array.array.elements[index] = initial
		}
	}
	return array, nil
}

// NewArrayWithCapacity 创建带预留容量的空数组
func NewArrayWithCapacity(capacity int64, elementKind Kind, nullable bool) (result Value, err error) {
	if err := checkArrayAllocation(capacity, elementKind); err != nil {
		return Value{}, err
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			result = Value{}
			err = fmt.Errorf("array allocation failed: %v", recovered)
		}
	}()
	array := &Array{elementKind: elementKind, nullable: nullable}
	if elementKind == KindInvalid {
		array.reserved = int(capacity)
	} else if elementKind == KindUint8 && !nullable {
		array.byteElements = make([]byte, 0, int(capacity))
	} else {
		array.elements = make([]Value, 0, int(capacity))
	}
	return Value{kind: KindArray, array: array}, nil
}

func checkArrayAllocation(count int64, elementKind Kind) error {
	if count < 0 {
		return fmt.Errorf("array size cannot be negative: %d", count)
	}
	bytesPerElement := arrayElementBytes(elementKind)
	if count > maxArrayAllocationBytes/bytesPerElement {
		return fmt.Errorf("array allocation exceeds %d byte limit", maxArrayAllocationBytes)
	}
	return nil
}

func arrayElementBytes(elementKind Kind) int64 {
	if elementKind == KindUint8 {
		return 1
	}
	return 96
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

// ArrayCapacity 返回数组容量
func (v Value) ArrayCapacity() (int, bool) {
	if v.kind != KindArray || v.array == nil {
		return 0, false
	}
	if v.array.reserved != 0 {
		return v.array.reserved, true
	}
	if v.array.byteElements != nil {
		return cap(v.array.byteElements), true
	}
	return cap(v.array.elements), true
}

// ByteArrayCopy 复制紧凑 uint8 数组
func (v Value) ByteArrayCopy() ([]byte, bool) {
	if v.kind != KindArray || v.array == nil || v.array.elementKind != KindUint8 || v.array.nullable {
		return nil, false
	}
	return append([]byte(nil), v.array.byteElements...), true
}

// ByteArrayRangeCopy 复制紧凑 uint8 数组的指定范围
func (v Value) ByteArrayRangeCopy(offset, length int64) ([]byte, error) {
	if v.kind != KindArray || v.array == nil || v.array.elementKind != KindUint8 || v.array.nullable {
		return nil, fmt.Errorf("operation requires []uint8")
	}
	if offset < 0 || length < 0 || offset > int64(len(v.array.byteElements)) || length > int64(len(v.array.byteElements))-offset {
		return nil, fmt.Errorf("byte range offset %d length %d is out of bounds for length %d", offset, length, len(v.array.byteElements))
	}
	return append([]byte(nil), v.array.byteElements[offset:offset+length]...), nil
}

// WriteByteArrayRange 写入紧凑 uint8 数组的已有范围
func (v Value) WriteByteArrayRange(offset int64, source []byte) error {
	if v.kind != KindArray || v.array == nil || v.array.elementKind != KindUint8 || v.array.nullable {
		return fmt.Errorf("operation requires []uint8")
	}
	if offset < 0 || offset > int64(len(v.array.byteElements)) || int64(len(source)) > int64(len(v.array.byteElements))-offset {
		return fmt.Errorf("byte range offset %d length %d is out of bounds for length %d", offset, len(source), len(v.array.byteElements))
	}
	copy(v.array.byteElements[offset:], source)
	return nil
}

// UTF8StringCopy 校验并复制紧凑 uint8 数组
func (v Value) UTF8StringCopy() (string, bool, error) {
	if v.kind != KindArray || v.array == nil || v.array.elementKind != KindUint8 || v.array.nullable {
		return "", false, fmt.Errorf("conversion requires []uint8")
	}
	if !utf8.Valid(v.array.byteElements) {
		return "", false, nil
	}
	return string(v.array.byteElements), true, nil
}

// ReserveArray 保证数组具有足够的剩余容量
func (v Value) ReserveArray(additional int64) (err error) {
	if v.kind != KindArray || v.array == nil {
		return fmt.Errorf("reserve requires array")
	}
	if additional < 0 {
		return fmt.Errorf("additional capacity cannot be negative: %d", additional)
	}
	length, _ := v.ArrayLen()
	if additional > int64(^uint(0)>>1)-int64(length) {
		return fmt.Errorf("array capacity overflow")
	}
	required := int64(length) + additional
	if err := checkArrayAllocation(required, v.array.elementKind); err != nil {
		return err
	}
	capacity, _ := v.ArrayCapacity()
	if int64(capacity) >= required {
		return nil
	}
	if v.array.elementKind == KindInvalid && v.array.byteElements == nil && v.array.elements == nil {
		v.array.reserved = int(required)
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("array allocation failed: %v", recovered)
		}
	}()
	maxCapacity := int64(maxArrayAllocationBytes) / arrayElementBytes(v.array.elementKind)
	newCapacity := grownArrayCapacity(capacity, int(required), int(maxCapacity))
	if v.array.byteElements != nil {
		storage := make([]byte, length, newCapacity)
		copy(storage, v.array.byteElements)
		v.array.byteElements = storage
	} else {
		storage := make([]Value, length, newCapacity)
		copy(storage, v.array.elements)
		v.array.elements = storage
	}
	return nil
}

func grownArrayCapacity(current, required, limit int) int {
	capacity := current
	if capacity < 4 {
		capacity = 4
	}
	for capacity < required {
		if capacity < 1024 {
			capacity *= 2
		} else {
			capacity += capacity / 2
		}
		if capacity < 0 || capacity > limit {
			return required
		}
	}
	return capacity
}

// AppendArrayElement 向共享数组追加元素
func (v Value) AppendArrayElement(element Value) error {
	if v.kind != KindArray || v.array == nil {
		return fmt.Errorf("push requires array")
	}
	if element.kind == KindNil && !v.array.nullable {
		if v.array.elementKind != KindInvalid {
			return fmt.Errorf("non-nullable array cannot contain nil")
		}
		v.array.nullable = true
	}
	if element.kind != KindNil && v.array.elementKind != KindInvalid && element.kind != v.array.elementKind {
		return fmt.Errorf("array requires %s value, got %s", v.array.elementKind, element.kind)
	}
	if v.array.elementKind == KindInvalid && element.kind != KindNil {
		v.array.elementKind = element.kind
	}
	if v.array.byteElements == nil && v.array.elements == nil {
		capacity := v.array.reserved
		v.array.reserved = 0
		if v.array.elementKind == KindUint8 && !v.array.nullable {
			v.array.byteElements = make([]byte, 0, capacity)
		} else {
			v.array.elements = make([]Value, 0, capacity)
		}
	}
	if err := v.ReserveArray(1); err != nil {
		return err
	}
	if v.array.byteElements != nil {
		v.array.byteElements = append(v.array.byteElements, uint8(element.integer))
	} else {
		v.array.elements = append(v.array.elements, element)
	}
	return nil
}

// TruncateArray 缩短共享数组并保留容量
func (v Value) TruncateArray(newLength int64) error {
	if v.kind != KindArray || v.array == nil {
		return fmt.Errorf("truncate requires array")
	}
	length, _ := v.ArrayLen()
	if newLength < 0 || newLength > int64(length) {
		return fmt.Errorf("array length %d is outside range [0, %d]", newLength, length)
	}
	if v.array.byteElements != nil {
		v.array.byteElements = v.array.byteElements[:newLength]
		return nil
	}
	clear(v.array.elements[newLength:])
	v.array.elements = v.array.elements[:newLength]
	return nil
}

// AppendArray 向共享数组批量追加浅复制元素
func (v Value) AppendArray(source Value) error {
	if v.kind != KindArray || v.array == nil || source.kind != KindArray || source.array == nil {
		return fmt.Errorf("append_all requires arrays")
	}
	sourceLength, _ := source.ArrayLen()
	if sourceLength == 0 {
		return nil
	}
	if v.array.elementKind != KindInvalid && source.array.elementKind != KindInvalid && v.array.elementKind != source.array.elementKind {
		return fmt.Errorf("array requires %s values, got %s", v.array.elementKind, source.array.elementKind)
	}
	if !v.array.nullable && source.array.nullable {
		return fmt.Errorf("non-nullable array cannot append nullable values")
	}
	if v.array.elementKind == KindInvalid {
		v.array.elementKind = source.array.elementKind
		v.array.nullable = source.array.nullable
	}
	if v.array.byteElements == nil && v.array.elements == nil {
		capacity := v.array.reserved
		v.array.reserved = 0
		if v.array.elementKind == KindUint8 && !v.array.nullable {
			v.array.byteElements = make([]byte, 0, capacity)
		} else {
			v.array.elements = make([]Value, 0, capacity)
		}
	}
	if err := v.ReserveArray(int64(sourceLength)); err != nil {
		return err
	}
	destinationLength, _ := v.ArrayLen()
	if v.array.byteElements != nil && source.array.byteElements != nil {
		v.array.byteElements = v.array.byteElements[:destinationLength+sourceLength]
		copy(v.array.byteElements[destinationLength:], source.array.byteElements[:sourceLength])
		return nil
	}
	if v.array.byteElements != nil || source.array.byteElements != nil {
		return fmt.Errorf("array storage layout mismatch")
	}
	v.array.elements = v.array.elements[:destinationLength+sourceLength]
	copy(v.array.elements[destinationLength:], source.array.elements[:sourceLength])
	return nil
}

// CloneArray 创建独立的浅复制数组
func (v Value) CloneArray() (Value, error) {
	if v.kind != KindArray || v.array == nil {
		return Value{}, fmt.Errorf("clone requires array")
	}
	clone := &Array{elementKind: v.array.elementKind, nullable: v.array.nullable, reserved: v.array.reserved}
	if v.array.byteElements != nil {
		clone.byteElements = make([]byte, len(v.array.byteElements))
		copy(clone.byteElements, v.array.byteElements)
	} else {
		clone.elements = make([]Value, len(v.array.elements))
		copy(clone.elements, v.array.elements)
	}
	return Value{kind: KindArray, array: clone}, nil
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
