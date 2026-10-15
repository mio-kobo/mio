package com.exmikupro.mio.highlight

import com.exmikupro.mio.MioIcons
import com.intellij.openapi.editor.colors.TextAttributesKey
import com.intellij.openapi.fileTypes.SyntaxHighlighter
import com.intellij.openapi.options.colors.AttributesDescriptor
import com.intellij.openapi.options.colors.ColorDescriptor
import com.intellij.openapi.options.colors.ColorSettingsPage
import javax.swing.Icon

class MioColorSettingsPage : ColorSettingsPage {
    override fun getIcon(): Icon = MioIcons.FILE

    override fun getHighlighter(): SyntaxHighlighter = MioSyntaxHighlighter()

    override fun getDemoText(): String = """
        yobu "kiso/math";

        // 计算两个数字的较大值
        koukai waza maximum(left: int64, right: int64) -> int64 {
            moshi (left > right) {
                kaesu left;
            } sore {
                kaesu right;
            }
        }

        kaku greeting = "こんにちは, Mio!";
        kaku symbol: rune = '美';
        kaku result = maximum(20, 22);
        /* hai、mu 和括号在注释中不参与代码解析。 */
    """.trimIndent()

    override fun getAdditionalHighlightingTagToDescriptorMap(): Map<String, TextAttributesKey>? = null

    override fun getAttributeDescriptors(): Array<AttributesDescriptor> = DESCRIPTORS

    override fun getColorDescriptors(): Array<ColorDescriptor> = ColorDescriptor.EMPTY_ARRAY

    override fun getDisplayName(): String = "Mio"

    companion object {
        private val DESCRIPTORS = arrayOf(
            AttributesDescriptor("Keyword", MioSyntaxHighlighter.KEYWORD),
            AttributesDescriptor("Built-in type", MioSyntaxHighlighter.TYPE),
            AttributesDescriptor("Boolean and mu", MioSyntaxHighlighter.CONSTANT),
            AttributesDescriptor("Number", MioSyntaxHighlighter.NUMBER),
            AttributesDescriptor("String", MioSyntaxHighlighter.STRING),
            AttributesDescriptor("Rune", MioSyntaxHighlighter.RUNE),
            AttributesDescriptor("Line comment", MioSyntaxHighlighter.LINE_COMMENT),
            AttributesDescriptor("Block comment", MioSyntaxHighlighter.BLOCK_COMMENT),
            AttributesDescriptor("Operator", MioSyntaxHighlighter.OPERATOR),
            AttributesDescriptor("Parentheses", MioSyntaxHighlighter.PARENTHESES),
            AttributesDescriptor("Brackets", MioSyntaxHighlighter.BRACKETS),
            AttributesDescriptor("Braces", MioSyntaxHighlighter.BRACES),
            AttributesDescriptor("Punctuation", MioSyntaxHighlighter.PUNCTUATION),
            AttributesDescriptor("Bad character", MioSyntaxHighlighter.BAD_CHARACTER),
        )
    }
}
