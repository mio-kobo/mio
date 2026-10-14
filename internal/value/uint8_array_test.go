package value

import "testing"

// 测试 uint8 数组使用紧凑连续存储
func TestUint8ArrayCompactStorage(t *testing.T) {
	elements := []Value{NewUint8(0), NewUint8(1), NewUint8(255)}
	array := NewArray(elements)
	elements[1] = NewUint8(99)

	if array.array == nil || array.array.elements != nil {
		t.Fatalf("elements=%v", array.array.elements)
	}
	if got := array.array.byteElements; len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 255 {
		t.Fatalf("byteElements=%v", got)
	}
	for index, want := range []uint8{0, 1, 255} {
		element, ok := array.ArrayElement(int64(index))
		if !ok {
			t.Fatalf("index %d read failed", index)
		}
		got, ok := element.Uint8()
		if !ok || got != want {
			t.Fatalf("index %d=%d ok=%v, want %d", index, got, ok, want)
		}
	}
}

// 测试单元素 uint8 数组使用紧凑存储
func TestUint8ArraySingleElement(t *testing.T) {
	array := NewArray([]Value{NewUint8(255)})
	if len(array.array.byteElements) != 1 || array.array.byteElements[0] != 255 || array.array.elements != nil {
		t.Fatalf("storage=%+v", array.array)
	}
}

// 测试空数组与普通数组继续使用 Value 存储
func TestArrayStorageSelection(t *testing.T) {
	tests := []struct {
		name     string
		elements []Value
	}{
		{name: "空数组"},
		{name: "int64", elements: []Value{NewInt(1)}},
		{name: "float32", elements: []Value{NewFloat32(1)}},
		{name: "string", elements: []Value{NewString("Mio")}},
		{name: "混合运行时值", elements: []Value{NewUint8(1), NewInt(2)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			array := NewArray(test.elements)
			if array.array.byteElements != nil {
				t.Fatalf("byteElements=%v", array.array.byteElements)
			}
			if len(array.array.elements) != len(test.elements) {
				t.Fatalf("len(elements)=%d, want %d", len(array.array.elements), len(test.elements))
			}
		})
	}
}

// 测试 uint8 数组别名共享修改
func TestUint8ArraySharedMutation(t *testing.T) {
	array := NewArray([]Value{NewUint8(0), NewUint8(255)})
	alias := array
	if !alias.SetArrayElement(0, NewUint8(255)) {
		t.Fatal("修改 uint8 元素失败")
	}
	element, ok := array.ArrayElement(0)
	if !ok {
		t.Fatal("读取 uint8 元素失败")
	}
	got, ok := element.Uint8()
	if !ok || got != 255 {
		t.Fatalf("element=%v", element)
	}
	if array.SetArrayElement(0, NewInt(1)) {
		t.Fatal("不应接受非 uint8 元素")
	}
}

// 测试可空写入时保留数组公开语义
func TestUint8ArrayNullableMutationFallsBack(t *testing.T) {
	array := NewArray([]Value{NewUint8(1), NewUint8(2)})
	alias := array
	if !array.SetNullableArrayElement(1, NewNil()) {
		t.Fatal("写入 nil 失败")
	}
	if array.array.byteElements != nil || len(array.array.elements) != 2 {
		t.Fatalf("storage=%+v", array.array)
	}
	element, ok := alias.ArrayElement(1)
	if !ok || !element.IsNil() {
		t.Fatalf("element=%v ok=%v", element, ok)
	}
	if !alias.SetNullableArrayElement(1, NewUint8(255)) {
		t.Fatal("恢复 uint8 元素失败")
	}
	element, _ = array.ArrayElement(1)
	got, ok := element.Uint8()
	if !ok || got != 255 {
		t.Fatalf("element=%v", element)
	}
}

// 测试大型 uint8 数组读写与边界检查
func TestUint8ArrayLargeStorage(t *testing.T) {
	const size = 1 << 20
	elements := make([]Value, size)
	for index := range elements {
		elements[index] = NewUint8(uint8(index))
	}
	array := NewArray(elements)
	if len(array.array.byteElements) != size || array.array.elements != nil {
		t.Fatalf("byteElements=%d elements=%d", len(array.array.byteElements), len(array.array.elements))
	}
	for _, index := range []int64{0, 1, size / 2, size - 1} {
		if !array.SetArrayElement(index, NewUint8(255)) {
			t.Fatalf("index %d write failed", index)
		}
		element, ok := array.ArrayElement(index)
		got, uint8OK := element.Uint8()
		if !ok || !uint8OK || got != 255 {
			t.Fatalf("index %d=%v ok=%v uint8=%v", index, element, ok, uint8OK)
		}
	}
	for _, index := range []int64{-1, size} {
		if _, ok := array.ArrayElement(index); ok || array.SetArrayElement(index, NewUint8(1)) {
			t.Fatalf("index %d unexpectedly succeeded", index)
		}
	}
}

// 测试连续创建 uint8 数组保持独立存储
func TestUint8ArrayRepeatedAllocation(t *testing.T) {
	elements := make([]Value, 256)
	for index := range elements {
		elements[index] = NewUint8(uint8(index))
	}
	for iteration := 0; iteration < 10000; iteration++ {
		array := NewArray(elements)
		if !array.SetArrayElement(0, NewUint8(uint8(iteration))) {
			t.Fatalf("iteration %d write failed", iteration)
		}
		if array.array.byteElements[1] != 1 {
			t.Fatalf("iteration %d storage=%v", iteration, array.array.byteElements[:2])
		}
	}
}
