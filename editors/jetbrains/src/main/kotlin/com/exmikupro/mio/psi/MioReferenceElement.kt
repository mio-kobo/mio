package com.exmikupro.mio.psi

import com.exmikupro.mio.lexer.MioTokenTypes
import com.exmikupro.mio.navigation.MioIdentifierReference
import com.exmikupro.mio.navigation.MioImportReference
import com.exmikupro.mio.navigation.MioSymbolResolver
import com.exmikupro.mio.semantic.MioProjectModel
import com.intellij.extapi.psi.ASTWrapperPsiElement
import com.intellij.lang.ASTNode
import com.intellij.openapi.util.TextRange
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiNamedElement
import com.intellij.psi.PsiReference
import com.intellij.util.IncorrectOperationException

class MioReferenceElement(node: ASTNode) : ASTWrapperPsiElement(node), PsiNamedElement {
    override fun getName(): String = text

    override fun setName(name: String): PsiElement = throw IncorrectOperationException("Mio 暂不支持安全重命名")

    override fun getReference(): PsiReference? {
        val leaf = firstChild ?: return null
        return when (leaf.node.elementType) {
            MioTokenTypes.IDENTIFIER -> if (MioSymbolResolver.declaration(leaf) == null) MioIdentifierReference(this) else null
            MioTokenTypes.STRING -> importReference()
            else -> null
        }
    }

    private fun importReference(): PsiReference? {
        val file = containingFile ?: return null
        val offset = textRange.startOffset
        val import = MioProjectModel.getInstance(project).model(file).imports.firstOrNull {
            offset >= it.start && textRange.endOffset <= it.end
        } ?: return null
        if (import.path != text.removeSurrounding("\"")) return null
        val range = if (textLength >= 2) TextRange(1, textLength - 1) else TextRange.EMPTY_RANGE
        return MioImportReference(this, range)
    }
}
