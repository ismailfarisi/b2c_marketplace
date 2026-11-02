# Push notifications

The server sends push to every signed-in device of a customer: Android tokens
through Firebase Cloud Messaging (HTTP v1), iOS tokens straight to APNs. With
no keys configured, push stays on the log provider (development) or off.

## API (services/api)

| Variable | What |
|---|---|
| `FCM_CREDENTIALS_JSON` | Firebase service-account JSON key (role: Firebase Cloud Messaging API Admin) |
| `APNS_KEY_P8` | PEM text of the Apple `.p8` auth key |
| `APNS_KEY_ID` | The key's id |
| `APNS_TEAM_ID` | Apple developer team id |
| `APNS_TOPIC` | iOS bundle id |
| `APNS_SANDBOX` | `true` for development builds |

The four `APNS_*` values must be set together. Tokens that FCM or APNs report
as dead are cleared from the session. Logging out clears the device's token.

## Android

Pass the Firebase app's values as Gradle properties (e.g. in
`~/.gradle/gradle.properties`, never committed): `FIREBASE_APP_ID`,
`FIREBASE_API_KEY`, `FIREBASE_PROJECT_ID`, `FIREBASE_SENDER_ID`. Without
them the app builds and runs with push off. The notification permission is
asked for after sign-in.

## iOS

Enable the Push Notifications capability on the app target (adds the
`aps-environment` entitlement). The app asks for permission after sign-in and
registers the APNs token with the server.
