package com.exmikupro.mio.cli

import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Files

class MioBinaryCompatibilityTest {
    @Test
    fun `rejects foreign elf architecture`() {
        val path = Files.createTempFile("mio-arm64-", "")
        val header = ByteArray(64)
        header[0] = 0x7f
        "ELF".toByteArray().copyInto(header, 1)
        header[5] = 1
        header[18] = 0xb7.toByte()
        header[19] = 0
        Files.write(path, header)
        assertTrue(MioBinaryCompatibility.inspect(path, "Linux", "x86_64")!!.contains("aarch64"))
        assertNull(MioBinaryCompatibility.inspect(path, "Linux", "aarch64"))
        Files.deleteIfExists(path)
    }
}
