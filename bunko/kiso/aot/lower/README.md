# kiso/aot/lower

纯 Mio Mochi v1–v14 lowering。输入 `kiso/encoding/mochi.File`，输出交给
`kiso/aot/macho`；目标机器码、编码和镜像写入不经过 Go AOT。

当前实现 int64/bool 常量与整数运算、分支、全局/局部、静态调用及栈参数、
Nullable/type-106 box、M16b Struct、Array 与 Enum allocation/tag，以及 NativeFS MMA bridge。
Array 支持可证明布局的 literal/make/with_capacity/push/capacity/reserve/clone/truncate/append_all、
索引和写入。非 nullable uint8 使用紧凑 byte storage，其余使用 canonical 8-byte slot。
增长遵循至少 4、低于 1024 加倍、之后增加 50%；引用尾槽清零。

所有分配与调用使用求解后的精确 Shadow Frame；type 101/102 Descriptor/Storage、
100 Struct、103 Closure / 104 Environment / 105 Cell、106 box 和 107 Enum 由有界 Heap、mark/sweep 与 32-bin free list 管理。
`compile(file)` 为普通模式，`compile_gc_stress(file)` 在每次分配入口额外收集。
managed arena 独立 mmap，默认 128 MiB；`compile_with_heap(file, capacity, gc_stress)`
显式配置 64 bytes..1 GiB、16-byte 对齐的目标容量。对象连续跨 OS 页；sweep 合并空闲区并
回退空闲尾部，allocator 拆分大空闲块。分配失败仍 collect/retry 一次后明确 OOM。
字符串槽支持 tagged ID 与动态 Descriptor，mark 跳过 tagged ID。

Closure/共享 Cell/间接调用已接入 M15b 的 type-103/104/105、x25 隐藏环境与精确 Root 协议；
支持函数目标/签名/capture 固定点和按 awase tag 收窄的 Enum payload。DataConvert、六类 FP 转换、
基本浮点运算/比较及 Math is_nan/is_inf/is_finite/error 已迁入并有定向 ARM64 差分。
未知或不匹配的数组布局也明确拒绝；完整通用数值库和任意多态函数值仍不在已验证范围。
Array 元素表示现在区分数值基类型关系与实际 Value 流；OpIsNil 不制造 nullable 约束，
CFG 非 nil 分支上的 scalar GetLocal 使用 checked unbox，避免把普通 int64 元素误并为 boxed nullable。
ARM64 条件 fixup 固定预留两条指令，远分支反转条件再用 B，保留 imm26 检查。
泛型数组转发体和浮点谓词按完整字节码匹配，在直接调用点使用具体表示；函数可达性保守保留
Call/Function/Closure 目标，未知可达布局不放宽。真实纯 Mio Native A→B→C→D 已通过，
B=C=D Mochi 黄金哈希一致，B/C 原生镜像一致且都实际执行；M18 未启动。

完整覆盖矩阵、真实执行回归和验收状态见 `docs/NATIVE_AOT_M17E.md`。
