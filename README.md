<p align="center">
  <a href="assets/mascot/mio-and-mochi.png">
    <img src="assets/mascot/mio-and-mochi.webp" alt="Mio 与 Mochi：蓝白长发的 Mio 抱着白色 Mochi 小团子" width="300" />
  </a>
</p>

<h1 align="center">Mio</h1>

<p align="center"><strong>小巧的核心，广阔的数学与程序世界。</strong></p>
<p align="center"><em>Minimal Core · Modular Bunko · Pay for What You Use</em></p>

<p align="center">
  <a href="https://github.com/ExMikuPro/Mio/tree/Mio-1.0.0">Mio-1.0.0 开发分支</a>
  · <a href="https://github.com/ExMikuPro/Mio/releases">Releases</a>
  · <a href="editors/jetbrains/README.md">JetBrains 插件</a>
  · <a href="docs/BUNKO.md">Bunko 标准库</a>
</p>

<p align="center">
  <a href="https://github.com/ExMikuPro/Mio/actions/workflows/mio-ci-release-with-plugin.yml?query=branch%3AMio-1.0.0"><img alt="CI (Mio-1.0.0)" src="https://github.com/ExMikuPro/Mio/actions/workflows/mio-ci-release-with-plugin.yml/badge.svg?branch=Mio-1.0.0" /></a>
  <a href="https://github.com/ExMikuPro/Mio/releases"><img alt="Latest release" src="https://img.shields.io/github/v/release/ExMikuPro/Mio?include_prereleases&label=release" /></a>
  <img alt="Project status: beta" src="https://img.shields.io/badge/status-beta-orange" />
</p>

**Mio** 是一门正在开发的**静态强类型编程语言**，采用日语罗马音关键字，重视数学数值语义、清晰的类型规则和模块化标准库。Mio 的设计方向是用尽可能精简的语言 Core，组合按需导入的 **Bunko** 模块，支持从普通应用到数值计算的开发需求。

第一代工具链以 **Mochi 字节码**为执行目标。纯 Mio 编译器已经完成 Lexer、Parser、Resolver、TypeChecker、Constant Evaluator 与 Codegen，并成功走通 A → B → C → D 连续自编译；Compiler B、C、D 字节完全一致。Mio v1 仍通过 **Go 实现的 Mochi VM** 运行编译产物。

当前 `1.0.0-beta.1` 面向测试、兼容性验证与反馈收集，**不建议用于生产环境**。

## Mio 与 Mochi

上方角色图中，少女是 **Mio**，她抱着的小团子叫 **Mochi**。Mochi 既是项目吉祥物之一，也是 Mio 第一代字节码执行系统的名字。

- **Mio v1**：以纯 Mio 编写编译器，实现自编译，通过 Mochi VM 运行。
- **Mio v2（规划中）**：构建 Native AOT 编译器、独立 Runtime 与 GC，让最终生成的原生程序不再依赖 Mochi VM 和 Go Runtime。
- **角色形象**：即使将来更换执行后端，Mochi 也会作为 Mio 的伙伴留在项目中。

点击角色图片可以查看原始高清 PNG。角色美术与程序源代码的授权分别说明，见文末。

## 快速开始

开发分支需要 **Go 1.22.2 或兼容版本**进行首次源码构建：

```bash
git clone --branch Mio-1.0.0 https://github.com/ExMikuPro/Mio.git
cd Mio
go build -o mio ./cmd/mio
./mio version
```

版本命令应输出：

```text
Mio 1.0.0-beta.1
```

新建 `hello.mio`：

```mio
chant("Hello, Mio!");
```

检查、运行、生成 Mochi 字节码：

```bash
./mio check hello.mio
./mio run hello.mio
./mio build hello.mio
./mio exec hello.mochi
./mio disasm hello.mochi
```

使用 Bunko 数学库：

```mio
yobu "kiso/math";

kaku value: float64 = 144.0;
chant(math.sqrt(value));
```

`bunko/` 是标准库的物理目录，Mio 源码中使用 `yobu "kiso/math";`，**不是** `yobu "bunko/kiso/math";`。从源码运行时应保留正确的 Bunko 根目录；必要时可通过 `MIO_BUNKO` 配置。Release 安装包包含 CLI、Bunko 源码与许可证。

## 语言能力

