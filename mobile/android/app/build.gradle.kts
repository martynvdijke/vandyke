import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// Release signing material is supplied locally through mobile/android/key.properties
// and in CI through repository secrets. The file and keystore are gitignored.
val keystorePropertiesFile = rootProject.file("key.properties")
val keystoreProperties = Properties().apply {
    if (keystorePropertiesFile.exists()) {
        keystorePropertiesFile.inputStream().use { load(it) }
    }
}

android {
    namespace = "xyz.vandijke.vandyke"
    // permission_handler_android requires compileSdk 37 or newer.
    compileSdk = 37
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        applicationId = "xyz.vandijke.vandyke"
        // firebase_messaging needs API 23; the Flutter default is already
        // higher, and this keeps that floor explicit.
        minSdk = maxOf(flutter.minSdkVersion, 23)
        targetSdk = flutter.targetSdkVersion
        // Uses the version code from pubspec.yaml. ABI splits add 1000 * ABI_VERSION
        // automatically by Flutter unless -P force-version-code-ignoring-abi=true.
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    signingConfigs {
        if (keystorePropertiesFile.exists()) {
            create("release") {
                storeFile = rootProject.file(keystoreProperties.getProperty("storeFile"))
                storePassword = keystoreProperties.getProperty("storePassword")
                keyAlias = keystoreProperties.getProperty("keyAlias")
                keyPassword = keystoreProperties.getProperty("keyPassword")
            }
        }
    }

    val releaseSigningConfig = if (keystorePropertiesFile.exists()) {
        signingConfigs.getByName("release")
    } else {
        signingConfigs.getByName("debug")
    }

    buildTypes {
        debug {
            // Installs alongside a production build on the same device.
            applicationIdSuffix = ".dev"
            versionNameSuffix = "-dev"
        }
        release {
            // Falls back to the debug key for local builds without a keystore.
            signingConfig = releaseSigningConfig
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}
