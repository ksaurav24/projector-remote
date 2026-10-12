plugins {
    id("com.android.application")
}

android {
    namespace = "com.projectorremote.home"
    compileSdk = 36
    defaultConfig {
        applicationId = "com.projectorremote.home"
        minSdk = 30
        targetSdk = 36
        versionCode = 1
        versionName = "1.0"
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
}

dependencies {
    implementation("com.google.zxing:core:3.5.3")
    testImplementation("junit:junit:4.13.2")
}
