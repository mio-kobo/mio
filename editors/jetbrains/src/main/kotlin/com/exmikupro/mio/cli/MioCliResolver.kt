package com.exmikupro.mio.cli

import com.exmikupro.mio.diagnostics.MioSettings
import com.exmikupro.mio.sdk.MioSdkExecutableProbe
import com.exmikupro.mio.sdk.MioSdkSelection
import com.intellij.openapi.Disposable
import com.intellij.openapi.components.Service
import com.intellij.openapi.project.Project
import com.intellij.openapi.util.SystemInfoRt
import java.nio.file.Files
import java.nio.file.Path
import kotlin.io.path.fileSize
import kotlin.io.path.getLastModifiedTime
import kotlin.io.path.isExecutable
import kotlin.io.path.isRegularFile

enum class MioCliProblem {
    NOT_FOUND,
    NOT_EXECUTABLE,
    INCOMPATIBLE,
    VERSION_UNAVAILABLE,
    JSON_UNSUPPORTED,
    RUN_UNSUPPORTED,
}

data class MioCliResolution(
    val executable: Path?,
    val version: String = "",
    val problem: MioCliProblem? = null,
    val message: String = "",
    val supportsProgramArguments: Boolean = false,
) {
    val isAvailable: Boolean
        get() = executable != null && problem == null
}

@Service(Service.Level.PROJECT)
class MioCliResolver(private val project: Project) : Disposable {
    private data class CacheKey(val path: Path, val modified: Long, val size: Long)

    @Volatile
    private var cached: Pair<CacheKey, MioCliResolution>? = null

    fun resolve(configurationPath: String = ""): MioCliResolution {
        val settings = MioSettings.getInstance(project)
        val selected = when {
            configurationPath.isNotBlank() -> configured(configurationPath)
            MioSdkSelection.selected(project)?.executablePath?.isNotBlank() == true ->
                configured(MioSdkSelection.selected(project)!!.executablePath)
            settings.executablePath.isNotBlank() -> configured(settings.executablePath)
            else -> discover()
        }
        if (selected.problem != null) return selected
        val path = selected.executable ?: return selected
        val key = runCatching {
            CacheKey(path, path.getLastModifiedTime().toMillis(), path.fileSize())
        }.getOrElse { return failure(path, MioCliProblem.NOT_EXECUTABLE, "无法读取 Mio CLI：${it.message}") }
        cached?.takeIf { it.first == key }?.let { return it.second }
        val probed = MioSdkExecutableProbe.inspect(path)
        cached = key to probed
        return probed
    }

    fun clear() {
        cached = null
    }

    fun inspect(path: Path): MioCliResolution = validate(path)?.takeIf { it.problem != null } ?: MioSdkExecutableProbe.inspect(path)

    override fun dispose() = clear()

    private fun configured(value: String): MioCliResolution {
        val path = runCatching { Path.of(value).toAbsolutePath().normalize() }.getOrNull()
            ?: return failure(null, MioCliProblem.NOT_FOUND, "配置的 Mio CLI 路径无效。")
        return validate(path)!!
    }

    private fun discover(): MioCliResolution {
        val projectRoot = project.basePath?.let { runCatching { Path.of(it).toRealPath() }.getOrNull() }
        val executableNames = if (SystemInfoRt.isWindows) listOf("mio.exe", "mio.cmd", "mio.bat") else listOf("mio")
        val candidates = System.getenv("PATH").orEmpty().split(java.io.File.pathSeparatorChar)
            .asSequence()
            .filter(String::isNotBlank)
            .flatMap { directory -> executableNames.asSequence().map { Path.of(directory).resolve(it).toAbsolutePath().normalize() } }
            .filterNot { candidate ->
                if (projectRoot == null) false else runCatching { candidate.toRealPath().startsWith(projectRoot) }.getOrDefault(false)
            }
        var firstProblem: MioCliResolution? = null
        for (candidate in candidates) {
            val result = validate(candidate, missingIsProblem = false) ?: continue
            if (result.problem == null) return result
            if (firstProblem == null) firstProblem = result
        }
        if (firstProblem != null) return firstProblem
        return failure(null, MioCliProblem.NOT_FOUND, "未在 PATH 中找到可信的 Mio CLI，请在 Mio 设置中手动配置绝对路径。")
    }

