package darwinarm64

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"strings"
	"testing"
)

func TestM9RootMapEncodingAndTypeLayouts(t *testing.T) {
	layouts := []M9TypeLayout{
		{TypeID: 11, PayloadBytes: 16, RefOffsets: []uint32{0}},
		{TypeID: 14, PayloadBytes: 32, RefOffsets: []uint32{0, 8, 16, 24}},
	}
	root := M9RootMap{FrameBytes: 64, FrameRefs: []uint32{0, 16}, GlobalSlots: 2, GlobalRefs: []uint32{0}}
	data, err := M9EncodeMetadata(layouts, root)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(layouts)*M9TypeEntryBytes+M9RootMapBytes {
		t.Fatalf("metadata size=%d", len(data))
	}
	word := func(offset int) uint32 { return binary.LittleEndian.Uint32(data[offset : offset+4]) }
	for at, want := range map[int]uint32{
		0: 11, 4: 16, 8: 1, 16: 0,
		32: 14, 36: 32, 40: 4, 48: 0, 52: 8, 56: 16, 60: 24,
		64: 64, 68: 2, 72: 2, 76: 1, 80: 0, 84: 16, 112: 0,
	} {
		if got := word(at); got != want {
			t.Fatalf("metadata offset %d got=%d want=%d", at, got, want)
		}
	}
}

func TestM9RejectsInvalidTypeDescriptors(t *testing.T) {
	roots := M9RootMap{FrameBytes: 32, FrameRefs: []uint32{0}, GlobalSlots: 2, GlobalRefs: []uint32{0}}
	cases := []struct {
		name    string
		types   []M9TypeLayout
		errText string
	}{
		{"empty", nil, "type count"},
		{"type zero", []M9TypeLayout{{TypeID: 0, PayloadBytes: 16}}, "type ID"},
		{"type too large", []M9TypeLayout{{TypeID: 65536, PayloadBytes: 16}}, "type ID"},
		{"duplicated type", []M9TypeLayout{{TypeID: 1, PayloadBytes: 16}, {TypeID: 1, PayloadBytes: 16}}, "duplicate"},
		{"oversized payload", []M9TypeLayout{{TypeID: 1, PayloadBytes: M8MaxHeapCapacity + 1}}, "payload"},
		{"unaligned field", []M9TypeLayout{{TypeID: 1, PayloadBytes: 16, RefOffsets: []uint32{1}}}, "reference offset"},
		{"out of range field", []M9TypeLayout{{TypeID: 1, PayloadBytes: 16, RefOffsets: []uint32{16}}}, "reference offset"},
		{"duplicated field", []M9TypeLayout{{TypeID: 1, PayloadBytes: 16, RefOffsets: []uint32{0, 0}}}, "duplicate reference"},
		{"too many fields", []M9TypeLayout{{TypeID: 1, PayloadBytes: 64, RefOffsets: []uint32{0, 8, 16, 24, 32}}}, "reference count"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := M9EncodeMetadata(tc.types, roots)
			if err == nil || !strings.Contains(err.Error(), tc.errText) {
				t.Fatalf("expected %q error, got %v", tc.errText, err)
			}
		})
	}
	tooMany := make([]M9TypeLayout, M9MaxTypes+1)
	for i := range tooMany {
		tooMany[i].TypeID = uint32(i + 1)
	}
	if _, err := M9EncodeMetadata(tooMany, roots); err == nil {
		t.Fatal("M9 must reject excessive type metadata")
	}
}

