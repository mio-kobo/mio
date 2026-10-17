// Package aot 将通过校验的 Mochi 字节码编译为原生程序。
// M0 暂时只支持空程序，运行后以状态码 0 退出。
package aot

import (
	"fmt"

	"Mio/internal/aot/darwinarm64"
	"Mio/internal/compiler"
	"Mio/internal/mochi"
)

// CompileDarwinARM64 将空的 Mochi 程序编译为 macOS ARM64 可执行文件。
// 尚未支持的指令必须明确报错，不能把非空程序静默替换成 exit(0)。
func CompileDarwinARM64(program *compiler.Bytecode) ([]byte, error) {
	if err := mochi.Validate(program); err != nil {
		return nil, fmt.Errorf("invalid Mochi bytecode: %w", err)
	}
	if len(program.Instructions) != 0 || len(program.Constants) != 0 ||
		program.GlobalsCount != 0 || len(program.Functions) != 0 {
		return nil, fmt.Errorf("AOT M0 supports only an empty Mochi program (got %d instruction bytes, %d constants, %d globals, %d functions)",
			len(program.Instructions), len(program.Constants), program.GlobalsCount, len(program.Functions))
	}
	return darwinarm64.ExitProgram(0)
}
