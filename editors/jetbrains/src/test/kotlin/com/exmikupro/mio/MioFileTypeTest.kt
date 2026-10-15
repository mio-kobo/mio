package com.exmikupro.mio

import com.intellij.openapi.fileTypes.FileTypeManager
import com.intellij.testFramework.fixtures.BasePlatformTestCase

class MioFileTypeTest : BasePlatformTestCase() {
    fun testMioExtensionIsRegistered() {
        assertSame(MioFileType.INSTANCE, FileTypeManager.getInstance().getFileTypeByExtension("mio"))
    }
}
