pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositories {
        google()
        mavenCentral()
        // The gomobile AAR resolves as a module, not a file: lintVital bundles
        // :core into an AAR and AGP rejects .aar file dependencies outright.
        flatDir { dirs("core/libs") }
    }
}

rootProject.name = "pangeavpn-android"

// The gomobile AAR is not a Gradle module: it is built from daemon/mobile and
// dropped at core/libs/pangeacore.aar (see core/build.gradle.kts).
include(":core", ":ui-common", ":app-mobile", ":app-tv")
