package darwinarm64

import (
	"bytes"
	"debug/macho"
	"encoding/binary"
	"testing"
)

func TestExitProgramMachO(t *testing.T) {
	program, err := ExitProgram(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(program) != int(pageSize) {
		t.Fatalf("size=%d, want %d", len(program), pageSize)
	}
	file, err := macho.NewFile(bytes.NewReader(program))
	if err != nil {
		t.Fatalf("invalid Mach-O: %v", err)
	}
	defer file.Close()
	if file.Cpu != macho.CpuArm64 || file.Type != macho.TypeExec {
		t.Fatalf("unexpected target: cpu=%v type=%v", file.Cpu, file.Type)
	}
	section := file.Section("__text")
	if section == nil {
		t.Fatal("missing __text section")
	}
	if section.Offset != uint32(entryOffset) || section.Size != 12 {
		t.Fatalf("invalid code section: offset=0x%x size=%d", section.Offset, section.Size)
	}
	if file.Segment("__PAGEZERO") == nil || file.Segment("__LINKEDIT") == nil {
		t.Fatal("missing required Mach-O segment")
	}
	linkedit := file.Segment("__LINKEDIT")
	if linkedit.Addr != textVMAddr+pageSize || linkedit.Memsz != pageSize ||
		linkedit.Offset != pageSize || linkedit.Filesz != 0 {
		t.Fatalf("invalid __LINKEDIT: addr=0x%x vmsize=0x%x fileoff=0x%x filesize=0x%x",
			linkedit.Addr, linkedit.Memsz, linkedit.Offset, linkedit.Filesz)
	}
	if linkedit.Prot != vmProtRead || linkedit.Maxprot != vmProtRead {
		t.Fatalf("__LINKEDIT must be read-only: prot=%d maxprot=%d", linkedit.Prot, linkedit.Maxprot)
	}
	if binary.LittleEndian.Uint32(program[16:20]) != 8 {
		t.Fatalf("unexpected number of load commands=%d", binary.LittleEndian.Uint32(program[16:20]))
	}
	if binary.LittleEndian.Uint32(program[20:24]) != 480 {
		t.Fatalf("unexpected sizeofcmds=%d", binary.LittleEndian.Uint32(program[20:24]))
	}
	checkEmptySymbolTables(t, program)
	want := []uint32{0xD2800000, 0xD2800030, 0xD4001001}
	for i, w := range want {
		off := int(entryOffset) + i*4
		got := binary.LittleEndian.Uint32(program[off : off+4])
		if got != w {
			t.Fatalf("instruction %d: got %#08x, want %#08x", i, got, w)
		}
	}
}

func TestExitStatusReallyChangesMachineCode(t *testing.T) {
	program, err := ExitProgram(42)
	if err != nil {
		t.Fatal(err)
	}
	got := binary.LittleEndian.Uint32(program[entryOffset : entryOffset+4])
	want := uint32(0xD2800540) // movz x0, #42
	if got != want {
		t.Fatalf("exit(42) encoding: %#x, want %#x", got, want)
	}
}

// checkEmptySymbolTables 验证两个空符号表命令的结构和位置。
// 回归实机遇到的 dyld 空指针 +0x48 崩溃，避免进入 __text 前再次失败。
func checkEmptySymbolTables(t *testing.T, program []byte) {
	t.Helper()
	ncmds := binary.LittleEndian.Uint32(program[16:20])
	sizeofcmds := binary.LittleEndian.Uint32(program[20:24])
	off := headerSize
	counts := map[uint32]int{}
	for i := uint32(0); i < ncmds; i++ {
		if off+8 > len(program) || off+8 > headerSize+int(sizeofcmds) {
			t.Fatalf("truncated load command %d at %#x", i, off)
		}
		cmd := binary.LittleEndian.Uint32(program[off : off+4])
		size := binary.LittleEndian.Uint32(program[off+4 : off+8])
		if size < 8 || size%8 != 0 || off+int(size) > headerSize+int(sizeofcmds) {
			t.Fatalf("invalid load command %d: cmd=%#x size=%d", i, cmd, size)
		}
		counts[cmd]++
		switch cmd {
		case lcSymtab:
			if size != 24 {
				t.Fatalf("LC_SYMTAB cmdsize=%d", size)
			}
			for pos := off + 8; pos < off+int(size); pos += 4 {
				if v := binary.LittleEndian.Uint32(program[pos : pos+4]); v != 0 {
					t.Fatalf("LC_SYMTAB empty table must be all zero, offset=%d value=%d", pos, v)
				}
			}
		case lcDysymtab:
			if size != 80 {
				t.Fatalf("LC_DYSYMTAB cmdsize=%d", size)
			}
			for pos := off + 8; pos < off+int(size); pos += 4 {
				if v := binary.LittleEndian.Uint32(program[pos : pos+4]); v != 0 {
					t.Fatalf("LC_DYSYMTAB empty table must be all zero, offset=%d value=%d", pos, v)
				}
			}
		}
		off += int(size)
	}
	if off != headerSize+int(sizeofcmds) {
		t.Fatalf("command bytes=%d, sizeofcmds=%d", off-headerSize, sizeofcmds)
	}
	if counts[lcSymtab] != 1 || counts[lcDysymtab] != 1 {
		t.Fatalf("missing or duplicate symtabs: LC_SYMTAB=%d LC_DYSYMTAB=%d",
			counts[lcSymtab], counts[lcDysymtab])
	}
	if off+16 > int(entryOffset) {
		t.Fatalf("load commands have no room for codesign: end=%#x entry=%#x", off, entryOffset)
	}
}
