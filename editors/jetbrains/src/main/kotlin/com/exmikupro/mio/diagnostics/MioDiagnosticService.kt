package com.exmikupro.mio.diagnostics

import com.exmikupro.mio.cli.MioCliResolver
import com.exmikupro.mio.sdk.MioSdkSelection
import com.intellij.openapi.Disposable
import com.intellij.openapi.components.Service
import com.intellij.openapi.diagnostic.Logger
import com.intellij.openapi.progress.ProcessCanceledException
import com.intellij.openapi.progress.ProgressManager
import com.intellij.openapi.project.Project
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.LinkOption
import java.nio.file.Path
import java.util.concurrent.ConcurrentHashMap
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicLong
import kotlin.io.path.absolutePathString
import kotlin.io.path.exists
import kotlin.io.path.isDirectory
import kotlin.io.path.name

@Service(Service.Level.PROJECT)
class MioDiagnosticService(private val project: Project) : Disposable {
    private data class CacheKey(val file: String, val source: String, val executable: String, val packageStamp: String)
    private data class Running(val generation: Long, val process: Process)

    private val generations = ConcurrentHashMap<String, AtomicLong>()
    private val running = ConcurrentHashMap<String, Running>()
    private val cache = ConcurrentHashMap<CacheKey, MioDiagnosticResult>()

    fun check(fileKey: String, fileName: String, source: String, stamp: Long): MioDiagnosticResult {
        val resolution = project.getService(MioCliResolver::class.java).resolve()
        val executable = resolution.executable?.takeIf { resolution.isAvailable }?.absolutePathString()
            ?: return problem(fileKey, stamp, resolution.message)
        val key = CacheKey(fileKey, source, executable, packageStamp(fileKey))
        val generation = generations.computeIfAbsent(fileKey) { AtomicLong() }.incrementAndGet()
        cache[key]?.let { cached ->
            running.remove(fileKey)?.process?.destroyForcibly()
            return cached.copy(sourceStamp = stamp, generation = generation)
        }
        var tempDirectory: Path? = null
        try {
            ProgressManager.checkCanceled()
            tempDirectory = Files.createTempDirectory("mio-ide-check-")
            val safeName = fileName.takeIf { it.endsWith(".mio") }?.let(Path::of)?.name ?: "source.mio"
            val input = preparePackage(fileKey, safeName, source, tempDirectory)
            val builder = ProcessBuilder(executable, "check", "--format=json", "--color=never", input.absolutePathString())
                .redirectOutput(ProcessBuilder.Redirect.PIPE)
                .redirectError(ProcessBuilder.Redirect.PIPE)
            project.basePath?.let { java.io.File(it) }?.takeIf { it.isDirectory }?.let(builder::directory)
            findBunkoRoot(fileKey)?.let { builder.environment()["MIO_BUNKO"] = it.absolutePathString() }
            val process = builder.start()
            running.put(fileKey, Running(generation, process))?.process?.destroyForcibly()
            val stdout = process.inputStream.bufferedReader(StandardCharsets.UTF_8).readTextAsync()
            val stderr = process.errorStream.bufferedReader(StandardCharsets.UTF_8).readTextAsync()
            val deadline = System.nanoTime() + TimeUnit.SECONDS.toNanos(TIMEOUT_SECONDS)
            while (!process.waitFor(50, TimeUnit.MILLISECONDS)) {
                ProgressManager.checkCanceled()
                if (System.nanoTime() >= deadline) {
                    process.destroyForcibly()
                    return problem(fileKey, stamp, "Mio 检查超时（${TIMEOUT_SECONDS} 秒）。", generation)
                }
            }
            val errorText = stderr.get(2, TimeUnit.SECONDS)
            stdout.get(2, TimeUnit.SECONDS)
            val result = when (process.exitValue()) {
                0 -> MioDiagnosticResult(null, null, stamp, generation, fileKey)
                1 -> MioDiagnosticJson.parse(errorText)?.let {
                    if (input.isDirectory()) {
                        problem(fileKey, stamp, "Mio 已发现包错误，但当前 CLI 未标识具体源文件，无法安全映射错误范围。", generation)
                    } else {
                        MioDiagnosticResult(it, null, stamp, generation, fileKey)
                    }
                } ?: problem(fileKey, stamp, "Mio 返回了无法解析的诊断。", generation)
                else -> problem(fileKey, stamp, errorText.trim().ifEmpty { "Mio 检查进程异常退出。" }, generation)
            }
            if (isCurrent(fileKey, generation)) cache[key] = result
            return result
        } catch (cancelled: ProcessCanceledException) {
            running[fileKey]?.takeIf { it.generation == generation }?.process?.destroyForcibly()
            throw cancelled
        } catch (error: Exception) {
            LOG.info("Mio 诊断检查失败", error)
            return problem(fileKey, stamp, "无法运行 Mio：${error.message ?: error.javaClass.simpleName}", generation)
        } finally {
            running.computeIfPresent(fileKey) { _, current -> current.takeUnless { it.generation == generation } }
            tempDirectory?.toFile()?.deleteRecursively()
        }
    }

