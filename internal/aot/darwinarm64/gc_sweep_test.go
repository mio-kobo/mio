package darwinarm64

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"strings"
	"testing"
)

func TestM10GCProbeMachO(t *testing.T) {
	modes := []string{"smoke", "cycles", "split", "coalesce", "reset", "badtype", "badsize", "badlink", "oom", "interior", "holes", "zerobyte", "stale", "stress", "largeindex"}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			image, err := M10GCProbe(mode)
			if err != nil {
				t.Fatal(err)
			}
			if len(image) != int(2*pageSize) {
				t.Fatalf("expected 32 KiB unsigned Mach-O, got %d", len(image))
			}
			file, err := macho.NewFile(bytes.NewReader(image))
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if file.Cpu != macho.CpuArm64 || file.Type != macho.TypeExec {
				t.Fatalf("unexpected Mach-O CPU/type: %v/%v", file.Cpu, file.Type)
			}
			text := file.Section("__text")
			constants := file.Section("__const")
			globals := file.Section("__data")
			if text == nil || constants == nil || globals == nil {
				t.Fatalf("expected __text, __const and __data, got text=%v const=%v data=%v", text, constants, globals)
			}
			if text.Seg != "__TEXT" || constants.Seg != "__TEXT" || globals.Seg != "__DATA" {
				t.Fatal("missing expected segment permissions / layout")
			}
			if globals.Size != 16 {
				t.Fatalf("unexpected root globals section size %d", globals.Size)
			}
			if constants.Size != 5*M9TypeEntryBytes+M9RootMapBytes {
				t.Fatalf("invalid metadata length: %d", constants.Size)
			}
			raw, err := constants.Data()
			if err != nil {
				t.Fatal(err)
			}
			if got := binary.LittleEndian.Uint32(raw[0:4]); got != 11 {
				t.Fatalf("first type ID=%d, want 11", got)
			}
			if got := binary.LittleEndian.Uint32(raw[4*M9TypeEntryBytes:]); got != 15 {
				t.Fatalf("last type ID=%d, want 15", got)
			}
			for _, section := range []*macho.Section{text, constants, globals} {
				if len(section.Relocs) != 0 {
					t.Fatalf("%s has unexpected relocation records", section.Name)
				}
			}
			instructions, err := text.Data()
			if err != nil {
				t.Fatal(err)
			}
			for _, required := range []uint32{
				m10Lsl(5, 5, 4), m10Lsr(18, 18, 4),
				m9LdrW(9, 25, 0), strX(31, 9, 0),
				movzX(16, m8DarwinSYSMmap), movzX(16, m8DarwinSYSMunmap),
			} {
				if !bytes.Contains(instructions, little32(required)) {
					t.Fatalf("M10 missing native instruction %#08x", required)
				}
			}
		})
	}
}

func TestM10RuntimeRelocatableAndABI(t *testing.T) {
	words, entries, err := M10RuntimeWords()
	if err != nil {
		t.Fatal(err)
	}
	if len(words) < 400 {
		t.Fatalf("M10 runtime suspiciously short: %d words", len(words))
	}
	positions := map[string]int{
		"init": entries.Init, "alloc": entries.Alloc, "destroy": entries.Destroy,
		"mark": entries.Mark, "validate": entries.Validate,
		"sweep": entries.Sweep, "collect": entries.Collect,
	}
	seen := map[int]bool{}
	for name, position := range positions {
		if position < 0 || position >= len(words) || seen[position] {
			t.Fatalf("bad or overlapping entry %s at %d", name, position)
		}
		seen[position] = true
	}
	// Collect、Validate、Sweep 和 Alloc 都不进行系统调用。
	// mmap/munmap 只存在于原有 M8 heap.init / heap.destroy 中。
	for _, pos := range []int{entries.Collect, entries.Validate, entries.Sweep, entries.Alloc} {
		if pos >= entries.Init {
			t.Fatalf("bad runtime ordering at %d", pos)
		}
	}
	for _, word := range words[:entries.Init] {
		if word == svc(0x80) {
			t.Fatal("GC phase must not make direct Darwin syscalls")
		}
	}
	if words[entries.Validate] != movzX(3, 1) || words[entries.Sweep] != movzX(3, 0) {
		t.Fatal("Validate must be read-only; Sweep must mutate only after checking")
	}
	if words[entries.Collect] != subImmX(31, 31, 64) {
		t.Fatal("Collector must save all arguments and LR before calling validator")
	}
}

func TestM10ShiftEncodersMatchARM64Assembler(t *testing.T) {
	for _, test := range []struct {
		got, want uint32
	}{
		{m10Lsl(5, 4, 4), 0xD37CEC85},  // lsl x5, x4, #4
		{m10Lsr(7, 12, 3), 0xD343FD87}, // lsr x7, x12, #3
	} {
		if test.got != test.want {
			t.Fatalf("got %#08x want %#08x", test.got, test.want)
		}
	}
}

func TestM10UnknownProbeRejected(t *testing.T) {
	_, err := M10GCProbe("unrecognized")
	if err == nil || !strings.Contains(err.Error(), "unknown M10 probe") {
		t.Fatalf("expected a clear probe mode error, got %v", err)
	}
}
