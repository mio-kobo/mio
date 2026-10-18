package darwinarm64

import "fmt"

// The native FS table is a private 4 KiB mapping. Each 128-byte slot carries
// fd, generation, kind, errno and directory iteration state. Mio sees only a
// (generation << 8 | slot+1) token, never a host pointer or descriptor.
const (
	m14SYSRead          = 3
	m14SYSOpen          = 5
	m14SYSClose         = 6
	m14SYSMkdir         = 136
	m14SYSStat64        = 338
	m14SYSGetdirentries = 344
	m14HandleSlots      = 32
	m14SlotShift        = 7
	m14FileKind         = 1
	m14DirectoryKind    = 2
	m14DirBufferSize    = 16 << 10
)

func m14LdrH(dst, base uint32, byteOffset uint32) uint32 {
	return 0x79400000 | (byteOffset/2)<<10 | base<<5 | dst
}

func m14Enter(b *m8Assembler) { b.emit(0xA9BF7BFD, addImmX(29, 31, 0)) }
func m14Leave(b *m8Assembler) { b.emit(addImmX(31, 29, 0), 0xA8C17BFD, m8Ret()) }

func m14ReturnTagged(b *m8Assembler, tagged uint64) {
	b.emit(m8MovBits(0, tagged)...)
	m14Leave(b)
}

