package value

import (
	"fmt"
	"testing"
	"unsafe"
)

var benchmarkArray Value
var benchmarkValue Value
var benchmarkBool bool
var benchmarkIndex int64

func benchmarkUint8Elements(size int) []Value {
	elements := make([]Value, size)
	for index := range elements {
		elements[index] = NewUint8(uint8(index))
	}
	return elements
}

func newLegacyUint8Array(elements []Value) Value {
	return Value{kind: KindArray, array: &Array{elements: append([]Value(nil), elements...)}}
}

// 测量紧凑与旧式 uint8 数组的创建成本
func BenchmarkUint8ArrayCreation(b *testing.B) {
	for _, size := range []int{1024, 65536, 1048576} {
		elements := benchmarkUint8Elements(size)
		b.Run(fmt.Sprintf("compact/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(unsafe.Sizeof(Array{}))+float64(size), "backing-B/op")
			for iteration := 0; iteration < b.N; iteration++ {
				benchmarkArray = NewArray(elements)
			}
		})
		b.Run(fmt.Sprintf("legacy/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.ReportMetric(float64(unsafe.Sizeof(Array{}))+float64(size)*float64(unsafe.Sizeof(Value{})), "backing-B/op")
			for iteration := 0; iteration < b.N; iteration++ {
				benchmarkArray = newLegacyUint8Array(elements)
			}
		})
	}
}

// 测量 uint8 数组顺序读取成本
func BenchmarkUint8ArraySequentialRead(b *testing.B) {
	benchmarkUint8ArrayAccess(b, func(array Value, index int) {
		benchmarkValue, _ = array.ArrayElement(int64(index))
	})
}

// 测量 uint8 数组顺序写入成本
func BenchmarkUint8ArraySequentialWrite(b *testing.B) {
	benchmarkUint8ArrayAccess(b, func(array Value, index int) {
		array.SetArrayElement(int64(index), NewUint8(uint8(index)))
	})
}

// 测量 uint8 数组随机索引成本
func BenchmarkUint8ArrayRandomRead(b *testing.B) {
	for _, size := range []int{1024, 65536, 1048576} {
		elements := benchmarkUint8Elements(size)
		for _, layout := range []struct {
			name  string
			array Value
		}{
			{name: "compact", array: NewArray(elements)},
			{name: "legacy", array: newLegacyUint8Array(elements)},
		} {
			b.Run(fmt.Sprintf("%s/%d", layout.name, size), func(b *testing.B) {
				b.ReportAllocs()
				mask := uint64(size - 1)
				state := uint64(1)
				b.ResetTimer()
				for iteration := 0; iteration < b.N; iteration++ {
					state = state*6364136223846793005 + 1
					benchmarkValue, _ = layout.array.ArrayElement(int64((state >> 32) & mask))
				}
			})
		}
	}
}

func benchmarkUint8ArrayAccess(b *testing.B, operation func(Value, int)) {
	for _, size := range []int{1024, 65536, 1048576} {
		elements := benchmarkUint8Elements(size)
		for _, layout := range []struct {
			name  string
			array Value
		}{
			{name: "compact", array: NewArray(elements)},
			{name: "legacy", array: newLegacyUint8Array(elements)},
		} {
			b.Run(fmt.Sprintf("%s/%d", layout.name, size), func(b *testing.B) {
				b.ReportAllocs()
				b.ResetTimer()
				for iteration := 0; iteration < b.N; iteration++ {
					operation(layout.array, iteration&(size-1))
				}
			})
		}
	}
}

// 测量 bytes 算法顺序比较的直接数组成本
func BenchmarkUint8ArrayDirectCompare(b *testing.B) {
	for _, size := range []int{1024, 65536, 1048576} {
		left := benchmarkCompactUint8Array(size)
		right := benchmarkCompactUint8Array(size)
		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				benchmarkBool = equalUint8Arrays(left, right)
			}
		})
	}
}

// 测量 bytes 算法失败查找的直接数组成本
func BenchmarkUint8ArrayDirectIndexMiss(b *testing.B) {
	for _, size := range []int{1024, 65536, 1048576} {
		array := benchmarkCompactUint8Array(size)
		b.Run(fmt.Sprintf("%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				benchmarkIndex = indexUint8Array(array, 255)
			}
		})
	}
}

func benchmarkCompactUint8Array(size int) Value {
	elements := make([]byte, size)
	for index := range elements {
		elements[index] = uint8(index % 255)
	}
	return Value{kind: KindArray, array: &Array{byteElements: elements}}
}

func equalUint8Arrays(left, right Value) bool {
	leftLength, _ := left.ArrayLen()
	rightLength, _ := right.ArrayLen()
	if leftLength != rightLength {
		return false
	}
	for index := 0; index < leftLength; index++ {
		leftElement, _ := left.ArrayElement(int64(index))
		rightElement, _ := right.ArrayElement(int64(index))
		leftByte, _ := leftElement.Uint8()
		rightByte, _ := rightElement.Uint8()
		if leftByte != rightByte {
			return false
		}
	}
	return true
}

func indexUint8Array(array Value, target uint8) int64 {
	length, _ := array.ArrayLen()
	for index := 0; index < length; index++ {
		element, _ := array.ArrayElement(int64(index))
		current, _ := element.Uint8()
		if current == target {
			return int64(index)
		}
	}
	return -1
}
