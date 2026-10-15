package com.exmikupro.mio.semantic

import com.exmikupro.mio.lexer.MioLexer
import com.exmikupro.mio.lexer.MioTokenTypes
import com.intellij.psi.TokenType
import com.intellij.psi.tree.IElementType

enum class MioSymbolKind {
    VARIABLE,
    PARAMETER,
    FUNCTION,
    CONSTANT,
    STRUCT,
    ENUM,
    FIELD,
    ENUM_VARIANT,
    TYPE_PARAMETER,
}

data class MioParameter(val name: String, val type: String, val declarationOffset: Int)

data class MioSymbol(
    val name: String,
    val kind: MioSymbolKind,
    val type: String? = null,
    val parameters: List<MioParameter> = emptyList(),
    val returnType: String? = null,
    val exported: Boolean = false,
    val declarationOffset: Int = 0,
    val scopeStart: Int = 0,
    val scopeEnd: Int = Int.MAX_VALUE,
    val members: List<MioSymbol> = emptyList(),
    val documentation: String? = null,
) {
    val presentation: String
        get() = when (kind) {
            MioSymbolKind.FUNCTION -> buildString {
                append(name)
                append('(')
                append(parameters.joinToString(", ") { "${it.name}: ${it.type}" })
                append(')')
                returnType?.let { append(" -> ").append(it) }
            }
            else -> if (type.isNullOrBlank()) name else "$name: $type"
        }
}

data class MioImport(val path: String, val alias: String, val start: Int, val end: Int)

data class MioLexeme(val type: IElementType, val text: String, val start: Int, val end: Int)

