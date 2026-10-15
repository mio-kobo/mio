# kiso/bytes v0.2

`kiso/bytes` 提供紧凑 `[]uint8` 的比较、查找、复制、克隆与可复用缓冲区。

`ByteBuffer` 只保存共享的 `[]uint8` 字段 `data`，长度和容量都由数组维护。值复制会共享数组，`reset` 保留容量，`buffer_clone` 和 `buffer_bytes` 返回独立后备存储。

Mio 当前没有结构体字段可见性规则，因此公开结构体的 `data` 字段也可由调用方访问。需要隔离可变存储时应使用 `buffer_bytes`。

批量追加和截断由 `kiso/array` 的通用 VM intrinsic 实现，其余 ByteBuffer 行为使用 Mio 源码组合完成，没有 bytes 专属 Opcode。
