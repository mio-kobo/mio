package value

import "testing"

// 测试动态数组创建、容量预留和连续追加
func TestDynamicArrayCapacityAndAppend(t *testing.T) {
	array, err := NewArrayWithCapacity(4, KindUint8, false)
	if err != nil {
		t.Fatal(err)
	}
	if length, _ := array.ArrayLen(); length != 0 {
		t.Fatalf("len=%d", length)
	}
	if capacity, _ := array.ArrayCapacity(); capacity != 4 {
		t.Fatalf("cap=%d", capacity)
	}
	if array.array.byteElements == nil || array.array.elements != nil {
		t.Fatalf("storage=%+v", array.array)
	}
	alias := array
	for index := 0; index < 4096; index++ {
		if err := array.AppendArrayElement(NewUint8(uint8(index))); err != nil {
			t.Fatal(err)
		}
	}
	if length, _ := alias.ArrayLen(); length != 4096 {
		t.Fatalf("alias len=%d", length)
	}
	if capacity, _ := array.ArrayCapacity(); capacity < 4096 {
		t.Fatalf("cap=%d", capacity)
	}
}

// 测试指定长度初始化和非法长度
func TestFilledArray(t *testing.T) {
	array, err := NewFilledArray(3, NewString("Mio"), KindString, false)
	if err != nil {
		t.Fatal(err)
	}
	for index := int64(0); index < 3; index++ {
		item, ok := array.ArrayElement(index)
		text, textOK := item.Text()
		if !ok || !textOK || text != "Mio" {
			t.Fatalf("index %d=%v", index, item)
		}
	}
	for _, length := range []int64{-1, maxArrayAllocationBytes + 1} {
		if _, err := NewFilledArray(length, NewUint8(0), KindUint8, false); err == nil {
			t.Fatalf("length %d succeeded", length)
		}
	}
}

// 测试 reserve 不改变长度和已有元素
func TestArrayReserve(t *testing.T) {
	array := NewTypedArray([]Value{NewInt(1), NewInt(2)}, KindInt, false)
	if err := array.ReserveArray(100); err != nil {
		t.Fatal(err)
	}
	if length, _ := array.ArrayLen(); length != 2 {
		t.Fatalf("len=%d", length)
	}
	if capacity, _ := array.ArrayCapacity(); capacity < 102 {
		t.Fatalf("cap=%d", capacity)
	}
	item, _ := array.ArrayElement(1)
	if number, _ := item.Int64(); number != 2 {
		t.Fatalf("item=%v", item)
	}
	if err := array.ReserveArray(-1); err == nil {
		t.Fatal("负数 reserve 应失败")
	}
}

// 测试 clone 独立复制外层并浅复制嵌套引用
func TestArrayCloneShallowCopy(t *testing.T) {
	inner := NewTypedArray([]Value{NewInt(1)}, KindInt, false)
	outer := NewTypedArray([]Value{inner}, KindArray, false)
	clone, err := outer.CloneArray()
	if err != nil {
		t.Fatal(err)
	}
	replacement := NewTypedArray([]Value{NewInt(9)}, KindInt, false)
	if !clone.SetArrayElement(0, replacement) {
		t.Fatal("替换 clone 元素失败")
	}
	originalInner, _ := outer.ArrayElement(0)
	item, _ := originalInner.ArrayElement(0)
	if number, _ := item.Int64(); number != 1 {
		t.Fatalf("original=%d", number)
	}
	sharedClone, _ := outer.CloneArray()
	sharedInner, _ := sharedClone.ArrayElement(0)
	if !sharedInner.SetArrayElement(0, NewInt(7)) {
		t.Fatal("修改嵌套数组失败")
	}
	item, _ = inner.ArrayElement(0)
	if number, _ := item.Int64(); number != 7 {
		t.Fatalf("nested=%d", number)
	}
}

// 测试数组追加拒绝错误类型和非可空 mu
func TestArrayAppendTypeSafety(t *testing.T) {
	array := NewTypedArray(nil, KindInt, false)
	if err := array.AppendArrayElement(NewString("Mio")); err == nil {
		t.Fatal("错误元素类型应失败")
	}
	if err := array.AppendArrayElement(NewNil()); err == nil {
		t.Fatal("非可空数组写入 nil 应失败")
	}
	nullable := NewTypedArray(nil, KindInt, true)
	if err := nullable.AppendArrayElement(NewNil()); err != nil {
		t.Fatal(err)
	}
}

// 测试截断共享长度、保留容量并清除通用值引用
func TestArrayTruncate(t *testing.T) {
	array := NewTypedArray([]Value{NewString("a"), NewString("b"), NewString("c")}, KindString, false)
	alias := array
	capacity, _ := array.ArrayCapacity()
	storage := array.array.elements[:capacity]
	if err := array.TruncateArray(1); err != nil {
		t.Fatal(err)
	}
	if length, _ := alias.ArrayLen(); length != 1 {
		t.Fatalf("length=%d", length)
	}
	if current, _ := alias.ArrayCapacity(); current != capacity {
		t.Fatalf("capacity=%d, want %d", current, capacity)
	}
	if storage[1].kind != KindInvalid || storage[2].kind != KindInvalid {
		t.Fatal("truncated references were not cleared")
	}
	if _, ok := alias.ArrayElement(1); ok {
		t.Fatal("truncated index remains accessible")
	}
	if err := alias.AppendArrayElement(NewString("d")); err != nil {
		t.Fatal(err)
	}
	item, _ := array.ArrayElement(1)
	if text, _ := item.Text(); text != "d" {
		t.Fatalf("item=%q", text)
	}
	for _, length := range []int64{-1, 3} {
		if err := array.TruncateArray(length); err == nil {
			t.Fatalf("length %d unexpectedly succeeded", length)
		}
	}
}

// 测试紧凑字节数组截断与自追加
func TestByteArrayTruncateAndAppendAll(t *testing.T) {
	array := NewTypedArray([]Value{NewUint8(0), NewUint8(255)}, KindUint8, false)
	if err := array.AppendArray(array); err != nil {
		t.Fatal(err)
	}
	if length, _ := array.ArrayLen(); length != 4 {
		t.Fatalf("length=%d", length)
	}
	for index, want := range []uint8{0, 255, 0, 255} {
		item, _ := array.ArrayElement(int64(index))
		got, _ := item.Uint8()
		if got != want {
			t.Fatalf("item[%d]=%d, want %d", index, got, want)
		}
	}
	capacity, _ := array.ArrayCapacity()
	if err := array.TruncateArray(0); err != nil {
		t.Fatal(err)
	}
	if array.array.byteElements == nil {
		t.Fatal("compact byte storage was lost")
	}
	if current, _ := array.ArrayCapacity(); current != capacity {
		t.Fatalf("capacity=%d, want %d", current, capacity)
	}
}

// 测试通用数组批量追加采用浅复制
func TestArrayAppendAllShallowCopy(t *testing.T) {
	inner := NewTypedArray([]Value{NewInt(1)}, KindInt, false)
	destination := NewTypedArray(nil, KindArray, false)
	source := NewTypedArray([]Value{inner}, KindArray, false)
	if err := destination.AppendArray(source); err != nil {
		t.Fatal(err)
	}
	appended, _ := destination.ArrayElement(0)
	if err := appended.AppendArrayElement(NewInt(2)); err != nil {
		t.Fatal(err)
	}
	if length, _ := inner.ArrayLen(); length != 2 {
		t.Fatalf("inner length=%d", length)
	}
}
