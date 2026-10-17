// mochi-aot M0 命令行入口：将空 Mochi 程序编译为 macOS ARM64 可执行文件。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"Mio/internal/aot"
	"Mio/internal/mochi"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mochi-aot:", err)
		os.Exit(1)
	}
}

func run() error {
	out := flag.String("o", "", "native executable output file")
	target := flag.String("target", "darwin-arm64", "target architecture (M0: darwin-arm64 only)")
	flag.Parse()
	if flag.NArg() != 1 || *out == "" || *target != "darwin-arm64" {
		return fmt.Errorf("usage: go run ./cmd/mochi-aot -o <output> [-target darwin-arm64] <input.mochi>")
	}
	input, err := os.Open(flag.Arg(0))
	if err != nil {
		return fmt.Errorf("open input: %w", err)
	}
	defer input.Close()

	bytecode, err := mochi.Decode(input)
	if err != nil {
		return fmt.Errorf("decode Mochi: %w", err)
	}
	native, err := aot.CompileDarwinARM64(bytecode)
	if err != nil {
		return err
	}
	if err := writeExecutable(*out, native); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes, unsigned Mach-O arm64)\n", *out, len(native))
	fmt.Println("on macOS, run: codesign --force --sign - " + *out)
	return nil
}

func writeExecutable(path string, image []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".mochi-aot-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(image); err != nil {
		temp.Close()
		return fmt.Errorf("write image: %w", err)
	}
	if err := temp.Chmod(0o755); err != nil {
		temp.Close()
		return fmt.Errorf("chmod image: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close image: %w", err)
	}
	if err := os.Rename(temp.Name(), path); err != nil {
		return fmt.Errorf("rename image: %w", err)
	}
	return nil
}
