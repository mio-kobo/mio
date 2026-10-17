// Mochi AOT M9：生成真正使用原生类型描述符、Root Map 和 Mark 例程的自检程序。
// 目前仅生成独立 Mach-O，不添加 Mochi Opcode，也不回收对象。
package main

import (
	"Mio/internal/aot/darwinarm64"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	mode := flag.String("mode", "roots", "测试模式：roots、cycles、reset、interior")
	output := flag.String("o", "", "输出的 macOS ARM64 可执行文件")
	flag.Parse()
	if *output == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: go run ./examples/aot_m9/make_gc_probe.go -mode roots -o build/m9-roots")
		os.Exit(2)
	}
	image, err := darwinarm64.M9GCProbe(*mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, "M9:", err)
		os.Exit(1)
	}
	f, err := os.CreateTemp(filepath.Dir(*output), ".m9-aot-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(image); err == nil {
		err = f.Chmod(0o755)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), *output)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, unsigned Mach-O arm64, M9 %s)\n", *output, len(image), *mode)
}
