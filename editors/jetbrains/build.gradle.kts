import org.jetbrains.intellij.platform.gradle.IntelliJPlatformType
import org.jetbrains.intellij.platform.gradle.TestFrameworkType

plugins {
    kotlin("jvm") version "2.2.20"
    id("org.jetbrains.intellij.platform") version "2.19.0"
}

group = "com.exmikupro.mio"
version = "0.6.1"

repositories {
    mavenCentral()
    intellijPlatform {
        defaultRepositories()
    }
}

dependencies {
    intellijPlatform {
        intellijIdea("2025.3.2")
        testFramework(TestFrameworkType.Platform)
    }
    testImplementation("junit:junit:4.13.2")
}

kotlin {
    jvmToolchain(21)
}

intellijPlatform {
    publishing {
        token = providers.environmentVariable("PUBLISH_TOKEN")
        channels = listOf("default")
    }

    pluginConfiguration {
        id = "com.exmikupro.mio"
        name = "Mio Language Support"
        version = project.version.toString()

        ideaVersion {
            sinceBuild = "253"
        }

        description = """
            Mio programming language support for IntelliJ IDEA and GoLand.
            Provides verified compiler diagnostics, native run configurations, syntax highlighting, semantic completion, navigation, find usages, documentation, Bunko package completion, and safe automatic imports.
        """.trimIndent()

        changeNotes = """
            <p>0.6.1: Added the Mio plugin logo and refreshed the README artwork.</p>
            <p>0.6.0: Added the Mio new-project wizard, shared SDK registry, per-project SDK and Bunko selection, and verified GitHub Releases installation.</p>
            <p>0.5.0: Added verified Mio CLI discovery, native run configurations, editor run actions, package execution, and package-safe unsaved diagnostics.</p>
            <p>0.4.0: Added asynchronous Mio compiler diagnostics, UTF-8-aware error highlighting, diagnostic details, executable settings, and safe syntax quick fixes.</p>
            <p>0.3.0: Added definition navigation, find usages, and reliable symbol documentation.</p>
            <p>0.2.0: Added scope-aware completion, Bunko package members, import path completion, and automatic imports.</p>
            <p>0.1.0: Initial Mio file type and editor support.</p>
        """.trimIndent()
    }

    pluginVerification {
        ides {
            create(IntelliJPlatformType.IntellijIdea, "2025.3.2")
            create(IntelliJPlatformType.GoLand, "2025.3.2")
        }
    }
}

tasks {
    register("printReleaseVersion") {
        val releaseVersion = project.version.toString()
        doLast { println("MIO_RELEASE_VERSION=$releaseVersion") }
    }
    publishPlugin {
        // Publish exactly the archive produced and verified by the build job.
        providers.gradleProperty("releaseArchive").orNull?.let {
            archiveFiles.setFrom(file(it))
        }
    }
    test {
        useJUnit()
    }
}
