package com.exmikupro.mio

import org.junit.Assert.assertNotNull
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import javax.xml.parsers.DocumentBuilderFactory

class PluginRegistrationTest {
    @Test
    fun `plugin descriptor references loadable implementations`() {
        val resource = javaClass.classLoader.getResourceAsStream("META-INF/plugin.xml")
        assertNotNull(resource)
        val document = resource.use { DocumentBuilderFactory.newInstance().newDocumentBuilder().parse(it) }
        val attributes = listOf("implementationClass", "implementation")
        val classNames = buildList {
            val nodes = document.getElementsByTagName("*")
            for (index in 0 until nodes.length) {
                val node = nodes.item(index)
                for (attribute in attributes) {
                    node.attributes?.getNamedItem(attribute)?.nodeValue?.let(::add)
                }
            }
        }
        assertTrue(classNames.isNotEmpty())
        classNames.forEach { assertNotNull(Class.forName(it)) }
        assertTrue(document.documentElement.textContent.contains("com.exmikupro.mio"))
        assertEquals(
            1,
            document.getElementsByTagName("directoryProjectGenerator").length,
        )
    }
}
