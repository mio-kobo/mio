package com.exmikupro.mio.run

import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory
import com.intellij.openapi.options.SettingsEditor
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.TextFieldWithBrowseButton
import java.awt.GridBagConstraints
import java.awt.GridBagLayout
import java.awt.Insets
import javax.swing.JComponent
import javax.swing.JLabel
import javax.swing.JPanel
import javax.swing.JTextField

class MioRunSettingsEditor(private val project: Project) : SettingsEditor<MioRunConfiguration>() {
    private val targetField = TextFieldWithBrowseButton()
    private val executableField = TextFieldWithBrowseButton()
    private val workingDirectoryField = TextFieldWithBrowseButton()
    private val argumentsField = JTextField()
    private val panel = JPanel(GridBagLayout())

    init {
        targetField.addBrowseFolderListener(
            project,
            FileChooserDescriptorFactory.createSingleFileOrFolderDescriptor()
                .withTitle("选择 Mio 文件或包目录"),
        )
        executableField.addBrowseFolderListener(
            project,
            FileChooserDescriptorFactory.singleFile().withTitle("选择 Mio CLI"),
        )
        workingDirectoryField.addBrowseFolderListener(
            project,
            FileChooserDescriptorFactory.singleDir().withTitle("选择工作目录"),
        )
        addRow(0, "运行目标：", targetField)
        addRow(1, "Mio CLI：", executableField)
        addRow(2, "工作目录：", workingDirectoryField)
        addRow(3, "程序参数：", argumentsField)
    }

    override fun resetEditorFrom(configuration: MioRunConfiguration) {
        targetField.text = configuration.targetPath
        executableField.text = configuration.executablePath
        workingDirectoryField.text = configuration.workingDirectory
        argumentsField.text = configuration.programArguments
    }

    override fun applyEditorTo(configuration: MioRunConfiguration) {
        configuration.targetPath = targetField.text.trim()
        configuration.executablePath = executableField.text.trim()
        configuration.workingDirectory = workingDirectoryField.text.trim()
        configuration.programArguments = argumentsField.text.trim()
    }

    override fun createEditor(): JComponent = panel

    private fun addRow(row: Int, label: String, component: JComponent) {
        panel.add(JLabel(label), GridBagConstraints().apply {
            gridx = 0
            gridy = row
            anchor = GridBagConstraints.WEST
            insets = Insets(4, 0, 4, 8)
        })
        panel.add(component, GridBagConstraints().apply {
            gridx = 1
            gridy = row
            weightx = 1.0
            fill = GridBagConstraints.HORIZONTAL
            insets = Insets(4, 0, 4, 0)
        })
    }
}
