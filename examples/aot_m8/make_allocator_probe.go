// Mochi AOT M8：生成实际调用原生 mmap/munmap 的内存分配器自检程序。
// 这个程序不使用新的 Mochi Opcode，也不会静默跳过真实内存分配。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"Mio/internal/aot/darwinarm64"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "M8:", err)
		os.Exit(1)
	}
}

func run() error {
	output := flag.String("o", "", "输出的 macOS ARM64 原生 Mach-O 文件")
	mode := flag.String("mode", "smoke", "原生测试场景：smoke、limits、metadata")
	flag.Parse()
	if *output == "" || flag.NArg() != 0 {
		return fmt.Errorf("usage: go run ./examples/aot_m8/make_allocator_probe.go -mode smoke -o build/m8-smoke")
	}
	image, err := darwinarm64.M8AllocatorProbe(*mode)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(*output), ".m8-aot-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(image); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Chmod(0o755); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(temp.Name(), *output); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes, unsigned Mach-O arm64, M8 %s)\n", *output, len(image), *mode)
	return nil
}
