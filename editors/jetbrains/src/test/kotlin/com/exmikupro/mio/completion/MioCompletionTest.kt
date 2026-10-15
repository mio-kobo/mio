package com.exmikupro.mio.completion

import com.intellij.openapi.command.undo.UndoManager
import com.intellij.openapi.command.impl.UndoManagerImpl
import com.intellij.openapi.fileEditor.FileEditor
import com.intellij.openapi.fileEditor.impl.CurrentEditorProvider
import com.intellij.openapi.fileEditor.impl.text.TextEditorProvider
import com.intellij.psi.PsiDocumentManager
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.openapi.command.WriteCommandAction
import com.exmikupro.mio.semantic.MioProjectModel
import kotlin.system.measureTimeMillis

class MioCompletionTest : BasePlatformTestCase() {
    fun testKeywordAndBuiltinTypeCompletion() {
        myFixture.configureByText("main.mio", "flo<caret>")
        myFixture.completeBasic()
        assertContainsElements(myFixture.lookupElementStrings ?: emptyList(), "float32", "float64")

        myFixture.configureByText("main.mio", "wa<caret>")
        myFixture.completeBasic()
        assertTrue(myFixture.file.text == "waza" || (myFixture.lookupElementStrings ?: emptyList()).contains("waza"))
    }

    fun testFunctionParametersLocalsAndShadowing() {
        myFixture.configureByText(
            "main.mio",
            """
                waza sample(value: int64) -> int64 {
                    kaku outer = value;
                    moshi (hai) {
                        kaku value: float64 = 1.0;
                        val<caret>
                    }
                }
            """.trimIndent(),
        )
        myFixture.completeBasic()
        assertTrue(myFixture.file.text.contains("value\n") || (myFixture.lookupElementStrings ?: emptyList()).contains("value"))
    }

    fun testFunctionCompletionInsertsCallWithoutDuplicateParentheses() {
        myFixture.configureByText(
            "main.mio",
            "waza calculate(value: float64) -> float64 { kaesu value; }\ncal<caret>",
        )
        myFixture.completeBasic()
        assertTrue(myFixture.file.text.endsWith("calculate()"))
        assertEquals(myFixture.file.text.lastIndexOf('(') + 1, myFixture.editor.caretModel.offset)

        myFixture.configureByText(
            "main.mio",
            "waza calculate(value: float64) -> float64 { kaesu value; }\ncal<caret>(1.0);",
        )
        myFixture.completeBasic()
        assertTrue(myFixture.file.text.endsWith("calculate(1.0);"))
    }

    fun testImportedPackageMembersComeFromSourceAndPrivateMembersStayHidden() {
        addMathPackage()
        myFixture.configureByText("main.mio", "yobu \"kiso/math\";\nmath.<caret>")
        myFixture.completeBasic()
        val variants = myFixture.lookupElementStrings ?: emptyList()
        assertContainsElements(variants, "sqrt", "pi64")
        assertFalse(variants.contains("hidden"))
    }

    fun testUnimportedPackageCompletionAddsImportAndUndoRevertsBothEdits() {
        addMathPackage()
        myFixture.configureByText("main.mio", "math.sq<caret>")
        val fileEditor = TextEditorProvider.getInstance().getTextEditor(myFixture.editor)
        val undoManager = UndoManager.getInstance(project) as UndoManagerImpl
        undoManager.setOverriddenEditorProvider(object : CurrentEditorProvider {
            override fun getCurrentEditor(project: com.intellij.openapi.project.Project?): FileEditor = fileEditor
        })
        try {
            myFixture.completeBasic()
            assertEquals("yobu \"kiso/math\";\n\nmath.sqrt()", myFixture.file.text)

            undoManager.flushCurrentCommandMerger()
            val undoState = undoManager.dumpState(fileEditor, "Mio completion")
            assertTrue(undoState, undoState.contains("with 3 actions") && undoState.contains("yobu \"kiso/math\";"))
            undoManager.undo(fileEditor)
            PsiDocumentManager.getInstance(project).commitDocument(myFixture.editor.document)
            assertEquals("math.sq", myFixture.file.text)
        } finally {
            undoManager.setOverriddenEditorProvider(CurrentEditorProvider.getInstance())
        }
    }

    fun testImportPathCompletionUsesPhysicalPackagesWithoutBunkoPrefix() {
        addMathPackage()
        myFixture.addFileToProject("bunko/kiso/bytes/bytes.mio", "koukai waza equal() -> bool { kaesu hai; }")
        myFixture.configureByText("main.mio", "yobu \"kiso/<caret>")
        myFixture.completeBasic()
        val variants = myFixture.lookupElementStrings ?: emptyList()
        assertContainsElements(variants, "kiso/math", "kiso/bytes")
        assertFalse(variants.any { it.startsWith("bunko/") })

        myFixture.configureByText("path.mio", "yobu \"kiso/ma<caret>")
        myFixture.completeBasic()
        assertEquals("yobu \"kiso/math", myFixture.file.text)
    }