// appendM14NativeFSRuntime returns position-independent Darwin/ARM64 code.
// The external call offsets target M12 ArrayWithCapacity and are patched by
// the final linker after both runtimes have their final positions.
func appendM14NativeFSRuntime(emptyTagged uint64) ([]uint32, map[string]int, []int, error) {
	b := newM8Assembler()
	external := make([]int, 0, 1)

	// string.ptr(x0) -> x0=bytes, x1=len. Static strings use x28 __const.
	b.label("m14.string.ptr")
	b.emit(movzX(9, 1), m8And(9, 0, 9), cmpZeroX(9))
	b.bc(0, "m14.string.dynamic")
	b.emit(m10Lsr(9, 0, 1), m10Lsl(10, 9, 3), addX(10, 28, 10), m9LdrW(9, 10, 0), m9LdrW(1, 10, 4), addX(0, 28, 9), m8Ret())
	b.label("m14.string.dynamic")
	b.emit(ldrX(1, 0, 1), ldrX(0, 0, 5), addImmX(0, 0, m12PayloadBase), m8Ret())

	// array.ptr(x0) -> x0=payload, x1=len.
	b.label("m14.array.ptr")
	b.emit(ldrX(1, 0, 1), ldrX(0, 0, 5), addImmX(0, 0, m12PayloadBase), m8Ret())

	// table.ensure() -> x0=table or zero. x27 owns the private mapping; the
	// heap's word 7 remains the M10 free-list head.
	b.label("m14.table.ensure")
	b.emit(movX(9, 27), cmpZeroX(9))
	b.bc(1, "m14.table.have")
	b.emit(movzX(0, 0), movzX(1, 4096), movzX(2, 3), movzX(3, m8DarwinMapAnonPriv), m8Movn(4, 0), movzX(5, 0), movzX(16, m8DarwinSYSMmap), svc(0x80))
	b.bc(2, "m14.table.fail")
	b.emit(movX(27, 0), m8Ret())
	b.label("m14.table.have")
	b.emit(movX(0, 9), m8Ret())
	b.label("m14.table.fail")
	b.emit(movzX(0, 0), m8Ret())

	// handle.alloc(x0=kind,x1=fd,x2=errno) -> token or zero.
	b.label("m14.handle.alloc")
	m14Enter(b)
	b.emit(subImmX(31, 31, 32), strX(0, 31, 0), strX(1, 31, 1), strX(2, 31, 2))
	b.bl("m14.table.ensure")
	b.cbz(0, "m14.handle.alloc.fail")
	b.emit(movX(9, 0), movzX(10, 0))
	b.label("m14.handle.alloc.scan")
	b.emit(m10Lsl(11, 10, m14SlotShift), addX(11, 9, 11), ldrX(12, 11, 2), cmpZeroX(12))
	b.bc(0, "m14.handle.alloc.found")
	b.emit(addImmX(10, 10, 1), cmpImmX(10, m14HandleSlots))
	b.bc(3, "m14.handle.alloc.scan")
	b.label("m14.handle.alloc.fail")
	b.emit(addImmX(31, 31, 32), movzX(0, 0))
	m14Leave(b)
	b.label("m14.handle.alloc.found")
	b.emit(ldrX(12, 11, 1), addImmX(12, 12, 1), cmpZeroX(12))
	b.bc(1, "m14.handle.alloc.gen")
	b.emit(movzX(12, 1))
	b.label("m14.handle.alloc.gen")
	b.emit(ldrX(13, 31, 0), ldrX(14, 31, 1), ldrX(15, 31, 2), strX(14, 11, 0), strX(12, 11, 1), strX(13, 11, 2), strX(15, 11, 3))
	b.emit(m10Lsl(0, 12, 8), addImmX(10, 10, 1), m12Or(0, 0, 10), addImmX(31, 31, 32))
	m14Leave(b)

	// handle.lookup(x0=token,x1=kind) -> x0=slot or zero.
	b.label("m14.handle.lookup")
	b.emit(movX(12, 1), movX(13, 0), movX(9, 27), cmpZeroX(9))
	b.bc(0, "m14.handle.lookup.fail")
	b.emit(0xD3400000|7<<10|13<<5|10, subImmX(10, 10, 1), cmpImmX(10, m14HandleSlots)) // UBFM low byte
	b.bc(2, "m14.handle.lookup.fail")
	b.emit(m10Lsr(14, 13, 8), m10Lsl(11, 10, m14SlotShift), addX(11, 9, 11), ldrX(15, 11, 1), m8Cmp(14, 15))
	b.bc(1, "m14.handle.lookup.fail")
	b.emit(ldrX(15, 11, 2), m8Cmp(12, 15))
	b.bc(1, "m14.handle.lookup.fail")
	b.emit(movX(0, 11), m8Ret())
	b.label("m14.handle.lookup.fail")
	b.emit(movzX(0, 0), m8Ret())

	// status.classify(x0=errno,x1=invalid-path) -> x0=kiso status code.
	b.label("m14.status.classify")
	b.cbz(0, "m14.status.ok")
	for _, item := range []struct{ errno, code uint16 }{{2, 1}, {13, 2}, {1, 2}, {9, 3}, {28, 6}} {
		b.emit(cmpImmX(0, uint32(item.errno)))
		label := fmt.Sprintf("m14.status.code.%d.%d", item.errno, item.code)
		b.bc(0, label)
		b.label(label + ".next")
	}
	b.emit(cmpImmX(0, 22))
	b.bc(0, "m14.status.einval")
	b.emit(movzX(0, 5), m8Ret())
	b.label("m14.status.einval")
	b.cbz(1, "m14.status.invalid")
	b.emit(movzX(0, 7), m8Ret())
	b.label("m14.status.invalid")
	b.emit(movzX(0, 4), m8Ret())
	b.label("m14.status.ok")
	b.emit(movzX(0, 0), m8Ret())
	for _, item := range []struct{ errno, code uint16 }{{2, 1}, {13, 2}, {1, 2}, {9, 3}, {28, 6}} {
		label := fmt.Sprintf("m14.status.code.%d.%d", item.errno, item.code)
		b.label(label)
		b.emit(movzX(0, item.code), m8Ret())
	}

	// cpath(x0=string) -> x0=mmap, x1=size; zero means invalid/OOM.
	b.label("m14.cpath")
	m14Enter(b)
	b.emit(subImmX(31, 31, 32), strX(0, 31, 0))
	b.bl("m14.string.ptr")
	b.emit(strX(0, 31, 1), strX(1, 31, 2), addImmX(1, 1, 1), strX(1, 31, 3), movzX(0, 0), movzX(2, 3), movzX(3, m8DarwinMapAnonPriv), m8Movn(4, 0), movzX(5, 0), movzX(16, m8DarwinSYSMmap), svc(0x80))
	b.bc(2, "m14.cpath.fail")
	b.emit(movX(12, 0), ldrX(10, 31, 1), ldrX(11, 31, 2), movzX(9, 0))
	b.label("m14.cpath.copy")
	b.emit(m8Cmp(9, 11))
	b.bc(2, "m14.cpath.done")
	b.emit(addX(13, 10, 9), m8LdrB(14, 13), cmpZeroX(14))
	b.bc(0, "m14.cpath.nul")
	b.emit(addX(13, 12, 9), m8StrB(14, 13), addImmX(9, 9, 1))
	b.b("m14.cpath.copy")
	b.label("m14.cpath.done")
	b.emit(addX(13, 12, 11), m8StrB(31, 13), movX(0, 12), ldrX(1, 31, 3), addImmX(31, 31, 32))
	m14Leave(b)
	b.label("m14.cpath.nul")
	b.emit(movX(0, 12), ldrX(1, 31, 3), movzX(16, m8DarwinSYSMunmap), svc(0x80))
	b.label("m14.cpath.fail")
	b.emit(addImmX(31, 31, 32), movzX(0, 0), movzX(1, 0))
	m14Leave(b)

	// Writes one-byte status to an array: x0=array,x1=code.
	b.label("m14.status.byte")
	b.emit(ldrX(9, 0, 1), cmpZeroX(9))
	b.bc(0, "m14.status.byte.done")
	b.emit(ldrX(9, 0, 5), addImmX(9, 9, m12PayloadBase), m8StrB(1, 9))
	b.label("m14.status.byte.done")
	b.emit(m8Ret())

	appendM14FileEntries(b, emptyTagged)
	appendM14PathEntries(b, emptyTagged)
	appendM14DirectoryEntries(b, emptyTagged, &external)
	appendM14Cleanup(b)

	entries := make(map[string]int)
	for _, name := range []string{"open", "file_status", "read", "write", "close", "stat", "mkdir", "open_dir", "dir_status", "dir_next", "dir_name", "dir_is_dir", "dir_is_file", "dir_close", "cleanup"} {
		entries[name] = b.labels["m14."+name]
	}
	words, err := b.assemble()
	return words, entries, external, err
}

