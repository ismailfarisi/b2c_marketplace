---
target: app navbar
total_score: 23
max_score: 40
na_heuristics: 
p0_count: 0
p1_count: 3
target_identity: "file:/home/ubuntu/repos/b2c_marketplace/apps/mobile-kmm/androidApp/src/main/kotlin/ae/marketplace/mobile/android/BottomNav.kt"
target_fingerprint: "sha256:7a695a09efa9d56bdae7c5656e8e2da328f7de5dd6646cd4530a0bafd968bef4"
target_path: /home/ubuntu/repos/b2c_marketplace/apps/mobile-kmm/androidApp/src/main/kotlin/ae/marketplace/mobile/android/BottomNav.kt
timestamp: 2026-10-31T11-31-44Z
slug: e-marketplace-mobile-android-bottomnav-kt-c5124cb2
---
# Critique: mobile bottom navigation (Android emulator EN/AR + iOS source)
Method: design review only (detector not applicable, native platform)
Score 23/40.
P1: signed-out Me shows Sell copy "Sign in to list" (Android+iOS); cancel goes Meals (Android) vs Feed (iOS); phone field keeps text across gates.
P1: Arabic Feed label "الرئيسية" (Home) collides with Meals as home.
P1: text-only 12sp labels, no icons; Meals / My meals (وجبات/وجباتي) near-identical.
P2: Saved and Sell off-screen at end of Feed filter row, styled like filters.
P2: Android tap areas label+padding only (~35dp dead gaps); iOS fixed 12pt ignores Dynamic Type; 52dp bar.
Minor: Android underline uses Accent amber not Chilli on light bar; My meals header pill duplicates tab; dark sign-in gate over light My meals; NotBuiltYet/nav_not_built still shipped; stale KDoc.
