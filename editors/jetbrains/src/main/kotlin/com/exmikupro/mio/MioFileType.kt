package com.exmikupro.mio

import com.intellij.openapi.fileTypes.LanguageFileType
import javax.swing.Icon

class MioFileType private constructor() : LanguageFileType(MioLanguage) {
    override fun getName(): String = "Mio"

    override fun getDescription(): String = "Mio source file"

    override fun getDefaultExtension(): String = "mio"

    override fun getIcon(): Icon = MioIcons.FILE

    companion object {
        @JvmField
        val INSTANCE = MioFileType()
    }
}
