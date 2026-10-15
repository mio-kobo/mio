package com.exmikupro.mio.sdk

import com.exmikupro.mio.cli.MioCliResolution
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.openapi.progress.ProgressManager
import java.io.BufferedInputStream
import java.io.DataInputStream
import java.io.InputStream
import java.nio.charset.StandardCharsets
import java.nio.file.Files
import java.nio.file.LinkOption
import java.nio.file.Path
import java.nio.file.StandardCopyOption
import java.nio.file.attribute.PosixFilePermission
import java.security.MessageDigest
import java.util.UUID
import java.util.zip.GZIPInputStream
import java.util.zip.ZipInputStream
import kotlin.io.path.absolutePathString

class MioSdkInstallException(message: String) : Exception(message)

data class MioSdkInstallRequest(
    val release: MioRelease,
    val asset: MioReleaseAsset,
    val installRoot: Path,
    val platform: MioHostPlatform,
)

class MioSdkInstaller(
    private val transport: MioReleaseTransport = JetBrainsMioReleaseTransport(),
    private val verifier: (Path) -> MioCliResolution,
) {
    fun install(request: MioSdkInstallRequest, indicator: ProgressIndicator? = null): MioSdk {
        require(request.asset.url.startsWith("https://")) { "只允许通过 HTTPS 下载 Mio SDK。" }
        val root = request.installRoot.toAbsolutePath().normalize()
        val target = root.resolve(safeDirectoryName(request.release.tag))
        if (Files.exists(target, LinkOption.NOFOLLOW_LINKS)) throw MioSdkInstallException("Mio SDK 已安装：$target")
        if (request.asset.size <= 0 || request.asset.size > MAX_ARCHIVE_BYTES) {
            throw MioSdkInstallException("GitHub Release 资产大小异常：${request.asset.size} 字节。")
        }
        Files.createDirectories(root)
        val work = root.resolve(".mio-install-${UUID.randomUUID()}")
        val archive = work.resolve(request.asset.name)
        val extracted = work.resolve("content")
        try {
            Files.createDirectory(work)
            transport.download(request.asset.url, archive, indicator)
            indicator?.checkCanceled() ?: ProgressManager.checkCanceled()
            if (request.asset.size > 0 && Files.size(archive) != request.asset.size) {
                throw MioSdkInstallException("下载文件大小与 GitHub Release 元数据不一致。")
            }
            verifySha256(archive, request.asset.sha256)
            Files.createDirectory(extracted)
            when {
                request.asset.name.endsWith(".zip") -> extractZip(archive, extracted, indicator)
                request.asset.name.endsWith(".tar.gz") -> extractTarGz(archive, extracted, indicator)
                else -> throw MioSdkInstallException("不支持的 Mio SDK 压缩格式：${request.asset.name}")
            }
            val executableName = if (request.platform.os == "windows") "mio.exe" else "mio"
            val executable = Files.walk(extracted).use { paths ->
                paths.filter { Files.isRegularFile(it, LinkOption.NOFOLLOW_LINKS) && it.fileName.toString() == executableName }
                    .toList()
                    .singleOrNull()
            } ?: throw MioSdkInstallException("安装包中没有唯一的 $executableName。")
            if (request.platform.os != "windows") makeExecutable(executable)
            val resolution = verifier(executable)
            if (!resolution.isAvailable) throw MioSdkInstallException(resolution.message)
            val expected = request.release.tag.removePrefix("v")
            if (resolution.version != expected) {
                throw MioSdkInstallException("SDK 版本不匹配：Release 为 $expected，实际为 ${resolution.version}。")
            }
            val staging = work.resolve("ready")
            Files.createDirectory(staging)
            Files.walk(extracted).use { paths ->
                paths.forEach { source ->
                    val relative = extracted.relativize(source)
                    if (relative.toString().isEmpty()) return@forEach
                    val destination = staging.resolve(relative)
                    if (Files.isDirectory(source, LinkOption.NOFOLLOW_LINKS)) Files.createDirectories(destination) else {
                        Files.createDirectories(destination.parent)
                        Files.copy(source, destination, StandardCopyOption.COPY_ATTRIBUTES)
                    }
                }
            }
            val executableRelative = extracted.relativize(executable)
            moveAtomically(staging, target)
            return MioSdk(
                name = "Mio ${resolution.version}",
                version = resolution.version,
                executablePath = target.resolve(executableRelative).absolutePathString(),
                platform = request.platform.displayName,
                validation = "CLI 可用；Bunko 未配置",
                supportsJsonDiagnostics = true,
                supportsRun = true,
            )
        } finally {
            work.toFile().deleteRecursively()
        }
    }

    private fun verifySha256(path: Path, expected: String) {
        val digest = MessageDigest.getInstance("SHA-256")
        Files.newInputStream(path).use { input ->
            val buffer = ByteArray(64 * 1024)
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                digest.update(buffer, 0, count)
            }
        }
        val actual = digest.digest().joinToString("") { "%02x".format(it) }
        if (!actual.equals(expected, true)) throw MioSdkInstallException("SHA-256 校验失败，已拒绝安装。")
    }

    private fun extractZip(archive: Path, destination: Path, indicator: ProgressIndicator?) {
        rejectZipSymlinks(archive)
        ZipInputStream(BufferedInputStream(Files.newInputStream(archive))).use { zip ->
            var entries = 0
            var total = 0L
            while (true) {
                indicator?.checkCanceled() ?: ProgressManager.checkCanceled()
                val entry = zip.nextEntry ?: break
                if (++entries > MAX_ENTRIES) throw MioSdkInstallException("SDK 压缩包文件数量异常。")
                val target = safeTarget(destination, entry.name)
                if (entry.isDirectory) Files.createDirectories(target) else {
                    Files.createDirectories(target.parent)
                    total += copyLimited(zip, target, MAX_EXTRACTED_BYTES - total)
                }
                zip.closeEntry()
            }
        }
    }

    private fun extractTarGz(archive: Path, destination: Path, indicator: ProgressIndicator?) {
        GZIPInputStream(BufferedInputStream(Files.newInputStream(archive))).use { input ->
            val header = ByteArray(512)
            var entries = 0
            var total = 0L
            while (readFully(input, header)) {
                indicator?.checkCanceled() ?: ProgressManager.checkCanceled()
                if (header.all { it == 0.toByte() }) break
                if (++entries > MAX_ENTRIES) throw MioSdkInstallException("SDK 压缩包文件数量异常。")
                val name = tarString(header, 0, 100)
                val prefix = tarString(header, 345, 155)
                val fullName = if (prefix.isBlank()) name else "$prefix/$name"
                val size = tarString(header, 124, 12).trim().ifBlank { "0" }.toLongOrNull(8)
                    ?: throw MioSdkInstallException("SDK tar 文件大小字段无效。")
                val type = header[156].toInt().toChar()
                if (type == '1' || type == '2') throw MioSdkInstallException("SDK 压缩包包含符号链接或硬链接。")
                if (size < 0 || total + size > MAX_EXTRACTED_BYTES) throw MioSdkInstallException("SDK 解压体积异常。")
                val target = safeTarget(destination, fullName)
                when (type) {
                    '0', '\u0000' -> {
                        Files.createDirectories(target.parent)
                        Files.newOutputStream(target).use { output -> copyExactly(input, output, size) }
                        total += size
                    }
                    '5' -> Files.createDirectories(target)
                    else -> skipExactly(input, size)
                }
                val padding = (512 - size % 512) % 512
                skipExactly(input, padding)
            }
        }
    }

    private fun rejectZipSymlinks(archive: Path) {
        val bytes = Files.readAllBytes(archive)
        val minimum = maxOf(0, bytes.size - 65_557)
        val end = (bytes.size - 22 downTo minimum).firstOrNull { littleInt(bytes, it) == 0x06054b50 }
            ?: throw MioSdkInstallException("SDK ZIP 目录无效。")
        val entryCount = littleShort(bytes, end + 10)
        var index = littleInt(bytes, end + 16)
        if (index < 0 || index > bytes.size) throw MioSdkInstallException("SDK ZIP 目录偏移无效。")
        repeat(entryCount) {
            if (index + 46 > bytes.size || littleInt(bytes, index) != 0x02014b50) {
                throw MioSdkInstallException("SDK ZIP 目录条目无效。")
            }
            val nameLength = littleShort(bytes, index + 28)
            val extraLength = littleShort(bytes, index + 30)
            val commentLength = littleShort(bytes, index + 32)
            val external = littleInt(bytes, index + 38)
            val unixMode = external ushr 16
            if (unixMode and 0xF000 == 0xA000) throw MioSdkInstallException("SDK ZIP 包含符号链接。")
            index += 46 + nameLength + extraLength + commentLength
            if (index > bytes.size) throw MioSdkInstallException("SDK ZIP 目录条目被截断。")
        }
    }

    private fun safeTarget(root: Path, name: String): Path {
        if (name.isBlank() || name.indexOf('\u0000') >= 0) throw MioSdkInstallException("SDK 压缩包包含无效路径。")
        val target = root.resolve(name.replace('\\', '/')).normalize()
        if (!target.startsWith(root) || Path.of(name.replace('\\', '/')).isAbsolute) {
            throw MioSdkInstallException("SDK 压缩包包含路径穿越条目。")
        }
        return target
    }

    private fun copyLimited(input: InputStream, target: Path, remaining: Long): Long {
        var copied = 0L
        Files.newOutputStream(target).use { output ->
            val buffer = ByteArray(64 * 1024)
            while (true) {
                val count = input.read(buffer)
                if (count < 0) break
                copied += count
                if (copied > remaining) throw MioSdkInstallException("SDK 解压体积异常。")
                output.write(buffer, 0, count)
            }
        }
        return copied
    }

    private fun copyExactly(input: InputStream, output: java.io.OutputStream, size: Long) {
        var remaining = size
        val buffer = ByteArray(64 * 1024)
        while (remaining > 0) {
            val count = input.read(buffer, 0, minOf(buffer.size.toLong(), remaining).toInt())
            if (count < 0) throw MioSdkInstallException("SDK tar 文件被截断。")
            output.write(buffer, 0, count)
            remaining -= count
        }
    }

    private fun skipExactly(input: InputStream, size: Long) {
        var remaining = size
        while (remaining > 0) {
            val skipped = input.skip(remaining)
            if (skipped <= 0) {
                if (input.read() < 0) throw MioSdkInstallException("SDK tar 文件被截断。")
                remaining--
            } else remaining -= skipped
        }
    }

    private fun readFully(input: InputStream, buffer: ByteArray): Boolean {
        var offset = 0
        while (offset < buffer.size) {
            val count = input.read(buffer, offset, buffer.size - offset)
            if (count < 0) {
                if (offset == 0) return false
                throw MioSdkInstallException("SDK tar 文件被截断。")
            }
            offset += count
        }
        return true
    }

    private fun tarString(bytes: ByteArray, offset: Int, length: Int): String {
        val end = (offset until offset + length).firstOrNull { bytes[it] == 0.toByte() } ?: offset + length
        return String(bytes, offset, end - offset, StandardCharsets.UTF_8)
    }

    private fun makeExecutable(path: Path) {
        runCatching {
            val permissions = Files.getPosixFilePermissions(path).toMutableSet()
            permissions += PosixFilePermission.OWNER_EXECUTE
            permissions += PosixFilePermission.GROUP_EXECUTE
            Files.setPosixFilePermissions(path, permissions)
        }.getOrElse {
            if (!path.toFile().setExecutable(true, true)) throw MioSdkInstallException("无法设置 Mio CLI 执行权限。")
        }
    }

    private fun moveAtomically(source: Path, target: Path) {
        Files.move(source, target, StandardCopyOption.ATOMIC_MOVE)
    }

    private fun safeDirectoryName(tag: String): String = tag.replace(Regex("[^A-Za-z0-9._-]"), "_")

    private fun littleShort(bytes: ByteArray, offset: Int): Int =
        (bytes[offset].toInt() and 0xff) or ((bytes[offset + 1].toInt() and 0xff) shl 8)

    private fun littleInt(bytes: ByteArray, offset: Int): Int =
        littleShort(bytes, offset) or (littleShort(bytes, offset + 2) shl 16)

    companion object {
        const val MAX_ARCHIVE_BYTES = 128L * 1024 * 1024
        const val MAX_EXTRACTED_BYTES = 256L * 1024 * 1024
        const val MAX_ENTRIES = 4096
    }
}
