package value

import "testing"

// 测试数组拥有独立切片并保留共享值语义
func TestArrayValueStorage(t *testing.T) {
	elements := []Value{NewInt(10), NewString("Mio")}
	array := NewArray(elements)
	elements[0] = NewInt(99)

	if length, ok := array.ArrayLen(); !ok || length != 2 {
		t.Fatalf("length=%d ok=%v", length, ok)
	}
	first, ok := array.ArrayElement(0)
	if !ok || first != NewInt(10) {
		t.Fatalf("first=%v ok=%v", first, ok)
	}
	copyValue := array
	second, ok := copyValue.ArrayElement(1)
	if !ok || second != NewString("Mio") {
		t.Fatalf("second=%v ok=%v", second, ok)
	}
	for _, index := range []int64{-1, 2} {
		if _, ok := array.ArrayElement(index); ok {
			t.Fatalf("index %d unexpectedly succeeded", index)
		}
	}
}

// 测试数组值共享同一后备存储
func TestArrayValueSharedMutation(t *testing.T) {
	array := NewArray([]Value{NewInt(1), NewInt(2)})
	alias := array
	if !alias.SetArrayElement(1, NewInt(42)) {
		t.Fatal("修改数组元素失败")
	}
	element, ok := array.ArrayElement(1)
	if !ok {
		t.Fatal("读取数组元素失败")
	}
	integer, ok := element.Int64()
	if !ok || integer != 42 {
		t.Fatalf("element=%v", element)
	}
	if alias.SetArrayElement(0, NewString("Mio")) {
		t.Fatal("不应接受不同类型的数组元素")
	}
}
