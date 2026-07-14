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
    }
}

rootProject.name = "pangeavpn-android"

// :tunnel is not a Gradle module. The Go/gomobile AAR is built by the
// script under apps/android/tunnel/ and dropped at core/libs/pangeacore.aar,
// which :core consumes via a fileTree dependency (see core/build.gradle.kts).
include(":core", ":ui-common", ":app-mobile", ":app-tv")