func TestM9RejectsInvalidRootMaps(t *testing.T) {
	type0 := []M9TypeLayout{{TypeID: 1, PayloadBytes: 16}}
	cases := []struct {
		name    string
		root    M9RootMap
		errText string
	}{
		{"unaligned frame", M9RootMap{FrameBytes: 24}, "bounds"},
		{"too large frame", M9RootMap{FrameBytes: 65552}, "bounds"},
		{"frame ref out of range", M9RootMap{FrameBytes: 16, FrameRefs: []uint32{16}}, "frame root offset"},
		{"frame ref unaligned", M9RootMap{FrameBytes: 32, FrameRefs: []uint32{1}}, "frame root offset"},
		{"duplicate frame ref", M9RootMap{FrameBytes: 32, FrameRefs: []uint32{0, 0}}, "duplicate frame"},
		{"too many frame refs", M9RootMap{FrameBytes: 128, FrameRefs: []uint32{0, 8, 16, 24, 32, 40, 48, 56, 64}}, "bounds"},
		{"global ref out of range", M9RootMap{FrameBytes: 16, GlobalSlots: 2, GlobalRefs: []uint32{2}}, "global root slot"},
		{"duplicate global ref", M9RootMap{FrameBytes: 16, GlobalSlots: 2, GlobalRefs: []uint32{0, 0}}, "duplicate global"},
		{"too many globals", M9RootMap{FrameBytes: 16, GlobalSlots: uint32(MaxGlobalSlots + 1)}, "bounds"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := M9EncodeMetadata(type0, tc.root)
			if err == nil || !strings.Contains(err.Error(), tc.errText) {
				t.Fatalf("expected %q error, got %v", tc.errText, err)
			}
		})
	}
}

func TestM9NativeProbeMachOAndMetadata(t *testing.T) {
	for _, mode := range []string{"roots", "cycles", "reset", "interior", "badtype", "fanout"} {
		t.Run(mode, func(t *testing.T) {
			image, err := M9GCProbe(mode)
			if err != nil {
				t.Fatal(err)
			}
			if len(image) != int(pageSize*2) {
				t.Fatalf("expected 32 KiB text+data image, got %d", len(image))
			}
			file, err := macho.NewFile(bytes.NewReader(image))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if file.Cpu != macho.CpuArm64 || file.Type != macho.TypeExec {
				t.Fatalf("invalid Mach-O target: %v / %v", file.Cpu, file.Type)
			}
			section := file.Section("__const")
			if section == nil || section.Seg != "__TEXT" {
				t.Fatalf("missing read-only type/roots metadata section: %+v", section)
			}
			raw, err := section.Data()
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) != 4*M9TypeEntryBytes+M9RootMapBytes {
				t.Fatalf("invalid metadata length %d", len(raw))
			}
			if binary.LittleEndian.Uint32(raw[:4]) != 11 || binary.LittleEndian.Uint32(raw[3*M9TypeEntryBytes:]) != 14 {
				t.Fatalf("invalid type descriptor data")
			}
			roots := raw[4*M9TypeEntryBytes:]
			if binary.LittleEndian.Uint32(roots[4:8]) != 2 || binary.LittleEndian.Uint32(roots[12:16]) != 1 {
				t.Fatal("missing precise frame and global map counts")
			}
			data := file.Section("__data")
			if data == nil || data.Size != 2*8 {
				t.Fatal("missing two native global slots")
			}
			instructions := file.Section("__text")
			if instructions == nil || instructions.Size < 400 {
				t.Fatal("missing full ARM64 marker and M8 allocator runtime")
			}
			payload, err := instructions.Data()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(payload, little32(m9LdrW(10, 17, 16))) {
				t.Fatal("marker does not load reference field offsets")
			}
			if !bytes.Contains(payload, little32(movzX(16, m8DarwinSYSMmap))) || !bytes.Contains(payload, little32(movzX(16, m8DarwinSYSMunmap))) {
				t.Fatal("probe must contain M8 mmap/munmap runtime")
			}
		})
	}
}

func little32(word uint32) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], word)
	return b[:]
}

func TestM9MarkRuntimeIsRelocatableAndNoSyscalls(t *testing.T) {
	words, err := M9MarkRuntimeWords()
	if err != nil {
		t.Fatal(err)
	}
	if len(words) < 130 {
		t.Fatalf("marker unexpectedly short: %d words", len(words))
	}
	for _, word := range words {
		if word == svc(0x80) {
			t.Fatal("marking must not make syscalls")
		}
	}
	if words[0] != subImmX(31, 31, 112) {
		t.Fatal("marker must preserve callee-saved registers in native frame")
	}
	if words[len(words)-1] != m8Ret() {
		t.Fatal("marker helper should finish with RET")
	}
}

func TestM9InvalidProbeRejected(t *testing.T) {
	_, err := M9GCProbe("invalid")
	if err == nil || !strings.Contains(err.Error(), "unknown M9 probe") {
		t.Fatalf("expected unknown M9 probe error; got %v", err)
	}
}
