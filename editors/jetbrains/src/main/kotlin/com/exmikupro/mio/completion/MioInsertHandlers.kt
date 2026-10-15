package com.exmikupro.mio.completion

import com.exmikupro.mio.semantic.MioProjectModel
import com.exmikupro.mio.semantic.MioSymbol
import com.intellij.codeInsight.completion.InsertionContext
import com.intellij.codeInsight.completion.InsertHandler
import com.intellij.codeInsight.lookup.LookupElement
import com.intellij.openapi.editor.Document
import com.intellij.psi.PsiDocumentManager

internal class MioFunctionInsertHandler(
    private val symbol: MioSymbol,
    private val importPath: String?,
) : InsertHandler<LookupElement> {
    override fun handleInsert(context: InsertionContext, item: LookupElement) {
        val document = context.document
        var next = context.tailOffset
        while (next < document.textLength && document.charsSequence[next].isWhitespace()) next++
        if (next < document.textLength && document.charsSequence[next] == '(') {
            context.editor.caretModel.moveToOffset(if (symbol.parameters.isEmpty()) findClosingParen(document, next) + 1 else next + 1)
        } else {
            val tail = context.tailOffset
            document.insertString(tail, "()")
            context.editor.caretModel.moveToOffset(tail + if (symbol.parameters.isEmpty()) 2 else 1)
        }
        importPath?.let { MioImportEditor.addImport(context, it) }
    }

    private fun findClosingParen(document: Document, open: Int): Int {
        var depth = 0
        for (index in open until document.textLength) {
            if (document.charsSequence[index] == '(') depth++
            if (document.charsSequence[index] == ')' && --depth == 0) return index
        }
        return open
    }
}

internal class MioImportInsertHandler(private val importPath: String) : InsertHandler<LookupElement> {
    override fun handleInsert(context: InsertionContext, item: LookupElement) = MioImportEditor.addImport(context, importPath)
}

object MioImportEditor {
    fun addImport(context: InsertionContext, importPath: String) {
        val file = context.file
        val projectModel = MioProjectModel.getInstance(file.project)
        if (projectModel.packageImports(file).any { it.path == importPath }) return
        addImport(context.document, projectModel.model(file).imports, importPath)
        PsiDocumentManager.getInstance(file.project).commitDocument(context.document)
    }

    fun addImport(document: Document, imports: List<com.exmikupro.mio.semantic.MioImport>, importPath: String) {
        if (imports.any { it.path == importPath }) return
        val newline = if (document.charsSequence.contains("\r\n")) "\r\n" else "\n"
        val statement = "yobu \"$importPath\";"
        if (imports.isNotEmpty()) {
            val offset = imports.maxOf { it.end }
            document.insertString(offset, newline + statement)
        } else {
            val separator = if (document.textLength == 0) "" else newline + newline
            document.insertString(0, statement + separator)
        }
    }
}
