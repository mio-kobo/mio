# kiso/io

`kiso/io` 提供同步字节流的 `Reader`、`Writer`、`ReadResult`、`WriteResult`、`CopyResult` 和 `IoError`。

`Reader` 与 `Writer` 使用函数字段表达，不需要 Core Trait 或专用 Opcode。`reader_from_bytes` 与输入数组共享存储，但每个 Reader 拥有独立游标；读取期间对原数组的修改可见。`writer_to_buffer` 复用 `kiso/bytes.ByteBuffer` 的共享后备存储和扩容能力。

内存 Reader 的零长度读取返回 `count = 0`，并按当前游标报告 EOF；零长度 `read_full` 不调用底层 Reader，返回 `count = 0` 和 `eof = false`。零长度写入成功返回 `count = 0`。读取允许在返回数据的同时报告 EOF。`read_full`、`write_all` 和 `copy_stream` 会保留已完成数量，并把无进展转换为 `IoError.NoProgress`。提前 EOF 转换为 `IoError.UnexpectedEOF`。

`copy_stream` 复用固定的 32 KiB 缓冲区，不会一次性读取完整输入。本版本只提供内存流，不包含文件系统、路径、网络或异步 I/O。
