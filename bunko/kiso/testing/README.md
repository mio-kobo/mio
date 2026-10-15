# kiso/testing

`kiso/testing` 是仅由 `mio test` 提供运行时支持的轻量测试模块。

- `expect(condition: bool, message: string)` 在条件为假时记录失败。
- `fail(message: string)` 无条件记录失败。

普通程序不会注册该模块依赖的测试报告接口。
