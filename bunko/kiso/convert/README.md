# kiso/convert

`kiso/convert` 提供严格的 ASCII 十进制数值转换和 Mio 布尔文本转换。

公开 API：

```mio
parse_int(value: string) -> int64?
parse_uint(value: string) -> uint32?
format_int(value: int64) -> string
format_uint(value: uint32) -> string
parse_bool(value: string) -> bool?
format_bool(value: bool) -> string
parse_float32(value: string) -> float32?
parse_float64(value: string) -> float64?
format_float32(value: float32) -> string
format_float64(value: float64) -> string
```

整数解析不清理空白，也不接受十进制数字和可选符号之外的字符。越界或非法输入返回 `mu`。

布尔文本只接受语言关键字 `hai` 和 `iie`。

浮点解析接受十进制整数、小数、科学计数法和可选正负号，不清理空白，也不接受逗号、下划线或非 ASCII 数字。特殊值只接受 `NaN`、`Infinity`、`+Infinity` 和 `-Infinity`。有限值按 IEEE 754 roundTiesToEven 直接舍入到目标精度；上溢返回 `mu`，下溢可以得到带符号的零。

浮点格式化输出最短往返十进制表示，并稳定输出 `NaN`、`Infinity`、`-Infinity`、`0` 和 `-0`。NaN 文本不保留载荷。

实现使用基数为 10^9 的包内多精度整数。解析通过精确有理数除法完成舍入，`float32` 不经过 `float64` 中间结果；格式化先精确展开二进制值，再选择首个可往返的最短有效数字候选。科学计数法使用小写 `e`，正指数带 `+`，指数不补前导零。

单次浮点解析最多接受 10000 字节输入。超过边界返回 `mu`，避免恶意输入导致无界临时内存和 VM 执行成本。
