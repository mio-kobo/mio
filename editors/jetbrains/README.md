<p align="center">
  <a href="assets/mio-language-support.png">
    <img src="assets/mio-language-support.webp" alt="Mio 与 Mochi 展示 Mio Language Support JetBrains 插件" width="650" />
  </a>
</p>

<h1 align="center">Mio Language Support</h1>

<p align="center"><strong>为 Mio 编程语言打造的 JetBrains IDE 插件</strong></p>

<p align="center">
  <a href="https://github.com/ExMikuPro/Mio">Mio 项目</a> ·
  <a href="https://github.com/ExMikuPro/Mio/releases">Releases</a> ·
  <a href="../../docs/LANGUAGE.md">语言文档</a>
</p>

Mio Language Support 提供 IntelliJ IDEA 和 GoLand 的 `.mio` 文件编辑、语言分析与运行支持。当前插件版本为 **`0.6.1`**，目标平台为 **IntelliJ IDEA 2025.3 / GoLand 2025.3**；基于通用 IntelliJ Platform API，不依赖 GoLand 私有 API、Go 插件或 Java 插件。

> 上图为 Mio 与 Mochi 的插件宣传插画。点击图片可查看原始 PNG。它是项目角色美术素材，使用与再分发授权应与代码许可分别确认。

## 功能

- 将 `*.mio` 识别为 Mio 源文件，并显示 Mio 文件图标。
- 高亮 V0.5 已启用的关键字、内置类型、布尔值、`mu`、数字、字符串、rune、注释、运算符、括号和分隔符。
- 支持 `//` 行注释和 `/* ... */` 块注释，兼容 IDE 的注释快捷键。
- 支持 `()`、`[]`、`{}` 括号匹配。
- 提供 Mio 配色设置页，并继承 JetBrains 默认语法颜色以适配深色和浅色主题。
- 安全处理 Unicode 字符串、rune 和注释，以及未完成的字符串、rune 和块注释。
- 提供容错的轻量 ParserDefinition 与 PSI 结构节点。
- 补全当前函数参数、可见局部变量、顶层函数、常量、类型和同目录包声明，并遵守局部名称遮蔽。
- 函数候选显示参数与返回类型，插入调用括号并定位参数光标。
- 从真实 Bunko `.mio` 源码提取 `koukai` 成员，支持已导入及未导入包成员补全。
- 补全真实存在的 `yobu` 路径，不暴露物理 `bunko/` 前缀。
- 对未导入的公开包成员提供补全时导入和 `Alt+Enter` 安全导入；检查同目录文件、重复导入、别名冲突和局部遮蔽。
- 支持局部变量、参数、函数、常量、类型、字段、枚举变体和 Bunko 公开成员的 `Ctrl+B`/`Ctrl+Click` 跳转。
- 支持基于 PSI 引用解析的 Find Usages，排除注释、字符串、遮蔽符号和无关同名标识符。
- Quick Documentation 显示可靠的签名、符号类别、包、可见性和相邻源码文档注释。
- 在后台调用 Mio CLI 的 `check --format=json`，显示语法错误、名称解析错误和类型错误。
- 将 CLI 的 UTF-8 字节范围转换为 JetBrains UTF-16 文档范围，正确标记 Unicode 前后的源码。
- 错误悬停说明包含诊断编号、标签、期望/实际类型、说明和修复建议。
- 对缺失分号、右括号等确定性语法错误提供安全的插入 Quick Fix。
- 编辑期间取消旧检查并丢弃过期结果，同一源码结果会复用，避免阻塞 IDE 或重复运行 CLI。
- 提供原生 Mio Run Configuration，可运行单个 `.mio` 文件或 Mio 包目录，并配置 CLI、工作目录和程序参数。
- 在编辑器装订区域及运行上下文菜单提供当前 Mio 文件的运行入口；运行输出、错误、退出状态和停止操作使用 IDE 标准 Run 工具窗口。
- 在 New Project 页面提供唯一的 Mio 入口，可选择已安装 SDK、暂不配置 SDK，并生成可直接运行的 `main.mio`。
- 全局维护 Mio SDK 列表，并允许每个项目独立选择 SDK 与 Bunko 标准库。
- 从官方 GitHub Releases 列出稳定版和可选预发布版，按当前平台选择安装包并安全安装。

## 编译器配置

在 **Settings | Languages & Frameworks | Mio SDK** 中可以查看已安装 SDK、添加本地 CLI、从 GitHub Releases 下载、配置 SDK 的 Bunko 路径，并为当前项目选择 SDK。全局 SDK 列表和项目选择分别持久化。Run Configuration 中的显式 CLI 路径具有最高优先级，其次是项目选择的 SDK、v0.5 兼容路径，最后才是 `PATH`。插件不会自动执行项目目录中的同名文件。

