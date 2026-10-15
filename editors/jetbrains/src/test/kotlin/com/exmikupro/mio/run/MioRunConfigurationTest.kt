package com.exmikupro.mio.run

import com.exmikupro.mio.MioFileType
import com.exmikupro.mio.diagnostics.MioSettings
import com.intellij.execution.configurations.ConfigurationTypeUtil
import com.intellij.execution.executors.DefaultRunExecutor
import com.intellij.execution.process.ProcessAdapter
import com.intellij.execution.process.ProcessEvent
import com.intellij.execution.process.ProcessOutputTypes
import com.intellij.execution.runners.ExecutionEnvironmentBuilder
import com.intellij.openapi.util.Key
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import org.jdom.Element
import java.nio.file.Files

class MioRunConfigurationTest : BasePlatformTestCase() {
    fun testGutterMarkerTargetsTopLevelExecutionNotOrdinaryFunction() {
        val contributor = MioRunLineMarkerContributor()
        val functionFile = myFixture.configureByText(MioFileType.INSTANCE, "waza helper() -> unit { kaesu; }")
        assertNull(contributor.getInfo(functionFile.findElementAt(0)!!))

        val programFile = myFixture.configureByText(MioFileType.INSTANCE, "chant(1);")
        assertNotNull(contributor.getInfo(programFile.findElementAt(0)!!))
    }

    fun testPersistsUnicodeAndSpacePaths() {
        val directory = Files.createTempDirectory("Mio 项目 ")
        val source = directory.resolve("主 程序.mio")
        Files.writeString(source, "chant(1);")
        val original = configuration().apply {
            targetPath = source.toString()
            workingDirectory = directory.toString()
            executablePath = directory.resolve("mio 工具").toString()
            programArguments = "--name \"Mio 用户\""
        }
        val element = Element("configuration")
        original.writeExternal(element)
        val restored = configuration()
        restored.readExternal(element)

        assertEquals(original.targetPath, restored.targetPath)
        assertEquals(original.workingDirectory, restored.workingDirectory)
        assertEquals(original.executablePath, restored.executablePath)
        assertEquals(original.programArguments, restored.programArguments)
        directory.toFile().deleteRecursively()
    }

    fun testCapturesStdoutStderrAndExitCode() {
        val directory = Files.createTempDirectory("mio-run-")
        val source = directory.resolve("main.mio")
        Files.writeString(source, "")
        val executable = fakeCli(directory, sleep = false)
        val configuration = configuration().apply {
            targetPath = source.toString()
            executablePath = executable.toString()
            workingDirectory = directory.toString()
        }
        val environment = ExecutionEnvironmentBuilder.create(DefaultRunExecutor.getRunExecutorInstance(), configuration).build()
        val handler = MioCommandLineState(environment, configuration).startProcess()
        val stdout = StringBuilder()
        val stderr = StringBuilder()
        handler.addProcessListener(object : ProcessAdapter() {
            override fun onTextAvailable(event: ProcessEvent, outputType: Key<*>) {
                if (outputType == ProcessOutputTypes.STDERR) stderr.append(event.text) else stdout.append(event.text)
            }
        })
        handler.startNotify()
        assertTrue(handler.waitFor(5_000))
        assertEquals(7, handler.exitCode)
        assertTrue(stdout.toString().contains("标准输出"))
        assertTrue(stderr.toString().contains("标准错误"))
        directory.toFile().deleteRecursively()
    }

    fun testStopsRunningProcess() {
        val directory = Files.createTempDirectory("mio-stop-")
        val source = directory.resolve("main.mio")
        Files.writeString(source, "")
        val executable = fakeCli(directory, sleep = true)
        val configuration = configuration().apply {
            targetPath = source.toString()
            executablePath = executable.toString()
            workingDirectory = directory.toString()
        }
        val environment = ExecutionEnvironmentBuilder.create(DefaultRunExecutor.getRunExecutorInstance(), configuration).build()
        val handler = MioCommandLineState(environment, configuration).startProcess()
        handler.startNotify()
        handler.destroyProcess()
        assertTrue(handler.waitFor(5_000))
        assertTrue(handler.isProcessTerminated)
        directory.toFile().deleteRecursively()
    }

    fun testPassesProjectBunkoToSelectedCli() {
        val directory = Files.createTempDirectory("mio-run-bunko-")
        val source = directory.resolve("main.mio")
        val bunko = directory.resolve("bunko")
        val executable = directory.resolve("mio")
        Files.writeString(source, "chant(1);")
        Files.createDirectories(bunko.resolve("kiso/math"))
        Files.writeString(bunko.resolve("kiso/math/math.mio"), "")
        Files.writeString(
            executable,
            """
                #!/bin/sh
                if [ "${'$'}1" = version ]; then echo 'Mio 0.5.0'; exit 0; fi
                if [ "${'$'}1" = check ]; then echo '{"code":"E0001","severity":"error","message":"探测","labels":[]}' >&2; exit 1; fi
                if grep -q chant "${'$'}2" 2>/dev/null; then echo "${'$'}MIO_BUNKO"; exit 0; fi
                [ "${'$'}#" -eq 2 ]
            """.trimIndent(),
        )
        executable.toFile().setExecutable(true)
        val settings = MioSettings.getInstance(project)
        try {
            settings.bunkoPath = bunko.toString()
            val configuration = configuration().apply {
                targetPath = source.toString()
                executablePath = executable.toString()
                workingDirectory = directory.toString()
            }
            val environment = ExecutionEnvironmentBuilder.create(DefaultRunExecutor.getRunExecutorInstance(), configuration).build()
            val handler = MioCommandLineState(environment, configuration).startProcess()
            val stdout = StringBuilder()
            handler.addProcessListener(object : ProcessAdapter() {
                override fun onTextAvailable(event: ProcessEvent, outputType: Key<*>) {
                    if (outputType != ProcessOutputTypes.STDERR) stdout.append(event.text)
                }
            })
            handler.startNotify()
            assertTrue(handler.waitFor(5_000))
            assertTrue(stdout.toString().lineSequence().any { it.trim() == bunko.toString() })
        } finally {
            settings.bunkoPath = ""
            directory.toFile().deleteRecursively()
        }
    }

    private fun configuration(): MioRunConfiguration {
        val type = ConfigurationTypeUtil.findConfigurationType(MioRunConfigurationType::class.java)
        return type.configurationFactories.single().createTemplateConfiguration(project) as MioRunConfiguration
    }

    private fun fakeCli(directory: java.nio.file.Path, sleep: Boolean): java.nio.file.Path {
        val executable = directory.resolve("mio")
        Files.writeString(
            executable,
            """
                #!/bin/sh
                if [ "${'$'}1" = version ]; then echo 'Mio 0.5.0'; exit 0; fi
                if [ "${'$'}1" = check ]; then echo '{"code":"E0001","severity":"error","message":"探测","labels":[]}' >&2; exit 1; fi
                if [ "${'$'}2" != '${directory.resolve("main.mio")}' ]; then [ "${'$'}#" -eq 2 ]; exit; fi
                ${if (sleep) "sleep 30" else "echo '标准输出'; echo '标准错误' >&2; exit 7"}
            """.trimIndent(),
        )
        executable.toFile().setExecutable(true)
        return executable
    }
}
