package com.exmikupro.mio.lexer

import com.intellij.lexer.LexerBase
import com.intellij.psi.TokenType
import com.intellij.psi.tree.IElementType

class MioLexer : LexerBase() {
    private var buffer: CharSequence = ""
    private var bufferEnd = 0
    private var tokenStart = 0
    private var tokenEnd = 0
    private var tokenType: IElementType? = null

    override fun start(buffer: CharSequence, startOffset: Int, endOffset: Int, initialState: Int) {
        this.buffer = buffer
        bufferEnd = endOffset
        tokenStart = startOffset.coerceIn(0, endOffset)
        tokenEnd = tokenStart
        locateToken()
    }

    override fun getState(): Int = STATE_DEFAULT

    override fun getTokenType(): IElementType? = tokenType

    override fun getTokenStart(): Int = tokenStart

    override fun getTokenEnd(): Int = tokenEnd

    override fun advance() {
        tokenStart = tokenEnd
        locateToken()
    }

    override fun getBufferSequence(): CharSequence = buffer

    override fun getBufferEnd(): Int = bufferEnd

    private fun locateToken() {
        if (tokenStart >= bufferEnd) {
            tokenEnd = tokenStart
            tokenType = null
            return
        }

        val current = buffer[tokenStart]
        when {
            current.isMioWhitespace() -> scanWhitespace()
            startsWith("//") -> scanLineComment()
            startsWith("/*") -> scanBlockComment()
            current == '"' -> scanQuoted('"', MioTokenTypes.STRING)
            current == '\'' -> scanQuoted('\'', MioTokenTypes.RUNE)
            current.isAsciiLetter() || current == '_' -> scanIdentifier()
            current.isDigit() -> scanNumber()
            else -> scanSymbol()
        }
    }

    private fun scanWhitespace() {
        tokenEnd = tokenStart + 1
        while (tokenEnd < bufferEnd && buffer[tokenEnd].isMioWhitespace()) tokenEnd++
        tokenType = TokenType.WHITE_SPACE
    }

    private fun scanLineComment() {
        tokenEnd = tokenStart + 2
        while (tokenEnd < bufferEnd && buffer[tokenEnd] != '\n' && buffer[tokenEnd] != '\r') tokenEnd++
        tokenType = MioTokenTypes.LINE_COMMENT
    }

    private fun scanBlockComment() {
        tokenEnd = tokenStart + 2
        while (tokenEnd < bufferEnd) {
            if (tokenEnd + 1 < bufferEnd && buffer[tokenEnd] == '*' && buffer[tokenEnd + 1] == '/') {
                tokenEnd += 2
                break
            }
            tokenEnd++
        }
        tokenType = MioTokenTypes.BLOCK_COMMENT
    }

    private fun scanQuoted(quote: Char, type: IElementType) {
        tokenEnd = tokenStart + 1
        var escaped = false
        while (tokenEnd < bufferEnd) {
            val current = buffer[tokenEnd]
            if (current == '\n' || current == '\r') break
            tokenEnd++
            if (!escaped && current == quote) break
            escaped = !escaped && current == '\\'
        }
        tokenType = type
    }

    private fun scanIdentifier() {
        tokenEnd = tokenStart + 1
        while (tokenEnd < bufferEnd) {
            val current = buffer[tokenEnd]
            if (!current.isAsciiLetter() && !current.isDigit() && current != '_') break
            tokenEnd++
        }
        tokenType = when (buffer.subSequence(tokenStart, tokenEnd).toString()) {
            in KEYWORDS -> MioTokenTypes.KEYWORD
            in BUILTIN_TYPES -> MioTokenTypes.TYPE
            "hai", "iie" -> MioTokenTypes.BOOLEAN
            "mu" -> MioTokenTypes.NIL
            else -> MioTokenTypes.IDENTIFIER
        }
    }

    private fun scanNumber() {
        tokenEnd = tokenStart
        while (tokenEnd < bufferEnd && buffer[tokenEnd].isDigit()) tokenEnd++
        var floating = false

        if (tokenEnd < bufferEnd && buffer[tokenEnd] == '.') {
            floating = true
            tokenEnd++
            while (tokenEnd < bufferEnd && buffer[tokenEnd].isDigit()) tokenEnd++
        }

        if (tokenEnd < bufferEnd && (buffer[tokenEnd] == 'e' || buffer[tokenEnd] == 'E')) {
            floating = true
            tokenEnd++
            if (tokenEnd < bufferEnd && (buffer[tokenEnd] == '+' || buffer[tokenEnd] == '-')) tokenEnd++
            while (tokenEnd < bufferEnd && buffer[tokenEnd].isDigit()) tokenEnd++
        }

        while (tokenEnd < bufferEnd) {
            val current = buffer[tokenEnd]
            if (!current.isAsciiLetter() && !current.isDigit() && current != '_' && current != '.') break
            tokenEnd++
        }
        tokenType = if (floating) MioTokenTypes.FLOAT else MioTokenTypes.INTEGER
    }

    private fun scanSymbol() {
        val twoCharacters = if (tokenStart + 1 < bufferEnd) {
            buffer.subSequence(tokenStart, tokenStart + 2).toString()
        } else {
            ""
        }
        if (twoCharacters in DOUBLE_OPERATORS) {
            tokenEnd = tokenStart + 2
            tokenType = MioTokenTypes.OPERATOR
            return
        }

        tokenEnd = tokenStart + 1
        tokenType = when (buffer[tokenStart]) {
            '+', '-', '*', '/', '=', '!', '?', '<', '>' -> MioTokenTypes.OPERATOR
            ',' -> MioTokenTypes.COMMA
            ';' -> MioTokenTypes.SEMICOLON
            ':' -> MioTokenTypes.COLON
            '.' -> MioTokenTypes.DOT
            '(' -> MioTokenTypes.LEFT_PAREN
            ')' -> MioTokenTypes.RIGHT_PAREN
            '[' -> MioTokenTypes.LEFT_BRACKET
            ']' -> MioTokenTypes.RIGHT_BRACKET
            '{' -> MioTokenTypes.LEFT_BRACE
            '}' -> MioTokenTypes.RIGHT_BRACE
            else -> TokenType.BAD_CHARACTER
        }
    }

    private fun startsWith(value: String): Boolean {
        if (tokenStart + value.length > bufferEnd) return false
        for (index in value.indices) {
            if (buffer[tokenStart + index] != value[index]) return false
        }
        return true
    }

    private fun Char.isAsciiLetter(): Boolean = this in 'a'..'z' || this in 'A'..'Z'

    private fun Char.isMioWhitespace(): Boolean = this == ' ' || this == '\t' || this == '\n' || this == '\r'

    companion object {
        private const val STATE_DEFAULT = 0

        private val KEYWORDS = setOf(
            "kaku", "waza", "moshi", "sore", "kaesu",
            "aida", "nukeru", "tsuzuku", "yobu",
            "kata", "sentaku", "awase", "baai", "koukai", "teisu",
        )

        private val BUILTIN_TYPES = setOf(
            "int8", "int16", "int32", "int64", "rune",
            "uint8", "uint16", "uint32", "float32", "float64",
            "bool", "string", "unit", "Map", "Number", "Integer", "Signed", "Float",
        )

        private val DOUBLE_OPERATORS = setOf("->", "==", "!=", "<=", ">=", "&&", "||")
    }
}
