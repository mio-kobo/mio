# kiso/string v0.1

`kiso/string` 提供 UTF-8 字符串的字节转换、码点计数、字节语义查找、拼接与可复用 `StringBuilder`。

公开 API：

- `byte_len(value)`、`rune_count(value)`
- `has_prefix(value, prefix)`、`has_suffix(value, suffix)`
- `contains(value, pattern)`、`index(value, pattern)`、`concat(left, right)`
- `to_bytes(value)`、`from_utf8(data)`
- `StringBuilder`
- `new_builder()`、`builder_with_capacity(capacity)`
- `write_string(builder, value)`、`write_rune(builder, value)`
- `builder_len(builder)`、`builder_capacity(builder)`、`builder_reset(builder)`、`builder_string(builder)`

`index` 返回 UTF-8 字节偏移，未找到时返回 `-1`。模块不进行 Unicode 规范化、大小写折叠或字素簇处理。

`StringBuilder` 组合 `kiso/bytes.ByteBuffer`，追加和重置遵守数组的共享引用语义。Mio 当前没有结构体字段可见性规则，因此 `buffer` 及其 `data` 字段可以由调用方访问。外部写入非法 UTF-8 后，`builder_string` 会报告运行时错误；成功导出的字符串总是独立不可变副本。

Core 只提供受限的 `string` 与紧凑 `[]uint8` 复制转换，以及 O(1) 字节长度读取。UTF-8 验证复用 `kiso/utf8`，Core 在创建字符串前还会进行防御性校验。码点统计、查找、拼接和 Builder 组合逻辑均由 Mio 源码实现。

正常 Mio 源码和 Mochi 常量只能产生合法 UTF-8 字符串。若宿主通过 MMA 传入包含非法 UTF-8 的 Go 字符串，字节语义 API 会保留原始字节，`rune_count` 和需要导出字符串的 Builder 操作会报告运行时错误。
