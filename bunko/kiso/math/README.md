# kiso/math

`kiso/math` 是 Mio 的基础数学标准库，通过 `yobu "kiso/math";` 导入。

## API

- 数值泛型：`min`、`max`、`clamp`、`abs`、`checked_abs`
- 整数：`gcd`、`lcm`、`checked_gcd`、`checked_lcm`
- 浮点泛型：`sqrt`、`floor`、`ceil`、`round`、`trunc`
- 浮点分类：`is_nan`、`is_inf`、`is_finite`
- 常量：`pi32`、`pi64`、`tau32`、`tau64`

`checked_abs` 和 `abs` 接受 `Signed`，不会接收无符号整数。`checked_gcd` 和 `checked_lcm` 保持 `int64` 语义。结果无法表示时 checked 函数返回 `mu`，对应的非 checked 函数产生带函数名的运行时错误。`clamp` 在下界大于上界时产生运行时错误。

`min` 和 `max` 传播 NaN，并为相等的零选择 IEEE 754 规定的符号。`round` 使用 ties-to-even。浮点函数通过 `Float` 约束同时支持 `float32` 和 `float64`，且不进行隐式精度转换。

## 实现

比较、绝对值、范围限制、gcd 和 lcm 的控制流与算法使用 Mio 实现。正确舍入的平方根、浮点舍入、特殊值分类、符号检测和运行时数学错误使用 Bunko 私有 `OpMath` 原语。常量由 `teisu` 在编译期折叠，不占用全局槽位。
