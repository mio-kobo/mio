package com.exmikupro.mio.documentation

import com.exmikupro.mio.navigation.MioSymbolResolver
import com.exmikupro.mio.semantic.MioSymbol
import com.exmikupro.mio.semantic.MioSymbolKind
import com.intellij.lang.documentation.AbstractDocumentationProvider
import com.intellij.openapi.util.text.StringUtil
import com.intellij.psi.PsiElement

class MioDocumentationProvider : AbstractDocumentationProvider() {
    override fun generateDoc(element: PsiElement, originalElement: PsiElement?): String? {
        val location = MioSymbolResolver.declaration(element)
            ?: originalElement?.let(MioSymbolResolver::resolveSymbol)
            ?: MioSymbolResolver.resolveSymbol(element)
            ?: return null
        val symbol = location.symbol
        return buildString {
            append("<div class='definition'><pre>")
            append(StringUtil.escapeXmlEntities(signature(symbol)))
            append("</pre></div>")
            append("<div class='content'>")
            append("<p><b>类型：</b>").append(kindName(symbol.kind)).append("</p>")
            append("<p><b>包：</b>").append(StringUtil.escapeXmlEntities(location.packagePath)).append("</p>")
            append("<p><b>可见性：</b>").append(if (symbol.exported) "公开" else "包内或局部").append("</p>")
            symbol.documentation?.let {
                append("<p>").append(StringUtil.escapeXmlEntities(it).replace("\n", "<br>")).append("</p>")
            }
            append("</div>")
        }
    }

    private fun signature(symbol: MioSymbol): String = when (symbol.kind) {
        MioSymbolKind.FUNCTION -> "waza ${symbol.presentation}"
        MioSymbolKind.STRUCT -> "kata ${symbol.name}"
        MioSymbolKind.ENUM -> "sentaku ${symbol.name}"
        MioSymbolKind.CONSTANT -> "teisu ${symbol.presentation}"
        MioSymbolKind.VARIABLE -> "kaku ${symbol.presentation}"
        MioSymbolKind.PARAMETER, MioSymbolKind.FIELD -> symbol.presentation
        MioSymbolKind.ENUM_VARIANT -> symbol.name
        MioSymbolKind.TYPE_PARAMETER -> symbol.name
    }

    private fun kindName(kind: MioSymbolKind): String = when (kind) {
        MioSymbolKind.VARIABLE -> "变量"
        MioSymbolKind.PARAMETER -> "函数参数"
        MioSymbolKind.FUNCTION -> "函数"
        MioSymbolKind.CONSTANT -> "常量"
        MioSymbolKind.STRUCT -> "结构体"
        MioSymbolKind.ENUM -> "枚举"
        MioSymbolKind.FIELD -> "结构体字段"
        MioSymbolKind.ENUM_VARIANT -> "枚举变体"
        MioSymbolKind.TYPE_PARAMETER -> "类型参数"
    }
}
