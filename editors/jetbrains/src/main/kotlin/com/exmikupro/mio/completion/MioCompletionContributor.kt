package com.exmikupro.mio.completion

import com.exmikupro.mio.MioLanguage
import com.exmikupro.mio.semantic.MioProjectModel
import com.exmikupro.mio.semantic.MioSymbol
import com.exmikupro.mio.semantic.MioSymbolKind
import com.intellij.codeInsight.completion.CompletionContributor
import com.intellij.codeInsight.completion.CompletionParameters
import com.intellij.codeInsight.completion.CompletionProvider
import com.intellij.codeInsight.completion.CompletionResultSet
import com.intellij.codeInsight.completion.CompletionType
import com.intellij.codeInsight.lookup.LookupElementBuilder
import com.intellij.patterns.PlatformPatterns
import com.intellij.util.ProcessingContext

class MioCompletionContributor : CompletionContributor() {
    init {
        extend(
            CompletionType.BASIC,
            PlatformPatterns.psiElement().withLanguage(MioLanguage),
            Provider(),
        )
    }

    private class Provider : CompletionProvider<CompletionParameters>() {
        override fun addCompletions(parameters: CompletionParameters, context: ProcessingContext, result: CompletionResultSet) {
            val file = parameters.originalFile
            val projectModel = MioProjectModel.getInstance(file.project)
            val model = projectModel.model(file)
            val offset = parameters.offset.coerceAtMost(file.textLength)

            importPrefix(file.text, offset)?.let { prefix ->
                projectModel.importPaths(file)
                    .filter { it.startsWith(prefix) }
                    .forEach { result.addElement(LookupElementBuilder.create(it)) }
                return
            }
            if (model.isInComment(offset) || model.isInString(offset)) return

            val qualifier = qualifierBefore(file.text, offset)
            if (qualifier != null) {
                addMemberCompletions(qualifier, offset, file, projectModel, result)
                return
            }

            val seen = HashSet<String>()
            model.visibleSymbols(offset).forEach { symbol -> addSymbol(result, symbol, seen) }
            projectModel.packageSymbols(file).forEach { symbol -> addSymbol(result, symbol, seen) }

            val statementContext = isStatementContext(file.text, offset)
            val words = buildList {
                addAll(BUILTIN_TYPES)
                addAll(VALUES)
                if (statementContext) addAll(STATEMENT_KEYWORDS) else addAll(EXPRESSION_KEYWORDS)
            }
            words.filter(seen::add).forEach { result.addElement(LookupElementBuilder.create(it).bold()) }
        }

        private fun addMemberCompletions(
            qualifier: String,
            offset: Int,
            file: com.intellij.psi.PsiFile,
            projectModel: MioProjectModel,
            result: CompletionResultSet,
        ) {
            val local = projectModel.model(file).visibleSymbols(offset).firstOrNull {
                it.name == qualifier && it.kind in VALUE_KINDS
            }
            if (local != null) {
                val type = local.type?.removeSuffix("?") ?: return
                val typeName = type.substringAfterLast('.')
                val aggregate = if ('.' in type) {
                    val packageAlias = type.substringBefore('.')
                    val importPath = projectModel.packageImports(file).firstOrNull { it.alias == packageAlias }?.path
                    projectModel.availablePackages(file).firstOrNull { it.path == importPath }
                        ?.symbols?.firstOrNull { it.name == typeName && it.kind in TYPE_KINDS }
                } else {
                    projectModel.packageSymbols(file).firstOrNull { it.name == typeName && it.kind in TYPE_KINDS }
                }
                aggregate?.members?.forEach { addSymbol(result, it, HashSet()) }
                return
            }

            val imports = projectModel.packageImports(file)
            val imported = imports.firstOrNull { it.alias == qualifier }
            if (imported != null) {
                projectModel.availablePackages(file).firstOrNull { it.path == imported.path }?.symbols?.forEach { symbol ->
                    addPackageSymbol(result, symbol, imported.path, null)
                }
                return
            }

            val currentType = projectModel.packageSymbols(file).firstOrNull { it.name == qualifier && it.kind in TYPE_KINDS }
            if (currentType != null) {
                currentType.members.forEach { addSymbol(result, it, HashSet()) }
                return
            }

            if (imports.any { it.alias == qualifier }) return
            projectModel.availablePackages(file)
                .filter { it.path.substringAfterLast('/') == qualifier }
                .forEach { pkg -> pkg.symbols.forEach { addPackageSymbol(result, it, pkg.path, pkg.path) } }
        }

