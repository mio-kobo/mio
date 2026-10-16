package com.exmikupro.mio.project

import com.exmikupro.mio.MioIcons
import com.exmikupro.mio.diagnostics.MioSettings
import com.exmikupro.mio.run.MioRunConfiguration
import com.exmikupro.mio.run.MioRunConfigurationType
import com.exmikupro.mio.sdk.MioSdk
import com.exmikupro.mio.sdk.MioSdkRegistry
import com.exmikupro.mio.sdk.MioSdkUi
import com.intellij.execution.RunManager
import com.intellij.execution.configurations.ConfigurationTypeUtil
import com.intellij.facet.ui.ValidationResult
import com.intellij.ide.util.projectWizard.SettingsStep
import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.module.Module
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.ValidationInfo
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.platform.DirectoryProjectGeneratorBase
import com.intellij.platform.ProjectGeneratorPeer
import com.intellij.util.ui.FormBuilder
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.StandardCopyOption
import java.util.UUID
import javax.swing.JButton
import javax.swing.JCheckBox
import javax.swing.JComboBox
import javax.swing.JComponent
import javax.swing.JPanel

data class MioProjectSettings(val sdkId: String, val createSample: Boolean)

class MioDirectoryProjectGenerator : DirectoryProjectGeneratorBase<MioProjectSettings>() {
    override fun getName(): String = "Mio"

    override fun getDescription(): String = "创建可直接运行的 Mio 项目"

    override fun getLogo() = MioIcons.FILE

    override fun createPeer(): ProjectGeneratorPeer<MioProjectSettings> = MioProjectGeneratorPeer()

    override fun validate(baseDirPath: String): ValidationResult =
        if (baseDirPath.isBlank()) ValidationResult("请选择项目路径。") else ValidationResult.OK

    override fun generateProject(project: Project, baseDir: VirtualFile, settings: MioProjectSettings, module: Module) {
        val projectSettings = MioSettings.getInstance(project)
        val previousSdk = projectSettings.selectedSdkId
        val main = baseDir.toNioPath().resolve("main.mio")
        var createdMain = false
        try {
            projectSettings.selectedSdkId = settings.sdkId
            if (settings.createSample && !Files.exists(main)) {
                val temporary = main.resolveSibling(".main.mio-${UUID.randomUUID()}.tmp")
                try {
                    Files.writeString(temporary, "chant(\"Hello, Mio!\");\n", StandardCharsets.UTF_8)
                    Files.move(temporary, main, StandardCopyOption.ATOMIC_MOVE)
                    createdMain = true
                } finally {
                    Files.deleteIfExists(temporary)
                }
            }
            LocalFileSystem.getInstance().refreshAndFindFileByNioFile(main)
            if (Files.isRegularFile(main)) createRunConfiguration(project, main.toString())
        } catch (error: Exception) {
            projectSettings.selectedSdkId = previousSdk
            if (createdMain) Files.deleteIfExists(main)
            throw error
        }
    }

    private fun createRunConfiguration(project: Project, mainPath: String) {
        val type = ConfigurationTypeUtil.findConfigurationType(MioRunConfigurationType::class.java)
        val factory = type.configurationFactories.first()
        val runManager = RunManager.getInstance(project)
        val configuration = runManager.createConfiguration("main.mio", factory)
        (configuration.configuration as MioRunConfiguration).apply {
            targetPath = mainPath
            workingDirectory = project.basePath.orEmpty()
        }
        runManager.addConfiguration(configuration)
        runManager.selectedConfiguration = configuration
    }
}

class MioProjectGeneratorPeer : ProjectGeneratorPeer<MioProjectSettings> {
    private val sdkCombo = JComboBox<SdkChoice>()
    private val createSample = JCheckBox("创建示例 main.mio", true)
    private val component = FormBuilder.createFormBuilder()
        .addLabeledComponent("Mio SDK：", sdkCombo)
        .addComponent(createSample)
        .addComponent(JPanel(java.awt.FlowLayout(java.awt.FlowLayout.LEFT, 6, 0)).apply {
            add(JButton("添加本地 SDK").apply {
                addActionListener { MioSdkUi.addLocal(null) { refreshSdks(it.id) } }
            })
            add(JButton("从 GitHub 下载").apply {
                addActionListener { MioSdkUi.download(null) { refreshSdks(it.id) } }
            })
        })
        .panel

    init {
        refreshSdks()
    }

    override fun buildUI(settingsStep: SettingsStep) {
        // DirectoryProjectGenerator 在 2025.3 通过 getComponent 挂载设置界面。
    }

    override fun getComponent(locationField: TextFieldWithBrowseButton, checkValid: Runnable): JComponent = component

    override fun getSettings(): MioProjectSettings =
        MioProjectSettings((sdkCombo.selectedItem as? SdkChoice)?.id.orEmpty(), createSample.isSelected)

    override fun validate(): ValidationInfo? = null

    override fun isBackgroundJobRunning(): Boolean = false

    private fun refreshSdks(selectId: String? = null) {
        val selected = selectId ?: (sdkCombo.selectedItem as? SdkChoice)?.id
        sdkCombo.removeAllItems()
        sdkCombo.addItem(SdkChoice("", "暂不配置 SDK"))
        MioSdkRegistry.getInstance().all().forEach { sdkCombo.addItem(SdkChoice(it.id, label(it))) }
        sdkCombo.selectedItem = (0 until sdkCombo.itemCount)
            .map(sdkCombo::getItemAt)
            .firstOrNull { it.id == selected }
            ?: sdkCombo.getItemAt(0)
    }

    private fun label(sdk: MioSdk): String = if (sdk.version.isBlank()) sdk.name else "${sdk.name} (${sdk.version})"

    private data class SdkChoice(val id: String, val label: String) {
        override fun toString(): String = label
    }
}
