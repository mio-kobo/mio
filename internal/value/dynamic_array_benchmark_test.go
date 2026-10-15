package value

import (
	"fmt"
	"testing"
)

var benchmarkDynamicArray Value

// BenchmarkDynamicByteArrayMake 测量字节数组创建
func BenchmarkDynamicByteArrayMake(b *testing.B) {
	for _, size := range []int64{1 << 10, 64 << 10, 1 << 20} {
		b.Run(fmt.Sprintf("bytes-%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(size)
			for iteration := 0; iteration < b.N; iteration++ {
				array, err := NewFilledArray(size, NewUint8(0), KindUint8, false)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkDynamicArray = array
			}
		})
	}
}

// BenchmarkDynamicByteArrayAppend 比较预留与自动扩容追加
func BenchmarkDynamicByteArrayAppend(b *testing.B) {
	for _, size := range []int{1024, 65536, 1048576} {
		for _, reserved := range []bool{false, true} {
			name := "grow"
			if reserved {
				name = "reserved"
			}
			b.Run(fmt.Sprintf("%s-%d", name, size), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(size))
				var growths, copied int64
				for iteration := 0; iteration < b.N; iteration++ {
					capacity := int64(0)
					if reserved {
						capacity = int64(size)
					}
					array, err := NewArrayWithCapacity(capacity, KindUint8, false)
					if err != nil {
						b.Fatal(err)
					}
					previousCapacity, _ := array.ArrayCapacity()
					for index := 0; index < size; index++ {
						if err := array.AppendArrayElement(NewUint8(uint8(index))); err != nil {
							b.Fatal(err)
						}
						currentCapacity, _ := array.ArrayCapacity()
						if currentCapacity != previousCapacity {
							growths++
							copied += int64(index)
							previousCapacity = currentCapacity
						}
					}
					benchmarkDynamicArray = array
				}
				b.ReportMetric(float64(growths)/float64(b.N), "growths/op")
				b.ReportMetric(float64(copied)/float64(b.N), "copied-B/op")
			})
		}
	}
}

// BenchmarkDynamicByteArrayClone 测量独立复制
func BenchmarkDynamicByteArrayClone(b *testing.B) {
	for _, size := range []int64{1 << 10, 64 << 10, 1 << 20} {
		array, err := NewFilledArray(size, NewUint8(1), KindUint8, false)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("bytes-%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(size)
			for iteration := 0; iteration < b.N; iteration++ {
				clone, err := array.CloneArray()
				if err != nil {
					b.Fatal(err)
				}
				benchmarkDynamicArray = clone
			}
		})
	}
}

// BenchmarkDynamicByteArrayAppendAll 测量批量字节追加
func BenchmarkDynamicByteArrayAppendAll(b *testing.B) {
	for _, size := range []int64{1 << 10, 64 << 10, 1 << 20} {
		source, err := NewFilledArray(size, NewUint8(1), KindUint8, false)
		if err != nil {
			b.Fatal(err)
		}
		for _, reserved := range []bool{false, true} {
			name := "grow"
			if reserved {
				name = "reserved"
			}
			b.Run(fmt.Sprintf("%s-%d", name, size), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(size)
				for iteration := 0; iteration < b.N; iteration++ {
					capacity := int64(0)
					if reserved {
						capacity = size
					}
					destination, createErr := NewArrayWithCapacity(capacity, KindUint8, false)
					if createErr != nil {
						b.Fatal(createErr)
					}
					if appendErr := destination.AppendArray(source); appendErr != nil {
						b.Fatal(appendErr)
					}
					benchmarkDynamicArray = destination
				}
			})
		}
	}
}

// BenchmarkDynamicByteArrayResetReuse 测量清空后的容量复用
func BenchmarkDynamicByteArrayResetReuse(b *testing.B) {
	for _, size := range []int64{1 << 10, 64 << 10, 1 << 20} {
		source, err := NewFilledArray(size, NewUint8(1), KindUint8, false)
		if err != nil {
			b.Fatal(err)
		}
		destination, err := NewArrayWithCapacity(size, KindUint8, false)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("bytes-%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(size)
			for iteration := 0; iteration < b.N; iteration++ {
				if err := destination.TruncateArray(0); err != nil {
					b.Fatal(err)
				}
				if err := destination.AppendArray(source); err != nil {
					b.Fatal(err)
				}
			}
			benchmarkDynamicArray = destination
		})
	}
}

// BenchmarkDynamicByteArrayExport 测量缓冲区独立导出
func BenchmarkDynamicByteArrayExport(b *testing.B) {
	for _, size := range []int64{1 << 10, 64 << 10, 1 << 20} {
		array, err := NewFilledArray(size, NewUint8(1), KindUint8, false)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("bytes-%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(size)
			for iteration := 0; iteration < b.N; iteration++ {
				exported, cloneErr := array.CloneArray()
				if cloneErr != nil {
					b.Fatal(cloneErr)
				}
				benchmarkDynamicArray = exported
			}
		})
	}
}
