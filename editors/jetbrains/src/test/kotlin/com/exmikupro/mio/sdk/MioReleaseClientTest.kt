package com.exmikupro.mio.sdk

import com.intellij.openapi.progress.ProgressIndicator
import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test
import java.nio.file.Path

class MioReleaseClientTest {
    @Test
    fun `lists alpha releases without relying on latest endpoint`() {
        val transport = TextTransport(RELEASES_JSON)
        val client = MioReleaseClient(transport)

        assertEquals(listOf("v0.1.0"), client.releases(false).map { it.tag })
        val releases = client.releases(true)
        assertEquals(listOf("v0.1.0-alpha.1", "v0.1.0"), releases.map { it.tag })
        assertEquals(MioReleaseClient.RELEASES_URL, transport.lastUrl)
        assertEquals("mio_v0.1.0-alpha.1_linux_arm64.tar.gz", client.assetFor(releases.first(), MioHostPlatform("linux", "arm64"))?.name)
    }

    @Test
    fun `detects supported operating systems and architectures`() {
        assertEquals("windows/amd64", MioHostPlatform.current("Windows 11", "x86_64")?.displayName)
        assertEquals("linux/amd64", MioHostPlatform.current("Linux", "amd64")?.displayName)
        assertEquals("linux/arm64", MioHostPlatform.current("Linux", "aarch64")?.displayName)
        assertEquals("darwin/amd64", MioHostPlatform.current("Mac OS X", "x64")?.displayName)
        assertEquals("darwin/arm64", MioHostPlatform.current("Darwin", "arm64")?.displayName)
        assertNull(MioHostPlatform.current("Windows 11", "arm64"))
        assertNull(MioHostPlatform.current("Solaris", "sparc"))
    }

    private class TextTransport(private val text: String) : MioReleaseTransport {
        var lastUrl = ""

        override fun read(url: String, indicator: ProgressIndicator?): String {
            lastUrl = url
            return text
        }

        override fun download(url: String, target: Path, indicator: ProgressIndicator?) = error("not used")
    }

    companion object {
        private val RELEASES_JSON = """
            [
              {"tag_name":"v0.1.0-alpha.1","draft":false,"prerelease":true,"html_url":"https://github.com/ExMikuPro/Mio/releases/tag/v0.1.0-alpha.1","assets":[
                {"name":"mio_v0.1.0-alpha.1_linux_arm64.tar.gz","browser_download_url":"https://example.test/mio.tar.gz","size":100,"digest":"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
              ]},
              {"tag_name":"v0.1.0","draft":false,"prerelease":false,"html_url":"https://github.com/ExMikuPro/Mio/releases/tag/v0.1.0","assets":[]},
              {"tag_name":"draft","draft":true,"prerelease":false,"html_url":"https://example.test/draft","assets":[]}
            ]
        """.trimIndent()
    }
}