func appendM14PathEntries(b *m8Assembler, emptyTagged uint64) {
	// stat(path, status) follows symbolic links like os.Stat. status[0] is
	// 0=missing/error, 1=non-directory, 2=directory; status[1] is FsError.
	b.label("m14.stat")
	m14Enter(b)
	// Slots 0..2 preserve status, cpath address and cpath mapping length.
	// The Darwin arm64 stat64 buffer begins at SP+32 and is 144 bytes.
	b.emit(subImmX(31, 31, 192), strX(1, 31, 0))
	b.bl("m14.cpath")
	b.cbz(0, "m14.stat.badpath")
	b.emit(strX(0, 31, 1), strX(1, 31, 2), addImmX(1, 31, 32), movzX(16, m14SYSStat64), svc(0x80))
	b.bc(2, "m14.stat.syserr")
	// struct stat::st_mode is a uint16 at byte offset 4 on Darwin arm64.
	b.emit(m14LdrH(9, 31, 36))
	b.emit(m8MovBits(10, 0xf000)...)
	b.emit(m8And(9, 9, 10))
	b.emit(m8MovBits(10, 0x4000)...)
	b.emit(m8Cmp(9, 10), movzX(11, 1))
	b.bc(1, "m14.stat.kind.done")
	b.emit(movzX(11, 2))
	b.label("m14.stat.kind.done")
	b.emit(movzX(13, 0))
	b.b("m14.stat.release")
	b.label("m14.stat.syserr")
	b.emit(movzX(11, 0), movX(13, 0))
	b.label("m14.stat.release")
	b.emit(strX(11, 31, 3), strX(13, 31, 4), ldrX(0, 31, 1), ldrX(1, 31, 2), movzX(16, m8DarwinSYSMunmap), svc(0x80))
	b.emit(ldrX(0, 31, 0), ldrX(9, 0, 1), cmpImmX(9, 2))
	b.bc(3, "m14.stat.return")
	b.emit(ldrX(9, 0, 5), addImmX(9, 9, m12PayloadBase), ldrX(11, 31, 3), m8StrB(11, 9), ldrX(0, 31, 4), movzX(1, 1))
	b.bl("m14.status.classify")
	b.emit(addImmX(9, 9, 1), m8StrB(0, 9))
	b.label("m14.stat.return")
	b.emit(addImmX(31, 31, 192))
	m14ReturnTagged(b, emptyTagged)
	b.label("m14.stat.badpath")
	b.emit(ldrX(0, 31, 0), ldrX(9, 0, 1), cmpImmX(9, 2))
	b.bc(3, "m14.stat.badpath.return")
	b.emit(ldrX(9, 0, 5), addImmX(9, 9, m12PayloadBase), m8StrB(31, 9), movzX(10, 7), addImmX(9, 9, 1), m8StrB(10, 9))
	b.label("m14.stat.badpath.return")
	b.emit(addImmX(31, 31, 192))
	m14ReturnTagged(b, emptyTagged)

	// mkdir(path, status) creates exactly one directory. Darwin applies the
	// process umask to 0777, matching os.Mkdir(path, 0777).
	b.label("m14.mkdir")
	m14Enter(b)
	b.emit(subImmX(31, 31, 48), strX(1, 31, 0))
	b.bl("m14.cpath")
	b.cbz(0, "m14.mkdir.badpath")
	b.emit(strX(0, 31, 1), strX(1, 31, 2), movzX(1, 0x1ff), movzX(16, m14SYSMkdir), svc(0x80))
	b.bc(2, "m14.mkdir.syserr")
	b.emit(movzX(13, 0))
	b.b("m14.mkdir.release")
	b.label("m14.mkdir.syserr")
	b.emit(movX(13, 0))
	b.label("m14.mkdir.release")
	b.emit(strX(13, 31, 3), ldrX(0, 31, 1), ldrX(1, 31, 2), movzX(16, m8DarwinSYSMunmap), svc(0x80))
	b.emit(ldrX(0, 31, 3), movzX(1, 1))
	b.bl("m14.status.classify")
	b.emit(movX(1, 0), ldrX(0, 31, 0))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 48))
	m14ReturnTagged(b, emptyTagged)
	b.label("m14.mkdir.badpath")
	b.emit(ldrX(0, 31, 0), movzX(1, 7))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 48))
	m14ReturnTagged(b, emptyTagged)
}

