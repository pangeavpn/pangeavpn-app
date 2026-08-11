plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "org.pangeavpn.core"
    compileSdk = 35

    defaultConfig {
        minSdk = 24
        consumerProguardFiles("consumer-rules.pro")
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = "17"
    }

    buildFeatures {
        compose = true
    }

    // lintVital bundles :core into an AAR, which AGP refuses while the gomobile
    // AAR is a local file dependency. The apps still lint themselves.
    lint {
        checkReleaseBuilds = false
    }
}

dependencies {
    implementation(libs.core.ktx)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.runtime)
    implementation(libs.compose.foundation)
    implementation(libs.compose.material3)
    implementation(libs.activity.compose)
    implementation(libs.lifecycle.viewmodel.compose)
    implementation(libs.coroutines.android)
    implementation(libs.serialization.json)
    implementation(libs.security.crypto)

    // api, not implementation: TunnelBridge implements the AAR's StatusSink,
    // so consumers need that supertype on their classpath.
    api(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar"))))

    testImplementation(kotlin("test"))
}
