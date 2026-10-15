package com.exmikupro.mio.diagnostics

import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.nio.file.Files
import java.nio.file.Path

class MioDiagnosticServiceTest : BasePlatformTestCase() {
    fun testRunsConfiguredCompilerAndReusesUnchangedResult() {
        val directory = Files.createTempDirectory("mio-diagnostic-test-")
        val counter = directory.resolve("runs.txt")
        val command = directory.resolve("mio test")
        Files.writeString(
            command,
            """
                #!/bin/sh
                if [ "${'$'}1" = version ]; then echo 'Mio 0.5.0'; exit 0; fi
                if [ "${'$'}1" = run ]; then [ "${'$'}#" -eq 2 ]; exit; fi
                if grep -q '@' "${'$'}4" 2>/dev/null; then
                  echo '{"code":"E0001","severity":"error","message":"探测","labels":[]}' >&2
                  exit 1
                fi
                echo run >> '${counter.toAbsolutePath()}'
                echo '{"code":"E0101","severity":"error","message":"缺少 '\'';'\''","labels":[{"span":{"start":{"offset":5,"line":1,"column":6},"end":{"offset":5,"line":1,"column":6}},"primary":true}],"helps":[{"message":"请在此处添加 '\'';'\''"}]}' >&2
                exit 1
            """.trimIndent(),
        )
        command.toFile().setExecutable(true)
        val settings = MioSettings.getInstance(project)
        val service = project.getService(MioDiagnosticService::class.java)
        try {
            settings.executablePath = command.toString()
            val first = service.check("/project/main.mio", "main.mio", "kaku n = 1", 1)
            val second = service.check("/project/main.mio", "main.mio", "kaku n = 1", 2)

            assertEquals("E0101", first.diagnostic?.code)
            assertEquals(";", first.diagnostic?.message?.substringAfter("'")?.substringBefore("'"))
            assertEquals(1, Files.readAllLines(counter).size)
            assertTrue(second.generation > first.generation)
            assertTrue(service.isCurrent(second.fileKey, second.generation))
        } finally {
            settings.executablePath = ""
            service.clear()
            directory.toFile().deleteRecursively()
        }
    }

    fun testChecksUnsavedSourceWithSiblingPackageFile() {
        val directory = Files.createTempDirectory("mio 包 含空格-")
        val sourceDirectory = directory.resolve("源码")
        Files.createDirectories(sourceDirectory)
        val current = sourceDirectory.resolve("main.mio")
        val sibling = sourceDirectory.resolve("helper.mio")
        Files.writeString(current, "saved")
        Files.writeString(sibling, "sibling")
        val command = directory.resolve("mio")
        Files.writeString(
            command,
            """
                #!/bin/sh
                if [ "${'$'}1" = version ]; then echo 'Mio 0.5.0'; exit 0; fi
                if [ "${'$'}1" = run ]; then [ "${'$'}#" -eq 2 ]; exit; fi
                if [ -f "${'$'}4/probe.mio" ] || grep -q '@' "${'$'}4" 2>/dev/null; then
                  echo '{"code":"E0001","severity":"error","message":"探测","labels":[]}' >&2
                  exit 1
                fi
                test -d "${'$'}4" && test -f "${'$'}4/helper.mio" && grep -q unsaved "${'$'}4/main.mio" || exit 9
                exit 0
            """.trimIndent(),
        )
        command.toFile().setExecutable(true)
        val settings = MioSettings.getInstance(project)
        val service = project.getService(MioDiagnosticService::class.java)
        try {
            settings.executablePath = command.toString()
            val result = service.check(current.toString(), current.fileName.toString(), "unsaved", 3)
            assertNull(result.problem)
            assertNull(result.diagnostic)
            assertEquals("saved", Files.readString(current))
            assertEquals("sibling", Files.readString(sibling))
        } finally {
            settings.executablePath = ""
            service.clear()
            directory.toFile().deleteRecursively()
        }
    }

    fun testReportsAbnormalCompilerExit() {
        val directory = Files.createTempDirectory("mio-abnormal-")
        val source = directory.resolve("main.mio")
        Files.writeString(source, "")
        val command = directory.resolve("mio")
        Files.writeString(
            command,
            """
                #!/bin/sh
                if [ "${'$'}1" = version ]; then echo 'Mio 0.5.0'; exit 0; fi
                if [ "${'$'}1" = run ]; then [ "${'$'}#" -eq 2 ]; exit; fi
                if grep -q '@' "${'$'}4" 2>/dev/null; then
                  echo '{"code":"E0001","severity":"error","message":"探测","labels":[]}' >&2
                  exit 1
                fi
                echo 'compiler crashed' >&2
                exit 9
            """.trimIndent(),
        )
        command.toFile().setExecutable(true)
        val settings = MioSettings.getInstance(project)
        val service = project.getService(MioDiagnosticService::class.java)
        try {
            settings.executablePath = command.toString()
            val result = service.check(source.toString(), source.fileName.toString(), "", 4)
            assertTrue(result.problem.orEmpty().contains("compiler crashed"))
        } finally {
            settings.executablePath = ""
            service.clear()
            directory.toFile().deleteRecursively()
        }
    }
}
