package com.exmikupro.mio.diagnostics

import com.exmikupro.mio.cli.MioCliResolver
import com.exmikupro.mio.sdk.MioSdk
import com.exmikupro.mio.sdk.MioSdkRegistry
import com.exmikupro.mio.sdk.MioSdkUi
import com.exmikupro.mio.sdk.MioBunkoValidator
import com.intellij.openapi.components.PersistentStateComponent
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage
import com.intellij.openapi.options.Configurable
import com.intellij.openapi.options.ConfigurationException
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory
import com.intellij.codeInsight.daemon.DaemonCodeAnalyzer
import java.awt.BorderLayout
import javax.swing.JComponent
import javax.swing.JLabel
import javax.swing.JPanel
import javax.swing.JComboBox
import javax.swing.JButton
import javax.swing.DefaultListModel
import javax.swing.JList
import javax.swing.JScrollPane

@State(name = "MioSettings", storages = [Storage("mio.xml")])
class MioSettings : PersistentStateComponent<MioSettings.State> {
    data class State(
        var executablePath: String = "",
        var selectedSdkId: String = "",
        var bunkoPath: String = "",
    )

    private var value = State()

    override fun getState(): State = value

    override fun loadState(state: State) {
        value = state
    }

    var executablePath: String
        get() = value.executablePath
        set(path) {
            value.executablePath = path.trim()
        }

    var selectedSdkId: String
        get() = value.selectedSdkId
        set(id) {
            value.selectedSdkId = id
        }

    var bunkoPath: String
        get() = value.bunkoPath
        set(path) {
            value.bunkoPath = path.trim()
        }

    companion object {
        fun getInstance(project: Project): MioSettings = project.getService(MioSettings::class.java)
    }
}

class MioSettingsConfigurable(private val project: Project) : Configurable {
    private var executableField: TextFieldWithBrowseButton? = null
    private var bunkoField: TextFieldWithBrowseButton? = null
    private var sdkCombo: JComboBox<SdkChoice>? = null
    private var sdkList: JList<SdkChoice>? = null

    override fun getDisplayName(): String = "Mio SDK"

    override fun createComponent(): JComponent {
        val field = TextFieldWithBrowseButton().also { executableField = it }
        field.addBrowseFolderListener(
            project,
            FileChooserDescriptorFactory.singleFile()
                .withTitle("选择 Mio 可执行文件")
                .withDescription("选择支持 JSON 诊断的 Mio CLI。留空时仅在 PATH 中自动查找，不执行项目内同名文件。"),
        )
        val bunko = TextFieldWithBrowseButton().also { bunkoField = it }
        bunko.addBrowseFolderListener(
            project,
            FileChooserDescriptorFactory.singleDir()
                .withTitle("选择 Bunko 标准库")
                .withDescription("选择包含 kiso 等包的 Bunko 根目录。"),
        )
        val combo = JComboBox<SdkChoice>().also { sdkCombo = it }
        val list = JList<SdkChoice>().also { sdkList = it }
        val panel = JPanel(java.awt.GridBagLayout())
        fun row(index: Int, label: String, component: JComponent) {
            val left = java.awt.GridBagConstraints().apply {
                gridx = 0
                gridy = index
                anchor = java.awt.GridBagConstraints.WEST
                insets = java.awt.Insets(4, 4, 4, 8)
            }
            val right = java.awt.GridBagConstraints().apply {
                gridx = 1
                gridy = index
                weightx = 1.0
                fill = java.awt.GridBagConstraints.HORIZONTAL
                insets = java.awt.Insets(4, 0, 4, 4)
            }
            panel.add(JLabel(label), left)
            panel.add(component, right)
        }
        row(0, "项目 SDK：", combo)
        row(1, "兼容的 CLI 路径：", field)
        row(2, "Bunko 路径：", bunko)
        row(3, "已安装 SDK：", JScrollPane(list).apply { preferredSize = java.awt.Dimension(640, 150) })
        val buttons = JPanel(java.awt.FlowLayout(java.awt.FlowLayout.LEFT, 6, 0))
        buttons.add(JButton("添加本地 SDK").apply {
            addActionListener { MioSdkUi.addLocal(project) { refreshSdks(it.id) } }
        })
        buttons.add(JButton("从 GitHub 下载").apply {
            addActionListener { MioSdkUi.download(project) { refreshSdks(it.id) } }
        })
        buttons.add(JButton("设置 SDK Bunko").apply {
            addActionListener {
                val choice = list.selectedValue ?: return@addActionListener
                val sdk = MioSdkRegistry.getInstance().find(choice.id) ?: return@addActionListener
                val selected = com.intellij.openapi.fileChooser.FileChooser.chooseFile(
                    FileChooserDescriptorFactory.singleDir().withTitle("选择 ${sdk.name} 的 Bunko 标准库"),
                    project,
                    null,
                ) ?: return@addActionListener
                MioBunkoValidator.problem(selected.toNioPath())?.let { problem ->
                    com.intellij.openapi.ui.Messages.showErrorDialog(project, problem, "Bunko 不可用")
                    return@addActionListener
                }
                sdk.bunkoPath = selected.path
                sdk.validation = "JSON 诊断、run 与 Bunko 可用"
                MioSdkRegistry.getInstance().update(sdk)
                refreshSdks(sdk.id)
            }
        })
        buttons.add(JButton("移除记录").apply {
            addActionListener {
                val choice = list.selectedValue ?: return@addActionListener
                MioSdkRegistry.getInstance().remove(choice.id)
                if (MioSettings.getInstance(project).selectedSdkId == choice.id) combo.selectedIndex = 0
                refreshSdks()
            }
        })
        row(4, "", buttons)
        return panel
    }

