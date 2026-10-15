package com.exmikupro.mio.sdk

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.components.PersistentStateComponent
import com.intellij.openapi.components.Service
import com.intellij.openapi.components.State
import com.intellij.openapi.components.Storage
import java.nio.file.Files
import java.nio.file.Path
import java.util.UUID

data class MioSdk(
    var id: String = UUID.randomUUID().toString(),
    var name: String = "Mio SDK",
    var version: String = "",
    var executablePath: String = "",
    var bunkoPath: String = "",
    var platform: String = "",
    var validation: String = "尚未验证",
    var supportsJsonDiagnostics: Boolean = false,
    var supportsRun: Boolean = false,
)

@Service(Service.Level.APP)
@State(name = "MioSdkRegistry", storages = [Storage("mioSdks.xml")])
class MioSdkRegistry : PersistentStateComponent<MioSdkRegistry.State> {
    data class State(var sdks: MutableList<MioSdk> = mutableListOf())

    private var value = State()

    override fun getState(): State = value

    override fun loadState(state: State) {
        value = state
    }

    fun all(): List<MioSdk> = value.sdks.map(MioSdk::copy)

    fun find(id: String): MioSdk? = value.sdks.firstOrNull { it.id == id }?.copy()

    fun findByExecutable(path: Path): MioSdk? {
        val normalized = path.toAbsolutePath().normalize()
        return value.sdks.firstOrNull {
            runCatching { Path.of(it.executablePath).toAbsolutePath().normalize() == normalized }.getOrDefault(false)
        }?.copy()
    }

    @Synchronized
    fun add(sdk: MioSdk): MioSdk {
        require(sdk.executablePath.isNotBlank()) { "Mio SDK 可执行文件路径不能为空。" }
        val path = Path.of(sdk.executablePath).toAbsolutePath().normalize()
        require(Files.isRegularFile(path)) { "Mio SDK 可执行文件不存在：$path" }
        val existing = findByExecutable(path)
        if (existing != null) return existing
        val stored = sdk.copy(executablePath = path.toString())
        value.sdks.add(stored)
        return stored.copy()
    }

    @Synchronized
    fun update(sdk: MioSdk) {
        val index = value.sdks.indexOfFirst { it.id == sdk.id }
        require(index >= 0) { "找不到 Mio SDK：${sdk.id}" }
        value.sdks[index] = sdk.copy()
    }

    @Synchronized
    fun remove(id: String) {
        value.sdks.removeIf { it.id == id }
    }

    companion object {
        fun getInstance(): MioSdkRegistry = ApplicationManager.getApplication().getService(MioSdkRegistry::class.java)
    }
}
