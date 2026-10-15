package com.exmikupro.mio.semantic

import com.intellij.openapi.components.Service
import com.intellij.openapi.diagnostic.Logger
import com.intellij.openapi.progress.ProgressManager
import com.intellij.openapi.project.Project
import com.intellij.openapi.vfs.LocalFileSystem
import com.intellij.openapi.vfs.VirtualFile
import com.intellij.openapi.vfs.VirtualFileManager
import com.intellij.psi.PsiFile
import com.intellij.psi.PsiManager
import com.intellij.psi.util.CachedValueProvider
import com.intellij.psi.util.CachedValuesManager
import com.exmikupro.mio.sdk.MioSdkSelection
import java.nio.file.Path
import java.util.concurrent.ConcurrentHashMap

data class MioPackageInfo(val path: String, val symbols: List<MioSymbol>)
data class MioSymbolLocation(val symbol: MioSymbol, val file: PsiFile, val packagePath: String)

@Service(Service.Level.PROJECT)
class MioProjectModel(private val project: Project) {
    private data class CachedFile(val stamp: Long, val model: MioSemanticModel)
    private data class CachedCatalog(val stamp: Long, val paths: List<String>)

    private val fileModels = ConcurrentHashMap<String, CachedFile>()
    private val catalogs = ConcurrentHashMap<String, CachedCatalog>()

    fun model(file: PsiFile): MioSemanticModel = CachedValuesManager.getCachedValue(file) {
        CachedValueProvider.Result.create(MioSemanticModel.parse(file.text), file)
    }

    fun packageSymbols(file: PsiFile): List<MioSymbol> {
        val current = model(file).topLevelSymbols()
        val directory = file.virtualFile?.parent ?: return current
        val siblings = directory.children.asSequence()
            .filter { it.extension == "mio" && it != file.virtualFile }
            .flatMap { model(it).topLevelSymbols().asSequence() }
        return (current.asSequence() + siblings).distinctBy { it.name to it.kind }.toList()
    }

    fun packageSymbolLocations(file: PsiFile): List<MioSymbolLocation> {
        val directory = file.virtualFile?.parent
            ?: return model(file).topLevelSymbols().map { MioSymbolLocation(it, file, packageName(file)) }
        return mioFiles(directory).flatMap { packageFile ->
            model(packageFile).topLevelSymbols().map { MioSymbolLocation(it, packageFile, packageName(packageFile)) }
        }
    }

    fun importedSymbolLocations(context: PsiFile, path: String): List<MioSymbolLocation> = bunkoRoots(context).asSequence()
        .mapNotNull { resolvePackage(it, path) }
        .flatMap { directory -> mioFiles(directory).asSequence() }
        .flatMap { packageFile ->
            model(packageFile).topLevelSymbols().asSequence()
                .filter { it.exported }
                .map { MioSymbolLocation(it, packageFile, path) }
        }
        .toList()

    fun packageDirectory(context: PsiFile, path: String): VirtualFile? = bunkoRoots(context).asSequence()
        .mapNotNull { resolvePackage(it, path) }
        .firstOrNull()

    fun packageName(file: PsiFile): String {
        val directory = file.virtualFile?.parent ?: return "<default>"
        for (root in bunkoRoots(file)) {
            if (directory == root || !directory.path.startsWith(root.path + "/")) continue
            return directory.path.removePrefix(root.path + "/")
        }
        return directory.name.ifBlank { "<default>" }
    }

    fun packageImports(file: PsiFile): List<MioImport> {
        val current = model(file).imports
        val directory = file.virtualFile?.parent ?: return current
        val siblings = directory.children.asSequence()
            .filter { it.extension == "mio" && it != file.virtualFile }
            .flatMap { model(it).imports.asSequence() }
        return (current.asSequence() + siblings).distinctBy { it.path }.toList()
    }

