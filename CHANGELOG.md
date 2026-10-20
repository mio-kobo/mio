# Changelog

本文件记录 Mio 的公开版本变化。

## 2.0.0 - 发布准备（尚未发布）

- 新增已在 macOS ARM64 验证的 Native AOT 和 Mach-O 输出，保留 Mochi v14 与 v1–v14 解码兼容回归。
- M18 完成纯 Mio Decoder / Lowering / ARM64 Encoder / Mach-O Writer 原生自举：Seed 后 Stage 1–5 达到机器码固定点，正式 Go AOT 仅生成 Seed。
- 原生运行时包含精确 Mark-Sweep GC、Shadow Frame、typed Storage、mmap managed arena；256 MiB 自编译见证发生 GC 后仍重建相同产物。正式自举协议为 1 GiB。
- 提供 CLI、预编译 Go AOT 工具、纯 Mio 原生 AOT 编译器、Bunko、许可、构建来源与校验清单的候选包；本地打包和隔离无 Go 安装 smoke 不创建远程 Release。
- CLI / help / LSP 核心版本更新为 2.0.0；内置 LSP 和结构化诊断沿用，插件独立版本不跟随核心更新。
- 限制：源码前端与 VM 仍为 Go 实现，源码重建需 Go；AOT 不是 VM 全语言面的替代品；仅实测 macOS Apple Silicon；当前 ad-hoc 签名，未 Developer ID 签名或公证。
- 升级：重新编译字节码并检查 AOT 子集差异，不承诺任意 V1 程序无修改转为 Native；历史 beta 的多平台发行不构成 V2 平台承诺。未启动 M19 分代 GC，未实现三档 GC 配置。
- 完整安装、已知限制、验证结果见 `docs/releases/v2.0.0.md` 和 `v2.0.0-readiness.md`。

## 1.0.0-beta.1 - 2026-10-17

首个 v1 Beta 预发布版本，面向测试、兼容性验证与反馈收集，不建议用于生产环境。

### 编译器与自举

- 提供以纯 Mio 编写的编译器，实现 Lexer、Parser、Resolver、TypeChecker、Constant Evaluator 与 Mochi Codegen。
- 完成 Go 引导的 Compiler A → B → C → D 连续自编译；Compiler B、C、D 字节完全一致，并通过解码、结构验证与执行测试。
- 保留 Go 引导编译器用于首次构建与交叉验证；v1 编译产物仍由 Go 实现的 Mochi VM 执行。

### 语言与字节码

- 支持固定宽度整数、`float32` / `float64`、`rune`、可空类型、动态数组、Map、结构体、枚举和函数值。
- 支持泛型函数与数据类型、数值约束、词法闭包与共享捕获 Cell、目录包、显式导入、公开成员和编译期常量。
- Mochi 更新至 v14，解码器保持 v1–v14 兼容，并继续执行正式的栈、调用深度与指令预算安全限制。

### Bunko、MMA 与测试

- 提供 11 个 Bunko 包：`array`、`bytes`、`convert`、`encoding/binary`、`fs`、`io`、`math`、`path`、`string`、`testing` 与 `utf8`。
- MMA 提供稳定符号绑定、签名指纹、受控字节数组访问和带代次校验的不透明 Resource 生命周期。
- 建立 Fast、Full、Stress、Race 与 A → B → C → D Bootstrap 分级测试体系，覆盖 MinInt64、`float32` 正确舍入、泛型与闭包、Bunko、MMA 及 Mochi v1–v14。

### 工具与发布

- CLI 提供 `check`、`run`、`build`、`exec`、`disasm` 与 `test`，支持结构化诊断和三语言 i18n。
- JetBrains IDE 插件 0.6.0 提供语法高亮、补全、导航、诊断、运行配置、新项目创建与 SDK 管理。
- Release 工作流打包 CLI、Bunko、MIT License 与 JetBrains 插件，并对预发布 Tag 创建 GitHub Pre-release。

### 已知限制

- 当前不是 Native AOT 工具链，运行 Mochi 字节码仍依赖 Go 实现的 VM；独立 Runtime 与 GC 尚未实现。
- trait、HKT、型变、反射、异常、JIT、多维数组、切片和字符串修改尚未实现。
- LSP 与 VS Code 扩展仍在规划中。
- Mochi 文件没有校验和或数字签名；CLI 尚未提供指令预算的命令行配置。

### 许可

- 项目代码采用根目录中的 MIT License；Mio / Mochi 角色插画不随代码纳入 MIT License，须单独取得使用许可。

## 0.1.0-alpha.1 - 2026-10-15

首次公开实验版本。

### 语言

- 加入 `int64`、`bool`、`string`、`nil` 和 `unit` 的静态类型规则。
- 加入变量声明与赋值、词法作用域、条件分支和短路逻辑。
- 加入顶层函数、参数与返回类型、递归和相互递归。
- 加入 `chant` 内置输出函数。

### 工具链

- 提供 `check`、`run`、`build`、`exec` 和 `disasm` CLI 命令。
- 提供 Mochi v2 编码，并兼容读取 Mochi v1。
- 提供 Linux、Windows 和 macOS 的发布构建脚本。

### 运行时

- 加入 Value Stack、Stack Frame、全局与局部槽位检查。
- 加入递归深度、指令预算和执行取消机制。
- 加入非法字节码、跳转边界、返回指令和未初始化槽位检查。
- 明确 `int64` 二进制补码回绕语义，并保留除零错误。

### 已知限制

- 没有循环、集合、用户定义类型、模块、导入、闭包或异常处理。
- Mochi 文件没有校验和或数字签名。
- CLI 尚未提供指令预算的命令行配置。
- 项目许可证尚未由作者确定。
