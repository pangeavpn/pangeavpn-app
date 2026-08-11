plugins {
    alias(libs.plugins.android.application)
    alias(libs.plugins.kotlin.android)
    alias(libs.plugins.kotlin.compose)
}

@Suppress("UNCHECKED_CAST")
val keystore = rootProject.extra["pangeaKeystore"] as Map<String, String>?

android {
    namespace = "org.pangeavpn.tv"
    compileSdk = 35

    defaultConfig {
        applicationId = "org.pangeavpn.tv"
        minSdk = 24
        targetSdk = 35
        versionCode = rootProject.extra["pangeaVersionCode"] as Int
        versionName = rootProject.extra["pangeaVersionName"] as String
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
    implementation(libs.tv.material)
    implementation(libs.compose.foundation)
    implementation(libs.compose.material3)
    implementation(libs.activity.compose)
    implementation(libs.lifecycle.viewmodel.compose)
}
