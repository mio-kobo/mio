package com.exmikupro.mio.cli

import com.exmikupro.mio.diagnostics.MioSettings
import com.exmikupro.mio.sdk.MioSdk
import com.exmikupro.mio.sdk.MioSdkRegistry
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.nio.file.Files

class MioCliResolverTest : BasePlatformTestCase() {
    fun testExplicitRunPathThenProjectSdkThenLegacyPathPriority() {
        val directory = Files.createTempDirectory("mio-sdk-priority-")
        val selected = fakeCli(directory.resolve("selected"), "1.0.0")
        val legacy = fakeCli(directory.resolve("legacy"), "2.0.0")
        val explicit = fakeCli(directory.resolve("explicit"), "3.0.0")
        val registry = MioSdkRegistry.getInstance()
        val sdk = registry.add(MioSdk(name = "selected", executablePath = selected.toString()))
        val settings = MioSettings.getInstance(project)
        val resolver = project.getService(MioCliResolver::class.java)
        try {
            settings.selectedSdkId = sdk.id
            settings.executablePath = legacy.toString()
            assertEquals("1.0.0", resolver.resolve().version)
            assertEquals("3.0.0", resolver.resolve(explicit.toString()).version)
        } finally {
            settings.selectedSdkId = ""
            settings.executablePath = ""
            registry.remove(sdk.id)
            resolver.clear()
            directory.toFile().deleteRecursively()
        }
    }

    fun testConfiguredPathWithSpacesAndUnicodeHasPriority() {
        val directory = Files.createTempDirectory("mio cli 日本語 ")
        val executable = directory.resolve("mio 工具")
        Files.writeString(
            executable,
            """
                #!/bin/sh
                if [ "${'$'}1" = version ]; then echo 'Mio 0.5.0-test'; exit 0; fi
                if [ "${'$'}1" = run ]; then [ "${'$'}#" -eq 2 ]; exit; fi
                echo '{"code":"E0001","severity":"error","message":"探测","labels":[]}' >&2
                exit 1
            """.trimIndent(),
        )
        executable.toFile().setExecutable(true)
        val settings = MioSettings.getInstance(project)
        val resolver = project.getService(MioCliResolver::class.java)
        try {
            settings.executablePath = executable.toString()
            val resolution = resolver.resolve()
            assertTrue(resolution.isAvailable)
            assertEquals(executable.toAbsolutePath(), resolution.executable)
            assertEquals("0.5.0-test", resolution.version)
        } finally {
            settings.executablePath = ""
            resolver.clear()
            directory.toFile().deleteRecursively()
        }
    }

    fun testDistinguishesMissingAndNonExecutableCli() {
        val directory = Files.createTempDirectory("mio-cli-invalid-")
        val resolver = project.getService(MioCliResolver::class.java)
        val missing = resolver.resolve(directory.resolve("missing").toString())
        assertEquals(MioCliProblem.NOT_FOUND, missing.problem)

        val file = directory.resolve("mio")
        Files.writeString(file, "not executable")
        file.toFile().setExecutable(false)
        val denied = resolver.resolve(file.toString())
        if (!com.intellij.openapi.util.SystemInfoRt.isWindows) {
            assertEquals(MioCliProblem.NOT_EXECUTABLE, denied.problem)
        }
        directory.toFile().deleteRecursively()
    }

    fun testRejectsInsufficientCliWithoutJsonDiagnostics() {
        val directory = Files.createTempDirectory("mio-cli-old-")
        val executable = directory.resolve("mio")
        Files.writeString(
            executable,
            """
                #!/bin/sh
                if [ "${'$'}1" = version ]; then echo 'Mio 0.1.0'; exit 0; fi
                echo 'plain diagnostic' >&2
                exit 1
            """.trimIndent(),
        )
        executable.toFile().setExecutable(true)
        val result = project.getService(MioCliResolver::class.java).resolve(executable.toString())
        assertEquals(MioCliProblem.JSON_UNSUPPORTED, result.problem)
        directory.toFile().deleteRecursively()
    }

    private fun fakeCli(path: java.nio.file.Path, version: String): java.nio.file.Path {
        Files.writeString(
            path,
            """
                #!/bin/sh
                if [ "${'$'}1" = version ]; then echo 'Mio $version'; exit 0; fi
                if [ "${'$'}1" = run ]; then [ "${'$'}#" -eq 2 ]; exit; fi
                echo '{"code":"E0001","severity":"error","message":"探测","labels":[]}' >&2
                exit 1
            """.trimIndent(),
        )
        path.toFile().setExecutable(true)
        return path
    }
}