| 领域 | 当前开发代码中的实现 |
| --- | --- |
| 基本语法 | `kaku` 变量、`waza` 函数、`moshi` / `sore`、`aida`、`kaesu`、`nukeru`、`tsuzuku` |
| 数值类型 | `int8` / `int16` / `int32` / `int64`、`uint8` / `uint16` / `uint32`、`float32` / `float64`、`rune` |
| 其他类型 | `bool`、`string`、`unit`、`T?`、函数类型、不透明 `resource` |
| 复合数据 | 共享动态数组 `[]T`、共享可变 `Map<K,V>`、不可变值语义结构体、带关联数据的枚举 |
| 类型系统 | 静态检查、泛型函数 / 结构体 / 枚举、`Number` / `Integer` / `Signed` / `Float` 约束 |
| 函数 | 递归、函数值、闭包与共享捕获 Cell |
| 模块 | 目录包、多文件、`yobu` 显式导入、`koukai` 公开成员、`teisu` 编译期常量 |
| 工具链 | 源码检查、Mochi 编译与运行、反汇编、测试、结构化诊断、三语言 i18n |
| 扩展接口 | MMA 原生模块、受控字节数组读写、带代次校验的不透明 Resource Handle |

Mio 不允许普通数值类型之间隐式转换；固定宽度整数有明确的回绕语义，`float32` 与 `float64` 遵循各自精度规则。数组采用共享引用语义，结构体采用不可变值语义，`Map` 的索引读取返回 `V?`。

更多规则参见 [语言文档](docs/LANGUAGE.md)。

## Bunko 标准库

Mio 的标准库以 **Bunko** 为物理根目录，公开导入路径采用 `kiso/`。尽可能以 Mio 源码实现算法，仅在需要系统能力时通过受控原生接口交互。

| 模块 | 主要能力 |
| --- | --- |
| [`kiso/math`](bunko/kiso/math/README.md) | 数值比较、整数数学、浮点数学与常量 |
| [`kiso/array`](bunko/kiso/array/README.md) | 动态数组、容量、追加、克隆、截断 |
| [`kiso/bytes`](bunko/kiso/bytes/README.md) | 字节数组操作与 ByteBuffer |
| [`kiso/utf8`](bunko/kiso/utf8/README.md) | 严格 UTF-8 编码、解码与校验 |
| [`kiso/string`](bunko/kiso/string/README.md) | 字符串操作与 StringBuilder |
| [`kiso/convert`](bunko/kiso/convert/README.md) | 整数、布尔、IEEE 754 浮点数文本转换 |
| [`kiso/io`](bunko/kiso/io/README.md) | Reader / Writer、部分读写与流复制 |
| [`kiso/fs`](bunko/kiso/fs/README.md) | 文件与目录基础操作 |
| [`kiso/path`](bunko/kiso/path/README.md) | `/` 风格逻辑路径处理 |
| [`kiso/encoding/binary`](bunko/kiso/encoding/binary/README.md) | 大小端整数与 LEB128 编解码 |
| [`kiso/testing`](bunko/kiso/testing/README.md) | 测试断言与失败报告 |

标准库通过显式导入按需加载，普通程序不需要预载整套 Bunko。详见 [Bunko 设计说明](docs/BUNKO.md)。

## 从 Go 引导到 Mio 自举

Mio 的第一代实现包含两条相互验证的编译路径：

```text
Go 引导编译器 ──编译──> Compiler A.mochi
                             │
                             │ 由纯 Mio Compiler A 编译同一套编译器源码
                             ▼
                       Compiler B.mochi
                             │
                             ▼
                       Compiler C.mochi
                             │
                             ▼
                       Compiler D.mochi
```

纯 Mio 编译器源码按职责划分为：

| 自举组件 | 目录 | 职责 |
| --- | --- | --- |
| Lexer | [`selfhost/lexer`](selfhost/lexer) | 词法分析 |
| Parser | [`selfhost/parser`](selfhost/parser) | 语法分析与扁平 AST |
| Resolver | [`selfhost/resolver`](selfhost/resolver) | 名称绑定、作用域、捕获关系 |
| TypeChecker | [`selfhost/typecheck`](selfhost/typecheck) | 类型检查、泛型、常量求值与控制流分析 |
| Codegen | [`selfhost/codegen`](selfhost/codegen) | Mochi 指令生成、常量池和 v14 编码 |
| Bootstrap | [`selfhost/bootstrap`](selfhost/bootstrap) | 文件发现、依赖调度、完整编译器入口与自编译测试 |

### 自编译验收记录

2026-10-17 的**本地隔离快照**完成 A → B → C → D；其中 B、C、D 的二进制内容完全一致，并经过 Mochi 解码、结构验证和执行测试。

| 产物 | 文件大小 | 函数数 | 常量数 |
| --- | ---: | ---: | ---: |
| Compiler A（Go 引导） | 152,805 B | 377 | 4,597 |
| Compiler B（A 编译） | 120,707 B | 477 | 497 |
| Compiler C（B 编译） | 120,707 B | 477 | 497 |
| Compiler D（C 编译） | 120,707 B | 477 | 497 |

