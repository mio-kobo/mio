package com.exmikupro.mio.sdk

import com.exmikupro.mio.diagnostics.MioSettings
import com.intellij.openapi.project.Project
import java.nio.file.Files
import java.nio.file.Path

object MioSdkSelection {
    fun selected(project: Project): MioSdk? {
        val id = MioSettings.getInstance(project).selectedSdkId
        return id.takeIf(String::isNotBlank)?.let(MioSdkRegistry.getInstance()::find)
    }

    fun bunkoPath(project: Project): Path? {
        val settings = MioSettings.getInstance(project)
        val configured = settings.bunkoPath.ifBlank { selected(project)?.bunkoPath.orEmpty() }
        return configured.takeIf(String::isNotBlank)
            ?.let { runCatching { Path.of(it).toAbsolutePath().normalize() }.getOrNull() }
            ?.takeIf { MioBunkoValidator.problem(it) == null }
    }
}
