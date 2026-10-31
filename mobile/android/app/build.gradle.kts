plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
    id("org.jetbrains.kotlin.plugin.compose")
}

// Version from the release build (-PtwVersionName=0.28.0 -PtwVersionCode=280000), else a development version.
val twVersionName = (findProperty("twVersionName") as String?) ?: "0.0.0-dev"
val twVersionCode = ((findProperty("twVersionCode") as String?) ?: "1").toInt()

android {
    namespace = "com.github.nperiannan.termwright"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.github.nperiannan.termwright"
        minSdk = 26
        targetSdk = 35
        versionCode = twVersionCode
        versionName = twVersionName
    }

    // The release key comes from the environment (GitHub secrets); without it the
    // debug key is used, which is fine for trying the app out.
    val keystore = System.getenv("TW_KEYSTORE")
    signingConfigs {
        if (keystore != null && file(keystore).exists()) {
            create("release") {
                storeFile = file(keystore)
                storePassword = System.getenv("TW_KEYSTORE_PASSWORD")
                keyAlias = System.getenv("TW_KEY_ALIAS")
                keyPassword = System.getenv("TW_KEY_PASSWORD")
            }
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = false
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures { compose = true }
}

dependencies {
    // twcore.aar: the Go part (mobile/core), built with gomobile - see mobile/README.md
    implementation(files("libs/twcore.aar"))

    val bom = platform("androidx.compose:compose-bom:2024.12.01")
    implementation(bom)
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.material3:material3")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.activity:activity-compose:1.9.3")
    implementation("androidx.core:core-ktx:1.15.0")
    implementation("androidx.lifecycle:lifecycle-runtime-ktx:2.8.7")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.9.0")
}
