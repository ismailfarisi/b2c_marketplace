/*
 * A separate Gradle build, not part of the npm workspace.
 *
 * `apps/mobile` (Expo) is superseded by this and should be deleted once the KMM
 * app boots — it is left in place only because deleting someone else's scaffold
 * is not a decision a build file should make. See apps/mobile-kmm/README.md §1.
 */
rootProject.name = "marketplace-mobile"

pluginManagement {
    repositories {
        google {
            // Scoped rather than open. An unscoped google() in front of
            // mavenCentral() means every dependency resolution round-trips to
            // Google's repo first, which is the single biggest contributor to a
            // slow cold Android build.
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
        gradlePluginPortal()
    }
}

dependencyResolutionManagement {
    repositories {
        google {
            content {
                includeGroupByRegex("com\\.android.*")
                includeGroupByRegex("com\\.google.*")
                includeGroupByRegex("androidx.*")
            }
        }
        mavenCentral()
    }
}

include(":shared")
include(":androidApp")
