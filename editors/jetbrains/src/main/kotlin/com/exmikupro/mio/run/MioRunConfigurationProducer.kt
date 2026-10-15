package com.exmikupro.mio.run

import com.exmikupro.mio.psi.MioFile
import com.intellij.execution.actions.ConfigurationContext
import com.intellij.execution.actions.RunConfigurationProducer
import com.intellij.execution.configurations.ConfigurationFactory
import com.intellij.execution.configurations.ConfigurationTypeUtil
import com.intellij.openapi.util.Ref
import com.intellij.psi.PsiDirectory
import com.intellij.psi.PsiElement
import java.nio.file.Files
import java.nio.file.Path

class MioRunConfigurationProducer : RunConfigurationProducer<MioRunConfiguration>(false) {
    override fun getConfigurationFactory(): ConfigurationFactory =
        ConfigurationTypeUtil.findConfigurationType(MioRunConfigurationType::class.java).configurationFactories.single()

    override fun setupConfigurationFromContext(
        configuration: MioRunConfiguration,
        context: ConfigurationContext,
        sourceElement: Ref<PsiElement>,
    ): Boolean {
        val target = target(context.psiLocation) ?: return false
        configuration.targetPath = target.toString()
        configuration.workingDirectory = if (Files.isDirectory(target)) target.toString() else target.parent?.toString().orEmpty()
        configuration.name = "Mio: ${target.fileName}"
        sourceElement.set(context.psiLocation)
        return true
    }

    override fun isConfigurationFromContext(configuration: MioRunConfiguration, context: ConfigurationContext): Boolean {
        val target = target(context.psiLocation) ?: return false
        return runCatching { Path.of(configuration.targetPath).toAbsolutePath().normalize() == target }.getOrDefault(false)
    }

    private fun target(element: PsiElement?): Path? {
        val file = element?.containingFile
        if (file is MioFile) return file.virtualFile?.path?.let(Path::of)?.toAbsolutePath()?.normalize()
        val directory = element as? PsiDirectory ?: return null
        return directory.virtualFile.toNioPath().takeIf { path ->
            runCatching { Files.list(path).use { files -> files.anyMatch { it.fileName.toString().endsWith(".mio") } } }.getOrDefault(false)
        }
    }
}
