package darwinarm64

import (
	"encoding/binary"
	"testing"
)

func TestM11StackMapEncodingScales(t *testing.T) {
	frame := make([]int, 300)
	for i := range frame {
		frame[i] = i
	}
	data, err := M11EncodeStackMaps([]M11StackMap{{
		SafepointID: 7, NativeWord: 1234, FrameID: 9,
		FrameSlots: frame, GlobalSlots: []int{2, 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(data); got != m11StackMapMagic {
		t.Fatalf("magic=%#x", got)
	}
	if got := binary.LittleEndian.Uint32(data[4:]); got != 1 {
		t.Fatalf("map count=%d", got)
	}
}

func TestM11StackMapRejectsDuplicateSlots(t *testing.T) {
	_, err := M11EncodeStackMaps([]M11StackMap{{SafepointID: 1, FrameSlots: []int{3, 3}}})
	if err == nil {
		t.Fatal("expected duplicate root rejection")
	}
}
