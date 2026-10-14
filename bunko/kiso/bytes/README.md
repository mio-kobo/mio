# kiso/bytes v0.1

`kiso/bytes` 提供紧凑 `[]uint8` 的基础比较、查找和原地修改算法。模块完全使用 Mio 源码实现，仅在显式导入时加载，不增加 Core 或 VM Opcode。

```mio
yobu "kiso/bytes";
```

## API

- `equal(left: []uint8, right: []uint8) -> bool`：长度和内容都相同才返回 `hai`。
- `compare(left: []uint8, right: []uint8) -> int64`：按无符号字节进行字典序比较，返回 `-1`、`0` 或 `1`。
- `index(values: []uint8, target: uint8) -> int64`：返回首次出现位置，未找到时返回 `-1`。
- `contains(values: []uint8, target: uint8) -> bool`：报告目标字节是否存在。
- `has_prefix(values: []uint8, prefix: []uint8) -> bool`：报告是否具有指定前缀；空前缀始终匹配。
- `has_suffix(values: []uint8, suffix: []uint8) -> bool`：报告是否具有指定后缀；空后缀始终匹配。
- `copy(destination: []uint8, source: []uint8) -> int64`：将 `min(len(destination), len(source))` 个字节复制到目标开头并返回数量。目标数组会原地修改，已有别名可观察到修改。
- `fill(values: []uint8, value: uint8) -> unit`：原地填充整个数组，已有别名可观察到修改。

所有参数都是非可空数组，元素也是非可空 `uint8`。索引由实现内部保证在边界内，API 不产生数组越界错误。

## 当前边界

Mio 当前能创建有明确类型的空数组和数组字面量，但不能按运行时长度创建数组，也没有容量、追加或扩容操作。因此 v0.1 不提供 `clone`：返回原数组会违反独立副本语义，固定长度占位数组或反复复制也不是可接受的实现。

同一限制阻止了高效且语义可靠的 ByteBuffer。数组共享后备存储，而结构体按值复制；在没有可增长数组及共享长度状态前，ByteBuffer 的副本可能共享数据却持有不同长度。正式实现至少需要通用的按长度数组创建能力，以及具有明确摊还复杂度的容量增长或追加能力；这些应属于数组基础能力，而不是 bytes 专属 Opcode。
