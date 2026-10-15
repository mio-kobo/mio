package com.exmikupro.mio.highlight

import com.exmikupro.mio.lexer.MioLexer
import com.exmikupro.mio.lexer.MioTokenTypes
import com.intellij.lexer.Lexer
import com.intellij.openapi.editor.DefaultLanguageHighlighterColors
import com.intellij.openapi.editor.HighlighterColors
import com.intellij.openapi.editor.colors.TextAttributesKey
import com.intellij.openapi.fileTypes.SyntaxHighlighterBase
import com.intellij.psi.TokenType
import com.intellij.psi.tree.IElementType

class MioSyntaxHighlighter : SyntaxHighlighterBase() {
    override fun getHighlightingLexer(): Lexer = MioLexer()

    override fun getTokenHighlights(tokenType: IElementType): Array<TextAttributesKey> = when (tokenType) {
        MioTokenTypes.KEYWORD -> KEYWORD_KEYS
        MioTokenTypes.TYPE -> TYPE_KEYS
        MioTokenTypes.BOOLEAN, MioTokenTypes.NIL -> CONSTANT_KEYS
        MioTokenTypes.INTEGER, MioTokenTypes.FLOAT -> NUMBER_KEYS
        MioTokenTypes.STRING -> STRING_KEYS
        MioTokenTypes.RUNE -> RUNE_KEYS
        MioTokenTypes.LINE_COMMENT -> LINE_COMMENT_KEYS
        MioTokenTypes.BLOCK_COMMENT -> BLOCK_COMMENT_KEYS
        MioTokenTypes.OPERATOR -> OPERATOR_KEYS
        MioTokenTypes.LEFT_PAREN, MioTokenTypes.RIGHT_PAREN -> PARENTHESES_KEYS
        MioTokenTypes.LEFT_BRACKET, MioTokenTypes.RIGHT_BRACKET -> BRACKETS_KEYS
        MioTokenTypes.LEFT_BRACE, MioTokenTypes.RIGHT_BRACE -> BRACES_KEYS
        MioTokenTypes.COMMA, MioTokenTypes.SEMICOLON, MioTokenTypes.COLON, MioTokenTypes.DOT -> PUNCTUATION_KEYS
        TokenType.BAD_CHARACTER -> BAD_CHARACTER_KEYS
        else -> EMPTY_KEYS
    }

    companion object {
        @JvmField val KEYWORD = TextAttributesKey.createTextAttributesKey("MIO_KEYWORD", DefaultLanguageHighlighterColors.KEYWORD)
        @JvmField val TYPE = TextAttributesKey.createTextAttributesKey("MIO_TYPE", DefaultLanguageHighlighterColors.CLASS_NAME)
        @JvmField val CONSTANT = TextAttributesKey.createTextAttributesKey("MIO_CONSTANT", DefaultLanguageHighlighterColors.CONSTANT)
        @JvmField val NUMBER = TextAttributesKey.createTextAttributesKey("MIO_NUMBER", DefaultLanguageHighlighterColors.NUMBER)
        @JvmField val STRING = TextAttributesKey.createTextAttributesKey("MIO_STRING", DefaultLanguageHighlighterColors.STRING)
        @JvmField val RUNE = TextAttributesKey.createTextAttributesKey("MIO_RUNE", DefaultLanguageHighlighterColors.STRING)
        @JvmField val LINE_COMMENT = TextAttributesKey.createTextAttributesKey("MIO_LINE_COMMENT", DefaultLanguageHighlighterColors.LINE_COMMENT)
        @JvmField val BLOCK_COMMENT = TextAttributesKey.createTextAttributesKey("MIO_BLOCK_COMMENT", DefaultLanguageHighlighterColors.BLOCK_COMMENT)
        @JvmField val OPERATOR = TextAttributesKey.createTextAttributesKey("MIO_OPERATOR", DefaultLanguageHighlighterColors.OPERATION_SIGN)
        @JvmField val PARENTHESES = TextAttributesKey.createTextAttributesKey("MIO_PARENTHESES", DefaultLanguageHighlighterColors.PARENTHESES)
        @JvmField val BRACKETS = TextAttributesKey.createTextAttributesKey("MIO_BRACKETS", DefaultLanguageHighlighterColors.BRACKETS)
        @JvmField val BRACES = TextAttributesKey.createTextAttributesKey("MIO_BRACES", DefaultLanguageHighlighterColors.BRACES)
        @JvmField val PUNCTUATION = TextAttributesKey.createTextAttributesKey("MIO_PUNCTUATION", DefaultLanguageHighlighterColors.COMMA)
        @JvmField val BAD_CHARACTER = TextAttributesKey.createTextAttributesKey("MIO_BAD_CHARACTER", HighlighterColors.BAD_CHARACTER)

        private val EMPTY_KEYS = emptyArray<TextAttributesKey>()
        private val KEYWORD_KEYS = arrayOf(KEYWORD)
        private val TYPE_KEYS = arrayOf(TYPE)
        private val CONSTANT_KEYS = arrayOf(CONSTANT)
        private val NUMBER_KEYS = arrayOf(NUMBER)
        private val STRING_KEYS = arrayOf(STRING)
        private val RUNE_KEYS = arrayOf(RUNE)
        private val LINE_COMMENT_KEYS = arrayOf(LINE_COMMENT)
        private val BLOCK_COMMENT_KEYS = arrayOf(BLOCK_COMMENT)
        private val OPERATOR_KEYS = arrayOf(OPERATOR)
        private val PARENTHESES_KEYS = arrayOf(PARENTHESES)
        private val BRACKETS_KEYS = arrayOf(BRACKETS)
        private val BRACES_KEYS = arrayOf(BRACES)
        private val PUNCTUATION_KEYS = arrayOf(PUNCTUATION)
        private val BAD_CHARACTER_KEYS = arrayOf(BAD_CHARACTER)
    }
}
