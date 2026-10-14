package value

import (
	"testing"
	"unsafe"
)

// 测试固定宽度整数不增加 Value 大小
func TestFixedIntegerValueStorage(t *testing.T) {
	size := unsafe.Sizeof(Value{})
	if unsafe.Sizeof(NewUint8(1)) != size || unsafe.Sizeof(NewInt32(1)) != size {
		t.Fatal("fixed integer Value size differs")
	}
	t.Logf("Value=%d bytes, Array header=%d bytes, []Value element=%d bytes", size, unsafe.Sizeof(Array{}), size)
}

// 测量固定宽度整数 Value 构造成本
func BenchmarkFixedIntegerValue(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = NewUint8(uint8(i))
	}
}

// 测量固定宽度整数数组的实际分配成本
func BenchmarkFixedIntegerArray1024(b *testing.B) {
	elements := make([]Value, 1024)
	for i := range elements {
		elements[i] = NewUint8(uint8(i))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NewArray(elements)
	}
}
