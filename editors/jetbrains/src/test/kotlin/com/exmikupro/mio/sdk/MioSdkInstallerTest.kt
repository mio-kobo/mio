package com.exmikupro.mio.sdk

import com.exmikupro.mio.cli.MioCliResolution
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.openapi.progress.ProcessCanceledException
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertThrows
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.nio.file.Files
import java.nio.file.Path
import java.security.MessageDigest
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

class MioSdkInstallerTest {
    @Test
    fun `installs verified archive atomically`() {
        val root = Files.createTempDirectory("mio-sdk-install-")
        try {
            val archive = zipOf("mio" to "binary", "README.md" to "Mio")
            val request = request(root, archive)
            val installer = MioSdkInstaller(BytesTransport(archive)) { MioCliResolution(it, "1.2.3") }

            val sdk = installer.install(request)

            assertEquals("1.2.3", sdk.version)
            assertTrue(Files.isRegularFile(root.resolve("v1.2.3/mio")))
            assertFalse(Files.list(root).use { it.anyMatch { path -> path.fileName.toString().startsWith(".mio-install-") } })
        } finally {
            root.toFile().deleteRecursively()
        }
    }

    @Test
    fun `rejects checksum mismatch and cleans temporary files`() {
        val root = Files.createTempDirectory("mio-sdk-checksum-")
        try {
            val archive = zipOf("mio" to "binary")
            val bad = request(root, archive).copy(asset = request(root, archive).asset.copy(sha256 = "0".repeat(64)))
            val installer = MioSdkInstaller(BytesTransport(archive)) { MioCliResolution(it, "1.2.3") }

            assertThrows(MioSdkInstallException::class.java) { installer.install(bad) }
            assertTrue(Files.list(root).use { it.findAny().isEmpty })
        } finally {
            root.toFile().deleteRecursively()
        }
    }

    @Test
    fun `rejects path traversal`() {
        val root = Files.createTempDirectory("mio-sdk-traversal-")
        try {
            val archive = zipOf("../mio" to "binary")
            val installer = MioSdkInstaller(BytesTransport(archive)) { MioCliResolution(it, "1.2.3") }
            val error = assertThrows(MioSdkInstallException::class.java) { installer.install(request(root, archive)) }
            assertTrue(error.message.orEmpty().contains("路径穿越"))
            assertFalse(Files.exists(root.parent.resolve("mio")))
        } finally {
            root.toFile().deleteRecursively()
        }
    }

    @Test
    fun `rejects zip symbolic links`() {
        val root = Files.createTempDirectory("mio-sdk-symlink-")
        try {
            val archive = zipOf("mio" to "target").also(::markFirstZipEntryAsSymlink)
            val installer = MioSdkInstaller(BytesTransport(archive)) { MioCliResolution(it, "1.2.3") }
            val error = assertThrows(MioSdkInstallException::class.java) { installer.install(request(root, archive)) }
            assertTrue(error.message.orEmpty().contains("符号链接"))
        } finally {
            root.toFile().deleteRecursively()
        }
    }

    @Test
    fun `does not overwrite an installed version`() {
        val root = Files.createTempDirectory("mio-sdk-duplicate-")
        try {
            Files.createDirectory(root.resolve("v1.2.3"))
            val archive = zipOf("mio" to "binary")
            val installer = MioSdkInstaller(BytesTransport(archive)) { MioCliResolution(it, "1.2.3") }
            assertThrows(MioSdkInstallException::class.java) { installer.install(request(root, archive)) }
        } finally {
            root.toFile().deleteRecursively()
        }
    }

    @Test
    fun `rejects SDK version mismatch`() {
        val root = Files.createTempDirectory("mio-sdk-version-")
        try {
            val archive = zipOf("mio" to "binary")
            val installer = MioSdkInstaller(BytesTransport(archive)) { MioCliResolution(it, "9.9.9") }
            val error = assertThrows(MioSdkInstallException::class.java) { installer.install(request(root, archive)) }
            assertTrue(error.message.orEmpty().contains("版本不匹配"))
            assertFalse(Files.exists(root.resolve("v1.2.3")))
        } finally {
            root.toFile().deleteRecursively()
        }
    }

    @Test
    fun `cleans up after network failure and cancellation`() {
        val root = Files.createTempDirectory("mio-sdk-network-")
        try {
            val archive = zipOf("mio" to "binary")
            val failure = object : MioReleaseTransport {
                override fun read(url: String, indicator: ProgressIndicator?) = error("not used")
                override fun download(url: String, target: Path, indicator: ProgressIndicator?) {
                    throw java.io.IOException("connection reset")
                }
            }
            assertThrows(java.io.IOException::class.java) {
                MioSdkInstaller(failure) { MioCliResolution(it, "1.2.3") }.install(request(root, archive))
            }
            val cancellation = object : MioReleaseTransport {
                override fun read(url: String, indicator: ProgressIndicator?) = error("not used")
                override fun download(url: String, target: Path, indicator: ProgressIndicator?) {
                    throw ProcessCanceledException()
                }
            }
            assertThrows(ProcessCanceledException::class.java) {
                MioSdkInstaller(cancellation) { MioCliResolution(it, "1.2.3") }.install(request(root, archive))
            }
            assertTrue(Files.list(root).use { it.findAny().isEmpty })
        } finally {
            root.toFile().deleteRecursively()
        }
    }

    private fun request(root: Path, bytes: ByteArray): MioSdkInstallRequest {
        val hash = MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") { "%02x".format(it) }
        val asset = MioReleaseAsset("mio_v1.2.3_linux_amd64.zip", "https://example.test/mio.zip", bytes.size.toLong(), hash)
        return MioSdkInstallRequest(
            MioRelease("v1.2.3", false, "https://example.test/release", listOf(asset)),
            asset,
            root,
            MioHostPlatform("linux", "amd64"),
        )
    }

    private fun zipOf(vararg files: Pair<String, String>): ByteArray {
        val output = ByteArrayOutputStream()
        ZipOutputStream(output).use { zip ->
            files.forEach { (name, contents) ->
                zip.putNextEntry(ZipEntry(name))
                zip.write(contents.toByteArray())
                zip.closeEntry()
            }
        }
        return output.toByteArray()
    }

    private fun markFirstZipEntryAsSymlink(bytes: ByteArray) {
        val signature = byteArrayOf(0x50, 0x4b, 0x01, 0x02)
        val index = bytes.indices.first { offset ->
            offset + signature.size <= bytes.size && bytes.copyOfRange(offset, offset + signature.size).contentEquals(signature)
        }
        val external = 0xA1FF0000.toInt()
        for (part in 0..3) bytes[index + 38 + part] = (external ushr (8 * part)).toByte()
    }

    private class BytesTransport(private val bytes: ByteArray) : MioReleaseTransport {
        override fun read(url: String, indicator: ProgressIndicator?) = error("not used")

        override fun download(url: String, target: Path, indicator: ProgressIndicator?) {
            Files.write(target, bytes)
        }
    }
}
