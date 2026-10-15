# kiso/array v0.2

`kiso/array` 提供通用动态数组操作：`make`、`with_capacity`、`push`、`capacity`、`reserve`、`clone`、`truncate` 和 `append_all`。

数组赋值保留共享引用语义；`clone` 创建独立数组并按浅复制规则复制元素。`truncate` 原地缩短共享长度并保留容量，`append_all` 一次预留后批量追加当前源内容。