func appendM14FileEntries(b *m8Assembler, emptyTagged uint64) {
	// open(path, mode) -> resource token. Open failures remain inspectable via
	// file_status, matching the host MMA's deferred-error resource semantics.
	b.label("m14.open")
	m14Enter(b)
	b.emit(subImmX(31, 31, 64), strX(1, 31, 0))
	b.bl("m14.cpath")
	b.cbz(0, "m14.open.badpath")
	b.emit(strX(0, 31, 1), strX(1, 31, 2), ldrX(9, 31, 0), movzX(1, 0), cmpZeroX(9))
	b.bc(0, "m14.open.flags")
	b.emit(cmpImmX(9, 1))
	b.bc(0, "m14.open.write")
	b.emit(cmpImmX(9, 2))
	b.bc(0, "m14.open.append")
	b.b("m14.open.invalidmode")
	b.label("m14.open.write")
	b.emit(movzX(1, 0x601)) // O_WRONLY|O_CREAT|O_TRUNC
	b.b("m14.open.flags")
	b.label("m14.open.append")
	b.emit(movzX(1, 0x209)) // O_WRONLY|O_CREAT|O_APPEND
	b.label("m14.open.flags")
	b.emit(movzX(2, 0x1b6), movzX(16, m14SYSOpen), svc(0x80))
	b.bc(2, "m14.open.syserr")
	b.emit(strX(0, 31, 3), movzX(9, 0), strX(9, 31, 4))
	b.b("m14.open.release")
	b.label("m14.open.syserr")
	b.emit(strX(0, 31, 4), m8Movn(9, 0), strX(9, 31, 3))
	b.label("m14.open.release")
	b.emit(ldrX(0, 31, 1), ldrX(1, 31, 2), movzX(16, m8DarwinSYSMunmap), svc(0x80), movzX(0, m14FileKind), ldrX(1, 31, 3), ldrX(2, 31, 4))
	b.bl("m14.handle.alloc")
	b.emit(addImmX(31, 31, 64))
	m14Leave(b)
	b.label("m14.open.invalidmode")
	b.emit(ldrX(0, 31, 1), ldrX(1, 31, 2), movzX(16, m8DarwinSYSMunmap), svc(0x80))
	b.label("m14.open.badpath")
	b.emit(movzX(0, m14FileKind), m8Movn(1, 0), movzX(2, 22))
	b.bl("m14.handle.alloc")
	b.emit(addImmX(31, 31, 64))
	m14Leave(b)

	b.label("m14.file_status")
	m14Enter(b)
	b.emit(subImmX(31, 31, 32), strX(1, 31, 0), movzX(1, m14FileKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14.file_status.invalid")
	b.emit(ldrX(0, 0, 3), movzX(1, 1))
	b.bl("m14.status.classify")
	b.emit(movX(1, 0), ldrX(0, 31, 0))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 32))
	m14ReturnTagged(b, emptyTagged)
	b.label("m14.file_status.invalid")
	b.emit(ldrX(0, 31, 0), movzX(1, 4))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 32))
	m14ReturnTagged(b, emptyTagged)

	appendM14IOEntry(b, "read", true, emptyTagged)
	appendM14IOEntry(b, "write", false, emptyTagged)

	b.label("m14.close")
	m14Enter(b)
	b.emit(subImmX(31, 31, 32), strX(1, 31, 0), movzX(1, m14FileKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14.close.invalid")
	b.emit(movX(10, 0), ldrX(0, 10, 0), cmpZeroX(0))
	b.bc(11, "m14.close.nosys")
	b.emit(movzX(16, m14SYSClose), svc(0x80))
	b.bc(2, "m14.close.syserr")
	b.emit(movzX(9, 0))
	b.b("m14.close.done")
	b.label("m14.close.syserr")
	b.emit(movX(9, 0))
	b.b("m14.close.done")
	b.label("m14.close.nosys")
	b.emit(ldrX(9, 10, 3))
	b.label("m14.close.done")
	b.emit(movzX(11, 0), strX(11, 10, 2), movX(0, 9), movzX(1, 0))
	b.bl("m14.status.classify")
	b.emit(movX(1, 0), ldrX(0, 31, 0))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 32))
	m14ReturnTagged(b, emptyTagged)
	b.label("m14.close.invalid")
	b.emit(ldrX(0, 31, 0), movzX(1, 3))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 32))
	m14ReturnTagged(b, emptyTagged)
}

