package com.exmikupro.mio.navigation

import com.exmikupro.mio.lexer.MioLexer
import com.exmikupro.mio.lexer.MioTokenTypes
import com.exmikupro.mio.semantic.MioSymbolKind
import com.intellij.lang.cacheBuilder.DefaultWordsScanner
import com.intellij.lang.cacheBuilder.WordsScanner
import com.intellij.lang.findUsages.FindUsagesProvider
import com.intellij.psi.PsiElement
import com.intellij.psi.tree.TokenSet

class MioFindUsagesProvider : FindUsagesProvider {
    override fun getWordsScanner(): WordsScanner = DefaultWordsScanner(
        MioLexer(),
        TokenSet.create(MioTokenTypes.IDENTIFIER),
        TokenSet.create(MioTokenTypes.LINE_COMMENT, MioTokenTypes.BLOCK_COMMENT),
        TokenSet.create(MioTokenTypes.STRING, MioTokenTypes.RUNE),
    )

    override fun canFindUsagesFor(psiElement: PsiElement): Boolean = MioSymbolResolver.declaration(psiElement) != null

    override fun getHelpId(psiElement: PsiElement): String? = null

    override fun getType(element: PsiElement): String = when (MioSymbolResolver.declaration(element)?.symbol?.kind) {
        MioSymbolKind.VARIABLE -> "变量"
        MioSymbolKind.PARAMETER -> "参数"
        MioSymbolKind.FUNCTION -> "函数"
        MioSymbolKind.CONSTANT -> "常量"
        MioSymbolKind.STRUCT -> "结构体"
        MioSymbolKind.ENUM -> "枚举"
        MioSymbolKind.FIELD -> "字段"
        MioSymbolKind.ENUM_VARIANT -> "枚举变体"
        MioSymbolKind.TYPE_PARAMETER -> "类型参数"
        null -> "符号"
    }

    override fun getDescriptiveName(element: PsiElement): String = element.text

    override fun getNodeText(element: PsiElement, useFullName: Boolean): String = element.text
}
