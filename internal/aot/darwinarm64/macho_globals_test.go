package darwinarm64

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"strings"
	"testing"
)

func TestM2MachOGlobalDataLayout(t *testing.T) {
	e := NewIntegerEmitter()
	if err := e.SetGlobalCount(1025); err != nil {
		t.Fatal(err)
	}
	if err := e.PushInt64(42); err != nil {
		t.Fatal(err)
	}
	if err := e.SetGlobal(0); err != nil {
		t.Fatal(err)
	}
	if err := e.PushInt64(99); err != nil {
		t.Fatal(err)
	}
	if err := e.SetGlobal(512); err != nil {
		t.Fatal(err)
	}
	if err := e.GetGlobal(512); err != nil {
		t.Fatal(err)
	}
	if err := e.Chant(); err != nil {
		t.Fatal(err)
	}
	image, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if len(image) != 2*int(pageSize) {
		t.Fatalf("image size=%d", len(image))
	}
	f, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	data := f.Segment("__DATA")
	if data == nil || data.Addr != textVMAddr+pageSize || data.Offset != pageSize ||
		data.Filesz != pageSize || data.Memsz != pageSize ||
		data.Prot != vmProtRead|vmProtWrite || data.Maxprot != vmProtRead|vmProtWrite {
		t.Fatalf("incorrect __DATA mapping: %+v", data)
	}
	section := f.Section("__data")
	if section == nil || section.Addr != textVMAddr+pageSize ||
		section.Size != 1025*8 || section.Offset != uint32(pageSize) {
		t.Fatalf("incorrect __data section: %+v", section)
	}
	link := f.Segment("__LINKEDIT")
	if link == nil || link.Addr != textVMAddr+pageSize*2 ||
		link.Offset != pageSize*2 || link.Memsz != pageSize ||
		link.Prot != vmProtRead {
		t.Fatalf("incorrect __LINKEDIT mapping: %+v", link)
	}
	if binary.LittleEndian.Uint32(image[16:20]) != 9 {
		t.Fatalf("expected 9 load commands")
	}
	if binary.LittleEndian.Uint32(image[20:24]) != 632 {
		t.Fatalf("expected 632-byte load command set, got %d", binary.LittleEndian.Uint32(image[20:24]))
	}
	for _, b := range image[pageSize:] {
		if b != 0 {
			t.Fatal("__DATA must be zero-initialized")
		}
	}
}

func TestM2ARM64GlobalAccessEncodings(t *testing.T) {
	// ADRP x8, __DATA 的 4KiB 子页，然后用 8 字节缩放的 LDR/STR 访问。
	e := NewIntegerEmitter()
	if err := e.SetGlobalCount(1025); err != nil {
		t.Fatal(err)
	}
	if err := e.PushInt64(12); err != nil {
		t.Fatal(err)
	}
	if err := e.SetGlobal(0); err != nil {
		t.Fatal(err)
	}
	if err := e.GetGlobal(512); err != nil {
		t.Fatal(err)
	}
	if err := e.SetGlobal(1024); err != nil {
		t.Fatal(err)
	}
	if len(e.words) != 10 {
		t.Fatalf("machine words=%d, want 10", len(e.words))
	}
	check := []struct {
		index  int
		target uint64
		opcode uint32
	}{
		{4, textVMAddr + pageSize + 0*8, strX(9, 8, 0)},
		{6, textVMAddr + pageSize + 512*8, ldrX(9, 8, 0)},
		{8, textVMAddr + pageSize + 1024*8, strX(9, 8, 0)},
	}
	for _, c := range check {
		atPC := textVMAddr + entryOffset + uint64(c.index)*4
		ad, err := adrpX(8, atPC, c.target)
		if err != nil {
			t.Fatal(err)
		}
		if got := e.words[c.index]; got != ad {
			t.Fatalf("ADRP at %d: got %#08x want %#08x", c.index, got, ad)
		}
		if got := e.words[c.index+1]; got != c.opcode {
			t.Fatalf("LDR/STR at %d: got %#08x want %#08x", c.index+1, got, c.opcode)
		}
	}
}

func TestM2GlobalStackAndBounds(t *testing.T) {
	e := NewIntegerEmitter()
	if err := e.SetGlobalCount(1); err != nil {
		t.Fatal(err)
	}
	if err := e.SetGlobal(0); err == nil || !strings.Contains(err.Error(), "underflow") {
		t.Fatalf("expected SetGlobal underflow; got %v", err)
	}
	if err := e.GetGlobal(1); err == nil || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("expected global bounds error; got %v", err)
	}
	if err := e.SetGlobalCount(MaxGlobalSlots + 1); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected global limit error; got %v", err)
	}
	if _, err := ExecutableWordsWithGlobals([]uint32{movzX(0, 0)}, -1); err == nil {
		t.Fatal("negative global count must be rejected")
	}
}

func TestM2M0MachOUnchanged(t *testing.T) {
	image, err := ExitProgram(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(image) != int(pageSize) {
		t.Fatalf("M0 size changed: %d", len(image))
	}
	m, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if m.Segment("__DATA") != nil {
		t.Fatal("M0/M1 must not acquire __DATA when unused")
	}
	if m.Segment("__LINKEDIT").Offset != pageSize {
		t.Fatal("M0/M1 linkedit offset changed")
	}
}
