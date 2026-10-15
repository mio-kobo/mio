package com.exmikupro.mio.navigation

import com.intellij.openapi.util.TextRange
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiReferenceBase

internal class MioIdentifierReference(element: PsiElement) :
    PsiReferenceBase<PsiElement>(element, TextRange(0, element.textLength), true) {
    override fun resolve(): PsiElement? = if (MioSymbolResolver.declaration(element) == null) {
        MioSymbolResolver.resolve(element)
    } else {
        null
    }
}

internal class MioImportReference(element: PsiElement, range: TextRange) :
    PsiReferenceBase<PsiElement>(element, range, true) {
    override fun resolve(): PsiElement? = MioSymbolResolver.resolveImport(element)
}