    fun testMultiFilePackageDeclarationsAndImportsAreVisible() {
        addMathPackage()
        myFixture.addFileToProject("app/shared.mio", "yobu \"kiso/math\";\nwaza shared() -> unit { kaesu; }")
        val first = myFixture.addFileToProject("app/main.mio", "sha")
        myFixture.configureFromExistingVirtualFile(first.virtualFile)
        myFixture.editor.caretModel.moveToOffset(3)
        myFixture.completeBasic()
        assertEquals("shared()", myFixture.file.text)

        val second = myFixture.addFileToProject("app/other.mio", "math.")
        myFixture.configureFromExistingVirtualFile(second.virtualFile)
        myFixture.editor.caretModel.moveToOffset(5)
        myFixture.completeBasic()
        assertContainsElements(myFixture.lookupElementStrings ?: emptyList(), "sqrt")
    }

    fun testCompletionIsSuppressedInCommentsAndOrdinaryStrings() {
        myFixture.configureByText("main.mio", "// flo<caret>")
        myFixture.completeBasic()
        assertFalse((myFixture.lookupElementStrings ?: emptyList()).any { it == "float32" || it == "float64" })

        myFixture.configureByText("main.mio", "\"flo<caret>\"")
        myFixture.completeBasic()
        assertFalse((myFixture.lookupElementStrings ?: emptyList()).any { it == "float32" || it == "float64" })
    }

    fun testPackageCacheUpdatesAfterSourceChange() {
        val library = myFixture.addFileToProject(
            "bunko/kiso/math/math.mio",
            "koukai waza sqrt(value: float64) -> float64 { kaesu value; }",
        )
        myFixture.configureByText("main.mio", "yobu \"kiso/math\";\nmath.<caret>")
        myFixture.completeBasic()
        assertContainsElements(myFixture.lookupElementStrings ?: emptyList(), "sqrt")

        val document = PsiDocumentManager.getInstance(project).getDocument(library)!!
        WriteCommandAction.runWriteCommandAction(project) {
            document.setText("koukai waza floor(value: float64) -> float64 { kaesu value; }")
            PsiDocumentManager.getInstance(project).commitDocument(document)
        }
        myFixture.configureByText("second.mio", "yobu \"kiso/math\";\nmath.<caret>")
        myFixture.completeBasic()
        val updated = myFixture.lookupElementStrings ?: emptyList()
        assertTrue(updated.contains("floor") || myFixture.file.text.endsWith("math.floor()"))
        assertFalse(updated.contains("sqrt"))
    }

    fun testStructAndEnumMemberCompletionUsesDeclaredTypes() {
        myFixture.addFileToProject(
            "bunko/kiso/bytes/bytes.mio",
            "koukai kata ByteBuffer { data: []uint8; }",
        )
        myFixture.configureByText(
            "main.mio",
            "yobu \"kiso/bytes\";\nkaku buffer: bytes.ByteBuffer;\nbuffer.<caret>",
        )
        myFixture.completeBasic()
        assertContainsElements(myFixture.lookupElementStrings ?: emptyList(), "data")

        myFixture.configureByText(
            "enum.mio",
            "sentaku Choice { Some(int64); None; }\nChoice.<caret>",
        )
        myFixture.completeBasic()
        assertContainsElements(myFixture.lookupElementStrings ?: emptyList(), "Some", "None")
    }

    fun testLargeBunkoCatalogUsesCachedDirectoryScan() {
        repeat(64) { index ->
            myFixture.addFileToProject(
                "bunko/library/package$index/api.mio",
                "koukai waza value$index() -> int64 { kaesu $index; }",
            )
        }
        val file = myFixture.configureByText("main.mio", "yobu \"library/<caret>")
        val projectModel = MioProjectModel.getInstance(project)
        assertEquals(64, projectModel.importPaths(file).size)

        var cachedPaths = emptyList<String>()
        val cachedDuration = measureTimeMillis { cachedPaths = projectModel.importPaths(file) }
        assertEquals(64, cachedPaths.size)
        assertTrue("cached catalog took ${cachedDuration}ms", cachedDuration < 2_000)
    }

    private fun addMathPackage() {
        myFixture.addFileToProject(
            "bunko/kiso/math/math.mio",
            """
                koukai waza sqrt<T: Float>(value: T) -> T { kaesu value; }
                koukai teisu pi64: float64 = 3.14;
                waza hidden() -> unit { kaesu; }
            """.trimIndent(),
        )
    }
}
