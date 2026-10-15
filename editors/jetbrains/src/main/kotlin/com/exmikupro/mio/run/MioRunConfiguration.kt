package com.exmikupro.mio.run

import com.exmikupro.mio.MioIcons
import com.exmikupro.mio.cli.MioCliResolver
import com.exmikupro.mio.sdk.MioSdkSelection
import com.intellij.execution.ExecutionException
import com.intellij.execution.Executor
import com.intellij.execution.configurations.CommandLineState
import com.intellij.execution.configurations.ConfigurationFactory
import com.intellij.execution.configurations.ConfigurationTypeBase
import com.intellij.execution.configurations.GeneralCommandLine
import com.intellij.execution.configurations.RunConfiguration
import com.intellij.execution.configurations.RunConfigurationBase
import com.intellij.execution.configurations.RunConfigurationOptions
import com.intellij.execution.configurations.RuntimeConfigurationError
import com.intellij.execution.process.OSProcessHandler
import com.intellij.execution.process.ProcessHandler
import com.intellij.execution.runners.ExecutionEnvironment
import com.intellij.openapi.project.Project
import com.intellij.util.execution.ParametersListUtil
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.Path

class MioRunConfigurationOptions : RunConfigurationOptions() {
    var targetPath by string("")
    var executablePath by string("")
    var workingDirectory by string("")
    var programArguments by string("")
}

class MioRunConfigurationType : ConfigurationTypeBase(
    ID,
    "Mio",
    "运行 Mio 文件或包目录",
    MioIcons.FILE,
) {
    init {
        addFactory(object : ConfigurationFactory(this) {
            override fun createTemplateConfiguration(project: Project): RunConfiguration =
                MioRunConfiguration(project, this, "Mio")

            override fun getId(): String = ID

            override fun getOptionsClass(): Class<out RunConfigurationOptions> = MioRunConfigurationOptions::class.java
        })
    }

    companion object {
        const val ID = "MioRunConfiguration"
    }
}

class MioRunConfiguration(
    project: Project,
    factory: ConfigurationFactory,
    name: String,
) : RunConfigurationBase<MioRunConfigurationOptions>(project, factory, name) {
    init {
        isAllowRunningInParallel = true
    }

    override fun getOptions(): MioRunConfigurationOptions = super.getOptions() as MioRunConfigurationOptions

    var targetPath: String
        get() = options.targetPath.orEmpty()
        set(value) {
            options.targetPath = value
        }

    var executablePath: String
        get() = options.executablePath.orEmpty()
        set(value) {
            options.executablePath = value
        }

    var workingDirectory: String
        get() = options.workingDirectory.orEmpty()
        set(value) {
            options.workingDirectory = value
        }

    var programArguments: String
        get() = options.programArguments.orEmpty()
        set(value) {
            options.programArguments = value
        }

    override fun getConfigurationEditor() = MioRunSettingsEditor(project)

    override fun checkConfiguration() {
        val target = path(targetPath, "请选择要运行的 Mio 文件或包目录。")
        if (!Files.exists(target)) throw RuntimeConfigurationError("运行目标不存在：$target")
        if (Files.isRegularFile(target) && !target.fileName.toString().endsWith(".mio")) {
            throw RuntimeConfigurationError("运行目标必须是 .mio 文件或目录。")
        }
        if (workingDirectory.isNotBlank()) {
            val directory = path(workingDirectory, "工作目录路径无效。")
            if (!Files.isDirectory(directory)) throw RuntimeConfigurationError("工作目录不存在：$directory")
        }
        if (executablePath.isNotBlank()) {
            val executable = path(executablePath, "Mio CLI 路径无效。")
            if (!Files.isRegularFile(executable)) throw RuntimeConfigurationError("Mio CLI 不存在：$executable")
        }
    }

    override fun getState(executor: Executor, environment: ExecutionEnvironment) = MioCommandLineState(environment, this)

    private fun path(value: String, message: String): Path = try {
        Path.of(value).toAbsolutePath().normalize()
    } catch (_: Exception) {
        throw RuntimeConfigurationError(message)
    }
}

class MioCommandLineState(
    environment: ExecutionEnvironment,
    private val configuration: MioRunConfiguration,
) : CommandLineState(environment) {
    public override fun startProcess(): ProcessHandler {
        val resolution = configuration.project.getService(MioCliResolver::class.java).resolve(configuration.executablePath)
        val executable = resolution.executable?.takeIf { resolution.isAvailable }
            ?: throw ExecutionException(resolution.message)
        if (configuration.programArguments.isNotBlank() && !resolution.supportsProgramArguments) {
            throw ExecutionException("当前 Mio CLI 不支持程序参数，请清空参数或更新 CLI。")
        }
        val target = Path.of(configuration.targetPath).toAbsolutePath().normalize()
        val workingDirectory = configuration.workingDirectory.takeIf(String::isNotBlank)
            ?: if (Files.isDirectory(target)) target.toString() else target.parent?.toString()
            ?: configuration.project.basePath
        val commandLine = GeneralCommandLine()
            .withExePath(executable.toString())
            .withWorkDirectory(workingDirectory)
            .withCharset(StandardCharsets.UTF_8)
            .withParameters("run", target.toString())
        MioSdkSelection.bunkoPath(configuration.project)?.let { commandLine.withEnvironment("MIO_BUNKO", it.toString()) }
        if (configuration.programArguments.isNotBlank()) {
            commandLine.addParameters(ParametersListUtil.parse(configuration.programArguments))
        }
        return OSProcessHandler(commandLine).also { it.setShouldDestroyProcessRecursively(true) }
    }
}