B / C / D 的 SHA-256 均为：

```text
b44a55bf3f9020e6b0ff83ad137b25774814a32dfe8acaf070abcd167bfd1465
```

单次测试记录中，A → B、B → C、C → D 各耗时约 **6 分 52 秒至 6 分 56 秒**，完整引导约 **20 分 44 秒**。这是一份未专项优化的 **Mochi VM 性能基线**，并非未来 Native AOT 的性能预测。

**自举成功 ≠ 原生独立执行。** v1 的自编译器本身仍是 Mochi 字节码，运行时仍需要 Go 实现的 Mochi VM。Beta 候选仍需通过干净、可复现版本快照上的发布验收。

## Mochi 字节码与 MMA

Mochi 是 Mio v1 的字节码容器。当前最高版本是 **v14**，解码器兼容 v1–v14；自举 Codegen 当前统一生成 v14，而原有 Go 编译器会按使用的语言特性选择可兼容版本。详见 [Mochi 格式文档](docs/MOCHI.md)。

MMA（Mio Module API）为文件、目录等宿主能力提供原生模块边界：使用稳定符号绑定与签名指纹，不向 Mio 暴露 Go 指针。`resource` 表示受控不透明句柄，支持显式关闭与失效校验。详见 [MMA 文档](docs/MMA.md)。

## 测试与开发

Mio 使用 `_test.mio` 作为测试文件后缀，测试函数以 `test_` 开头。普通构建不包括测试源码：

```bash
./mio test ./...
./mio test ./... --run=test_gcd
./mio test ./... --lang=zh-CN
./mio test ./... --format=json
```

开发中的 Go 回归测试分为 Fast、Full 和 Stress：

```bash
# 日常验证
go test -short ./... -count=1
go test -race -short ./... -count=1

# 完整确定性回归（自举单独验收）
go test -skip='^TestBootstrapSelfCompilation$' ./... -count=1 -timeout=35m

# 静态分析
go vet ./...
git diff --check

# 需要时才执行的大型压力测试
MIO_STRESS_TEST=1 go test ./... -count=1 -timeout=60m
```

单独进行连续自编译验证：

```bash
go test ./selfhost/bootstrap -run '^TestBootstrapSelfCompilation$' -count=1 -v -timeout=80m
```

Bootstrap 测试采用隔离的 VM 指令预算，**不改变普通 CLI 的默认安全限制**。测试使用较多时间和内存，请合理安排执行环境。更多信息参见 [测试文档](docs/TESTING.md) 与 [国际化文档](docs/I18N.md)。

## 编辑器支持

**[Mio Language Support（JetBrains）](editors/jetbrains/README.md)**：面向 IntelliJ IDEA / GoLand 的插件。当前插件版本 0.6.0 已实现语法高亮、代码补全、Bunko 导入、跳转定义、查找引用、编译诊断、运行配置、新项目创建与 SDK 管理；具体兼容 IDE 版本和安装方式见插件文档。

**VS Code 与 LSP**：后续开发计划；尚不能作为已完成的开发工具能力宣传。

## 路线图与兼容目标

| 阶段 | 状态与目标 |
| --- | --- |
| Mio v1 自举核心 | **已完成**：A → B → C → D，B / C / D 字节一致 |
| Mio v1.0.0 发布准备 | **Beta 阶段**：回归验证、兼容性测试与反馈收集 |
| Mio v2 Native AOT | **规划中**：原生机器码编译、独立 Runtime / GC、逐步移除 Mochi VM 执行依赖 |
| 后续能力 | 在原生基础稳定后探索并行、异步、数学性能优化等能力 |

**源码兼容目标：** 正式稳定的 v1 用户 `.mio` 源码与 Bunko 公开 API，未来应能在 v2 中不修改用户源码而重新编译。编译器内部、Mochi 字节码格式和当前 Go 原生适配实现不属于该承诺范围。

项目按独立版本分支推进；验收完成后再决定何时将对应分支设为 GitHub 默认分支，不要求把所有版本重新合并到 `main`。

## 文档导航

- [语言语法与静态类型](docs/LANGUAGE.md)
- [Bunko 标准库](docs/BUNKO.md)
- [Mochi 字节码 v1–v14](docs/MOCHI.md)
- [MMA 原生模块接口](docs/MMA.md)
- [测试体系](docs/TESTING.md)
- [诊断与 i18n](docs/I18N.md)
- [JetBrains IDE 插件](editors/jetbrains/README.md)

## 许可证与角色美术

项目代码已经采用 [MIT License](LICENSE)。

Mio 与 Mochi 的角色图像位于 [`assets/mascot/`](assets/mascot/)。**角色插画不随项目代码纳入 MIT License，其版权与使用许可需单独取得。**
