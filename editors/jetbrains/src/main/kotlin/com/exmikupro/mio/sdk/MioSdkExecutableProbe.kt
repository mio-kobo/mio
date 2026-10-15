package com.exmikupro.mio.sdk

import com.exmikupro.mio.cli.MioBinaryCompatibility
import com.exmikupro.mio.cli.MioCliProblem
import com.exmikupro.mio.cli.MioCliResolution
import com.exmikupro.mio.diagnostics.MioDiagnosticJson
import com.intellij.openapi.util.SystemInfoRt
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.Path
import java.util.concurrent.CompletableFuture
import java.util.concurrent.TimeUnit
import kotlin.io.path.absolutePathString
import kotlin.io.path.isExecutable
import kotlin.io.path.isRegularFile

object MioSdkExecutableProbe {
    fun inspect(path: Path): MioCliResolution {
        val normalized = path.toAbsolutePath().normalize()
        if (!normalized.isRegularFile()) return failure(normalized, MioCliProblem.NOT_FOUND, "找不到 Mio CLI：$normalized")
        if (!SystemInfoRt.isWindows && !normalized.isExecutable()) {
            return failure(normalized, MioCliProblem.NOT_EXECUTABLE, "Mio CLI 没有执行权限：$normalized")
        }
        MioBinaryCompatibility.inspect(normalized)?.let {
            return failure(normalized, MioCliProblem.INCOMPATIBLE, "Mio CLI 与当前平台不兼容：$it")
        }
        val version = run(normalized, listOf("version"))
        if (version == null || version.exitCode != 0 || !version.stdout.trim().startsWith("Mio ")) {
            return failure(normalized, MioCliProblem.VERSION_UNAVAILABLE, "该文件不是可识别的 Mio CLI，无法读取版本信息。")
        }
        val directory = Files.createTempDirectory("mio-ide-sdk-probe-")
        return try {
            val source = directory.resolve("probe.mio")
            Files.writeString(source, "@", StandardCharsets.UTF_8)
            val check = run(normalized, listOf("check", "--format=json", "--color=never", source.absolutePathString()))
            if (check == null || check.exitCode != 1 || MioDiagnosticJson.parse(check.stderr) == null) {
                failure(normalized, MioCliProblem.JSON_UNSUPPORTED, "Mio CLI 不支持 IDE 所需的 JSON 诊断。")
            } else {
                Files.writeString(source, "", StandardCharsets.UTF_8)
                val run = run(normalized, listOf("run", source.absolutePathString()))
                if (run == null || run.exitCode != 0) {
                    failure(normalized, MioCliProblem.RUN_UNSUPPORTED, "Mio CLI 不支持运行源码。")
                } else {
                    val arguments = run(normalized, listOf("run", source.absolutePathString(), "mio-ide-argument-probe"))
                    MioCliResolution(
                        normalized,
                        version.stdout.trim().removePrefix("Mio "),
                        supportsProgramArguments = arguments?.exitCode == 0,
                    )
                }
            }
        } finally {
            directory.toFile().deleteRecursively()
        }
    }

    private fun run(path: Path, arguments: List<String>): CommandResult? = runCatching {
        val process = ProcessBuilder(listOf(path.absolutePathString()) + arguments)
            .redirectOutput(ProcessBuilder.Redirect.PIPE)
            .redirectError(ProcessBuilder.Redirect.PIPE)
            .start()
        val stdout = CompletableFuture.supplyAsync { process.inputStream.bufferedReader(StandardCharsets.UTF_8).use { it.readText() } }
        val stderr = CompletableFuture.supplyAsync { process.errorStream.bufferedReader(StandardCharsets.UTF_8).use { it.readText() } }
        if (!process.waitFor(5, TimeUnit.SECONDS)) {
            process.destroyForcibly()
            return@runCatching null
        }
        CommandResult(process.exitValue(), stdout.get(1, TimeUnit.SECONDS), stderr.get(1, TimeUnit.SECONDS))
    }.getOrNull()

    private fun failure(path: Path, problem: MioCliProblem, message: String) =
        MioCliResolution(path, problem = problem, message = message)

    private data class CommandResult(val exitCode: Int, val stdout: String, val stderr: String)
}