        private fun addSymbol(result: CompletionResultSet, symbol: MioSymbol, seen: MutableSet<String>) {
            if (!seen.add(symbol.name)) return
            var builder = LookupElementBuilder.create(symbol.name)
                .withTypeText(symbolTypeText(symbol), true)
            if (symbol.kind == MioSymbolKind.FUNCTION) builder = builder.withInsertHandler(MioFunctionInsertHandler(symbol, null))
            result.addElement(builder)
        }

        private fun addPackageSymbol(result: CompletionResultSet, symbol: MioSymbol, path: String, importPath: String?) {
            var builder = LookupElementBuilder.create(symbol.name)
                .withPresentableText(symbol.presentation)
                .withTailText(" — $path", true)
                .withTypeText(symbolTypeText(symbol), true)
            builder = if (symbol.kind == MioSymbolKind.FUNCTION) {
                builder.withInsertHandler(MioFunctionInsertHandler(symbol, importPath))
            } else if (importPath != null) {
                builder.withInsertHandler(MioImportInsertHandler(importPath))
            } else builder
            result.addElement(builder)
        }

        private fun symbolTypeText(symbol: MioSymbol): String? = when (symbol.kind) {
            MioSymbolKind.FUNCTION -> symbol.returnType
            MioSymbolKind.STRUCT -> "kata"
            MioSymbolKind.ENUM -> "sentaku"
            MioSymbolKind.CONSTANT -> symbol.type ?: "teisu"
            else -> symbol.type
        }
    }

    companion object {
        private val STATEMENT_KEYWORDS = listOf(
            "kaku", "waza", "moshi", "sore", "kaesu", "aida", "nukeru", "tsuzuku",
            "yobu", "kata", "sentaku", "awase", "baai", "koukai", "teisu",
        )
        private val EXPRESSION_KEYWORDS = listOf("waza", "moshi", "awase", "baai", "kaesu")
        private val BUILTIN_TYPES = listOf(
            "int8", "int16", "int32", "int64", "rune", "uint8", "uint16", "uint32",
            "float32", "float64", "bool", "string", "unit", "Map", "Number", "Integer", "Signed", "Float",
        )
        private val VALUES = listOf("hai", "iie", "mu")
        private val TYPE_KINDS = setOf(MioSymbolKind.STRUCT, MioSymbolKind.ENUM)
        private val VALUE_KINDS = setOf(MioSymbolKind.VARIABLE, MioSymbolKind.PARAMETER, MioSymbolKind.CONSTANT)
    }
}

internal fun qualifierBefore(source: String, offset: Int): String? {
    var index = (offset - 1).coerceAtMost(source.lastIndex)
    while (index >= 0 && isIdentifierPart(source[index])) index--
    while (index >= 0 && source[index].isWhitespace()) index--
    if (index < 0 || source[index] != '.') return null
    index--
    while (index >= 0 && source[index].isWhitespace()) index--
    val end = index + 1
    while (index >= 0 && isIdentifierPart(source[index])) index--
    return source.substring(index + 1, end).takeIf { it.isNotEmpty() }
}

internal fun importPrefix(source: String, offset: Int): String? {
    val before = source.substring(0, offset.coerceAtMost(source.length))
    val quote = before.lastIndexOf('"')
    if (quote < 0 || before.indexOf('"', quote + 1) >= 0) return null
    val lineStart = before.lastIndexOfAny(charArrayOf('\n', '\r'), quote - 1).let { it + 1 }
    val prefix = before.substring(lineStart, quote).trim()
    if (prefix != "yobu") return null
    return before.substring(quote + 1)
}

private fun isStatementContext(source: String, offset: Int): Boolean {
    var index = (offset - 1).coerceAtMost(source.lastIndex)
    while (index >= 0 && (source[index].isLetterOrDigit() || source[index] == '_' || source[index].isWhitespace())) index--
    return index < 0 || source[index] == '{' || source[index] == ';' || source[index] == '}'
}

private fun isIdentifierPart(character: Char): Boolean = character in 'a'..'z' || character in 'A'..'Z' || character.isDigit() || character == '_'
