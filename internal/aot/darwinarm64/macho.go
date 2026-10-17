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

	// M2 固定一页 __DATA，暂时不实现多页全局变量区域。
	MaxGlobalSlots = int(pageSize / 8)

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
	vmProtWrite     uint32 = 2
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

// ExecutableWords 保留 M0/M1 原有的单页可执行文件格式。
func ExecutableWords(words []uint32) ([]byte, error) {
	return ExecutableWordsWithGlobals(words, 0)
}

// ExecutableWordsWithGlobals 将机器码封装为 macOS ARM64 Mach-O。
// 全局变量放在独立的可写 __DATA 段，每个槽位占 8 字节，初始内容为零。
// 不使用绝对机器地址；访存指令通过 ADRP 按 PC 相对地址定位数据。
func ExecutableWordsWithGlobals(words []uint32, globalsCount int) ([]byte, error) {
	if len(words) == 0 {
		return nil, fmt.Errorf("empty ARM64 instruction stream")
	}
	if globalsCount < 0 || globalsCount > MaxGlobalSlots {
		return nil, fmt.Errorf("AOT M2 global slot count %d exceeds limit %d", globalsCount, MaxGlobalSlots)
	}
	codeSize := uint64(len(words)) * 4
	hasData := globalsCount != 0

	commands := make([]byte, 0, 768)
	ncmds := uint32(0)

	// 低地址保留空洞，避免空指针访问意外映射到合法页。
	commands = appendSegment(commands, segment{
		name: "__PAGEZERO", vmaddr: 0, vmsize: textVMAddr,
	}, false, codeSize)
	ncmds++

	// __TEXT 包含 Mach-O 头、Load Commands 和原生机器码。
	commands = appendSegment(commands, segment{
		name: "__TEXT", vmaddr: textVMAddr, vmsize: pageSize,
		fileoff: 0, filesize: pageSize,
		maxprot:  vmProtRead | vmProtExecute,
		initprot: vmProtRead | vmProtExecute,
	}, true, codeSize)
	ncmds++

	// M2 只在确实存在全局槽位时，才创建可写的 __DATA 页。
	// __data 段为文件映射，保证下游 codesign 可以覆盖全部数据页。
	linkeditFileOff := pageSize
	linkeditVMAddr := textVMAddr + pageSize
	if hasData {
		commands = appendDataSegment(commands, globalsCount)
		ncmds++
		linkeditFileOff += pageSize
		linkeditVMAddr += pageSize
	}

	// dyld 会在 __LINKEDIT 后方追加代码签名；保持单独的只读区域。
	commands = appendSegment(commands, segment{
		name: "__LINKEDIT", vmaddr: linkeditVMAddr,
		vmsize: pageSize, fileoff: linkeditFileOff,
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
	commands = appendU32(commands, 0)
	ncmds++

	// 即使没有动态重定位，仍提供空的符号表命令，避免 dyld 回退路径崩溃。
	commands = appendEmptySymtab(commands)
	ncmds++
	commands = appendEmptyDysymtab(commands)
	ncmds++

	commands = appendU32(commands, lcMain)
	commands = appendU32(commands, 24)
	commands = appendU64(commands, entryOffset)
	commands = appendU64(commands, 0)
	ncmds++

	// 为 codesign 后续插入 LC_CODE_SIGNATURE 预留 16 字节。
	if headerSize+len(commands)+16 > int(entryOffset) {
		return nil, fmt.Errorf("mach-o load commands encroach on the entrypoint")
	}
	if entryOffset+codeSize > pageSize {
		return nil, fmt.Errorf("arm64 code does not fit in the __TEXT segment")
	}

	// M0/M1 仍保持 16 KiB；M2 含全局变量时多一页 __DATA，共 32 KiB。
	fileSize := pageSize
	if hasData {
		fileSize += pageSize
	}
	file := make([]byte, int(fileSize))
	binary.LittleEndian.PutUint32(file[0:4], mhMagic64)
	binary.LittleEndian.PutUint32(file[4:8], cpuTypeArm64)
	binary.LittleEndian.PutUint32(file[8:12], cpuSubtypeAll)
	binary.LittleEndian.PutUint32(file[12:16], mhExecute)
	binary.LittleEndian.PutUint32(file[16:20], ncmds)
	binary.LittleEndian.PutUint32(file[20:24], uint32(len(commands)))
	binary.LittleEndian.PutUint32(file[24:28], mhFlags)
	binary.LittleEndian.PutUint32(file[28:32], 0)
	copy(file[headerSize:], commands)

	for i, instruction := range words {
		off := int(entryOffset) + i*4
		binary.LittleEndian.PutUint32(file[off:off+4], instruction)
	}
	return file, nil
}

// appendDataSegment 编码单页可写的 __DATA 与 __data Section。
// 只记录实际使用的全局槽位尺寸，其余空间保留作为页内填充。
func appendDataSegment(dst []byte, slots int) []byte {
	const cmdSize = 72 + 80
	const dataFileOff = pageSize
	const dataVMAddr = textVMAddr + pageSize
	dst = appendU32(dst, lcSegment64)
	dst = appendU32(dst, cmdSize)
	dst = appendName16(dst, "__DATA")
	dst = appendU64(dst, dataVMAddr)
	dst = appendU64(dst, pageSize)
	dst = appendU64(dst, dataFileOff)
	dst = appendU64(dst, pageSize)
	dst = appendU32(dst, vmProtRead|vmProtWrite)
	dst = appendU32(dst, vmProtRead|vmProtWrite)
	dst = appendU32(dst, 1)
	dst = appendU32(dst, 0)

	dst = appendName16(dst, "__data")
	dst = appendName16(dst, "__DATA")
	dst = appendU64(dst, dataVMAddr)
	dst = appendU64(dst, uint64(slots)*8)
	dst = appendU32(dst, uint32(dataFileOff))
	dst = appendU32(dst, 3)  // int64 槽位按 8 字节对齐。
	for i := 0; i < 6; i++ { // reloff, nreloc, flags, reserved1..3
		dst = appendU32(dst, 0)
	}
	return dst
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
