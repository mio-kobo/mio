# Bunko

此目录是 Mio 标准库的物理根目录。公开导入路径从其子目录开始，例如 `bunko/kiso/math` 对应 `kiso/math`。

目录包规则、加载约束和当前前置依赖见 [`docs/BUNKO.md`](../docs/BUNKO.md)。

外部源码包管理器的参考实现、原生实现阻塞项及缓存说明见
[实现状态](../docs/bunko-package-manager.md)。`MIO_BUNKO_CACHE` 与标准库根目录
`MIO_BUNKO` 相互独立；此目录仍只用于标准库。
