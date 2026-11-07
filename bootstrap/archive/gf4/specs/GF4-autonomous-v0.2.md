# GF4 — 真正 Go-free 的完整源码多代自举

**目标：** 仅以一个已验证的原生 Mio Seed 和完整 `.mio` 编译器源码，在不运行 Go、Mochi VM 或其它代码生成器的环境中，完成真实 Seed→A→B→C→D 并达到原生固定点。

## 重要区别

GF0/M18/M19c 曾经验证 **Mochi → Native AOT** 的固定点，GF4 必须每一代从**完整 `.mio` 源码**经各代自身原生前后端生成新 `mio`。不要将只输入 `.mochi` 的后端 Stage 4 冒充 GF4 Seed。

## 可信 Seed

- 来源必须可追溯：记录构建来源/版本、纯 Mio 原生符号与 Go Runtime 缺席、Binary SHA256、Mach-O 架构、签名/执行证明。
- Seed 可以来自一次性历史引导，但在 GF4 正式链开始后不允许 Go/Go VM/Go AOT 或下载备用工具参与。
- 固定完整源码清单，包括纯 Mio CLI、Frontend、AOT、Runtime、内嵌 Bunko 来源、编译选项、ABI/绑定/IR版本、时间与随机性输入。
- 对自嵌入资源引起的自指问题要使用确定性的构建输入/版本策略，不能通过忽略不匹配字节来制造固定点。

## 真正的各代执行

```text
Trusted Native Seed --完整 .mio 源码--> Compiler A (unsigned + signed)
Compiler A         --相同 .mio 源码--> Compiler B
Compiler B         --相同 .mio 源码--> Compiler C
Compiler C         --相同 .mio 源码--> Compiler D
```

- 实际执行 Seed/A/B/C 的**原生**编译行为，A/B/C 必须能够产生下一代；D 必须生成、Mach-O 验证与签名。若额外验证 D 也执行，单独记录，不能臆造。
- 每代既输出用于比较的完整**未签名** Mach-O，又输出已签名、真正可运行的镜像；事先固定比较口径与 SHA 生成方案。
- **B=C=D**（完整 unsigned Mach-O 字节逐字节一致）。如果固定点未收敛，分析输入/嵌入资源/地址/排序/时间差异，不得简单覆盖黄金哈希或抹掉差异区域。
- 文件名不同不会导致不可重现嵌入路径/时间戳差异；否则需要显式设计确定性路径映射，保持同等完整的字节比较。

## 进程与资源隔离

- 创建仅许可 Seed、目标 Stage、审计工具和 OS 基础 API 的受控环境，跟踪并限制外部编译器/链接器/VM 进程调用；普通用户编译不得依赖工作树内 Go CLI 或源码路径。
- 允许由运行器驱动各代进程，但各代的内部编译不能 fork 第二个编译器；`mio run` 启动**用户程序**与这里的禁外部编译规则不冲突。
- 正式 GF4 自举沿用经过验证的 1 GiB 显式 managed 容量协议（若源码实现改动必须记录等价新预算）；384 MiB 辅助 GC 见证可保留，不能让 256 MiB 旧失败阻断已合法配置的正式链。
- 检查每代精确 Root、Shadow Frame 退出清空、LOS/大对象、Young/Old、OOM、函数值/Map/泛型/窄整数/浮点、NativeFS 模块加载与文件写入。

## GF4 PASS

不存在“Go 仅用于每代生成 Mochi”之类隐藏路径；每代从 `.mio` 完整源码编译下一代、无 Go/VM/外部链接器，B=C=D 全 unsigned 字节一致、各代实际执行与签名证据可验证，GC/LOS/OOM/语义不回退。保存哈希/命令/日志与生成链来源，**自动继续 GF5**。
