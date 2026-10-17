// Package darwinarm64 负责 ARM64 机器码编码与 macOS Mach-O 可执行文件封装。
// 延续已在 M4 Pro 上验证的 M0 装载结构，不依赖目标程序的 Go Runtime 或 Mochi VM。
package darwinarm64

import (
	"encoding/binary"
	"fmt"
)

const (
	// Apple Silicon 使用 16 KiB 内存页。
	pageSize    uint64 = 0x4000
	textVMAddr  uint64 = 0x100000000
	entryOffset uint64 = 0x300
	headerSize         = 32

	mhMagic64     uint32 = 0xfeedfacf
	cpuTypeArm64  uint32 = 0x0100000c
	cpuSubtypeAll uint32 = 0
	mhExecute     uint32 = 2
	mhFlags       uint32 = 0x00200085 // NOUNDEFS | DYLDLINK | TWOLEVEL | PIE

	lcSegment64    uint32 = 0x19
	lcSymtab       uint32 = 0x02
	lcDysymtab     uint32 = 0x0b
	lcLoadDylinker uint32 = 0x0e
	lcBuildVersion uint32 = 0x32
	lcMain         uint32 = 0x80000028

	vmProtRead      uint32 = 1
	vmProtExecute   uint32 = 4
	platformMacOS   uint32 = 1
	macOSMinVersion uint32 = 11 << 16

	sAttrPureInstructions uint32 = 0x80000000
	sAttrSomeInstructions uint32 = 0x00000400
)

// ExitProgram 保留 M0 的最小退出程序入口，供回归测试使用。
func ExitProgram(status uint8) ([]byte, error) {
	return ExecutableWords([]uint32{
		movzX(0, uint16(status)),
		movzX(16, 1),
		svc(0x80),
	})
}

// ExecutableWords 将 ARM64 指令字封装成可签名的 Mach-O 可执行文件。
// 这里仅负责文件布局，不解释 Mochi 字节码，也不插入任何运行时。
func ExecutableWords(words []uint32) ([]byte, error) {
	if len(words) == 0 {
		return nil, fmt.Errorf("empty ARM64 instruction stream")
	}
	codeSize := uint64(len(words)) * 4

	commands := make([]byte, 0, 512)
	ncmds := uint32(0)

	// 保留低地址空间，防止空指针落入可映射内存。
	commands = appendSegment(commands, segment{
		name: "__PAGEZERO", vmaddr: 0, vmsize: textVMAddr,
	}, false, codeSize)
	ncmds++

	// __TEXT 存放 Mach-O 头、装载命令和可执行机器码。
	commands = appendSegment(commands, segment{
		name: "__TEXT", vmaddr: textVMAddr, vmsize: pageSize,
		fileoff: 0, filesize: pageSize,
		maxprot:  vmProtRead | vmProtExecute,
		initprot: vmProtRead | vmProtExecute,
	}, true, codeSize)
	ncmds++

	// 为 __LINKEDIT 预留一页只读虚拟内存，供 codesign 写入签名信息。
	// 不能使用零长度映射，否则 dyld 可能无法正确访问链接信息。
	commands = appendSegment(commands, segment{
		name: "__LINKEDIT", vmaddr: textVMAddr + pageSize,
		vmsize: pageSize, fileoff: pageSize,
		maxprot: vmProtRead, initprot: vmProtRead,
	}, false, codeSize)
	ncmds++

	commands = appendDylinker(commands, "/usr/lib/dyld")
	ncmds++

	commands = appendU32(commands, lcBuildVersion)
	commands = appendU32(commands, 24)
	commands = appendU32(commands, platformMacOS)
	commands = appendU32(commands, macOSMinVersion)
	commands = appendU32(commands, macOSMinVersion)
	commands = appendU32(commands, 0) // 无构建工具记录
	ncmds++

	// 没有 LC_DYLD_INFO / LC_DYLD_CHAINED_FIXUPS 时，dyld 会回退到旧式重定位扫描。
	// 即使没有重定位，也必须提供 LC_DYSYMTAB，避免 dyld 在空指针 +0x48 处崩溃。
	// 当前 M1 仍不含动态符号或重定位，因此两个命令均使用空表。
	commands = appendEmptySymtab(commands)
	ncmds++
	commands = appendEmptyDysymtab(commands)
	ncmds++

	commands = appendU32(commands, lcMain)
	commands = appendU32(commands, 24)
	commands = appendU64(commands, entryOffset)
	commands = appendU64(commands, 0) // 使用默认栈大小
	ncmds++

	// 为 codesign 添加 LC_CODE_SIGNATURE 预留 16 字节空间。
	if headerSize+len(commands)+16 > int(entryOffset) {
		return nil, fmt.Errorf("mach-o load commands encroach on the entrypoint")
	}
	if entryOffset+codeSize > pageSize {
		return nil, fmt.Errorf("arm64 code does not fit in the __TEXT segment")
	}

	// 输出一整页 16 KiB 的 __TEXT 文件映像，其余部分补零。
	file := make([]byte, int(pageSize))
	binary.LittleEndian.PutUint32(file[0:4], mhMagic64)
	binary.LittleEndian.PutUint32(file[4:8], cpuTypeArm64)
	binary.LittleEndian.PutUint32(file[8:12], cpuSubtypeAll)
	binary.LittleEndian.PutUint32(file[12:16], mhExecute)
	binary.LittleEndian.PutUint32(file[16:20], ncmds)
	binary.LittleEndian.PutUint32(file[20:24], uint32(len(commands)))
	binary.LittleEndian.PutUint32(file[24:28], mhFlags)
	binary.LittleEndian.PutUint32(file[28:32], 0) // 头部保留字段
	copy(file[headerSize:], commands)

	for i, instruction := range words {
		off := int(entryOffset) + i*4
		binary.LittleEndian.PutUint32(file[off:off+4], instruction)
	}
	return file, nil
}

