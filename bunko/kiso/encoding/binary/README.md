# kiso/encoding/binary

该模块使用纯 Mio 在现有整数类型和紧凑 `[]uint8` 上实现二进制编解码。固定宽度 API 支持 `uint8`、`uint16`、`uint32`、`int8`、`int16`、`int32`、`int64`，多字节类型提供 Little Endian 与 Big Endian 版本，所有访问都使用显式 offset 并返回 `BinaryError`。

公开的固定宽度 API：

- `read_u8`、`write_u8`
- `read_u16_le`、`read_u16_be`、`write_u16_le`、`write_u16_be`
- `read_u32_le`、`read_u32_be`、`write_u32_le`、`write_u32_be`
- `read_i8`、`write_i8`
- `read_i16_le`、`read_i16_be`、`write_i16_le`、`write_i16_be`
- `read_i32_le`、`read_i32_be`、`write_i32_le`、`write_i32_be`
- `read_i64_le`、`read_i64_be`、`write_i64_le`、`write_i64_be`

读取返回对应的 `U8Result`、`U16Result`、`U32Result`、`I8Result`、`I16Result`、`I32Result` 或 `I64Result`。写入返回 `BinaryError?`，不会扩容目标数组。

`write_var_u32`/`read_var_u32` 使用无符号 LEB128，最多 5 字节。`write_var_i64`/`read_var_i64` 使用 DWARF 风格的有符号 LEB128，最多 10 字节。解码拒绝截断、溢出、超长和非规范编码。两种 Varint 不是 ZigZag 编码。

实现不会为固定宽度读取复制输入数组，也不会增加 `uint64` 或更宽的 Core 类型。`int64` 编解码使用有符号最高字节和向负无穷取整的逐字节算法，因此覆盖 MinInt64 的完整位模式。
