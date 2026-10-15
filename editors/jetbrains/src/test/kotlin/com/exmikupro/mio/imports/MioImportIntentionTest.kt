package com.exmikupro.mio.imports

import com.intellij.testFramework.fixtures.BasePlatformTestCase

class MioImportIntentionTest : BasePlatformTestCase() {
    fun testAltEnterImportsRealExportedMember() {
        addMathPackage()
        myFixture.configureByText("main.mio", "kaku value = math.sq<caret>rt(144.0);")

        val intention = myFixture.findSingleIntention("导入 kiso/math")
        myFixture.launchAction(intention)

        myFixture.checkResult("yobu \"kiso/math\";\n\nkaku value = math.sqrt(144.0);")
    }

    fun testNoIntentionForPrivateMemberOrLocalShadow() {
        addMathPackage()
        myFixture.configureByText("main.mio", "math.hid<caret>den();")
        assertEmpty(myFixture.filterAvailableIntentions("导入 kiso/math"))

        myFixture.configureByText("main.mio", "kaku math = 1;\nmath.sq<caret>rt(1.0);")
        assertEmpty(myFixture.filterAvailableIntentions("导入 kiso/math"))
    }

    fun testNoDuplicateImportAcrossPackageFiles() {
        addMathPackage()
        myFixture.addFileToProject("app/imports.mio", "yobu \"kiso/math\";")
        val main = myFixture.addFileToProject("app/main.mio", "math.sqrt(1.0);")
        myFixture.configureFromExistingVirtualFile(main.virtualFile)
        myFixture.editor.caretModel.moveToOffset("math.sq".length)

        assertEmpty(myFixture.filterAvailableIntentions("导入 kiso/math"))
    }

    fun testAliasConflictSuppressesImport() {
        addMathPackage()
        myFixture.configureByText("main.mio", "yobu \"other/math\";\nmath.sq<caret>rt(1.0);")

        assertEmpty(myFixture.filterAvailableIntentions("导入 kiso/math"))
    }

    private fun addMathPackage() {
        myFixture.addFileToProject(
            "bunko/kiso/math/math.mio",
            """
                koukai waza sqrt<T: Float>(value: T) -> T { kaesu value; }
                waza hidden() -> unit { kaesu; }
            """.trimIndent(),
        )
    }
}
