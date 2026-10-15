package com.exmikupro.mio.sdk

import com.exmikupro.mio.diagnostics.MioSettings
import com.intellij.testFramework.fixtures.BasePlatformTestCase
import java.nio.file.Files

class MioSdkSelectionTest : BasePlatformTestCase() {
    fun testProjectBunkoOverrideHasPriorityOverSdkBunko() {
        val directory = Files.createTempDirectory("mio-bunko-selection-")
        val executable = directory.resolve("mio")
        val sdkBunko = directory.resolve("sdk-bunko")
        val projectBunko = directory.resolve("project-bunko")
        Files.writeString(executable, "mio")
        Files.createDirectories(sdkBunko.resolve("kiso/math"))
        Files.createDirectories(projectBunko.resolve("kiso/math"))
        Files.writeString(sdkBunko.resolve("kiso/math/math.mio"), "")
        Files.writeString(projectBunko.resolve("kiso/math/math.mio"), "")
        val registry = MioSdkRegistry.getInstance()
        val sdk = registry.add(MioSdk(executablePath = executable.toString(), bunkoPath = sdkBunko.toString()))
        val settings = MioSettings.getInstance(project)
        try {
            settings.selectedSdkId = sdk.id
            assertEquals(sdkBunko, MioSdkSelection.bunkoPath(project))
            settings.bunkoPath = projectBunko.toString()
            assertEquals(projectBunko, MioSdkSelection.bunkoPath(project))
        } finally {
            settings.selectedSdkId = ""
            settings.bunkoPath = ""
            registry.remove(sdk.id)
            directory.toFile().deleteRecursively()
        }
    }

    fun testProjectSelectionStateRoundTripsIndependently() {
        val first = MioSettings().apply {
            selectedSdkId = "sdk-one"
            bunkoPath = "/bunko/one"
        }
        val second = MioSettings().apply {
            selectedSdkId = "sdk-two"
            bunkoPath = "/bunko/two"
        }
        val restoredFirst = MioSettings().apply { loadState(first.state) }
        val restoredSecond = MioSettings().apply { loadState(second.state) }

        assertEquals("sdk-one", restoredFirst.selectedSdkId)
        assertEquals("/bunko/one", restoredFirst.bunkoPath)
        assertEquals("sdk-two", restoredSecond.selectedSdkId)
        assertEquals("/bunko/two", restoredSecond.bunkoPath)
    }
}
