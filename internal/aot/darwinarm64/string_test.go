package darwinarm64

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"strings"
	"testing"
)

func TestM7StringImageContainsByteLengths(t *testing.T) {
	pool := []string{"Hello", "", "你好", "a\x00b", "こんにちは"}
	image, err := stringImage(pool)
	if err != nil {
		t.Fatal(err)
	}
	for i, want := range pool {
		base := int(binary.LittleEndian.Uint32(image[i*8:]))
		length := int(binary.LittleEndian.Uint32(image[i*8+4:]))
		if length != len(want) || string(image[base:base+length]) != want {
			t.Fatalf("string %d decoded=%q length=%d want=%q byteLength=%d", i, image[base:base+length], length, want, len(want))
		}
	}
	if !bytes.Contains(image, []byte{'a', 0, 'b'}) {
		t.Fatal("embedded NUL lost")
	}
}

func TestM7ConstantSectionWithWritableGlobals(t *testing.T) {
	e, err := NewStackEmitter(2, 0, 0, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.PushInt64(1); err != nil {
		t.Fatal(err)
	}
	if err := e.SetGlobal(0); err != nil {
		t.Fatal(err)
	}
	if err := e.PushInt64(0); err != nil {
		t.Fatal(err)
	}
	if err := e.ChantString(); err != nil {
		t.Fatal(err)
	}
	image, err := e.FinishWithFunctionsAndStrings(nil, []string{"猫\x00Mio!"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := macho.NewFile(bytes.NewReader(image))
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	text := m.Section("__text")
	ro := m.Section("__const")
	if text == nil || ro == nil || m.Segment("__DATA") == nil {
		t.Fatalf("M7 Mach-O lacks expected sections")
	}
	if ro.Seg != "__TEXT" || ro.Offset%8 != 0 || ro.Addr != textVMAddr+uint64(ro.Offset) || ro.Size != 8+uint64(len("猫\x00Mio!")) {
		t.Fatalf("invalid readonly constant section: %+v", ro)
	}
	if m.Segment("__TEXT").Prot&vmProtWrite != 0 || m.Segment("__DATA").Prot&vmProtWrite == 0 {
		t.Fatal("incorrect readonly / writable protections")
	}
	if !bytes.Contains(image[ro.Offset:ro.Offset+uint32(ro.Size)], []byte("猫\x00Mio!")) {
		t.Fatal("UTF-8 payload not present")
	}
	if len(image) != 32768 {
		t.Fatalf("image size=%d", len(image))
	}
}

func TestM7StringCallLinkedAndAddressResolved(t *testing.T) {
	e, err := NewStackEmitter(0, 0, 0, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.PushInt64(0); err != nil {
		t.Fatal(err)
	}
	if err := e.ChantString(); err != nil {
		t.Fatal(err)
	}
	at := e.stringCalls[0]
	img, err := e.FinishWithFunctionsAndStrings(nil, []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := macho.NewFile(bytes.NewReader(img))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	text, ro := f.Section("__text"), f.Section("__const")
	if text == nil || ro == nil {
		t.Fatal("missing sections")
	}
	call := binary.LittleEndian.Uint32(img[entryOffset+uint64(at)*4:])
	if call&0xfc000000 != 0x94000000 {
		t.Fatalf("expected BL, got %#x", call)
	}
	shift := int32(call & 0x03ffffff)
	if shift&(1<<25) != 0 {
		shift |= ^int32(0x03ffffff)
	}
	start := at + int(shift)
	first := binary.LittleEndian.Uint32(img[entryOffset+uint64(start)*4:])
	second := binary.LittleEndian.Uint32(img[entryOffset+uint64(start)*4+4:])
	if first&0x9f00001f != 0x90000009 || second != addImmX(9, 9, uint32(ro.Addr&0xfff)) {
		t.Fatalf("incorrect PC-relative __const address: %#x %#x", first, second)
	}
	if text.Size < uint64((start+19)*4) {
		t.Fatal("string print helper truncated")
	}
}

func TestM7StringSectionOverflowRejected(t *testing.T) {
	e, err := NewStackEmitter(0, 0, 0, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.PushInt64(0); err != nil {
		t.Fatal(err)
	}
	if err := e.ChantString(); err != nil {
		t.Fatal(err)
	}
	_, err = e.FinishWithFunctionsAndStrings(nil, []string{strings.Repeat("a", 16000)})
	if err == nil || !strings.Contains(err.Error(), "__const") {
		t.Fatalf("expected __const overflow, got %v", err)
	}
}

func TestM7InvalidUTF8Rejected(t *testing.T) {
	_, err := stringImage([]string{"\xff"})
	if err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("expected UTF-8 validation failure, got %v", err)
	}
}
