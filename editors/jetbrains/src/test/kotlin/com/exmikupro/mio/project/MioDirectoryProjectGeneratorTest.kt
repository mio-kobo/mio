package com.exmikupro.mio.project

import com.exmikupro.mio.diagnostics.MioSettings
import com.exmikupro.mio.run.MioRunConfiguration
import com.intellij.execution.RunManager
import com.intellij.openapi.module.ModuleManager
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.nio.file.Files

class MioDirectoryProjectGeneratorTest : BasePlatformTestCase() {
    fun testCreatesSampleSelectsSdkAndAddsRunConfiguration() {
        val directory = Files.createTempDirectory("mio-new-project-")
        try {
            val baseDir = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(directory)!!
            val module = ModuleManager.getInstance(project).modules.first()
            MioDirectoryProjectGenerator().generateProject(project, baseDir, MioProjectSettings("sdk-test", true), module)

            assertEquals("chant(\"Hello, Mio!\");\n", Files.readString(directory.resolve("main.mio")))
            assertEquals("sdk-test", MioSettings.getInstance(project).selectedSdkId)
            val configuration = RunManager.getInstance(project).allConfigurationsList.filterIsInstance<MioRunConfiguration>().single()
            assertEquals(directory.resolve("main.mio").toString(), configuration.targetPath)
        } finally {
            MioSettings.getInstance(project).selectedSdkId = ""
            directory.toFile().deleteRecursively()
        }
    }

    fun testCanCreateProjectWithoutSdkOrSample() {
        val directory = Files.createTempDirectory("mio-empty-project-")
        try {
            val baseDir = LocalFileSystem.getInstance().refreshAndFindFileByNioFile(directory)!!
            val module = ModuleManager.getInstance(project).modules.first()
            MioDirectoryProjectGenerator().generateProject(project, baseDir, MioProjectSettings("", false), module)
            assertFalse(Files.exists(directory.resolve("main.mio")))
            assertEquals("", MioSettings.getInstance(project).selectedSdkId)
        } finally {
            directory.toFile().deleteRecursively()
        }
    }
}
