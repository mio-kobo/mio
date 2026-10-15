package com.exmikupro.mio.navigation

import com.exmikupro.mio.documentation.MioDocumentationProvider
import com.intellij.psi.PsiDirectory
import com.intellij.psi.search.GlobalSearchScope
import com.intellij.psi.search.searches.ReferencesSearch
import com.intellij.testFramework.fixtures.BasePlatformTestCase

class MioNavigationTest : BasePlatformTestCase() {
    fun testLocalParameterAndShadowedVariableResolveToExactDeclarations() {
        val source = """
            waza sample(value: int64) -> int64 {
                kaku item: int64 = value;
                moshi (hai) {
                    kaku item: float64 = 1.0;
                    kaesu int64(it<caret>em);
                }
                kaesu item;
            }
        """.trimIndent()
        myFixture.configureByText("main.mio", source)
        val inner = myFixture.getReferenceAtCaretPositionWithAssertion().resolve()!!
        assertEquals(myFixture.file.text.indexOf("item: float64"), inner.textRange.startOffset)

        val outerUsage = myFixture.file.text.lastIndexOf("item;")
        myFixture.editor.caretModel.moveToOffset(outerUsage + 1)
        val outer = myFixture.getReferenceAtCaretPositionWithAssertion().resolve()!!
        assertEquals(myFixture.file.text.indexOf("item: int64"), outer.textRange.startOffset)

        val parameterUsage = myFixture.file.text.indexOf("value;")
        myFixture.editor.caretModel.moveToOffset(parameterUsage + 1)
        val parameter = myFixture.getReferenceAtCaretPositionWithAssertion().resolve()!!
        assertEquals(myFixture.file.text.indexOf("value: int64"), parameter.textRange.startOffset)
    }

    fun testSamePackageAndImportedPublicNavigation() {
        val shared = myFixture.addFileToProject("app/shared.mio", "koukai waza shared() -> unit { kaesu; }")
        val library = myFixture.addFileToProject(
            "bunko/kiso/math/math.mio",
            "koukai waza sqrt(value: float64) -> float64 { kaesu value; }\nwaza hidden() -> unit { kaesu; }",
        )
        val main = myFixture.addFileToProject(
            "app/main.mio",
            "yobu \"kiso/math\";\nshared();\nmath.sq<caret>rt(4.0);\nmath.hidden();",
        )
        myFixture.configureFromExistingVirtualFile(main.virtualFile)
        val imported = myFixture.getReferenceAtCaretPositionWithAssertion().resolve()!!
        assertEquals(library.virtualFile, imported.containingFile.virtualFile)
        assertEquals("sqrt", imported.text)

        val sharedOffset = myFixture.file.text.indexOf("shared();")
        myFixture.editor.caretModel.moveToOffset(sharedOffset + 1)
        assertEquals(shared.virtualFile, myFixture.getReferenceAtCaretPositionWithAssertion().resolve()!!.containingFile.virtualFile)

        val privateOffset = myFixture.file.text.indexOf("hidden();")
        myFixture.editor.caretModel.moveToOffset(privateOffset + 1)
        assertNull(myFixture.file.findReferenceAt(privateOffset)?.resolve())
    }

    fun testTypesFieldsEnumVariantsGenericsAndImportPathNavigate() {
        myFixture.addFileToProject(
            "bunko/kiso/model/model.mio",
            "koukai kata Box { value: int64; }\nkoukai sentaku Choice { Some(int64); None; }",
        )
        myFixture.configureByText(
            "main.mio",
            """
                yobu "kiso/model";
                waza identity<T>(value: T) -> T { kaesu value; }
                kaku box: model.Box;
                box.value;
                model.Choice.Some(1);
                identity<int64>(1);
            """.trimIndent(),
        )
        assertResolvesTo("model.Box", "Box")
        assertResolvesTo("box.value", "value")
        assertResolvesTo("Choice.Some", "Some")
        assertResolvesTo("identity<int64>", "identity")
        val typeParameterUsage = myFixture.file.text.indexOf("value: T") + "value: ".length
        myFixture.editor.caretModel.moveToOffset(typeParameterUsage)
        assertEquals(
            myFixture.file.text.indexOf("<T>") + 1,
            myFixture.getReferenceAtCaretPositionWithAssertion().resolve()!!.textRange.startOffset,
        )

        val importString = myFixture.file.text.indexOf("kiso/model") + 2
        myFixture.editor.caretModel.moveToOffset(importString)
        val directory = myFixture.getReferenceAtCaretPositionWithAssertion().resolve()!!
        assertEquals("model", (directory as PsiDirectory).name)
    }

