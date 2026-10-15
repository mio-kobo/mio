package com.exmikupro.mio.run

import com.exmikupro.mio.psi.MioFile
import com.exmikupro.mio.parser.MioElementTypes
import com.intellij.execution.lineMarker.RunLineMarkerContributor
import com.intellij.icons.AllIcons
import com.intellij.psi.PsiComment
import com.intellij.psi.PsiElement
import com.intellij.psi.PsiWhiteSpace
import com.intellij.psi.util.PsiTreeUtil

class MioRunLineMarkerContributor : RunLineMarkerContributor() {
    override fun getInfo(element: PsiElement): Info? {
        val file = element.containingFile as? MioFile ?: return null
        if (file.virtualFile == null || element != firstExecutableLeaf(file)) return null
        return withExecutorActions(AllIcons.RunConfigurations.TestState.Run)
    }

    private fun firstExecutableLeaf(file: MioFile): PsiElement? {
        var leaf: PsiElement? = PsiTreeUtil.getDeepestFirst(file)
        while (leaf != null) {
            if (leaf !is PsiWhiteSpace && leaf !is PsiComment && !insideNonEntryNode(leaf, file)) return leaf
            leaf = PsiTreeUtil.nextLeaf(leaf)
        }
        return null
    }

    private fun insideNonEntryNode(element: PsiElement, file: MioFile): Boolean {
        var current = element.parent
        while (current != null && current != file) {
            if (current.node.elementType == MioElementTypes.DECLARATION || current.node.elementType == MioElementTypes.IMPORT) return true
            current = current.parent
        }
        return false
    }
}
