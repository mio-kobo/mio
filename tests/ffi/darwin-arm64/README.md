# Darwin ARM64 C ABI 验收

使用已从待测源码构建的 Native Mio，在 Apple Silicon macOS 上执行以下命令。

```sh
python3 tests/ffi/darwin-arm64/run.py \
  --mio /path/to/current-source/mio \
  --output build/ffi-results
```

可选的 `--reference` 指向 Go Reference，验证同一批原型的静态检查与 C 调用执行错误。`--baseline` 指向基线 Native 编译器，验证普通 Darwin ARM64 样例的可执行文件逐字节一致。测试用 C 源码位于 `fixtures/`，Mio 程序负责发出参数和核对结果。

原始编译与执行日志和 `report.json` 保存在指定输出目录。使用新的输出目录保留多次运行的记录。统一 CI 入口和类型约定见 [Darwin ARM64 原生 C ABI](../../../docs/darwin-arm64-c-ffi.md)。