// movzX 和 svc 直接编码 AArch64 指令，后续以此扩展调用和栈操作。
func movzX(reg uint32, imm uint16) uint32 {
	return 0xD2800000 | uint32(imm)<<5 | reg
}

func svc(imm uint16) uint32 {
	return 0xD4000001 | uint32(imm)<<5
}

type segment struct {
	name     string
	vmaddr   uint64
	vmsize   uint64
	fileoff  uint64
	filesize uint64
	maxprot  uint32
	initprot uint32
}

func appendSegment(dst []byte, s segment, addTextSection bool, codeSize uint64) []byte {
	var nsects uint32
	cmdsize := uint32(72)
	if addTextSection {
		nsects, cmdsize = 1, 72+80
	}
	dst = appendU32(dst, lcSegment64)
	dst = appendU32(dst, cmdsize)
	dst = appendName16(dst, s.name)
	dst = appendU64(dst, s.vmaddr)
	dst = appendU64(dst, s.vmsize)
	dst = appendU64(dst, s.fileoff)
	dst = appendU64(dst, s.filesize)
	dst = appendU32(dst, s.maxprot)
	dst = appendU32(dst, s.initprot)
	dst = appendU32(dst, nsects)
	dst = appendU32(dst, 0) // Segment 标志
	if addTextSection {
		dst = appendName16(dst, "__text")
		dst = appendName16(dst, "__TEXT")
		dst = appendU64(dst, textVMAddr+entryOffset)
		dst = appendU64(dst, codeSize)
		dst = appendU32(dst, uint32(entryOffset))
		dst = appendU32(dst, 2) // 指令按 4 字节对齐：log2(4) = 2
		dst = appendU32(dst, 0) // 重定位表偏移
		dst = appendU32(dst, 0) // 重定位数量
		dst = appendU32(dst, sAttrPureInstructions|sAttrSomeInstructions)
		dst = appendU32(dst, 0) // 保留字段 1
		dst = appendU32(dst, 0) // 保留字段 2
		dst = appendU32(dst, 0) // 保留字段 3
	}
	return dst
}

func appendDylinker(dst []byte, path string) []byte {
	cmdsize := (12 + len(path) + 1 + 7) &^ 7 // Load Command 按 8 字节对齐
	dst = appendU32(dst, lcLoadDylinker)
	dst = appendU32(dst, uint32(cmdsize))
	dst = appendU32(dst, 12) // 路径从命令起始偏移 12 处开始
	dst = append(dst, path...)
	dst = append(dst, 0)
	for len(dst)%8 != 0 {
		dst = append(dst, 0)
	}
	return dst
}

// appendEmptySymtab 写入空的 LC_SYMTAB 命令（24 字节）。
// 当前没有符号和字符串表，相关数量与偏移均为 0。
func appendEmptySymtab(dst []byte) []byte {
	dst = appendU32(dst, lcSymtab)
	dst = appendU32(dst, 24)
	for i := 0; i < 4; i++ { // symoff、nsyms、stroff、strsize
		dst = appendU32(dst, 0)
	}
	return dst
}

// appendEmptyDysymtab 写入空的 LC_DYSYMTAB 命令（80 字节）。
// M0 没有动态符号及重定位，18 个字段全部置零。
func appendEmptyDysymtab(dst []byte) []byte {
	dst = appendU32(dst, lcDysymtab)
	dst = appendU32(dst, 80)
	for i := 0; i < 18; i++ {
		dst = appendU32(dst, 0)
	}
	return dst
}

func appendName16(dst []byte, name string) []byte {
	var buf [16]byte
	copy(buf[:], name)
	return append(dst, buf[:]...)
}

func appendU32(dst []byte, v uint32) []byte {
	return binary.LittleEndian.AppendUint32(dst, v)
}

func appendU64(dst []byte, v uint64) []byte {
	return binary.LittleEndian.AppendUint64(dst, v)
}
