package com.exmikupro.mio.semantic

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class MioSemanticModelTest {
    @Test
    fun `解析函数签名和作用域符号`() {
        val source = """
            waza calculate(value: float64) -> float64 {
                kaku outer: int64 = 1;
                moshi (hai) {
                    kaku inner = value;
                    inner;
                }
            }
        """.trimIndent()
        val model = MioSemanticModel.parse(source)
        val offset = source.lastIndexOf("inner;")
        val visible = model.visibleSymbols(offset).associateBy { it.name }

        assertEquals("float64", visible.getValue("value").type)
        assertEquals("int64", visible.getValue("outer").type)
        assertTrue("inner" in visible)
        assertEquals("calculate(value: float64) -> float64", model.topLevelSymbols().single().presentation)
    }

    @Test
    fun `内层声明遮蔽外层同名变量`() {
        val source = """
            waza example(value: int64) -> int64 {
                kaku item: int64 = value;
                moshi (hai) {
                    kaku item: float64 = 1.0;
                    kaesu int64(item);
                }
                kaesu item;
            }
        """.trimIndent()
        val model = MioSemanticModel.parse(source)

        assertEquals("float64", model.visibleSymbols(source.indexOf("int64(item)")).first { it.name == "item" }.type)
        assertEquals("int64", model.visibleSymbols(source.lastIndexOf("item;")).first { it.name == "item" }.type)
    }

    @Test
    fun `仅标记 koukai 声明为包导出`() {
        val model = MioSemanticModel.parse(
            """
                koukai waza visible(value: int64) -> int64 { kaesu value; }
                waza hidden() -> unit { kaesu; }
                koukai teisu answer: int64 = 42;
                koukai kata Box { value: int64; }
                koukai sentaku Choice { Some(int64); None; }
            """.trimIndent(),
        )

        assertTrue(model.topLevelSymbols().first { it.name == "visible" }.exported)
        assertFalse(model.topLevelSymbols().first { it.name == "hidden" }.exported)
        assertEquals(listOf("value"), model.topLevelSymbols().first { it.name == "Box" }.members.map { it.name })
        assertEquals(listOf("Some", "None"), model.topLevelSymbols().first { it.name == "Choice" }.members.map { it.name })
    }

    @Test
    fun `容忍 Unicode 内容和不完整源码`() {
        val source = "waza show(value: string) -> unit { kaku text = \"日本語🍡\"; /* 未完成"
        val model = MioSemanticModel.parse(source)

        assertTrue(model.symbols.any { it.name == "show" })
        assertTrue(model.symbols.any { it.name == "text" })
        assertTrue(model.isInComment(source.length))
    }

    @Test
    fun `拒绝越界和物理 Bunko 导入路径`() {
        assertTrue(isValidImportPath("kiso/math"))
        assertFalse(isValidImportPath("bunko/kiso/math"))
        assertFalse(isValidImportPath("../math"))
        assertFalse(isValidImportPath("kiso/../math"))
        assertFalse(isValidImportPath("/kiso/math"))
    }
}