class MioSemanticModel private constructor(
    val source: String,
    val imports: List<MioImport>,
    val symbols: List<MioSymbol>,
    val lexemes: List<MioLexeme>,
) {
    fun lexemeAt(offset: Int): MioLexeme? {
        val probe = if (offset == source.length && offset > 0) offset - 1 else offset
        return lexemes.firstOrNull { probe >= it.start && probe < it.end }
    }

    fun isInComment(offset: Int): Boolean = lexemeAt(offset)?.type in COMMENT_TYPES

    fun isInString(offset: Int): Boolean = lexemeAt(offset)?.type == MioTokenTypes.STRING

    fun visibleSymbols(offset: Int): List<MioSymbol> {
        val candidates = symbols.asSequence()
            .filter { offset in it.scopeStart..it.scopeEnd }
            .filter { symbol ->
                symbol.scopeStart == 0 && symbol.kind in HOISTED_KINDS || symbol.declarationOffset < offset
            }
            .sortedWith(compareByDescending<MioSymbol> { it.scopeStart }.thenByDescending { it.declarationOffset })
        val names = HashSet<String>()
        return candidates.filter { names.add(it.name) }.toList()
    }

    fun topLevelSymbols(): List<MioSymbol> = symbols.filter { it.scopeStart == 0 }

    companion object {
        private val COMMENT_TYPES = setOf(MioTokenTypes.LINE_COMMENT, MioTokenTypes.BLOCK_COMMENT)
        private val HOISTED_KINDS = setOf(MioSymbolKind.FUNCTION, MioSymbolKind.CONSTANT, MioSymbolKind.STRUCT, MioSymbolKind.ENUM)

        fun parse(source: String): MioSemanticModel = Parser(source).parse()
    }

    private class Parser(private val source: String) {
        private val allLexemes = tokenize(source)
        private val tokens = allLexemes.filter { it.type != TokenType.WHITE_SPACE && it.type !in COMMENT_TYPES }
        private val imports = mutableListOf<MioImport>()
        private val symbols = mutableListOf<MioSymbol>()
        private val braces = matchingPairs(MioTokenTypes.LEFT_BRACE, MioTokenTypes.RIGHT_BRACE)

        fun parse(): MioSemanticModel {
            parseImports()
            tokens.forEachIndexed { index, token ->
                when (token.text) {
                    "waza" -> parseFunction(index)
                    "kata" -> parseAggregate(index, MioSymbolKind.STRUCT)
                    "sentaku" -> parseAggregate(index, MioSymbolKind.ENUM)
                    "teisu" -> parseVariable(index, MioSymbolKind.CONSTANT)
                    "kaku" -> parseVariable(index, MioSymbolKind.VARIABLE)
                }
            }
            return MioSemanticModel(source, imports, symbols.distinctBy { Triple(it.name, it.kind, it.declarationOffset) }, allLexemes)
        }

        private fun parseImports() {
            tokens.forEachIndexed { index, token ->
                if (token.text != "yobu") return@forEachIndexed
                val pathToken = tokens.getOrNull(index + 1) ?: return@forEachIndexed
                if (pathToken.type != MioTokenTypes.STRING) return@forEachIndexed
                val path = pathToken.text.removeSurrounding("\"")
                if (!isValidImportPath(path)) return@forEachIndexed
                val end = tokens.getOrNull(index + 2)?.takeIf { it.type == MioTokenTypes.SEMICOLON }?.end ?: pathToken.end
                imports += MioImport(path, path.substringAfterLast('/'), token.start, end)
            }
        }

        private fun parseFunction(index: Int) {
            val name = tokens.getOrNull(index + 1)?.takeIf { it.type == MioTokenTypes.IDENTIFIER } ?: return
            val openParen = findToken(index + 2, MioTokenTypes.LEFT_PAREN, stopAt = setOf(MioTokenTypes.LEFT_BRACE, MioTokenTypes.SEMICOLON)) ?: return
            val closeParen = matchingEnd(openParen, MioTokenTypes.LEFT_PAREN, MioTokenTypes.RIGHT_PAREN) ?: return
            val bodyOpen = findToken(closeParen + 1, MioTokenTypes.LEFT_BRACE, stopAt = setOf(MioTokenTypes.SEMICOLON))
            val bodyClose = bodyOpen?.let(braces::get)
            val scope = containingScope(name.start)
            val exported = tokens.getOrNull(index - 1)?.text == "koukai"
            val parameters = parseParameters(openParen + 1, closeParen)
            val arrow = (closeParen + 1 until (bodyOpen ?: tokens.size)).firstOrNull { tokens[it].text == "->" }
            val returnType = arrow?.let { joinType(it + 1, bodyOpen ?: tokens.size) }?.ifBlank { null }
            symbols += MioSymbol(
                name = name.text,
                kind = MioSymbolKind.FUNCTION,
                parameters = parameters,
                returnType = returnType,
                exported = exported,
                declarationOffset = name.start,
                scopeStart = scope.first,
                scopeEnd = scope.second,
                documentation = documentationBefore(index),
            )
            if (bodyOpen != null && bodyClose != null) {
                val bodyStart = tokens[bodyOpen].end
                val bodyEnd = tokens[bodyClose].start
                parameters.forEach { parameter ->
                    symbols += MioSymbol(
                        parameter.name,
                        MioSymbolKind.PARAMETER,
                        parameter.type,
                        declarationOffset = parameter.declarationOffset,
                        scopeStart = bodyStart,
                        scopeEnd = bodyEnd,
                    )
                }
                parseTypeParameters(index + 2, openParen).forEach { parameter ->
                    symbols += MioSymbol(
                        parameter.text,
                        MioSymbolKind.TYPE_PARAMETER,
                        declarationOffset = parameter.start,
                        scopeStart = name.end,
                        scopeEnd = bodyEnd,
                    )
                }
            }
        }

        private fun parseAggregate(index: Int, kind: MioSymbolKind) {
            val name = tokens.getOrNull(index + 1)?.takeIf { it.type == MioTokenTypes.IDENTIFIER } ?: return
            val bodyOpen = findToken(index + 2, MioTokenTypes.LEFT_BRACE, stopAt = setOf(MioTokenTypes.SEMICOLON)) ?: return
            val bodyClose = braces[bodyOpen] ?: return
            val scope = containingScope(name.start)
            val exported = tokens.getOrNull(index - 1)?.text == "koukai"
            val members = if (kind == MioSymbolKind.STRUCT) parseStructFields(bodyOpen + 1, bodyClose) else parseEnumVariants(bodyOpen + 1, bodyClose)
            symbols += MioSymbol(
                name.text,
                kind,
                exported = exported,
                declarationOffset = name.start,
                scopeStart = scope.first,
                scopeEnd = scope.second,
                members = members,
                documentation = documentationBefore(index),
            )
        }

        private fun parseVariable(index: Int, kind: MioSymbolKind) {
            val name = tokens.getOrNull(index + 1)?.takeIf { it.type == MioTokenTypes.IDENTIFIER } ?: return
            val end = findStatementEnd(index + 2)
            val colon = (index + 2 until end).firstOrNull { tokens[it].type == MioTokenTypes.COLON }
            val assign = (index + 2 until end).firstOrNull { tokens[it].text == "=" }
            val typeEnd = assign ?: end
            val type = colon?.let { joinType(it + 1, typeEnd).ifBlank { null } }
                ?: assign?.let { inferStructLiteralType(it + 1, end) }
            val scope = containingScope(name.start)
            val exported = kind == MioSymbolKind.CONSTANT && tokens.getOrNull(index - 1)?.text == "koukai"
            symbols += MioSymbol(
                name.text,
                kind,
                type,
                exported = exported,
                declarationOffset = name.start,
                scopeStart = scope.first,
                scopeEnd = scope.second,
                documentation = documentationBefore(index),
            )
        }

        private fun parseParameters(start: Int, end: Int): List<MioParameter> {
            val result = mutableListOf<MioParameter>()
            var segmentStart = start
            var depth = 0
            for (index in start..end) {
                val atEnd = index == end
                val token = tokens.getOrNull(index)
                if (!atEnd) {
                    if (token?.type == MioTokenTypes.LEFT_BRACKET || token?.type == MioTokenTypes.LEFT_PAREN) depth++
                    if (token?.type == MioTokenTypes.RIGHT_BRACKET || token?.type == MioTokenTypes.RIGHT_PAREN) depth--
                }
                if (atEnd || token?.type == MioTokenTypes.COMMA && depth == 0) {
                    val colon = (segmentStart until index).firstOrNull { tokens[it].type == MioTokenTypes.COLON }
                    val parameterName = colon?.let { tokens.getOrNull(segmentStart) }
                    if (parameterName?.type == MioTokenTypes.IDENTIFIER) {
                        result += MioParameter(parameterName.text, joinType(colon + 1, index).ifBlank { "?" }, parameterName.start)
                    }
                    segmentStart = index + 1
                }
            }
            return result
        }

        private fun parseStructFields(start: Int, end: Int): List<MioSymbol> {
            val result = mutableListOf<MioSymbol>()
            var index = start
            while (index < end) {
                val name = tokens.getOrNull(index)
                if (name?.type == MioTokenTypes.IDENTIFIER && tokens.getOrNull(index + 1)?.type == MioTokenTypes.COLON) {
                    val separator = (index + 2 until end).firstOrNull { tokens[it].type in setOf(MioTokenTypes.COMMA, MioTokenTypes.SEMICOLON) } ?: end
                    result += MioSymbol(
                        name.text,
                        MioSymbolKind.FIELD,
                        joinType(index + 2, separator).ifBlank { null },
                        exported = true,
                        declarationOffset = name.start,
                        documentation = documentationBefore(index),
                    )
                    index = separator + 1
                } else index++
            }
            return result
        }

        private fun parseEnumVariants(start: Int, end: Int): List<MioSymbol> {
            val result = mutableListOf<MioSymbol>()
            var index = start
            while (index < end) {
                val token = tokens[index]
                if (token.type == MioTokenTypes.IDENTIFIER) {
                    result += MioSymbol(
                        token.text,
                        MioSymbolKind.ENUM_VARIANT,
                        exported = true,
                        declarationOffset = token.start,
                        documentation = documentationBefore(index),
                    )
                    val separator = (index + 1 until end).firstOrNull { tokens[it].type == MioTokenTypes.SEMICOLON } ?: end
                    index = separator + 1
                } else index++
            }
            return result
        }

        private fun containingScope(offset: Int): Pair<Int, Int> {
            var best = 0 to source.length
            braces.forEach { (openIndex, closeIndex) ->
                val start = tokens[openIndex].end
                val end = tokens[closeIndex].start
                if (offset in start..end && start >= best.first) best = start to end
            }
            return best
        }

        private fun findStatementEnd(start: Int): Int = (start until tokens.size).firstOrNull {
            tokens[it].type == MioTokenTypes.SEMICOLON
        } ?: tokens.size

        private fun findToken(start: Int, type: IElementType, stopAt: Set<IElementType>): Int? {
            for (index in start until tokens.size) {
                if (tokens[index].type == type) return index
                if (tokens[index].type in stopAt) return null
            }
            return null
        }

        private fun matchingEnd(start: Int, open: IElementType, close: IElementType): Int? {
            var depth = 0
            for (index in start until tokens.size) {
                if (tokens[index].type == open) depth++
                if (tokens[index].type == close && --depth == 0) return index
            }
            return null
        }

        private fun matchingPairs(open: IElementType, close: IElementType): Map<Int, Int> {
            val stack = ArrayDeque<Int>()
            val result = mutableMapOf<Int, Int>()
            tokens.forEachIndexed { index, token ->
                if (token.type == open) stack.addLast(index)
                if (token.type == close && stack.isNotEmpty()) result[stack.removeLast()] = index
            }
            return result
        }

        private fun joinType(start: Int, end: Int): String {
            if (start >= end) return ""
            return buildString {
                for (index in start until end.coerceAtMost(tokens.size)) {
                    val text = tokens[index].text
                    val previous = tokens.getOrNull(index - 1)?.text
                    if (isTypeWord(text) && previous?.let(::isTypeWord) == true) append(' ')
                    append(text)
                }
            }
        }

        private fun inferStructLiteralType(start: Int, end: Int): String? {
            val first = tokens.getOrNull(start)?.takeIf { it.type == MioTokenTypes.IDENTIFIER } ?: return null
            if (tokens.getOrNull(start + 1)?.type == MioTokenTypes.LEFT_BRACE) return first.text
            val dot = tokens.getOrNull(start + 1)?.takeIf { it.type == MioTokenTypes.DOT } ?: return null
            val second = tokens.getOrNull(start + 2)?.takeIf { it.type == MioTokenTypes.IDENTIFIER } ?: return null
            if (tokens.getOrNull(start + 3)?.type != MioTokenTypes.LEFT_BRACE || start + 3 >= end) return null
            return "${first.text}${dot.text}${second.text}"
        }

        private fun parseTypeParameters(start: Int, end: Int): List<MioLexeme> {
            val open = (start until end).firstOrNull { tokens[it].type == MioTokenTypes.OPERATOR && tokens[it].text == "<" }
                ?: return emptyList()
            val close = (open + 1 until end).firstOrNull { tokens[it].type == MioTokenTypes.OPERATOR && tokens[it].text == ">" }
                ?: return emptyList()
            val result = mutableListOf<MioLexeme>()
            var expectName = true
            for (index in open + 1 until close) {
                val token = tokens[index]
                if (expectName && token.type == MioTokenTypes.IDENTIFIER) {
                    result += token
                    expectName = false
                }
                if (token.type == MioTokenTypes.COMMA) expectName = true
            }
            return result
        }

        private fun documentationBefore(tokenIndex: Int): String? {
            val declarationStart = tokens.getOrNull(tokenIndex - 1)?.takeIf { it.text == "koukai" }?.start
                ?: tokens.getOrNull(tokenIndex)?.start
                ?: return null
            val comments = mutableListOf<String>()
            var cursor = allLexemes.indexOfLast { it.end <= declarationStart }
            while (cursor >= 0) {
                val lexeme = allLexemes[cursor]
                if (lexeme.type == TokenType.WHITE_SPACE) {
                    if (lexeme.text.count { it == '\n' } > 1) break
                    cursor--
                    continue
                }
                if (lexeme.type !in COMMENT_TYPES) break
                comments.add(0, cleanComment(lexeme.text))
                cursor--
            }
            return comments.joinToString("\n").trim().ifBlank { null }
        }

        private fun cleanComment(text: String): String = when {
            text.startsWith("//") -> text.removePrefix("//").trim()
            text.startsWith("/*") -> text.removePrefix("/*").removeSuffix("*/")
                .lines().joinToString("\n") { it.trim().removePrefix("*").trim() }.trim()
            else -> text.trim()
        }

        private fun isTypeWord(text: String): Boolean = text.firstOrNull()?.let { it.isLetterOrDigit() || it == '_' } == true
    }
}

fun tokenize(source: String): List<MioLexeme> {
    val lexer = MioLexer()
    lexer.start(source, 0, source.length, 0)
    return buildList {
        while (lexer.tokenType != null) {
            add(MioLexeme(lexer.tokenType!!, source.substring(lexer.tokenStart, lexer.tokenEnd), lexer.tokenStart, lexer.tokenEnd))
            lexer.advance()
        }
    }
}

fun isValidImportPath(path: String): Boolean {
    if (path.isBlank() || path.startsWith('/') || path.startsWith("bunko/") || '\\' in path) return false
    return path.split('/').all { part -> part.isNotBlank() && part != "." && part != ".." && part.all { it.isLetterOrDigit() || it == '_' || it == '-' } }
}