    override fun isModified(): Boolean {
        val settings = MioSettings.getInstance(project)
        return executableField?.text?.trim() != settings.executablePath ||
            bunkoField?.text?.trim() != settings.bunkoPath ||
            (sdkCombo?.selectedItem as? SdkChoice)?.id.orEmpty() != settings.selectedSdkId
    }

    override fun apply() {
        val settings = MioSettings.getInstance(project)
        bunkoField?.text?.trim()?.takeIf(String::isNotBlank)?.let { path ->
            MioBunkoValidator.problem(java.nio.file.Path.of(path))?.let { throw ConfigurationException(it) }
        }
        settings.executablePath = executableField?.text.orEmpty()
        settings.bunkoPath = bunkoField?.text.orEmpty()
        settings.selectedSdkId = (sdkCombo?.selectedItem as? SdkChoice)?.id.orEmpty()
        project.getService(MioCliResolver::class.java).clear()
        project.getService(MioDiagnosticService::class.java).clear()
        DaemonCodeAnalyzer.getInstance(project).restart(this)
    }

    override fun reset() {
        val settings = MioSettings.getInstance(project)
        executableField?.text = settings.executablePath
        bunkoField?.text = settings.bunkoPath
        refreshSdks()
        sdkCombo?.selectedItem = (0 until (sdkCombo?.itemCount ?: 0))
            .mapNotNull { sdkCombo?.getItemAt(it) }
            .firstOrNull { it.id == settings.selectedSdkId }
            ?: sdkCombo?.getItemAt(0)
    }

    override fun disposeUIResources() {
        executableField = null
        bunkoField = null
        sdkCombo = null
        sdkList = null
    }

    private fun sdkLabel(sdk: MioSdk): String = buildString {
        append(sdk.name)
        if (sdk.version.isNotBlank()) append(" (${sdk.version})")
        if (sdk.platform.isNotBlank()) append(" — ${sdk.platform}")
    }

    private fun sdkDetail(sdk: MioSdk): String = buildString {
        append(sdkLabel(sdk))
        append(" — ${sdk.validation} — ${sdk.executablePath}")
        if (sdk.bunkoPath.isNotBlank()) append(" — Bunko: ${sdk.bunkoPath}")
    }

    private fun refreshSdks(selectId: String? = null) {
        val selected = selectId ?: (sdkCombo?.selectedItem as? SdkChoice)?.id
        val sdks = MioSdkRegistry.getInstance().all()
        sdkCombo?.removeAllItems()
        sdkCombo?.addItem(SdkChoice("", "未配置（使用兼容路径或 PATH）"))
        sdks.forEach { sdkCombo?.addItem(SdkChoice(it.id, sdkLabel(it))) }
        val model = DefaultListModel<SdkChoice>()
        sdks.forEach { model.addElement(SdkChoice(it.id, sdkDetail(it))) }
        sdkList?.model = model
        sdkCombo?.selectedItem = (0 until (sdkCombo?.itemCount ?: 0))
            .mapNotNull { sdkCombo?.getItemAt(it) }
            .firstOrNull { it.id == selected }
            ?: sdkCombo?.getItemAt(0)
        sdkList?.selectedIndex = (0 until model.size()).firstOrNull { model.get(it).id == selected } ?: -1
    }

    private data class SdkChoice(val id: String, val label: String) {
        override fun toString(): String = label
    }
}
