plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

@Suppress("UNCHECKED_CAST")
val keystore = rootProject.extra["pangeaKeystore"] as Map<String, String>?

android {
    namespace = "org.pangeavpn.app"
    compileSdk = 36

    defaultConfig {
        applicationId = "org.pangeavpn.app"
        minSdk = 24
        targetSdk = 36
        versionCode = rootProject.extra["pangeaVersionCode"] as Int
        versionName = rootProject.extra["pangeaVersionName"] as String

        // Links to the website's billing pages. Off by default: Play's payments
        // policy forbids them. Build with -Ppangea.webBilling=true for other channels.
        buildConfigField("boolean", "WEB_BILLING", rootProject.extra["pangeaWebBilling"].toString())
    }

    androidResources {
        localeFilters += listOf("en", "es", "fr", "ru", "uk", "zh", "ar", "fa")
    }

    // 32-bit x86 has no devices left; dropping it saves a quarter of the native code.
    packaging {
        jniLibs { excludes += "**/x86/*.so" }
    }

    // One APK per ABI for direct download, plus a universal one. Off for bundles,
    // which split by ABI themselves and fail to build with this on.
    splits {
        abi {
            isEnable = gradle.startParameter.taskNames.none { it.contains("bundle", ignoreCase = true) }
            reset()
            include("arm64-v8a", "armeabi-v7a", "x86_64")
            isUniversalApk = true
        }
    }

    signingConfigs {
        keystore?.let { credentials ->
            create("release") {
                storeFile = file(credentials.getValue("storeFile"))
                storePassword = credentials.getValue("storePassword")
                keyAlias = credentials.getValue("keyAlias")
                keyPassword = credentials.getValue("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.findByName("release")
        }
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
        buildConfig = true
    }
}

dependencies {
    implementation(project(":core"))
    implementation(project(":ui-common"))

    implementation(libs.core.ktx)
    implementation(platform(libs.compose.bom))
    implementation(libs.compose.material3)
    implementation(libs.compose.foundation)
    implementation(libs.activity.compose)
    implementation(libs.lifecycle.viewmodel.compose)
    implementation(libs.coroutines.android)
}
