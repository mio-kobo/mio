package com.exmikupro.mio.parser

import com.exmikupro.mio.lexer.MioTokenTypes
import com.intellij.lang.ASTNode
import com.intellij.lang.PsiBuilder
import com.intellij.lang.PsiParser
import com.intellij.psi.tree.IElementType

class MioParser : PsiParser {
    override fun parse(root: IElementType, builder: PsiBuilder): ASTNode {
        val file = builder.mark()
        while (!builder.eof()) parseItem(builder)
        file.done(root)
        return builder.treeBuilt
    }

    private fun parseItem(builder: PsiBuilder) {
        val marker = builder.mark()
        val first = builder.tokenText
        val exported = first == "koukai"
        if (exported) advance(builder)
        val keyword = builder.tokenText
        when {
            keyword == "yobu" -> {
                consumeUntilStatementEnd(builder)
                marker.done(MioElementTypes.IMPORT)
            }
            keyword in DECLARATION_KEYWORDS -> {
                consumeDeclaration(builder)
                marker.done(MioElementTypes.DECLARATION)
            }
            exported -> {
                consumeUntilStatementEnd(builder)
                marker.done(MioElementTypes.DECLARATION)
            }
            else -> {
                advance(builder)
                marker.drop()
            }
        }
    }

    private fun consumeDeclaration(builder: PsiBuilder) {
        var bracketDepth = 0
        var parenDepth = 0
        while (!builder.eof()) {
            when (builder.tokenType) {
                MioTokenTypes.LEFT_BRACKET -> bracketDepth++
                MioTokenTypes.RIGHT_BRACKET -> if (bracketDepth > 0) bracketDepth--
                MioTokenTypes.LEFT_PAREN -> parenDepth++
                MioTokenTypes.RIGHT_PAREN -> if (parenDepth > 0) parenDepth--
                MioTokenTypes.LEFT_BRACE -> if (bracketDepth == 0 && parenDepth == 0) {
                    parseBlock(builder)
                    return
                }
                MioTokenTypes.SEMICOLON -> if (bracketDepth == 0 && parenDepth == 0) {
                    advance(builder)
                    return
                }
            }
            advance(builder)
        }
    }

    private fun parseBlock(builder: PsiBuilder) {
        val block = builder.mark()
        var depth = 0
        while (!builder.eof()) {
            when (builder.tokenType) {
                MioTokenTypes.LEFT_BRACE -> depth++
                MioTokenTypes.RIGHT_BRACE -> {
                    depth--
                    advance(builder)
                    if (depth == 0) break
                    continue
                }
            }
            advance(builder)
        }
        block.done(MioElementTypes.BLOCK)
    }

    private fun consumeUntilStatementEnd(builder: PsiBuilder) {
        while (!builder.eof()) {
            val end = builder.tokenType == MioTokenTypes.SEMICOLON
            advance(builder)
            if (end) return
        }
    }

    private fun advance(builder: PsiBuilder) {
        if (builder.tokenType in setOf(MioTokenTypes.IDENTIFIER, MioTokenTypes.STRING)) {
            val reference = builder.mark()
            builder.advanceLexer()
            reference.done(MioElementTypes.REFERENCE)
        } else {
            builder.advanceLexer()
        }
    }

    companion object {
        private val DECLARATION_KEYWORDS = setOf("waza", "kaku", "teisu", "kata", "sentaku")
    }
}
