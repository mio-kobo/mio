package darwinarm64

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"math"
	"testing"
)

// Linker 必须把无重定位的格式化例程完整拼到 Mach-O 末尾，并回填真实 BL 目标。
func TestM6bFloatRuntimeLinkedWithoutRelocations(t *testing.T) {
	for _, kind := range []uint8{fp32Kind, fp64Kind} {
		e, err := NewStackEmitter(0, 0, 0, 1, false)
		if err != nil {
			t.Fatal(err)
		}
		if kind == fp32Kind {
			err = e.PushFloat32(1.25)
		} else {
			err = e.PushFloat64(math.Pi)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err = e.ChantFloat(kind); err != nil {
			t.Fatal(err)
		}
		if len(e.floatCalls) != 1 {
			t.Fatalf("kind=%d call count=%d", kind, len(e.floatCalls))
		}
		callWord := e.floatCalls[0].word
		image, err := e.FinishWithFunctions(nil)
		if err != nil {
			t.Fatal(err)
		}
		m, err := macho.NewFile(bytes.NewReader(image))
		if err != nil {
			t.Fatal(err)
		}
		section := m.Section("__text")
		if section == nil {
			t.Fatal("missing __text")
		}
		if section.Size < uint64(len(floatRuntimeWords)*4) {
			t.Fatal("float runtime truncated")
		}
		startWord := int(section.Size/4) - len(floatRuntimeWords)
		for i, want := range floatRuntimeWords {
			off := int(entryOffset) + (startWord+i)*4
			if got := binary.LittleEndian.Uint32(image[off : off+4]); got != want {
				t.Fatalf("kind=%d runtime word %d got=%#x want=%#x", kind, i, got, want)
			}
		}
		off := int(entryOffset) + callWord*4
		call := binary.LittleEndian.Uint32(image[off : off+4])
		if call&0xfc000000 != 0x94000000 {
			t.Fatalf("kind=%d no BL at %d: %#x", kind, callWord, call)
		}
		delta := int32(call & 0x03ffffff)
		if delta&(1<<25) != 0 {
			delta |= ^int32(0x03ffffff)
		}
		if to := callWord + int(delta); to != startWord {
			t.Fatalf("kind=%d BL target=%d runtime start=%d", kind, to, startWord)
		}
		m.Close()
	}
}
