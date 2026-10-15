# kiso/fs

`kiso/fs` 提供同步文件读写、完整文件读写、文件状态、单级目录创建和目录项读取。`File` 内部保存 MMA `resource`，只能由受信任的 Runtime 创建；复制 `File` 会共享关闭状态，重复关闭及关闭后读写返回 `Closed`。

公开 API：

- `open_read`、`open_write`、`open_append`
- `read`、`write`、`close`
- `reader`、`writer`
- `read_all`、`write_all`
- `stat`、`exists`、`is_file`、`is_dir`
- `create_dir`、`read_dir`

流接口复用 `kiso/io` 的 `Reader`、`Writer`、`ReadResult` 和 `WriteResult`，支持部分读写、EOF、`read_full`、`write_all` 与 `copy_stream`。`read_all` 会按 32 KiB 分块读取；一般大文件处理应直接使用流接口。

文件错误通过 `FsError` 返回；`NotFound`、`PermissionDenied`、`Closed`、`InvalidArgument`、`NoSpace`、`InvalidPath` 和 `IOFailure` 都保留底层错误文本。CLI 默认授予与 Mio 进程相同的文件权限；路径规范化不是安全沙箱。嵌入方可以选择是否注册宿主模块，并在后续版本提供更严格的授权适配层。

`open_*` 返回 `FileResult`，`read_all` 返回 `ReadAllResult`，写入、关闭和建目录返回 `UnitResult`。`stat` 返回 `StatResult`，`read_dir` 返回由 `DirEntry` 组成的 `DirResult`。目录项通过宿主资源迭代器逐项读取，不暴露 Go 的切片表示。

当前目录创建只创建单级目录，不处理符号链接、递归遍历、权限修改或文件监控。稳定承诺覆盖 Mio 公共 API；`mio/fs/runtime` 是内部 ABI，不属于 v1/v2 兼容承诺。