GitHub 安装只会在用户明确点击后执行。插件使用 IDE 代理和可取消进度任务，从 `/releases` 读取 Alpha 和稳定版本，按 Windows x64、Linux x64/ARM64、macOS Intel/Apple Silicon 选择资产，验证 GitHub 提供的 SHA-256，拒绝路径穿越、链接和异常解压体积，再实际运行 `mio version`、JSON 诊断与 `run` 探测。安装先写入所选目录内的临时位置，通过后原子转移；不会覆盖已有版本，也不会要求管理员权限。

早期 Alpha Release 仅包含 CLI 和 README，不包含 Bunko；后续 Release 是否附带 Bunko，以对应版本的实际发布资产为准。未配置 Bunko 时 SDK 会明确显示为部分可用；可以为 SDK 或当前项目选择包含真实 `kiso/math` 源码的 Bunko 根目录。插件不会自动混用与所选 SDK 版本不匹配的仓库分支源码。有效路径会统一通过 `MIO_BUNKO` 供诊断和运行使用，并作为补全与自动导入的索引根。

检查使用编辑器内存中的最新文本，不要求先保存文件。插件会把当前文件所在目录的普通 `.mio` 文件复制到单次检查专用临时目录，只用内存文本替换当前文件，原始源码不会被写入；如果项目或当前源码的祖先目录中存在 `bunko/`，会通过 `MIO_BUNKO` 传给 CLI。检查完成、取消或项目关闭时会终止对应进程并删除临时目录。

## 运行配置

在 **Run | Edit Configurations...** 中添加 **Mio**，可设置：

- `.mio` 文件或包含 `.mio` 文件的包目录。
- 可选的 Mio CLI 路径；留空时沿用项目设置和安全自动发现。
- 工作目录。
- 程序参数。

当前仓库内 Mio CLI 尚不接受 `run <target>` 后的程序参数。配置中保留该字段以兼容后续 CLI；检测到当前 CLI 不支持时，插件会在运行前明确报错，不会静默丢弃参数。

插件词法规则以 Mio 当前 V0.5 Lexer 为准。Mio 标识符当前仅接受 ASCII 字母、数字和下划线；Unicode 内容可用于字符串、rune 和注释。数字是十进制整数或带小数点/指数的浮点数。当前关键字包括：

`kaku`、`waza`、`moshi`、`sore`、`kaesu`、`aida`、`nukeru`、`tsuzuku`、`yobu`、`kata`、`sentaku`、`awase`、`baai`、`koukai`、`teisu`、`hai`、`iie`、`mu`。

## 开发环境

- Kotlin 2.2.20
- Java 21
- Gradle 9.1.0 Wrapper
- IntelliJ Platform Gradle Plugin 2.19.0
- 编译目标 IntelliJ IDEA 2025.3.2（平台分支 253）

Gradle Wrapper 默认把依赖缓存放在本目录的 `.gradle-user-home/`，构建产物放在 `build/`，IDE 沙箱放在 `.intellijPlatform/`，不会在 Mio 仓库根目录创建 Gradle 文件。`verifyPlugin` 固定验证 IntelliJ IDEA 2025.3.2 与 GoLand 2025.3.2。

## 构建与测试

在本目录执行：

```bash
./gradlew test
./gradlew buildPlugin
./gradlew verifyPlugin
```

首次运行需要联网下载 Gradle、IntelliJ Platform 和测试依赖。可使用 `./gradlew runIde` 启动隔离的开发 IDE；其沙箱和缓存同样位于插件目录的构建或 Gradle 用户目录中。

## 安装 ZIP

执行 `./gradlew buildPlugin` 后，可安装 ZIP 位于 `build/distributions/`：

1. 打开 GoLand 2025.3。
2. 进入 **Settings | Plugins**。
3. 点击齿轮按钮，选择 **Install Plugin from Disk...**。
4. 选择 `build/distributions/` 中与 `build.gradle.kts` 当前版本对应的 `mio-language-support-*.zip`。
5. 按提示重启 IDE。

## 验证高亮

新建扩展名为 `.mio` 的文件并输入：

```mio
yobu "kiso/math";

koukai waza max(left: int64, right: int64) -> int64 {
    moshi (left > right) { kaesu left; }
    kaesu right;
}

kaku message = "こんにちは, Mio!";
kaku symbol: rune = '美';
```

IDE 应将文件识别为 Mio，并分别高亮关键字、类型、字符串、rune、数字和注释。`Ctrl + /`（macOS 默认是 `Cmd + /`）可切换行注释。

## 当前不支持

当前开发版尚未提供持久化 StubIndex、安全重命名、完整表达式类型推断、LSP 或 Debugger。字段跳转依赖显式类型或可直接识别的结构体字面量；解析结果不唯一时不会提供猜测式跳转。当前 Mio CLI 每次只返回第一条诊断，且目录输入和导入包错误的 JSON 尚未携带具体成员源文件路径；插件会保留多文件包语义完成检查，但不会把无法确认来源的范围猜测映射到当前编辑器。当前语义模型按 PSI 与 VFS 修改增量失效，Bunko 目录结构和源文件模型分别缓存。早期 Alpha Release 尚不包含 Bunko；插件不会自动下载未经 Release 校验信息覆盖的标准库源码。
