plugins {
    alias(libs.plugins.android.application) apply false
    alias(libs.plugins.android.library) apply false
    alias(libs.plugins.kotlin.android) apply false
    alias(libs.plugins.kotlin.serialization) apply false
    alias(libs.plugins.kotlin.compose) apply false
}

// The apps ship on the desktop's version line, so the number is read from the
// repo's package.json rather than hand-maintained in two module files.
val pangeaVersionName: String = run {
    val manifest = rootProject.file("../../package.json")
    val match = Regex("\"version\"\\s*:\\s*\"([^\"]+)\"").find(manifest.readText())
        ?: error("no version field in ${manifest.path}")
    match.groupValues[1]
}

// Monotonic while minor and patch stay under 100: 0.5.2 -> 502.
val pangeaVersionCode: Int = run {
    val parts = pangeaVersionName.substringBefore('-').split('.')
    val part = { index: Int -> parts.getOrNull(index)?.toIntOrNull() ?: 0 }
    part(0) * 10000 + part(1) * 100 + part(2)
}

extra["pangeaVersionName"] = pangeaVersionName
extra["pangeaVersionCode"] = pangeaVersionCode

// Release signing comes from a gitignored keystore.properties, or from the
// environment on CI. Absent, release builds stay unsigned rather than failing.
extra["pangeaKeystore"] = run {
    val properties = java.util.Properties()
    val file = rootProject.file("keystore.properties")
    if (file.exists()) file.inputStream().use(properties::load)

    val read = { key: String, env: String ->
        properties.getProperty(key) ?: System.getenv(env)
    }
    val storePath = read("storeFile", "PANGEA_KEYSTORE_FILE")
    val storePassword = read("storePassword", "PANGEA_KEYSTORE_PASSWORD")
    val keyAlias = read("keyAlias", "PANGEA_KEY_ALIAS")
    val keyPassword = read("keyPassword", "PANGEA_KEY_PASSWORD")

    if (storePath == null || storePassword == null || keyAlias == null || keyPassword == null) {
        null
    } else {
        mapOf(
            "storeFile" to storePath,
            "storePassword" to storePassword,
            "keyAlias" to keyAlias,
            "keyPassword" to keyPassword,
        )
    }
}
