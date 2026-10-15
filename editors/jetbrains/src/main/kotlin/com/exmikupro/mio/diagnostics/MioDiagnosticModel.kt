package com.exmikupro.mio.diagnostics

import com.google.gson.JsonElement
import com.google.gson.JsonObject
import com.google.gson.JsonParser
import com.intellij.openapi.util.TextRange

data class MioSourceLocation(val offset: Int, val line: Int, val column: Int)

data class MioSourceSpan(val start: MioSourceLocation, val end: MioSourceLocation)

data class MioDiagnosticLabel(val span: MioSourceSpan, val message: String, val primary: Boolean)

data class MioDiagnostic(
    val code: String,
    val severity: String,
    val message: String,
    val labels: List<MioDiagnosticLabel>,
    val notes: List<String>,
    val helps: List<String>,
    val expected: String,
    val actual: String,
) {
    val primaryLabel: MioDiagnosticLabel?
        get() = labels.firstOrNull { it.primary } ?: labels.firstOrNull()
}

data class MioDiagnosticResult(
    val diagnostic: MioDiagnostic?,
    val problem: String?,
    val sourceStamp: Long,
    val generation: Long,
    val fileKey: String,
)

object MioDiagnosticJson {
    fun parse(text: String): MioDiagnostic? {
        val root = runCatching { JsonParser.parseString(text.trim()).asJsonObject }.getOrNull() ?: return null
        val labels = root.array("labels").mapNotNull { element ->
            val label = element.asJsonObject
            val span = label.objectValue("span") ?: return@mapNotNull null
            val start = span.objectValue("start")?.location() ?: return@mapNotNull null
            val end = span.objectValue("end")?.location() ?: return@mapNotNull null
            MioDiagnosticLabel(
                MioSourceSpan(start, end),
                label.string("message"),
                label.boolean("primary"),
            )
        }
        return MioDiagnostic(
            code = root.string("code"),
            severity = root.string("severity"),
            message = root.string("message"),
            labels = labels,
            notes = root.array("notes").mapNotNull { it.asJsonObject.string("message").takeIf(String::isNotEmpty) },
            helps = root.array("helps").mapNotNull { it.asJsonObject.string("message").takeIf(String::isNotEmpty) },
            expected = root.string("expected"),
            actual = root.string("actual"),
        ).takeIf { it.message.isNotBlank() }
    }

    private fun JsonObject.location() = MioSourceLocation(
        offset = int("offset"),
        line = int("line"),
        column = int("column"),
    )

    private fun JsonObject.string(name: String): String = get(name)?.takeUnless { it.isJsonNull }?.asString.orEmpty()

    private fun JsonObject.int(name: String): Int = get(name)?.takeUnless { it.isJsonNull }?.asInt ?: 0

    private fun JsonObject.boolean(name: String): Boolean = get(name)?.takeUnless { it.isJsonNull }?.asBoolean ?: false

    private fun JsonObject.objectValue(name: String): JsonObject? = get(name)?.takeIf { it.isJsonObject }?.asJsonObject

    private fun JsonObject.array(name: String): Iterable<JsonElement> =
        get(name)?.takeIf { it.isJsonArray }?.asJsonArray ?: emptyList()
}

object MioOffsetMapper {
    fun range(text: String, startByte: Int, endByte: Int): TextRange {
        val start = byteToUtf16(text, startByte, false)
        val end = byteToUtf16(text, endByte.coerceAtLeast(startByte), true).coerceAtLeast(start)
        return TextRange(start, end)
    }

    fun insertionOffset(text: String, byteOffset: Int): Int = byteToUtf16(text, byteOffset, false)

    private fun byteToUtf16(text: String, requested: Int, endBias: Boolean): Int {
        val target = requested.coerceAtLeast(0)
        var utf16 = 0
        var bytes = 0
        while (utf16 < text.length) {
            if (bytes >= target) return utf16
            val codePoint = text.codePointAt(utf16)
            val utf16Length = Character.charCount(codePoint)
            val byteLength = String(Character.toChars(codePoint)).toByteArray(Charsets.UTF_8).size
            if (bytes + byteLength > target) return if (endBias) utf16 + utf16Length else utf16
            bytes += byteLength
            utf16 += utf16Length
        }
        return text.length
    }
}
