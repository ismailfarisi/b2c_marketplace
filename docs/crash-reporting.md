# Crash reporting (Sentry)

Each surface reports to Sentry only when its DSN is set; without one nothing
is initialised. Personal data (request bodies, headers, IPs) is not sent.

| Surface | Setting | Reports |
|---|---|---|
| API + worker | `SENTRY_DSN` env | Recovered HTTP panics (tagged `request_id`) and the fatal error that stops the process |
| Web (server) | `SENTRY_DSN`, `APP_ENV` env at runtime | Errors in server components, route handlers and middleware |
| Web (browser) | `NEXT_PUBLIC_SENTRY_DSN`, `NEXT_PUBLIC_APP_ENV` at build time | Unhandled browser errors |
| Android | Gradle property `SENTRY_DSN` | Crashes and ANRs |
| iOS | Add the `sentry-cocoa` Swift package and an Info.plist `SentryDSN` key | Crashes |

Source-map and ProGuard/dSYM upload are not set up yet; add them with the
release pipeline (needs a Sentry auth token).
