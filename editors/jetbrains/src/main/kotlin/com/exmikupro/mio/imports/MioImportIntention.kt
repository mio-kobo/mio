package com.exmikupro.mio.imports

import com.exmikupro.mio.MioLanguage
import com.exmikupro.mio.completion.MioImportEditor
import com.exmikupro.mio.semantic.MioProjectModel
import com.intellij.codeInsight.intention.PsiElementBaseIntentionAction
import com.intellij.openapi.command.WriteCommandAction
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.popup.JBPopupFactory
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiFile
import com.intellij.psi.PsiDocumentManager

class MioImportIntention : PsiElementBaseIntentionAction() {
    override fun getFamilyName(): String = "Mio 自动导入"

    override fun getText(): String = intentionText

    override fun isAvailable(project: Project, editor: Editor?, element: PsiElement): Boolean {
        val file = element.containingFile ?: return false
        if (file.language != MioLanguage || editor == null) return false
        val reference = qualifiedReferenceAt(file.text, editor.caretModel.offset) ?: return false
        val model = MioProjectModel.getInstance(project)
        val sourceModel = model.model(file)
        if (sourceModel.isInComment(editor.caretModel.offset) || sourceModel.isInString(editor.caretModel.offset)) return false
        if (sourceModel.visibleSymbols(editor.caretModel.offset).any { it.name == reference.first }) return false
        val imports = model.packageImports(file)
        if (imports.any { it.alias == reference.first }) return false
        val candidates = candidates(file, reference, model)
        if (candidates.isEmpty() || candidates.all { path -> imports.any { it.path == path } }) return false
        intentionText = if (candidates.size == 1) "导入 ${candidates.single()}" else "选择并导入 ${reference.first}.${reference.second}"
        return true
    }

    override fun invoke(project: Project, editor: Editor?, element: PsiElement) {
        val file = element.containingFile ?: return
        val actualEditor = editor ?: return
        val reference = qualifiedReferenceAt(file.text, actualEditor.caretModel.offset) ?: return
        val model = MioProjectModel.getInstance(project)
        val paths = candidates(file, reference, model).filterNot { path -> model.packageImports(file).any { it.path == path } }
        if (paths.size == 1) {
            insert(project, file, paths.single())
        } else if (paths.isNotEmpty()) {
            JBPopupFactory.getInstance().createPopupChooserBuilder(paths)
                .setTitle("选择 Mio 包")
                .setItemChosenCallback { path -> insert(project, file, path) }
                .createPopup()
                .showInBestPositionFor(actualEditor)
        }
    }

    override fun startInWriteAction(): Boolean = false

    private fun candidates(file: PsiFile, reference: Pair<String, String>, model: MioProjectModel): List<String> =
        model.availablePackages(file)
            .filter { it.path.substringAfterLast('/') == reference.first }
            .filter { pkg -> pkg.symbols.any { it.name == reference.second && it.exported } }
            .map { it.path }

    private fun insert(project: Project, file: PsiFile, path: String) {
        WriteCommandAction.runWriteCommandAction(project, "导入 $path", null, {
            val document = PsiDocumentManager.getInstance(project).getDocument(file) ?: return@runWriteCommandAction
            val imports = MioProjectModel.getInstance(project).model(file).imports
            MioImportEditor.addImport(document, imports, path)
            PsiDocumentManager.getInstance(project).commitDocument(document)
        }, file)
    }

    private var intentionText: String = "导入 Mio 包"
}

internal fun qualifiedReferenceAt(source: String, caret: Int): Pair<String, String>? {
    if (source.isEmpty()) return null
    var memberStart = caret.coerceIn(0, source.length)
    while (memberStart > 0 && isIdentifierPart(source[memberStart - 1])) memberStart--
    var memberEnd = caret.coerceIn(0, source.length)
    while (memberEnd < source.length && isIdentifierPart(source[memberEnd])) memberEnd++
    var dot = memberStart - 1
    while (dot >= 0 && source[dot].isWhitespace()) dot--
    if (dot < 0 || source[dot] != '.') return null
    var qualifierEnd = dot
    var qualifierStart = qualifierEnd
    while (qualifierStart > 0 && isIdentifierPart(source[qualifierStart - 1])) qualifierStart--
    val qualifier = source.substring(qualifierStart, qualifierEnd)
    val member = source.substring(memberStart, memberEnd)
    if (qualifier.isEmpty() || member.isEmpty()) return null
    return qualifier to member
}

private fun isIdentifierPart(character: Char): Boolean =
    character in 'a'..'z' || character in 'A'..'Z' || character.isDigit() || character == '_'
