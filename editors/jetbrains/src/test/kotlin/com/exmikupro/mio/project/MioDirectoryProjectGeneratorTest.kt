package com.exmikupro.mio.project

import com.exmikupro.mio.diagnostics.MioSettings
import com.exmikupro.mio.run.MioRunConfiguration
import com.intellij.execution.RunManager
import com.intellij.ide.util.projectWizard.AbstractNewProjectStep
import com.intellij.ide.util.projectWizard.ProjectSettingsStepBase
import com.intellij.openapi.module.ModuleManager
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.testFramework.EdtTestUtil
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import com.intellij.util.ThrowableRunnable
import com.intellij.util.ui.UIUtil
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import java.nio.file.Files
import javax.swing.JButton
import javax.swing.JCheckBox
import javax.swing.JComboBox

private class MioProjectWizardStep : ProjectSettingsStepBase<MioProjectSettings>(
    MioDirectoryProjectGenerator(),
    AbstractNewProjectStep.AbstractCallback<MioProjectSettings>(),
) {
    fun peer(): MioProjectGeneratorPeer = getPeer() as MioProjectGeneratorPeer
}

private class WizardUiAssertion : ThrowableRunnable<RuntimeException> {
    override fun run() {
        val step = MioProjectWizardStep()
        val panel = step.createPanel()

        assertEquals(1, UIUtil.findComponentsOfType(panel, JComboBox::class.java).size)
        val sample = UIUtil.findComponentsOfType(panel, JCheckBox::class.java).single()
        assertEquals("创建示例 main.mio", sample.text)
        assertEquals(
            setOf("添加本地 SDK", "从 GitHub 下载"),
            UIUtil.findComponentsOfType(panel, JButton::class.java)
                .map(JButton::getText)
                .filter { it in setOf("添加本地 SDK", "从 GitHub 下载") }
                .toSet(),
        )

        sample.isSelected = false
        val settings = step.peer().settings
        assertEquals("", settings.sdkId)
        assertFalse(settings.createSample)
    }
}

class MioDirectoryProjectGeneratorTest : BasePlatformTestCase() {
    fun testWizardMountsMioSettingsAndReadsTheirValues() {
        EdtTestUtil.runInEdtAndWait(WizardUiAssertion())
    }

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
