package com.exmikupro.mio.diagnostics

import com.exmikupro.mio.psi.MioFile
import com.intellij.codeInsight.intention.IntentionAction
import com.intellij.lang.annotation.AnnotationHolder
import com.intellij.lang.annotation.ExternalAnnotator
import com.intellij.lang.annotation.HighlightSeverity
import com.intellij.openapi.editor.Editor
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.TextRange
import com.intellij.openapi.util.text.StringUtil
import com.intellij.psi.PsiDocumentManager
import com.intellij.psi.PsiFile

data class MioAnnotationInput(
    val fileKey: String,
    val fileName: String,
    val source: String,
    val sourceStamp: Long,
    val project: Project,
)

class MioExternalAnnotator : ExternalAnnotator<MioAnnotationInput, MioDiagnosticResult>() {
    override fun collectInformation(file: PsiFile, editor: Editor, hasErrors: Boolean): MioAnnotationInput? {
        if (file !is MioFile || !file.isPhysical || file.project.isDisposed) return null
        return MioAnnotationInput(
            fileKey = file.virtualFile?.path ?: return null,
            fileName = file.name,
            source = editor.document.immutableCharSequence.toString(),
            sourceStamp = editor.document.modificationStamp,
            project = file.project,
        )
    }

    override fun doAnnotate(input: MioAnnotationInput): MioDiagnosticResult = input.project
        .getService(MioDiagnosticService::class.java)
        .check(input.fileKey, input.fileName, input.source, input.sourceStamp)

    override fun apply(file: PsiFile, result: MioDiagnosticResult, holder: AnnotationHolder) {
        val document = PsiDocumentManager.getInstance(file.project).getDocument(file) ?: return
        if (document.modificationStamp != result.sourceStamp) return
        val service = file.project.getService(MioDiagnosticService::class.java)
        if (!service.isCurrent(result.fileKey, result.generation)) return
        result.problem?.let {
            holder.newAnnotation(HighlightSeverity.WARNING, it)
                .range(visibleRange(document.textLength, TextRange(0, 0)))
                .create()
            return
        }
        val diagnostic = result.diagnostic ?: return
        val label = diagnostic.primaryLabel
        val exactRange = label?.span?.let { MioOffsetMapper.range(document.text, it.start.offset, it.end.offset) }
            ?: TextRange(0, 0)
        val message = listOfNotNull(diagnostic.code.takeIf(String::isNotEmpty), diagnostic.message).joinToString(": ")
        val builder = holder.newAnnotation(
            if (diagnostic.severity == "warning") HighlightSeverity.WARNING else HighlightSeverity.ERROR,
            message,
        ).range(visibleRange(document.textLength, exactRange)).tooltip(tooltip(diagnostic))
        safeInsertion(diagnostic)?.let { inserted ->
            val byteOffset = label?.span?.start?.offset ?: return@let
            builder.withFix(MioInsertTokenFix(inserted, MioOffsetMapper.insertionOffset(document.text, byteOffset)))
        }
        builder.create()
    }

    private fun visibleRange(length: Int, range: TextRange): TextRange {
        if (!range.isEmpty || length == 0) return range
        val offset = range.startOffset.coerceIn(0, length)
        return if (offset < length) TextRange(offset, offset + 1) else TextRange(length - 1, length)
    }

    private fun tooltip(diagnostic: MioDiagnostic): String {
        val lines = buildList {
            add("${diagnostic.code}: ${diagnostic.message}")
            diagnostic.primaryLabel?.message?.takeIf(String::isNotBlank)?.let(::add)
            if (diagnostic.expected.isNotBlank() || diagnostic.actual.isNotBlank()) {
                add("期望 ${diagnostic.expected.ifBlank { "未知" }}，实际 ${diagnostic.actual.ifBlank { "未知" }}")
            }
            diagnostic.notes.forEach { add("说明：$it") }
            diagnostic.helps.forEach { add("建议：$it") }
        }
        return "<html>${lines.joinToString("<br>") { StringUtil.escapeXmlEntities(it) }}</html>"
    }

    private fun safeInsertion(diagnostic: MioDiagnostic): String? {
        if (diagnostic.code != "E0101") return null
        val expected = Regex("^缺少 '(.+)'$").matchEntire(diagnostic.message)?.groupValues?.get(1) ?: return null
        return expected.takeIf { it in SAFE_TOKENS }
    }

    companion object {
        private val SAFE_TOKENS = setOf(";", "}", ")", "]", ",", ":")
    }
}

private class MioInsertTokenFix(private val token: String, private val offset: Int) : IntentionAction {
    override fun getText(): String = "插入 '$token'"

    override fun getFamilyName(): String = "Mio 语法修复"

    override fun isAvailable(project: Project, editor: Editor?, file: PsiFile?): Boolean =
        editor != null && file is MioFile && offset in 0..editor.document.textLength

    override fun invoke(project: Project, editor: Editor?, file: PsiFile?) {
        val document = editor?.document ?: return
        if (offset !in 0..document.textLength) return
        if (document.charsSequence.regionMatches(offset, token, 0, token.length)) return
        document.insertString(offset, token)
        PsiDocumentManager.getInstance(project).commitDocument(document)
    }

    override fun startInWriteAction(): Boolean = true
}
