package com.exmikupro.mio.sdk

import com.intellij.openapi.application.ApplicationManager
import com.intellij.openapi.fileChooser.FileChooser
import com.intellij.openapi.fileChooser.FileChooserDescriptorFactory
import com.intellij.openapi.progress.ProgressIndicator
import com.intellij.openapi.progress.ProgressManager
import com.intellij.openapi.progress.Task
import com.intellij.openapi.project.Project
import com.intellij.openapi.ui.Messages
import com.intellij.openapi.util.SystemInfoRt
import java.nio.file.Path

object MioSdkUi {
    fun addLocal(project: Project?, onAdded: (MioSdk) -> Unit) {
        val descriptor = FileChooserDescriptorFactory.singleFile()
            .withTitle("添加本地 Mio SDK")
            .withDescription("选择 Mio CLI 可执行文件。")
        val file = FileChooser.chooseFile(descriptor, project, null) ?: return
        var sdk: MioSdk? = null
        var error: String? = null
        ProgressManager.getInstance().runProcessWithProgressSynchronously(
            {
                val result = MioSdkExecutableProbe.inspect(file.toNioPath())
                if (!result.isAvailable) error = result.message else {
                    val path = result.executable!!
                    sdk = MioSdkRegistry.getInstance().add(
                        MioSdk(
                            name = "Mio ${result.version}",
                            version = result.version,
                            executablePath = path.toString(),
                            platform = MioHostPlatform.current()?.displayName.orEmpty(),
                            validation = "JSON 诊断与 run 可用；Bunko 未配置",
                            supportsJsonDiagnostics = true,
                            supportsRun = true,
                        ),
                    )
                }
            },
            "验证 Mio SDK",
            true,
            project,
        )
        if (error != null) Messages.showErrorDialog(project, error, "无法添加 Mio SDK")
        sdk?.let(onAdded)
    }

    fun download(project: Project?, onAdded: (MioSdk) -> Unit) {
        val platform = MioHostPlatform.current()
        if (platform == null) {
            Messages.showErrorDialog(project, "当前操作系统或 CPU 架构不受官方 Mio SDK 支持。", "无法下载 Mio SDK")
            return
        }
        val prereleaseChoice = Messages.showYesNoCancelDialog(
            project,
            "是否在版本列表中显示预发布版？\n当前官方仓库可能只有 Alpha Release。",
            "选择 Mio SDK 版本范围",
            "显示预发布版",
            "仅稳定版",
            "取消",
            Messages.getQuestionIcon(),
        )
        if (prereleaseChoice == Messages.CANCEL) return
        val includePrereleases = prereleaseChoice == Messages.YES
        var releases: List<MioRelease> = emptyList()
        var loadError: Throwable? = null
        val completed = ProgressManager.getInstance().runProcessWithProgressSynchronously(
            {
                runCatching { MioReleaseClient().releases(includePrereleases, ProgressManager.getInstance().progressIndicator) }
                    .onSuccess { releases = it }
                    .onFailure { loadError = it }
            },
            "读取 GitHub Releases",
            true,
            project,
        )
        if (!completed) return
        loadError?.let {
            Messages.showErrorDialog(project, "无法读取 GitHub Releases：${it.message}", "下载 Mio SDK")
            return
        }
        val compatible = releases.mapNotNull { release -> MioReleaseClient().assetFor(release, platform)?.let { release to it } }
        if (compatible.isEmpty()) {
            val message = if (includePrereleases) "没有适用于 ${platform.displayName} 的 Release。" else "没有稳定版 Mio Release。可重新选择显示预发布版。"
            Messages.showInfoMessage(project, message, "下载 Mio SDK")
            return
        }
        val labels = compatible.map { (release, _) -> if (release.prerelease) "${release.tag}（预发布版）" else release.tag }.toTypedArray()
        val selected = javax.swing.JOptionPane.showInputDialog(
            null,
            "选择要安装的 Mio SDK：",
            "下载 Mio SDK",
            javax.swing.JOptionPane.QUESTION_MESSAGE,
            null,
            labels,
            labels.first(),
        ) as? String ?: return
        val index = labels.indexOf(selected)
        val (release, asset) = compatible[index]
        val expectedVersion = release.tag.removePrefix("v")
        MioSdkRegistry.getInstance().all().firstOrNull {
            it.version == expectedVersion && it.platform == platform.displayName
        }?.let { existing ->
            Messages.showInfoMessage(project, "该版本已安装，将复用现有 SDK：\n${existing.executablePath}", "Mio SDK 已存在")
            onAdded(existing)
            return
        }
        val root = FileChooser.chooseFile(
            FileChooserDescriptorFactory.singleDir().withTitle("选择 Mio SDK 安装目录"),
            project,
            null,
        )?.toNioPath() ?: return
        val target = root.resolve(release.tag)
        val confirmed = Messages.showYesNoDialog(
            project,
            "版本：${release.tag}\n来源：${release.pageUrl}\n安装位置：$target\n\n下载后将校验 SHA-256，并在验证 CLI 后完成安装。",
            "确认安装 Mio SDK",
            Messages.getQuestionIcon(),
        )
        if (confirmed != Messages.YES) return
        var installed: MioSdk? = null
        ProgressManager.getInstance().run(object : Task.Backgroundable(project, "安装 Mio SDK ${release.tag}", true) {
            override fun run(indicator: ProgressIndicator) {
                indicator.text = "正在下载 ${asset.name}"
                installed = MioSdkInstaller(verifier = MioSdkExecutableProbe::inspect).install(
                    MioSdkInstallRequest(release, asset, root, platform),
                    indicator,
                )
            }

            override fun onSuccess() {
                val added = MioSdkRegistry.getInstance().add(installed ?: return)
                onAdded(added)
            }

            override fun onThrowable(error: Throwable) {
                Messages.showErrorDialog(project, error.message ?: error.javaClass.simpleName, "Mio SDK 安装失败")
            }
        })
    }
}