    fun testFindUsagesIgnoresCommentsStringsAndShadowedNames() {
        myFixture.configureByText(
            "main.mio",
            """
                waza run() -> unit { kaesu; }
                run();
                // run();
                kaku text = "run";
                waza other() -> unit { kaku run: int64 = 1; run; }
            """.trimIndent(),
        )
        val declaration = myFixture.file.findElementAt(myFixture.file.text.indexOf("run()"))!!.parent
        val usages = ReferencesSearch.search(declaration, GlobalSearchScope.projectScope(project)).findAll()
        assertEquals(1, usages.size)
        assertEquals(myFixture.file.text.indexOf("run();", declaration.textRange.endOffset), usages.single().element.textRange.startOffset)
    }

    fun testFindUsagesAcrossPackageFilesAndImportedMembers() {
        val library = myFixture.addFileToProject(
            "bunko/kiso/model/model.mio",
            "koukai kata Box { value: int64; }\nkoukai waza make() -> Box { kaesu Box { value: 1 }; }",
        )
        myFixture.addFileToProject("app/imports.mio", "yobu \"kiso/model\";")
        val main = myFixture.addFileToProject("app/main.mio", "kaku box: model.Box;\nbox.value;\nmodel.make();")
        myFixture.configureFromExistingVirtualFile(main.virtualFile)

        val functionDeclaration = library.findElementAt(library.text.indexOf("make()"))!!.parent
        val functionUsages = ReferencesSearch.search(functionDeclaration, GlobalSearchScope.projectScope(project)).findAll()
        assertEquals(1, functionUsages.size)
        assertEquals(main.virtualFile, functionUsages.single().element.containingFile.virtualFile)

        val fieldDeclaration = library.findElementAt(library.text.indexOf("value: int64"))!!.parent
        val fieldUsages = ReferencesSearch.search(fieldDeclaration, GlobalSearchScope.projectScope(project)).findAll()
        assertEquals(2, fieldUsages.size)
        assertTrue(fieldUsages.any { it.element.containingFile.virtualFile == main.virtualFile })
        assertTrue(fieldUsages.any { it.element.containingFile.virtualFile == library.virtualFile })
    }

    fun testDocumentationUsesSignaturePackageVisibilityAndSourceComment() {
        val file = myFixture.addFileToProject(
            "app/main.mio",
            "// 计算输入值\nkoukai waza calculate(value: int64) -> int64 { kaesu value; }\ncalculate(1);",
        )
        myFixture.configureFromExistingVirtualFile(file.virtualFile)
        myFixture.editor.caretModel.moveToOffset(myFixture.file.text.lastIndexOf("calculate") + 1)
        val reference = myFixture.getReferenceAtCaretPositionWithAssertion()
        val target = reference.resolve()!!
        val documentation = MioDocumentationProvider().generateDoc(target, reference.element)!!
        assertTrue(documentation.contains("calculate(value: int64) -&gt; int64"))
        assertTrue(documentation.contains("包：</b>app"))
        assertTrue(documentation.contains("可见性：</b>公开"))
        assertTrue(documentation.contains("计算输入值"))
    }

    private fun assertResolvesTo(fragment: String, expected: String) {
        val offset = myFixture.file.text.indexOf(fragment) + fragment.lastIndexOf(expected)
        myFixture.editor.caretModel.moveToOffset(offset)
        assertEquals(expected, myFixture.getReferenceAtCaretPositionWithAssertion().resolve()!!.text)
    }
}
