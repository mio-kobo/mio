package com.exmikupro.mio.sdk

import java.nio.file.Files
import java.nio.file.Path

object MioBunkoValidator {
    fun problem(path: Path): String? {
        val normalized = path.toAbsolutePath().normalize()
        if (!Files.isDirectory(normalized)) return "Bunko 路径不是目录：$normalized"
        val math = normalized.resolve("kiso").resolve("math")
        if (!Files.isDirectory(math)) return "Bunko 缺少 kiso/math 包：$normalized"
        val hasSource = Files.list(math).use { files -> files.anyMatch { Files.isRegularFile(it) && it.fileName.toString().endsWith(".mio") } }
        return if (hasSource) null else "Bunko 的 kiso/math 包不包含 Mio 源文件：$math"
    }
}
