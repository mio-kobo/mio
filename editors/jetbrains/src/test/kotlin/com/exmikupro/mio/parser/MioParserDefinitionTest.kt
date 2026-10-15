package com.exmikupro.mio.parser

import com.exmikupro.mio.psi.MioFile
import com.intellij.psi.util.PsiTreeUtil
import com.intellij.testFramework.fixtures.BasePlatformTestCase

class MioParserDefinitionTest : BasePlatformTestCase() {
    fun testBuildsTolerantStructuredPsi() {
        val file = myFixture.configureByText(
            "main.mio",
            """
                yobu "kiso/math";
                koukai waza calculate(value: float64) -> float64 {
                    kaku nested = value;
            """.trimIndent(),
        )

        assertTrue(file is MioFile)
        val types = PsiTreeUtil.collectElements(file) { true }.mapNotNull { it.node?.elementType }.toSet()
        assertTrue(types.contains(MioElementTypes.IMPORT))
        assertTrue(types.contains(MioElementTypes.DECLARATION))
        assertTrue(types.contains(MioElementTypes.BLOCK))
    }
}
