// mochi-aot M2 命令行入口：将受支持的 Mochi 字节码编译为 macOS ARM64 可执行文件。
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"Mio/internal/aot"
	"Mio/internal/mochi"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("mochi-aot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("o", "", "native executable output file")
	target := flags.String("target", "darwin-arm64", "target architecture (darwin-arm64 only)")
	m11 := flags.Bool("m11-gc", false, "enable M11a integrated native GC lifecycle self-test")
	gcStress := flags.Bool("gc-stress", false, "force precise GC at every safe object allocation")
	heapMiB := flags.Uint64("heap-mib", 1, "managed heap capacity in MiB (hard limit 1024)")
	gcTelemetry := flags.Bool("gc-telemetry", false, "write allocation/GC diagnostics to stderr")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 || *out == "" || *target != "darwin-arm64" || *m11 && *gcStress {
		fmt.Fprintln(stderr, "mochi-aot: usage: go run ./cmd/mochi-aot -o <output> [-target darwin-arm64] [-heap-mib 1..1024] [-gc-telemetry] [-m11-gc|-gc-stress] <input.mochi>")
		return 1
	}
	if *heapMiB == 0 || *heapMiB > 1024 {
		fmt.Fprintln(stderr, "mochi-aot: heap-mib must be between 1 and 1024")
		return 1
	}
	input, err := os.Open(flags.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "mochi-aot: open input:", err)
		return 1
	}
	defer input.Close()

	bytecode, err := mochi.Decode(input)
	if err != nil {
		fmt.Fprintln(stderr, "mochi-aot: decode Mochi:", err)
		return 1
	}
	var native []byte
	if *m11 {
		native, err = aot.CompileDarwinARM64WithM11Runtime(bytecode)
	} else {
		native, err = aot.CompileDarwinARM64WithOptions(bytecode, aot.DarwinARM64Options{
			HeapCapacity: *heapMiB << 20,
			GCTelemetry:  *gcTelemetry,
			GCStress:     *gcStress,
		})
	}
	if err != nil {
		fmt.Fprintln(stderr, "mochi-aot:", err)
		return 1
	}
	if err := writeExecutable(*out, native); err != nil {
		fmt.Fprintln(stderr, "mochi-aot:", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote %s (%d bytes, unsigned Mach-O arm64)\n", *out, len(native))
	fmt.Fprintln(stdout, "on macOS, run: codesign --force --sign - "+*out)
	return 0
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
