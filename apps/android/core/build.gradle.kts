plugins {
    alias(libs.plugins.android.library)
    alias(libs.plugins.kotlin.compose)
    alias(libs.plugins.kotlin.serialization)
}

android {
    namespace = "org.pangeavpn.core"
    compileSdk = 37

    defaultConfig {
        minSdk = 24
        consumerProguardFiles("consumer-rules.pro")
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    buildFeatures {
        compose = true
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
    api(group = "", name = "pangeacore", ext = "aar")

    // Built-in Kotlin does not infer kotlin-test's JUnit flavour the way kotlin-android did,
    // so name it: without it kotlin.test.Test is unresolved.
    testImplementation(libs.kotlin.test.junit)
}