    private fun validate(path: Path, missingIsProblem: Boolean = true): MioCliResolution? {
        if (!path.isRegularFile()) {
            return if (missingIsProblem) failure(path, MioCliProblem.NOT_FOUND, "找不到配置的 Mio CLI：$path") else null
        }
        if (!SystemInfoRt.isWindows && !path.isExecutable()) {
            return failure(path, MioCliProblem.NOT_EXECUTABLE, "Mio CLI 没有执行权限：$path")
        }
        val compatibility = MioBinaryCompatibility.inspect(path)
        if (compatibility != null) {
            return failure(path, MioCliProblem.INCOMPATIBLE, "Mio CLI 与当前平台不兼容：$compatibility")
        }
        return MioCliResolution(path)
    }

    private fun failure(path: Path?, problem: MioCliProblem, message: String) =
        MioCliResolution(path, problem = problem, message = message)
}

object MioBinaryCompatibility {
    fun inspect(path: Path, osName: String = System.getProperty("os.name"), archName: String = System.getProperty("os.arch")): String? {
        val bytes = runCatching { Files.newInputStream(path).use { it.readNBytes(256) } }.getOrNull() ?: return null
        val hostOs = normalizeOs(osName)
        val hostArch = normalizeArch(archName)
        val binary = binaryPlatform(bytes) ?: return null
        if (binary.first != hostOs) return "目标系统为 ${binary.first}，当前系统为 $hostOs"
        if (binary.second != "universal" && binary.second != hostArch) return "目标架构为 ${binary.second}，当前架构为 $hostArch"
        return null
    }

    private fun binaryPlatform(bytes: ByteArray): Pair<String, String>? {
        if (bytes.size >= 20 && bytes[0] == 0x7f.toByte() && bytes.copyOfRange(1, 4).contentEquals("ELF".toByteArray())) {
            val little = bytes[5].toInt() == 1
            val machine = unsigned16(bytes, 18, little)
            return "Linux" to when (machine) {
                0x3e -> "x86_64"
                0xb7 -> "aarch64"
                else -> "unknown-$machine"
            }
        }
        if (bytes.size >= 64 && bytes[0] == 'M'.code.toByte() && bytes[1] == 'Z'.code.toByte()) {
            val offset = unsigned32(bytes, 0x3c, true)
            if (offset >= 0 && offset + 6 <= bytes.size && bytes.copyOfRange(offset, offset + 4).contentEquals(byteArrayOf('P'.code.toByte(), 'E'.code.toByte(), 0, 0))) {
                return "Windows" to when (unsigned16(bytes, offset + 4, true)) {
                    0x8664 -> "x86_64"
                    0xaa64 -> "aarch64"
                    else -> "unknown"
                }
            }
        }
        if (bytes.size >= 8) {
            val magic = unsigned32(bytes, 0, false)
            if (magic == 0xcafebabe.toInt() || magic == 0xbebafeca.toInt()) return "macOS" to "universal"
            val little = magic == 0xcffaedfe.toInt()
            if (magic == 0xfeedfacf.toInt() || little) {
                val cpu = unsigned32(bytes, 4, little)
                return "macOS" to when (cpu) {
                    0x01000007 -> "x86_64"
                    0x0100000c -> "aarch64"
                    else -> "unknown"
                }
            }
        }
        return null
    }

    private fun unsigned16(bytes: ByteArray, offset: Int, little: Boolean): Int {
        val first = bytes[offset].toInt() and 0xff
        val second = bytes[offset + 1].toInt() and 0xff
        return if (little) first or (second shl 8) else (first shl 8) or second
    }

    private fun unsigned32(bytes: ByteArray, offset: Int, little: Boolean): Int {
        if (offset < 0 || offset + 4 > bytes.size) return -1
        var result = 0
        for (index in 0..3) {
            val source = if (little) offset + 3 - index else offset + index
            result = (result shl 8) or (bytes[source].toInt() and 0xff)
        }
        return result
    }

    private fun normalizeOs(value: String): String = when {
        value.contains("win", ignoreCase = true) -> "Windows"
        value.contains("mac", ignoreCase = true) || value.contains("darwin", ignoreCase = true) -> "macOS"
        else -> "Linux"
    }

    private fun normalizeArch(value: String): String = when (value.lowercase()) {
        "amd64", "x86_64", "x64" -> "x86_64"
        "arm64", "aarch64" -> "aarch64"
        else -> value.lowercase()
    }
}
