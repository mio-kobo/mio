package value

import (
	"fmt"
	"strings"
	"testing"
)

var benchmarkText string

// BenchmarkStringByteCopies 测量底层字符串与紧凑字节数组复制成本
func BenchmarkStringByteCopies(b *testing.B) {
	for _, size := range []int{1 << 10, 64 << 10, 1 << 20} {
		text := strings.Repeat("a", size)
		b.Run(fmt.Sprintf("to-bytes/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for iteration := 0; iteration < b.N; iteration++ {
				array, err := NewByteArrayFromString(text)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkArray = array
			}
		})
		array, err := NewByteArrayFromString(text)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("from-utf8/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for iteration := 0; iteration < b.N; iteration++ {
				converted, valid, err := array.UTF8StringCopy()
				if err != nil || !valid {
					b.Fatalf("valid=%v err=%v", valid, err)
				}
				benchmarkText = converted
			}
		})
		stringValue := NewString(text)
		b.Run(fmt.Sprintf("byte-len/%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(size))
			for iteration := 0; iteration < b.N; iteration++ {
				current, _ := stringValue.Text()
				benchmarkIndex = int64(len(current))
			}
		})
	}
}

// BenchmarkStringByteCopiesMixed 测量混合 UTF-8 底层转换成本
func BenchmarkStringByteCopiesMixed(b *testing.B) {
	text := strings.Repeat("中文日本語🍡", 8192)
	array, err := NewByteArrayFromString(text)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("to-bytes", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(text)))
		for iteration := 0; iteration < b.N; iteration++ {
			result, err := NewByteArrayFromString(text)
			if err != nil {
				b.Fatal(err)
			}
			benchmarkArray = result
		}
	})
	b.Run("from-utf8", func(b *testing.B) {
		b.ReportAllocs()
		b.SetBytes(int64(len(text)))
		for iteration := 0; iteration < b.N; iteration++ {
			result, valid, err := array.UTF8StringCopy()
			if err != nil || !valid {
				b.Fatalf("valid=%v err=%v", valid, err)
			}
			benchmarkText = result
		}
	})
}
