// M10 独立原生 GC 自检：不修改 Mochi 字节码，也不在 Go 中预计算回收结果。
package main

import (
	"flag"
	"fmt"
	"os"

	"Mio/internal/aot/darwinarm64"
)

func main() {
	mode := flag.String("mode", "smoke", "M10 原生自检场景；见 docs/NATIVE_AOT_M10.md")
	output := flag.String("o", "build/m10-smoke", "Mach-O 输出路径")
	flag.Parse()
	image, err := darwinarm64.M10GCProbe(*mode)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*output, image, 0755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d bytes, unsigned Mach-O arm64, M10 %s)\n", *output, len(image), *mode)
}
