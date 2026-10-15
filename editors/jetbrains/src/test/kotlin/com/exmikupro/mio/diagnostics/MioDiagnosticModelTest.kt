package com.exmikupro.mio.diagnostics

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNotNull
import org.junit.Test

class MioDiagnosticModelTest {
    @Test
    fun `parses compiler diagnostic details`() {
        val diagnostic = MioDiagnosticJson.parse(
            """
                {
                  "code":"E0301","severity":"error","message":"类型不匹配",
                  "labels":[{"span":{"start":{"offset":7,"line":1,"column":8},"end":{"offset":10,"line":1,"column":11}},"message":"这里","primary":true}],
                  "notes":[{"message":"补充说明"}],"helps":[{"message":"修改类型"}],
                  "expected":"int64","actual":"string"
                }
            """.trimIndent(),
        )

        assertNotNull(diagnostic)
        assertEquals("E0301", diagnostic!!.code)
        assertEquals(7, diagnostic.primaryLabel!!.span.start.offset)
        assertEquals("补充说明", diagnostic.notes.single())
        assertEquals("int64", diagnostic.expected)
    }

    @Test
    fun `maps utf8 byte spans to utf16 document ranges`() {
        val source = "kaku 文本 = \"😀é\";"
        val startBytes = "kaku ".toByteArray().size
        val endBytes = "kaku 文本".toByteArray().size
        val range = MioOffsetMapper.range(source, startBytes, endBytes)

        assertEquals(source.indexOf("文本"), range.startOffset)
        assertEquals(source.indexOf("文本") + "文本".length, range.endOffset)

        val emojiStart = source.indexOf("😀")
        val emojiStartBytes = source.substring(0, emojiStart).toByteArray().size
        val emojiRange = MioOffsetMapper.range(source, emojiStartBytes, emojiStartBytes + "😀".toByteArray().size)
        assertEquals(emojiStart, emojiRange.startOffset)
        assertEquals(emojiStart + 2, emojiRange.endOffset)
    }
}