    fun isCurrent(fileKey: String, generation: Long): Boolean = generations[fileKey]?.get() == generation

    fun clear() {
        cache.clear()
        running.values.forEach { it.process.destroyForcibly() }
        running.clear()
    }

    override fun dispose() = clear()

    private fun preparePackage(fileKey: String, safeName: String, source: String, directory: Path): Path {
        val original = runCatching { Path.of(fileKey).toAbsolutePath().normalize() }.getOrNull()
        val parent = original?.parent
        var copied = 0
        if (parent != null && parent.isDirectory()) {
            Files.newDirectoryStream(parent, "*.mio").use { entries ->
                for (entry in entries) {
                    ProgressManager.checkCanceled()
                    if (!Files.isRegularFile(entry, LinkOption.NOFOLLOW_LINKS)) continue
                    val target = directory.resolve(entry.fileName.toString())
                    if (entry == original) Files.writeString(target, source, StandardCharsets.UTF_8) else Files.copy(entry, target)
                    copied++
                }
            }
        }
        val current = directory.resolve(safeName)
        if (!current.exists()) {
            Files.writeString(current, source, StandardCharsets.UTF_8)
            copied++
        }
        return if (copied > 1) directory else current
    }

    private fun packageStamp(fileKey: String): String {
        val parent = runCatching { Path.of(fileKey).toAbsolutePath().parent }.getOrNull() ?: return ""
        if (!parent.isDirectory()) return ""
        return runCatching {
            Files.newDirectoryStream(parent, "*.mio").use { entries ->
                entries.asSequence()
                    .filter { Files.isRegularFile(it, LinkOption.NOFOLLOW_LINKS) }
                    .sortedBy { it.fileName.toString() }
                    .joinToString("|") { "${it.fileName}:${Files.getLastModifiedTime(it).toMillis()}:${Files.size(it)}" }
            }
        }.getOrDefault("")
    }

    private fun findBunkoRoot(fileKey: String): Path? {
        MioSdkSelection.bunkoPath(project)?.let { return it }
        System.getenv("MIO_BUNKO")?.takeIf(String::isNotBlank)?.let(Path::of)?.takeIf { it.isDirectory() }?.let { return it }
        var current = runCatching { Path.of(fileKey).toAbsolutePath().parent }.getOrNull()
            ?: project.basePath?.let(Path::of)
        while (current != null) {
            current.resolve("bunko").takeIf { it.isDirectory() }?.let { return it }
            current = current.parent
        }
        return project.basePath?.let(Path::of)?.resolve("bunko")?.takeIf { it.isDirectory() }
    }

    private fun problem(fileKey: String, stamp: Long, message: String, generation: Long = generations.computeIfAbsent(fileKey) { AtomicLong() }.incrementAndGet()) =
        MioDiagnosticResult(null, message, stamp, generation, fileKey)

    private fun java.io.BufferedReader.readTextAsync() = java.util.concurrent.CompletableFuture.supplyAsync { use { it.readText() } }

    companion object {
        private const val TIMEOUT_SECONDS = 30L
        private val LOG = Logger.getInstance(MioDiagnosticService::class.java)
    }
}
