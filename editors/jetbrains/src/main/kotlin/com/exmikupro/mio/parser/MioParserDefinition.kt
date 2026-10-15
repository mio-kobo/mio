package com.exmikupro.mio.parser

import com.exmikupro.mio.MioLanguage
import com.exmikupro.mio.lexer.MioLexer
import com.exmikupro.mio.lexer.MioTokenTypes
import com.exmikupro.mio.psi.MioFile
import com.exmikupro.mio.psi.MioReferenceElement
import com.intellij.lang.ASTNode
import com.intellij.lang.ParserDefinition
import com.intellij.lang.PsiParser
import com.intellij.lexer.Lexer
import com.intellij.openapi.project.Project
import com.intellij.extapi.psi.ASTWrapperPsiElement
import com.intellij.psi.FileViewProvider
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiFile
import com.intellij.psi.TokenType
import com.intellij.psi.tree.IFileElementType
import com.intellij.psi.tree.TokenSet

class MioParserDefinition : ParserDefinition {
    override fun createLexer(project: Project?): Lexer = MioLexer()

    override fun createParser(project: Project?): PsiParser = MioParser()

    override fun getFileNodeType() = FILE

    override fun getCommentTokens(): TokenSet = COMMENTS

    override fun getStringLiteralElements(): TokenSet = STRINGS

    override fun createElement(node: ASTNode): PsiElement = when (node.elementType) {
        MioElementTypes.REFERENCE -> MioReferenceElement(node)
        else -> ASTWrapperPsiElement(node)
    }

    override fun createFile(viewProvider: FileViewProvider): PsiFile = MioFile(viewProvider)

    companion object {
        @JvmField val FILE = IFileElementType(MioLanguage)
        private val COMMENTS = TokenSet.create(MioTokenTypes.LINE_COMMENT, MioTokenTypes.BLOCK_COMMENT)
        private val STRINGS = TokenSet.create(MioTokenTypes.STRING, MioTokenTypes.RUNE)
        @Suppress("unused")
        private val WHITE_SPACE = TokenSet.create(TokenType.WHITE_SPACE)
    }
}
