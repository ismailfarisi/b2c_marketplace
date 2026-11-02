# Deep links and push taps

Meals web URLs open the same screen in the apps; a push notification's `link`
(a web path such as `/en/subscriptions/{id}`) is routed the same way. The
mapping lives in one place, `MealsLinks` in the KMM shared module:

| Path (`/en` or `/ar` prefix optional) | App screen |
|---|---|
| `/` | Meals home |
| `/kitchens/{branchId}…` | Kitchen |
| `/subscriptions` | My meals (sign-in first) |
| `/subscriptions/{id}` | Subscription (sign-in first) |
| `/rotation` | Rotation checkout (sign-in first) |

Anything else is ignored and the app opens normally. The API adds `link` to
meal notifications (tomorrow's menu, dish-choice reminder, renewal due,
delivery updates).

## Setup per environment

**Web** serves the verification files from `/.well-known/` (404 until set):
- `ANDROID_CERT_SHA256`: release signing SHA-256 fingerprint(s), comma-separated
  (Play Console → App integrity). `ANDROID_PACKAGE` defaults to `ae.marketplace.mobile`.
- `APPLE_APP_ID`: `<TEAM_ID>.<bundle id>`.

**Android:** build with Gradle property `WEB_HOST=<web host>` (e.g. `-PWEB_HOST=meals.example.ae`);
the manifest's `autoVerify` intent filter uses it.

**iOS:** add the Associated Domains capability with `applinks:<web host>` to the
app target, alongside the Push Notifications capability from `docs/push.md`.
