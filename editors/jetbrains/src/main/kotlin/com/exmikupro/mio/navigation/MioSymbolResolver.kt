package com.exmikupro.mio.navigation

import com.exmikupro.mio.lexer.MioTokenTypes
import com.exmikupro.mio.psi.MioReferenceElement
import com.exmikupro.mio.semantic.MioProjectModel
import com.exmikupro.mio.semantic.MioSymbol
import com.exmikupro.mio.semantic.MioSymbolKind
import com.exmikupro.mio.semantic.MioSymbolLocation
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiFile
import com.intellij.psi.PsiManager

object MioSymbolResolver {
    private val TYPE_KINDS = setOf(MioSymbolKind.STRUCT, MioSymbolKind.ENUM)
    private val VALUE_KINDS = setOf(MioSymbolKind.VARIABLE, MioSymbolKind.PARAMETER, MioSymbolKind.CONSTANT)

    fun resolve(element: PsiElement): PsiElement? {
        val location = resolveSymbol(element)
        if (location != null) return declarationElement(location)
        return resolvePackageQualifier(element)
    }

    fun declarationElement(location: MioSymbolLocation): PsiElement? {
        val leaf = location.file.findElementAt(location.symbol.declarationOffset) ?: return null
        return leaf.parent.takeIf { it is MioReferenceElement } ?: leaf
    }

    fun resolveImport(element: PsiElement): PsiElement? {
        val file = element.containingFile ?: return null
        val path = element.text.removeSurrounding("\"")
        val directory = MioProjectModel.getInstance(file.project).packageDirectory(file, path) ?: return null
        return PsiManager.getInstance(file.project).findDirectory(directory)
    }

    fun declaration(element: PsiElement): MioSymbolLocation? {
        val file = element.containingFile ?: return null
        val offset = element.textRange.startOffset
        val model = MioProjectModel.getInstance(file.project).model(file)
        val symbol = model.symbols.asSequence()
            .flatMap { sequenceOf(it) + it.members.asSequence() }
            .firstOrNull { it.declarationOffset == offset }
            ?: return null
        return MioSymbolLocation(symbol, file, MioProjectModel.getInstance(file.project).packageName(file))
    }

    fun resolveSymbol(element: PsiElement): MioSymbolLocation? {
        declaration(element)?.let { return it }
        val file = element.containingFile ?: return null
        val offset = element.textRange.startOffset
        val name = element.text
        val projectModel = MioProjectModel.getInstance(file.project)
        val model = projectModel.model(file)
        if (model.isInComment(offset) || model.isInString(offset)) return null

        qualifiedParts(file, offset)?.let { (qualifier, _) ->
            if (qualifier == name) {
                val visibleQualifier = model.visibleSymbols(offset).firstOrNull { it.name == name }
                if (visibleQualifier != null) {
                    if (visibleQualifier.scopeStart != 0) {
                        return MioSymbolLocation(visibleQualifier, file, projectModel.packageName(file))
                    }
                    return unique(projectModel.packageSymbolLocations(file).filter { sameDeclaration(it, file, visibleQualifier) })
                }
                return unique(projectModel.packageSymbolLocations(file).filter { it.symbol.name == name })
            }
            resolveQualified(file, qualifier, name, offset, projectModel)?.let { return it }
            return null
        }

        resolveStructField(file, offset, name, projectModel)?.let { return it }

        val visible = model.visibleSymbols(offset).firstOrNull { it.name == name }
        if (visible != null) {
            if (visible.scopeStart != 0) return MioSymbolLocation(visible, file, projectModel.packageName(file))
            unique(projectModel.packageSymbolLocations(file).filter { sameDeclaration(it, file, visible) })?.let { return it }
        }
        return unique(projectModel.packageSymbolLocations(file).filter { it.symbol.name == name })
    }

    private fun resolveQualified(
        file: PsiFile,
        qualifier: String,
        member: String,
        offset: Int,
        projectModel: MioProjectModel,
    ): MioSymbolLocation? {
        val model = projectModel.model(file)
        val chain = qualifiedChain(file, offset)
        if (chain.size >= 3) {
            val packageAlias = chain[chain.lastIndex - 2]
            val ownerName = chain[chain.lastIndex - 1]
            val paths = projectModel.packageImports(file).filter { it.alias == packageAlias }.map { it.path }.distinct()
            if (paths.size == 1) {
                val owner = unique(projectModel.importedSymbolLocations(file, paths.single()).filter {
                    it.symbol.name == ownerName && it.symbol.kind in TYPE_KINDS
                })
                if (owner != null) return memberLocation(owner, member)
            }
        }
        val local = model.visibleSymbols(offset).firstOrNull { it.name == qualifier }
        if (local?.kind in VALUE_KINDS) {
            val type = local?.type ?: return null
            return resolveType(file, type, projectModel)?.let { memberLocation(it, member) }
        }

        val currentType = unique(projectModel.packageSymbolLocations(file).filter {
            it.symbol.name == qualifier && it.symbol.kind in TYPE_KINDS
        })
        if (currentType != null) return memberLocation(currentType, member)

        val importPaths = projectModel.packageImports(file).filter { it.alias == qualifier }.map { it.path }.distinct()
        if (importPaths.size != 1) return null
        return unique(projectModel.importedSymbolLocations(file, importPaths.single()).filter { it.symbol.name == member })
    }

