# kiso/encoding/mochi

这是 Mochi v1–v14 的纯 Mio Decoder，其中 v14 是当前写入格式。模块不调用 Go 的
`bytes.Buffer`、`encoding/binary` 或 `unsafe`，在 Mochi VM 与 Darwin ARM64 Native AOT
中执行同一份 `.mio` 实现。

公开接口：

- `decode([]uint8) -> DecodeResult`：解码并完整验证内存数据；不修改输入。
- `decode_file(string) -> DecodeResult`：通过 NativeFS 分块读取，在分配前执行 64 MiB
  文件上限。
- `canonical_bytes(File) -> []uint8`：从解码结构确定性重编码，可用于完整结构比较。
- `summarize(File) -> Summary`：返回版本、常量/函数/全局数量、总指令字节数和规范化大小。
- `error_code(DecodeError) -> int64`：为 VM/Native 差分测试提供稳定错误分类。

64 位整数和浮点常量按原始 `U64` 高低字保留，因此所有 NaN payload、负零和最大整数
位模式都能无损规范化。字符串和函数名额外执行严格 UTF-8 验证。

验证覆盖文件头、版本、资源限制、常量标签、函数 ID/名称/槽位/标志/捕获、全部 65 个
Opcode、版本门槛、操作数宽度、常量/全局/局部/函数/Upvalue 索引、Closure 捕获来源、
Math/Data/Integer 子操作、数组元素描述符、返回指令种类，以及跳转范围和指令边界。
尾随数据会被拒绝。
