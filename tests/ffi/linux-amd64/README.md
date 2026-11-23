# Linux x86-64（System V AMD64）C ABI 验收

使用已从待测源码构建的 Native Mio，在 Linux x86-64 主机上执行：

```sh
python3 tests/ffi/linux-amd64/run.py \
  --mio /path/to/current-source/mio \
  --output build/ffi-linux
```

测试用 C 源码位于 `fixtures/`，由主机的 GCC 和（如果安装了）Clang 编译成目标文件、静态库和共享库；Mio 程序负责发出参数并核对结果，生成的 ELF 可执行文件被实际运行。`scalars.c` 与 Darwin 夹具函数名一致，共用 `tests/ffi/darwin-arm64/cases/scalars.mio` 和它的期望输出；`sysv.c` 含任务规定的 `mio_c_*` 函数和读取入口栈的汇编探针，用来验证栈对齐、栈参数位置、窄整数返回值的扩展。

可选参数：`--reference` 指向 Go Reference CLI（`go build ./cmd/mio`），验证同一批原型的静态检查和 C 调用的执行拒绝；`--baseline` 指向基线 Native 编译器，验证普通程序（Linux x86-64 以及其他五个目标）的产物逐字节一致。没有 Clang、没有基线或没有 Go Reference 时对应检查报告 SKIP，不计为通过。Darwin ARM64 的 C ABI 在本主机上报告 `NOT_APPLICABLE`。

原始编译与执行日志和 `report.json` 保存在指定输出目录。完整说明见 [Linux x86-64 原生 C ABI](../../../docs/linux-amd64-c-ffi.md)。
