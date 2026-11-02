# SMS (sign-in codes)

Sign-in codes are queued in the notifications outbox (template `auth.otp`,
English and Arabic) in the same transaction that stores the code, and the
worker sends them. Production refuses to boot without an SMS provider.

| Variable | What |
|---|---|
| `SMS_PROVIDER` | `sns` (Amazon SNS). Empty in development keeps the log provider. |
| `SMS_REGION` | SNS region; defaults to `S3_REGION`. Pick one that supports SMS. |
| `SMS_SENDER_ID` | Registered alphanumeric sender ID (required by UAE carriers). |

Credentials come from the default AWS chain (task role, instance profile or
`AWS_ACCESS_KEY_ID`/`AWS_SECRET_ACCESS_KEY`); the role needs `sns:Publish`.
Before launch: move the SNS account out of the SMS sandbox, raise the monthly
spend limit, and register the sender ID for the UAE (and each GCC country).
Invalid or opted-out numbers fail permanently; throttling is retried.