func appendM14IOEntry(b *m8Assembler, name string, read bool, emptyTagged uint64) {
	b.label("m14." + name)
	m14Enter(b)
	// saved: array, offset, length, status
	b.emit(subImmX(31, 31, 64), strX(1, 31, 0), strX(2, 31, 1), strX(3, 31, 2), strX(4, 31, 3), movzX(1, m14FileKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14."+name+".closed")
	b.emit(ldrX(9, 0, 0), cmpZeroX(9))
	b.bc(11, "m14."+name+".closed")
	b.emit(strX(9, 31, 4), ldrX(0, 31, 0))
	b.bl("m14.array.ptr")
	b.emit(strX(0, 31, 5), ldrX(10, 31, 1), ldrX(11, 31, 2), cmpZeroX(10))
	b.bc(11, "m14."+name+".invalid")
	b.emit(cmpZeroX(11))
	b.bc(11, "m14."+name+".invalid")
	b.emit(addX(12, 10, 11), m8Cmp(12, 1))
	b.bc(8, "m14."+name+".invalid")
	b.emit(ldrX(0, 31, 4), ldrX(1, 31, 5), addX(1, 1, 10), movX(2, 11), movzX(16, func() uint16 {
		if read {
			return m14SYSRead
		}
		return m8DarwinSYSWrite
	}()), svc(0x80))
	b.bc(2, "m14."+name+".syserr")
	b.emit(movX(9, 0), movzX(10, 0), movzX(11, 0))
	if read {
		b.emit(cmpZeroX(9), csetW(10, 0)) // zero-byte successful read is EOF
	}
	b.b("m14." + name + ".status")
	b.label("m14." + name + ".syserr")
	b.emit(movzX(9, 0), movzX(10, 0), movX(11, 0))
	b.b("m14." + name + ".status")
	b.label("m14." + name + ".closed")
	b.emit(movzX(9, 0), movzX(10, 0), movzX(11, 9))
	b.b("m14." + name + ".status")
	b.label("m14." + name + ".invalid")
	b.emit(movzX(9, 0), movzX(10, 0), movzX(11, 22))
	b.label("m14." + name + ".status")
	// status[0:8]=count, [8]=eof, [9]=classified error.
	b.emit(ldrX(12, 31, 3), ldrX(13, 12, 1), cmpImmX(13, 10))
	b.bc(3, "m14."+name+".return")
	b.emit(ldrX(12, 12, 5), addImmX(12, 12, m12PayloadBase), strX(9, 12, 0), addImmX(13, 12, 8), m8StrB(10, 13), movX(0, 11), movzX(1, 0))
	b.bl("m14.status.classify")
	b.emit(addImmX(13, 12, 9), m8StrB(0, 13))
	b.label("m14." + name + ".return")
	b.emit(addImmX(31, 31, 64))
	m14ReturnTagged(b, emptyTagged)
}

func appendM14DirectoryEntries(b *m8Assembler, emptyTagged uint64, external *[]int) {
	b.label("m14.open_dir")
	m14Enter(b)
	b.emit(subImmX(31, 31, 64))
	b.bl("m14.cpath")
	b.cbz(0, "m14.open_dir.badpath")
	b.emit(strX(0, 31, 0), strX(1, 31, 1), movzX(1, 0), movkX(1, 0x10, 1), movzX(2, 0), movzX(16, m14SYSOpen), svc(0x80)) // O_DIRECTORY
	b.bc(2, "m14.open_dir.syserr")
	b.emit(strX(0, 31, 2), movzX(9, 0), strX(9, 31, 3))
	// A private directory buffer is owned by the handle slot.
	b.emit(movzX(0, 0))
	b.emit(m8MovBits(1, m14DirBufferSize)...)
	b.emit(movzX(2, 3), movzX(3, m8DarwinMapAnonPriv), m8Movn(4, 0), movzX(5, 0), movzX(16, m8DarwinSYSMmap), svc(0x80))
	b.bc(2, "m14.open_dir.buffererr")
	b.emit(strX(0, 31, 4))
	b.b("m14.open_dir.release")
	b.label("m14.open_dir.buffererr")
	b.emit(strX(0, 31, 3), ldrX(0, 31, 2), movzX(16, m14SYSClose), svc(0x80), m8Movn(9, 0), strX(9, 31, 2), movzX(9, 0), strX(9, 31, 4))
	b.b("m14.open_dir.release")
	b.label("m14.open_dir.syserr")
	b.emit(strX(0, 31, 3), m8Movn(9, 0), strX(9, 31, 2), movzX(9, 0), strX(9, 31, 4))
	b.label("m14.open_dir.release")
	b.emit(ldrX(0, 31, 0), ldrX(1, 31, 1), movzX(16, m8DarwinSYSMunmap), svc(0x80), movzX(0, m14DirectoryKind), ldrX(1, 31, 2), ldrX(2, 31, 3))
	b.bl("m14.handle.alloc")
	b.emit(strX(0, 31, 5), movzX(1, m14DirectoryKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14.open_dir.return")
	b.emit(ldrX(9, 31, 4), strX(9, 0, 4))
	b.label("m14.open_dir.return")
	b.emit(ldrX(0, 31, 5), addImmX(31, 31, 64))
	m14Leave(b)
	b.label("m14.open_dir.badpath")
	b.emit(movzX(0, m14DirectoryKind), m8Movn(1, 0), movzX(2, 22))
	b.bl("m14.handle.alloc")
	b.emit(addImmX(31, 31, 64))
	m14Leave(b)

	b.label("m14.dir_status")
	m14Enter(b)
	b.emit(subImmX(31, 31, 32), strX(1, 31, 0), movzX(1, m14DirectoryKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14.dir_status.invalid")
	b.emit(ldrX(0, 0, 3), movzX(1, 1))
	b.bl("m14.status.classify")
	b.emit(movX(1, 0), ldrX(0, 31, 0))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 32))
	m14ReturnTagged(b, emptyTagged)
	b.label("m14.dir_status.invalid")
	b.emit(ldrX(0, 31, 0), movzX(1, 4))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 32))
	m14ReturnTagged(b, emptyTagged)

	b.label("m14.dir_next")
	m14Enter(b)
	b.emit(subImmX(31, 31, 32), movzX(1, m14DirectoryKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14.dir_next.false")
	b.emit(movX(10, 0), strX(10, 31, 0), ldrX(9, 10, 3), cmpZeroX(9))
	b.bc(1, "m14.dir_next.false")
	b.label("m14.dir_next.scan")
	b.emit(ldrX(11, 10, 6), ldrX(12, 10, 5), m8Cmp(11, 12))
	b.bc(3, "m14.dir_next.entry")
	// Refill with getdirentries64(fd, buffer, 16KiB, &basep).
	b.emit(ldrX(0, 10, 0), ldrX(1, 10, 4))
	b.emit(m8MovBits(2, m14DirBufferSize)...)
	b.emit(addImmX(3, 31, 8), movzX(9, 0), strX(9, 31, 1), movzX(16, m14SYSGetdirentries), svc(0x80))
	b.bc(2, "m14.dir_next.error")
	b.cbz(0, "m14.dir_next.false")
	b.emit(strX(0, 10, 5), movzX(11, 0), strX(11, 10, 6))
	b.label("m14.dir_next.entry")
	b.emit(ldrX(9, 10, 4), ldrX(11, 10, 6), addX(9, 9, 11), m14LdrH(12, 9, 16), cmpZeroX(12))
	b.bc(0, "m14.dir_next.error.invalid")
	b.emit(addX(11, 11, 12), strX(11, 10, 6), m14LdrH(13, 9, 18), addImmX(14, 9, 21), addImmX(15, 9, 20), m8LdrB(15, 15), strX(14, 10, 7), strX(13, 10, 8), strX(15, 10, 9))
	// Skip the synthetic . and .. records.
	b.emit(cmpImmX(13, 1))
	b.bc(1, "m14.dir_next.checkdotdot")
	b.emit(m8LdrB(11, 14), cmpImmX(11, '.'))
	b.bc(0, "m14.dir_next.scan")
	b.b("m14.dir_next.true")
	b.label("m14.dir_next.checkdotdot")
	b.emit(cmpImmX(13, 2))
	b.bc(1, "m14.dir_next.true")
	b.emit(m8LdrB(11, 14), cmpImmX(11, '.'))
	b.bc(1, "m14.dir_next.true")
	b.emit(addImmX(11, 14, 1), m8LdrB(11, 11), cmpImmX(11, '.'))
	b.bc(0, "m14.dir_next.scan")
	b.label("m14.dir_next.true")
	b.emit(addImmX(31, 31, 32), movzX(0, 1))
	m14Leave(b)
	b.label("m14.dir_next.error.invalid")
	b.emit(movzX(0, 22))
	b.label("m14.dir_next.error")
	b.emit(strX(0, 10, 3))
	b.label("m14.dir_next.false")
	b.emit(addImmX(31, 31, 32), movzX(0, 0))
	m14Leave(b)

	b.label("m14.dir_name")
	m14Enter(b)
	b.emit(subImmX(31, 31, 32), movzX(1, m14DirectoryKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14.dir_name.fail")
	b.emit(ldrX(9, 0, 7), ldrX(10, 0, 8), cmpZeroX(9))
	b.bc(0, "m14.dir_name.fail")
	b.emit(strX(9, 31, 0), strX(10, 31, 1), movX(0, 23), movX(1, 10), movzX(3, 1), movzX(4, 0))
	*external = append(*external, len(b.words))
	b.emit(0) // M12 ArrayWithCapacity
	b.cbz(0, "m14.dir_name.fail")
	b.emit(movX(14, 0), ldrX(9, 31, 0), ldrX(10, 31, 1), ldrX(11, 14, 5), addImmX(11, 11, m12PayloadBase), movzX(12, 0))
	b.label("m14.dir_name.copy")
	b.emit(m8Cmp(12, 10))
	b.bc(2, "m14.dir_name.done")
	b.emit(addX(13, 9, 12), m8LdrB(13, 13), addX(15, 11, 12), m8StrB(13, 15), addImmX(12, 12, 1))
	b.b("m14.dir_name.copy")
	b.label("m14.dir_name.done")
	b.emit(strX(10, 14, 1), movX(0, 14), addImmX(31, 31, 32))
	m14Leave(b)
	b.label("m14.dir_name.fail")
	b.emit(movzX(0, 0), addImmX(31, 31, 32))
	m14Leave(b)

	appendM14DirPredicate(b, "dir_is_dir", 4)
	appendM14DirPredicate(b, "dir_is_file", 8)

	b.label("m14.dir_close")
	m14Enter(b)
	b.emit(subImmX(31, 31, 32), strX(1, 31, 0), movzX(1, m14DirectoryKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14.dir_close.invalid")
	b.emit(movX(10, 0), ldrX(0, 10, 0), cmpZeroX(0))
	b.bc(11, "m14.dir_close.buffer")
	b.emit(movzX(16, m14SYSClose), svc(0x80))
	b.label("m14.dir_close.buffer")
	b.emit(ldrX(0, 10, 4), cmpZeroX(0))
	b.bc(0, "m14.dir_close.clear")
	b.emit(m8MovBits(1, m14DirBufferSize)...)
	b.emit(movzX(16, m8DarwinSYSMunmap), svc(0x80))
	b.label("m14.dir_close.clear")
	b.emit(movzX(9, 0), strX(9, 10, 2), ldrX(0, 31, 0), movzX(1, 0))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 32))
	m14ReturnTagged(b, emptyTagged)
	b.label("m14.dir_close.invalid")
	b.emit(ldrX(0, 31, 0), movzX(1, 3))
	b.bl("m14.status.byte")
	b.emit(addImmX(31, 31, 32))
	m14ReturnTagged(b, emptyTagged)
}

func appendM14DirPredicate(b *m8Assembler, name string, direntType uint16) {
	b.label("m14." + name)
	m14Enter(b)
	b.emit(movzX(1, m14DirectoryKind))
	b.bl("m14.handle.lookup")
	b.cbz(0, "m14."+name+".false")
	b.emit(ldrX(9, 0, 9), cmpImmX(9, uint32(direntType)), csetW(0, 0))
	m14Leave(b)
	b.label("m14." + name + ".false")
	b.emit(movzX(0, 0))
	m14Leave(b)
}

func appendM14Cleanup(b *m8Assembler) {
	b.label("m14.cleanup")
	m14Enter(b)
	b.emit(movX(9, 27), cmpZeroX(9))
	b.bc(0, "m14.cleanup.done")
	b.emit(movX(12, 9), movzX(10, 0))
	b.label("m14.cleanup.loop")
	b.emit(m10Lsl(11, 10, m14SlotShift), addX(11, 12, 11), ldrX(13, 11, 2), cmpZeroX(13))
	b.bc(0, "m14.cleanup.next")
	b.emit(ldrX(0, 11, 0), cmpZeroX(0))
	b.bc(11, "m14.cleanup.buffer")
	b.emit(movzX(16, m14SYSClose), svc(0x80))
	b.label("m14.cleanup.buffer")
	b.emit(cmpImmX(13, m14DirectoryKind))
	b.bc(1, "m14.cleanup.clear")
	b.emit(ldrX(0, 11, 4), cmpZeroX(0))
	b.bc(0, "m14.cleanup.clear")
	b.emit(m8MovBits(1, m14DirBufferSize)...)
	b.emit(movzX(16, m8DarwinSYSMunmap), svc(0x80))
	b.label("m14.cleanup.clear")
	b.emit(movzX(13, 0), strX(13, 11, 2))
	b.label("m14.cleanup.next")
	b.emit(addImmX(10, 10, 1), cmpImmX(10, m14HandleSlots))
	b.bc(3, "m14.cleanup.loop")
	b.emit(movX(0, 12), movzX(1, 4096), movzX(16, m8DarwinSYSMunmap), svc(0x80), movzX(27, 0))
	b.label("m14.cleanup.done")
	b.emit(movzX(0, 0))
	m14Leave(b)
}
