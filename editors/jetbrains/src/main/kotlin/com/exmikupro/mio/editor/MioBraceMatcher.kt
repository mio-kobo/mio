package com.exmikupro.mio.editor

import com.exmikupro.mio.lexer.MioTokenTypes
import com.intellij.lang.BracePair
import com.intellij.lang.PairedBraceMatcher
import com.intellij.psi.PsiFile
import com.intellij.psi.tree.IElementType

class MioBraceMatcher : PairedBraceMatcher {
    override fun getPairs(): Array<BracePair> = PAIRS

    override fun isPairedBracesAllowedBeforeType(leftBraceType: IElementType, contextType: IElementType?): Boolean = true

    override fun getCodeConstructStart(file: PsiFile?, openingBraceOffset: Int): Int = openingBraceOffset

    companion object {
        private val PAIRS = arrayOf(
            BracePair(MioTokenTypes.LEFT_PAREN, MioTokenTypes.RIGHT_PAREN, false),
            BracePair(MioTokenTypes.LEFT_BRACKET, MioTokenTypes.RIGHT_BRACKET, false),
            BracePair(MioTokenTypes.LEFT_BRACE, MioTokenTypes.RIGHT_BRACE, true),
        )
    }
}
