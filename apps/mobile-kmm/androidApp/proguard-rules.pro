# Release shrinking. Ktor, kotlinx.serialization, Firebase and Sentry ship
# their own consumer rules; keep crash stack traces readable once the mapping
# file is uploaded to Sentry.
-keepattributes SourceFile,LineNumberTable
-renamesourcefileattribute SourceFile
