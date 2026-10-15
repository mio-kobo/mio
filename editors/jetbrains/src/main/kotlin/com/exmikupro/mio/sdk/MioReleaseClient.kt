package com.exmikupro.mio.sdk

import com.google.gson.JsonParser
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.util.io.HttpRequests
import java.nio.file.Path

data class MioReleaseAsset(
    val name: String,
    val url: String,
    val size: Long,
    val sha256: String,
)

data class MioRelease(
    val tag: String,
    val prerelease: Boolean,
    val pageUrl: String,
    val assets: List<MioReleaseAsset>,
)

data class MioHostPlatform(val os: String, val arch: String) {
    val assetSuffix: String
        get() = when (os) {
            "windows" -> "_${os}_${arch}.zip"
            else -> "_${os}_${arch}.tar.gz"
        }

    val displayName: String
        get() = "$os/$arch"

    companion object {
        fun current(
            osName: String = System.getProperty("os.name"),
            archName: String = System.getProperty("os.arch"),
        ): MioHostPlatform? {
            val os = when {
                osName.contains("mac", true) || osName.contains("darwin", true) -> "darwin"
                osName.contains("win", true) -> "windows"
                osName.contains("linux", true) -> "linux"
                else -> return null
            }
            val arch = when (archName.lowercase()) {
                "amd64", "x86_64", "x64" -> "amd64"
                "arm64", "aarch64" -> "arm64"
                else -> return null
            }
            if (os == "windows" && arch != "amd64") return null
            return MioHostPlatform(os, arch)
        }
    }
}

interface MioReleaseTransport {
    fun read(url: String, indicator: ProgressIndicator?): String
    fun download(url: String, target: Path, indicator: ProgressIndicator?)
}

class JetBrainsMioReleaseTransport : MioReleaseTransport {
    override fun read(url: String, indicator: ProgressIndicator?): String = request(url).readString(indicator)

    override fun download(url: String, target: Path, indicator: ProgressIndicator?) {
        request(url).saveToFile(target, indicator)
    }

    private fun request(url: String) = HttpRequests.request(url)
        .forceHttps(true)
        .useProxy(true)
        .followRedirects(true)
        .userAgent("Mio-JetBrains-Plugin")
        .accept("application/vnd.github+json")
}

class MioReleaseClient(private val transport: MioReleaseTransport = JetBrainsMioReleaseTransport()) {
    fun releases(includePrereleases: Boolean, indicator: ProgressIndicator? = null): List<MioRelease> {
        val root = JsonParser.parseString(transport.read(RELEASES_URL, indicator)).asJsonArray
        return root.mapNotNull { element ->
            val item = element.asJsonObject
            if (item["draft"]?.asBoolean == true) return@mapNotNull null
            val prerelease = item["prerelease"]?.asBoolean == true
            if (prerelease && !includePrereleases) return@mapNotNull null
            val assets = item["assets"]?.asJsonArray.orEmpty().mapNotNull { assetElement ->
                val asset = assetElement.asJsonObject
                val url = asset["browser_download_url"]?.asString.orEmpty()
                val digest = asset["digest"]?.asString.orEmpty()
                if (!url.startsWith("https://") || !digest.startsWith("sha256:") || digest.length != 71) return@mapNotNull null
                MioReleaseAsset(
                    asset["name"].asString,
                    url,
                    asset["size"]?.asLong ?: 0L,
                    digest.removePrefix("sha256:").lowercase(),
                )
            }
            MioRelease(
                item["tag_name"].asString,
                prerelease,
                item["html_url"].asString,
                assets,
            )
        }
    }

    fun assetFor(release: MioRelease, platform: MioHostPlatform): MioReleaseAsset? =
        release.assets.singleOrNull { it.name.endsWith(platform.assetSuffix) }

    companion object {
        const val RELEASES_URL = "https://api.github.com/repos/ExMikuPro/Mio/releases"
    }
}

private fun com.google.gson.JsonArray?.orEmpty(): List<com.google.gson.JsonElement> = this?.toList().orEmpty()
