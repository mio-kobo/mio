# kiso/utf8 v0.1

`kiso/utf8` 使用 Mio 源码实现严格 UTF-8 编解码，输入输出均为紧凑 `[]uint8`。模块只处理 Unicode 标量值和 UTF-8 字节，不处理规范化、大小写折叠、字素簇或显示宽度。

公开 API：

- `DecodeResult { value: rune, width: int64, valid: bool }`
- `decode_one(data, offset)`
- `encode_rune(value)`
- `append_rune(destination, value)`
- `valid(data)`
- `rune_count(data)`
- `rune_width(value)`
- `is_valid_rune(value)`

`decode_one` 成功时返回 1–4 字节宽度及 `valid = hai`。非法 UTF-8 返回 U+FFFD、宽度 1 及 `valid = iie`，调用方可按单字节继续扫描。`offset == len(data)` 表示正好到达末尾，返回 U+FFFD、宽度 0 及 `valid = iie`；其他越界 offset 产生数组边界运行时错误。

模块内部使用单个 `int64` 携带码点和宽度，负值分别表示非法输入与末尾位置。`decode_one` 只在公开 API 边界构造 `DecodeResult`，`valid` 和 `rune_count` 直接使用标量结果。

U+FFFD 本身是合法标量值，编码或解码成功时仍返回 `valid = hai`。`rune_count` 对合法输入返回码点数量，对非法输入返回 `mu`。`rune_width`、`encode_rune` 和 `append_rune` 遇到负值、UTF-16 代理码点或大于 U+10FFFF 的 rune 时产生运行时错误；`append_rune` 在报错前不会修改目标数组。

编码使用 `kiso/bytes.ByteBuffer` 预留精确容量，追加使用 `kiso/array` 的动态数组操作。模块没有 UTF-8 专属 Opcode。
