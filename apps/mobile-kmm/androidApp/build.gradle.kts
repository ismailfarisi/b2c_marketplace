plugins {
    alias(libs.plugins.androidApplication)
    alias(libs.plugins.kotlinAndroid)
    alias(libs.plugins.composeCompiler)
}

android {
    namespace = "ae.marketplace.mobile.android"
    compileSdk = 36

    defaultConfig {
        applicationId = "ae.marketplace.mobile"
        minSdk = 26
        targetSdk = 36
        versionCode = 1
        versionName = "0.1.0"

        // Pointed at the Go API. 10.0.2.2 is the emulator's route to the host,
        // which is where `make dev` runs cmd/api.
        buildConfigField("String", "API_BASE_URL", "\"http://10.0.2.2:8080/v1\"")
    }

    buildFeatures {
        compose = true
        buildConfig = true
    }

    buildTypes {
        debug {
            // Cleartext to the emulator host only. A blanket
            // `usesCleartextTraffic` in the manifest would ship to release too;
            // this keeps the exception in the debug network config where it
            // cannot escape.
            isMinifyEnabled = false
        }
        release {
            isMinifyEnabled = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions { jvmTarget = "17" }
}

dependencies {
    implementation(project(":shared"))

    implementation(libs.androidx.activity.compose)
    implementation(platform(libs.androidx.compose.bom))
    implementation(libs.androidx.compose.ui)
    implementation(libs.androidx.compose.foundation)
    implementation(libs.androidx.compose.material3)
    implementation(libs.androidx.compose.ui.tooling.preview)

    // Posters and thumbnails.
    //
    // The feed is video, so this is not the app's main pixel path — it is the
    // *first* frame of it. Without a poster, every reel opens on black while
    // the manifest is fetched and the first segment decoded, and every list of
    // saved or searched items draws grey rectangles where the thing being
    // chosen between should be.
    implementation(libs.coil.compose)
    implementation(libs.coil.network.okhttp)

    // The view that hosts a surface. The player itself comes from :shared —
    // this module never constructs an ExoPlayer.
    implementation(libs.media3.ui)
    implementation(libs.media3.exoplayer)

    // The recorder. `camera-view` brings PreviewView, which is what makes a
    // viewfinder five lines instead of a SurfaceHolder callback and a rotation
    // matrix.
    implementation(libs.camera.core)
    implementation(libs.camera.camera2)
    implementation(libs.camera.lifecycle)
    implementation(libs.camera.video)
    implementation(libs.camera.view)
}
