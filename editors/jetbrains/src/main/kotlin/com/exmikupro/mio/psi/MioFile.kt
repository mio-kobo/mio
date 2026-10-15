package com.exmikupro.mio.psi

import com.exmikupro.mio.MioFileType
import com.exmikupro.mio.MioLanguage
import com.intellij.extapi.psi.PsiFileBase
import com.intellij.psi.FileViewProvider

class MioFile(viewProvider: FileViewProvider) : PsiFileBase(viewProvider, MioLanguage) {
    override fun getFileType() = MioFileType.INSTANCE

    override fun toString(): String = "Mio File"
}