    private fun resolveType(file: PsiFile, rawType: String, projectModel: MioProjectModel): MioSymbolLocation? {
        val type = rawType.removeSuffix("?").substringBefore('<').trim()
        val qualifier = type.substringBeforeLast('.', "")
        val name = type.substringAfterLast('.').removePrefix("[]")
        if (qualifier.isNotEmpty()) {
            val paths = projectModel.packageImports(file).filter { it.alias == qualifier }.map { it.path }.distinct()
            if (paths.size != 1) return null
            return unique(projectModel.importedSymbolLocations(file, paths.single()).filter {
                it.symbol.name == name && it.symbol.kind in TYPE_KINDS
            })
        }
        return unique(projectModel.packageSymbolLocations(file).filter {
            it.symbol.name == name && it.symbol.kind in TYPE_KINDS
        })
    }

    private fun memberLocation(owner: MioSymbolLocation, name: String): MioSymbolLocation? {
        val member = owner.symbol.members.singleOrNull { it.name == name } ?: return null
        return MioSymbolLocation(member, owner.file, owner.packagePath)
    }

    private fun resolveStructField(
        file: PsiFile,
        offset: Int,
        name: String,
        projectModel: MioProjectModel,
    ): MioSymbolLocation? {
        val lexemes = projectModel.model(file).lexemes.filterNot {
            it.type == com.intellij.psi.TokenType.WHITE_SPACE || it.type in setOf(MioTokenTypes.LINE_COMMENT, MioTokenTypes.BLOCK_COMMENT)
        }
        val current = lexemes.indexOfFirst { it.start == offset }
        if (current < 0 || lexemes.getOrNull(current + 1)?.type != MioTokenTypes.COLON) return null
        var depth = 0
        for (index in current - 1 downTo 0) {
            when (lexemes[index].type) {
                MioTokenTypes.RIGHT_BRACE -> depth++
                MioTokenTypes.LEFT_BRACE -> {
                    if (depth > 0) depth-- else {
                        val typeToken = lexemes.getOrNull(index - 1)?.takeIf { it.type == MioTokenTypes.IDENTIFIER }
                            ?: return null
                        val packageAlias = lexemes.getOrNull(index - 2)?.takeIf { it.type == MioTokenTypes.DOT }
                            ?.let { lexemes.getOrNull(index - 3) }
                            ?.takeIf { it.type == MioTokenTypes.IDENTIFIER }
                            ?.text
                        val owner = if (packageAlias == null) {
                            unique(projectModel.packageSymbolLocations(file).filter {
                                it.symbol.name == typeToken.text && it.symbol.kind == MioSymbolKind.STRUCT
                            })
                        } else {
                            val paths = projectModel.packageImports(file).filter { it.alias == packageAlias }.map { it.path }.distinct()
                            paths.singleOrNull()?.let { path ->
                                unique(projectModel.importedSymbolLocations(file, path).filter {
                                    it.symbol.name == typeToken.text && it.symbol.kind == MioSymbolKind.STRUCT
                                })
                            }
                        } ?: return null
                        return memberLocation(owner, name)
                    }
                }
            }
        }
        return null
    }

    private fun resolvePackageQualifier(element: PsiElement): PsiElement? {
        val file = element.containingFile ?: return null
        val parts = qualifiedParts(file, element.textRange.startOffset) ?: return null
        if (parts.first != element.text) return null
        val projectModel = MioProjectModel.getInstance(file.project)
        if (projectModel.model(file).visibleSymbols(element.textRange.startOffset).any { it.name == element.text }) return null
        val paths = projectModel.packageImports(file).filter { it.alias == element.text }.map { it.path }.distinct()
        if (paths.size != 1) return null
        val directory = projectModel.packageDirectory(file, paths.single()) ?: return null
        return PsiManager.getInstance(file.project).findDirectory(directory)
    }

    private fun qualifiedParts(file: PsiFile, offset: Int): Pair<String, String>? {
        val tokens = MioProjectModel.getInstance(file.project).model(file).lexemes.filterNot {
            it.type == com.intellij.psi.TokenType.WHITE_SPACE || it.type in setOf(MioTokenTypes.LINE_COMMENT, MioTokenTypes.BLOCK_COMMENT)
        }
        val current = tokens.indexOfFirst { it.start == offset }
        if (current < 0) return null
        if (tokens.getOrNull(current - 1)?.type == MioTokenTypes.DOT) {
            val qualifier = tokens.getOrNull(current - 2)?.takeIf { it.type == MioTokenTypes.IDENTIFIER } ?: return null
            return qualifier.text to tokens[current].text
        }
        if (tokens.getOrNull(current + 1)?.type == MioTokenTypes.DOT) {
            val member = tokens.getOrNull(current + 2)?.takeIf { it.type == MioTokenTypes.IDENTIFIER } ?: return null
            return tokens[current].text to member.text
        }
        return null
    }

    private fun qualifiedChain(file: PsiFile, offset: Int): List<String> {
        val tokens = MioProjectModel.getInstance(file.project).model(file).lexemes.filterNot {
            it.type == com.intellij.psi.TokenType.WHITE_SPACE || it.type in setOf(MioTokenTypes.LINE_COMMENT, MioTokenTypes.BLOCK_COMMENT)
        }
        val current = tokens.indexOfFirst { it.start == offset }
        if (current < 0 || tokens[current].type != MioTokenTypes.IDENTIFIER) return emptyList()
        val result = ArrayDeque<String>()
        result.addFirst(tokens[current].text)
        var index = current - 1
        while (tokens.getOrNull(index)?.type == MioTokenTypes.DOT && tokens.getOrNull(index - 1)?.type == MioTokenTypes.IDENTIFIER) {
            result.addFirst(tokens[index - 1].text)
            index -= 2
        }
        return result.toList()
    }

    private fun sameDeclaration(location: MioSymbolLocation, file: PsiFile, symbol: MioSymbol): Boolean =
        location.file == file && location.symbol.declarationOffset == symbol.declarationOffset

    private fun unique(locations: List<MioSymbolLocation>): MioSymbolLocation? = locations.singleOrNull()
}
