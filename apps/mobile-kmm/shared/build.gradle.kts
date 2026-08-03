import org.jetbrains.kotlin.gradle.ExperimentalKotlinGradlePluginApi
import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    alias(libs.plugins.kotlinMultiplatform)
    alias(libs.plugins.kotlinSerialization)
    alias(libs.plugins.androidLibrary)

    // The Swift half of the interop.
    //
    // Kotlin/Native's default ObjC export is lossy in two ways that matter
    // here, and the iOS code was written as though neither were true:
    //
    //   - A `StateFlow` exports as an opaque class with a `collect` method
    //     taking a callback. `for await state in viewModel.state` — which is
    //     what FeedScreen already does — needs it to be an AsyncSequence.
    //   - A sealed interface exports as a set of unrelated classes, so
    //     `switch failure` has no exhaustiveness and every screen would need a
    //     chain of `is` casts ending in a default that silently swallows a new
    //     case. `Failure` has eight of them.
    //
    // SKIE generates both. It runs only on the Apple link tasks, so an Android
    // build does not pay for it.
    alias(libs.plugins.skie)
}

kotlin {
    androidTarget {
        @OptIn(ExperimentalKotlinGradlePluginApi::class)
        compilerOptions { jvmTarget.set(JvmTarget.JVM_17) }
    }

    // Three iOS targets, not one. The simulator on an Apple Silicon Mac is
    // arm64 and the device is also arm64 but a different target triple; a build
    // that ships only `iosArm64` compiles and then fails to link in the
    // simulator, which is where every developer runs it first.
    listOf(iosX64(), iosArm64(), iosSimulatorArm64()).forEach { target ->
        target.binaries.framework {
            baseName = "Shared"
            isStatic = true
        }
    }

    sourceSets {
        commonMain.dependencies {
            implementation(libs.kotlinx.coroutines.core)
            implementation(libs.kotlinx.serialization.json)
            implementation(libs.kotlinx.datetime)
            implementation(libs.ktor.client.core)
            implementation(libs.ktor.client.content.negotiation)
            implementation(libs.ktor.serialization.json)
            implementation(libs.ktor.client.auth)
            implementation(libs.androidx.lifecycle.viewmodel)
        }

        commonTest.dependencies {
            implementation(kotlin("test"))
            implementation(libs.kotlinx.coroutines.test)
            implementation(libs.ktor.client.mock)
        }

        androidMain.dependencies {
            implementation(libs.ktor.client.okhttp)
            implementation(libs.androidx.security.crypto)
            // Media3 lives here rather than in androidApp because the *player*
            // is an actual for a common expect. The app module gets a
            // `ReelPlayer` and never names ExoPlayer.
            implementation(libs.media3.exoplayer)
            implementation(libs.media3.exoplayer.hls)
        }

        iosMain.dependencies {
            implementation(libs.ktor.client.darwin)
        }
    }
}

android {
    namespace = "ae.marketplace.mobile.shared"
    compileSdk = 36

    defaultConfig {
        // 26 is where the Android Keychain-equivalent (EncryptedSharedPreferences
        // on a hardware-backed key) is reliable. Below it, the token store would
        // silently fall back to something weaker on a share of devices.
        minSdk = 26
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

skie {
    features {
        group {
            // Suspend functions become Swift `async` throwing functions rather
            // than callback pairs. The iOS app already calls
            // `try await MarketplaceSdk.companion.create(...)`, which is this.
            coroutinesInterop.set(true)
        }
    }
}
