package com.exmikupro.mio.parser

import com.exmikupro.mio.MioLanguage
import com.intellij.psi.tree.IElementType

class MioElementType(debugName: String) : IElementType(debugName, MioLanguage)

object MioElementTypes {
    @JvmField val DECLARATION = MioElementType("DECLARATION")
    @JvmField val IMPORT = MioElementType("IMPORT")
    @JvmField val BLOCK = MioElementType("BLOCK")
    @JvmField val REFERENCE = MioElementType("REFERENCE")
}
