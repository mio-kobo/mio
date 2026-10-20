# kiso/aot/lower

纯 Mio Mochi v1–v14 lowering。输入 `kiso/encoding/mochi.File`，输出交给
`kiso/aot/macho`；目标机器码、编码和镜像写入不经过 Go AOT。

当前实现 int64/bool 常量与整数运算、分支、全局/局部、静态调用及栈参数、
Nullable/type-106 box、M16b Struct、Array 与 Enum allocation/tag，以及 NativeFS MMA bridge。
Array 支持可证明布局的 literal/make/with_capacity/push/capacity/reserve/clone/truncate/append_all、
索引和写入。非 nullable uint8 使用紧凑 byte storage，其余使用 canonical 8-byte slot。
增长遵循至少 4、低于 1024 加倍、之后增加 50%；引用尾槽清零。

所有分配与调用使用求解后的精确 Shadow Frame；type 101/102 Descriptor/Storage、
100 Struct、106 box 和 107 Enum 由有界 Heap、mark/sweep 与 32-bin free list 管理。
`compile(file)` 为普通模式，`compile_gc_stress(file)` 在每次分配入口额外收集。
字符串槽支持 tagged ID 与动态 Descriptor，mark 跳过 tagged ID。

Closure/共享 Cell/间接调用、Enum payload 类型细化、FP 运算及 DataConvert 仍 fail closed。
未知或不匹配的数组布局也明确拒绝；当前不能描述为完整 Typed Array 或 Compiler A 后端。
Compiler A 最新首个阻塞为 `kiso/io::write_all@67 OpCallValue 3`（detail 20006745）。

完整覆盖矩阵、真实执行回归和验收状态见 `docs/NATIVE_AOT_M17E.md`。