    fun availablePackages(context: PsiFile): List<MioPackageInfo> = bunkoRoots(context).asSequence()
        .flatMap { root -> packagePaths(root).asSequence().map { path -> root to path } }
        .mapNotNull { (root, path) -> resolvePackage(root, path)?.let { path to it } }
        .map { (path, directory) -> MioPackageInfo(path, packageModels(directory).flatMap { it.topLevelSymbols() }.filter { it.exported }) }
        .filter { it.symbols.isNotEmpty() }
        .distinctBy { it.path }
        .toList()

    fun importPaths(context: PsiFile): List<String> = bunkoRoots(context).asSequence()
        .flatMap { packagePaths(it).asSequence() }
        .distinct()
        .sorted()
        .toList()

    private fun model(file: VirtualFile): MioSemanticModel {
        PsiManager.getInstance(project).findFile(file)?.let { return model(it) }
        val key = file.url
        val cached = fileModels[key]
        if (cached?.stamp == file.modificationStamp) return cached.model
        ProgressManager.checkCanceled()
        val parsed = runCatching { MioSemanticModel.parse(String(file.contentsToByteArray(), file.charset)) }
            .getOrElse {
                LOG.warn("无法读取 Mio 源文件 ${file.path}", it)
                MioSemanticModel.parse("")
            }
        fileModels[key] = CachedFile(file.modificationStamp, parsed)
        return parsed
    }

    private fun packageModels(directory: VirtualFile): List<MioSemanticModel> = directory.children.asSequence()
        .filter { !it.isDirectory && it.extension == "mio" }
        .map(::model)
        .toList()

    private fun mioFiles(directory: VirtualFile): List<PsiFile> = directory.children.asSequence()
        .filter { !it.isDirectory && it.extension == "mio" }
        .mapNotNull { PsiManager.getInstance(project).findFile(it) }
        .toList()

    private fun bunkoRoots(context: PsiFile): List<VirtualFile> {
        val roots = LinkedHashSet<VirtualFile>()
        MioSdkSelection.bunkoPath(project)?.let(LocalFileSystem.getInstance()::findFileByNioFile)
            ?.takeIf { it.isDirectory }?.let(roots::add)
        System.getenv("MIO_BUNKO")?.takeIf { it.isNotBlank() }?.let { path ->
            LocalFileSystem.getInstance().findFileByNioFile(Path.of(path))?.takeIf { it.isDirectory }?.let(roots::add)
        }
        project.basePath?.let(Path::of)?.let(LocalFileSystem.getInstance()::findFileByNioFile)
            ?.findChild("bunko")?.takeIf { it.isDirectory }?.let(roots::add)
        var directory = context.virtualFile?.parent
        while (directory != null) {
            directory.findChild("bunko")?.takeIf { it.isDirectory }?.let(roots::add)
            directory = directory.parent
        }
        return roots.toList()
    }

    private fun packagePaths(root: VirtualFile): List<String> {
        val stamp = VirtualFileManager.VFS_STRUCTURE_MODIFICATIONS.modificationCount
        catalogs[root.url]?.takeIf { it.stamp == stamp }?.let { return it.paths }
        val paths = mutableListOf<String>()
        fun visit(directory: VirtualFile, prefix: String) {
            ProgressManager.checkCanceled()
            val children = directory.children
            if (prefix.isNotEmpty() && children.any { !it.isDirectory && it.extension == "mio" }) paths += prefix
            children.asSequence().filter { it.isDirectory && !it.name.startsWith('.') }.forEach { child ->
                visit(child, if (prefix.isEmpty()) child.name else "$prefix/${child.name}")
            }
        }
        visit(root, "")
        return paths.sorted().also { catalogs[root.url] = CachedCatalog(stamp, it) }
    }

    private fun resolvePackage(root: VirtualFile, path: String): VirtualFile? {
        if (!isValidImportPath(path)) return null
        var current = root
        for (part in path.split('/')) {
            current = current.findChild(part)?.takeIf { it.isDirectory } ?: return null
        }
        return current
    }

    companion object {
        private val LOG = Logger.getInstance(MioProjectModel::class.java)

        fun getInstance(project: Project): MioProjectModel = project.getService(MioProjectModel::class.java)
    }
}
