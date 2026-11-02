# Store release (Google Play and App Store)

What the repo provides, and what each store submission still needs from you.

## In the repo

| Item | Where |
|---|---|
| Launcher icon (adaptive + themed) and Android 12+ splash | `androidApp/src/main/res/` (`ic_launcher*`, `values-v31/themes.xml`) |
| iOS app icon (1024 px) and accent colour | `iosApp/iosApp/Assets.xcassets` |
| Play Store hi-res icon (512 px) | `docs/store/play-icon-512.png` |
| Release signing, version, URLs from Gradle properties/env | `androidApp/build.gradle.kts` |
| Store-review sign-in (fixed code, no SMS) | API `OTP_REVIEW_PHONE` + `OTP_REVIEW_CODE` |
| Privacy policy / terms (EN, AR) | `https://<web>/en/privacy`, `/en/terms` (legal review pending) |
| In-app account deletion | Me → Delete account; web: My subscriptions → Delete account |
| Help / support | `https://<web>/en/support` |

## Android release build

```bash
cd apps/mobile-kmm
./gradlew :androidApp:bundleRelease \
  -PVERSION_CODE=2 -PVERSION_NAME=1.0.0 \
  -PAPI_BASE_URL=https://api.<domain>/v1 -PWEB_BASE_URL=https://<web-domain> -PWEB_HOST=<web-domain> \
  -PRELEASE_STORE_FILE=/path/upload.jks -PRELEASE_STORE_PASSWORD=… \
  -PRELEASE_KEY_ALIAS=upload -PRELEASE_KEY_PASSWORD=… \
  -PFIREBASE_APP_ID=… -PFIREBASE_API_KEY=… -PFIREBASE_PROJECT_ID=… -PFIREBASE_SENDER_ID=… \
  -PSENTRY_DSN=…
```

The same keys can be environment variables (CI secrets). Without `RELEASE_STORE_FILE`
the bundle is unsigned. Without `API_BASE_URL` it points at the emulator host and
will not work on a phone. Use Play App Signing; the keystore above is the upload key.
Its SHA-256, and the Play app-signing SHA-256, go in the web's `ANDROID_CERT_SHA256` (App Links).

## iOS release build

There is no Xcode project in the repo yet (CI only typechecks Swift). To ship:
1. Create the iOS app target around `iosApp/iosApp` and link the KMM `Shared` framework.
2. Bundle id and team → `APPLE_APP_ID` on the web (Universal Links) and `APNS_TOPIC`/`APNS_TEAM_ID` on the API.
3. Capabilities: Push Notifications, Associated Domains (`applinks:<web-domain>`).
4. Info.plist: `NSCameraUsageDescription`, `NSMicrophoneUsageDescription`
   (Sell video recording), `NSPhotoLibraryUsageDescription` (picking photos),
   `NSLocationWhenInUseUsageDescription` (kitchens near you), `SentryDSN`, and the API/web base URLs.
5. Add the `sentry-cocoa` package (see `docs/crash-reporting.md`).

## Listing copy

**Name:** the app shows `Marketplace` / `السوق` today (`app_name`). Pick the store name before submitting.

**Short description (80 chars)**
- EN: Home-style lunch subscriptions from local kitchens, delivered every weekday.
- AR: اشتراكات غداء منزلي من مطابخ قريبة، يصلك كل يوم عمل.

**Full description**
- EN: Subscribe to a home-style kitchen near you — Kerala, Hyderabadi, Lahori, Emirati, Lebanese, Filipino and more. Each kitchen cooks a fixed weekday menu, often with a second dish to choose. Compare plans by price per meal, skip or pause days before the cutoff, or rotate between four kitchens in a month. Office lunches for companies, delivery updates by push or WhatsApp, Arabic and English.
- AR: اشترك في مطبخ منزلي قريب منك — كيرالا، حيدر آبادي، لاهوري، إماراتي، لبناني، فلبيني وغيرها. يطبخ كل مطبخ قائمة ثابتة لأيام الأسبوع، وغالبًا مع طبق ثانٍ للاختيار. قارن الخطط بسعر الوجبة، وتخطَّ أو أوقف الأيام قبل الموعد النهائي، أو تنقّل بين أربعة مطابخ خلال الشهر. غداء المكاتب للشركات، وتحديثات التوصيل عبر الإشعارات أو واتساب، بالعربية والإنجليزية.

**Category:** Food & Drink. **Content rating:** Everyone (user reviews and chat are moderated).

**Screenshots (phone, EN + AR):** Meals home, kitchen page with day tabs, plan picker, checkout, My meals, 4-kitchen rotation.

## Privacy answers (Play Data safety / App Store privacy)

| Data | Collected | Purpose | Shared with |
|---|---|---|---|
| Phone number | Yes, required | Account, sign-in codes, delivery contact | SMS provider (AWS SNS), WhatsApp (Meta), the kitchen and driver for your orders |
| Name, delivery addresses | Yes | Delivery | Kitchen and driver |
| Approximate location | Optional | Kitchens that deliver to you | No |
| Purchase history, invoices | Yes | Orders, tax invoices | Payment provider (when added) |
| Ratings, reviews, support messages | Optional | Service quality, support | No |
| Device push token | Optional | Notifications | Firebase / Apple Push |
| Crash logs | Yes (no default PII) | Stability | Sentry |
| Camera / microphone | Only when selling a video listing | Recording listing videos | No |

Data is encrypted in transit; users can delete their account in the app.

## App review sign-in

Reviewers cannot receive our SMS. Set `OTP_REVIEW_PHONE` (a number you own and
never give to anyone else) and `OTP_REVIEW_CODE` (6 digits) on the production API,
and enter both in the review notes. That number always accepts that code and is
never texted. Remove them after approval if you prefer.
