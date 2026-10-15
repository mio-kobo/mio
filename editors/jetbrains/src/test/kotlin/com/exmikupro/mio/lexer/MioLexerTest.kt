package com.exmikupro.mio.lexer

import com.intellij.lexer.Lexer
import com.intellij.psi.TokenType
import com.intellij.psi.tree.IElementType
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test

class MioLexerTest {
    @Test
    fun `highlights all current keywords`() {
        val source = "kaku waza kaesu moshi sore aida nukeru tsuzuku yobu kata sentaku awase baai koukai teisu"
        assertTrue(tokens(source).all { it.type == MioTokenTypes.KEYWORD })
    }

    @Test
    fun `highlights types and numbers`() {
        val actual = tokens("int64 float32 Map Number 42 3.14 6e2")
        assertEquals(
            listOf(
                MioTokenTypes.TYPE, MioTokenTypes.TYPE, MioTokenTypes.TYPE, MioTokenTypes.TYPE,
                MioTokenTypes.INTEGER, MioTokenTypes.FLOAT, MioTokenTypes.FLOAT,
            ),
            actual.map { it.type },
        )
    }

    @Test
    fun `recognizes line and block comments`() {
        val actual = tokens("// kaku 1\n/* waza { } */ kaku")
        assertEquals(
            listOf(MioTokenTypes.LINE_COMMENT, MioTokenTypes.BLOCK_COMMENT, MioTokenTypes.KEYWORD),
            actual.map { it.type },
        )
    }

    @Test
    fun `keeps escaped strings and runes together`() {
        val actual = tokens("\"kaesu \\\"Mio\\\" \\\\\" '\\n' '日'")
        assertEquals(listOf(MioTokenTypes.STRING, MioTokenTypes.RUNE, MioTokenTypes.RUNE), actual.map { it.type })
    }

    @Test
    fun `handles Unicode content without losing offsets`() {
        val source = "\"你好、Mio\" // 日本語 🍡\n'美'"
        val actual = tokens(source)
        assertEquals(listOf(MioTokenTypes.STRING, MioTokenTypes.LINE_COMMENT, MioTokenTypes.RUNE), actual.map { it.type })
        assertEquals(source.length, actual.last().end)
    }

    @Test
    fun `handles incomplete string and block comment`() {
        val actual = tokens("\"unfinished\n/* unfinished")
        assertEquals(listOf(MioTokenTypes.STRING, MioTokenTypes.BLOCK_COMMENT), actual.map { it.type })
    }

    @Test
    fun `marks unknown symbols as bad characters`() {
        assertEquals(TokenType.BAD_CHARACTER, tokens("@").single().type)
    }

    @Test
    fun `always advances through arbitrary input`() {
        val source = buildString {
            repeat(2000) { append(if (it % 7 == 0) '@' else "kaku/*x*/\"y\"") }
        }
        val lexer = MioLexer()
        lexer.start(source)
        var previousEnd = 0
        var count = 0
        while (lexer.tokenType != null) {
            assertTrue("token did not advance at ${lexer.tokenStart}", lexer.tokenEnd > lexer.tokenStart)
            assertTrue(lexer.tokenStart >= previousEnd)
            previousEnd = lexer.tokenEnd
            lexer.advance()
            count++
            assertTrue("lexer produced too many tokens", count <= source.length)
        }
        assertEquals(source.length, previousEnd)
    }

    private fun tokens(source: String): List<Token> {
        val lexer = MioLexer()
        lexer.start(source)
        return buildList {
            while (lexer.tokenType != null) {
                if (lexer.tokenType != TokenType.WHITE_SPACE) {
                    add(Token(lexer.tokenType!!, lexer.tokenStart, lexer.tokenEnd))
                }
                lexer.advance()
            }
        }
    }

    private data class Token(val type: IElementType, val start: Int, val end: Int)
}

private fun Lexer.start(source: String) = start(source, 0, source.length, 0)
