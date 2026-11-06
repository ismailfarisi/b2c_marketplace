# WhatsApp (Meta WhatsApp Cloud API)

Meal messages go out on WhatsApp through Meta's Cloud API
(`services/api/internal/modules/notify/meta_whatsapp.go`). Without credentials
the API logs them instead (development) or marks them failed (production).

## Turn it on

1. In Meta Business Manager, create a WhatsApp Business account, add and verify
   the sending phone number, and create a **System User** with a permanent
   access token that has `whatsapp_business_messaging`.
2. Create and submit the templates below for approval, in **English (`en`)**
   and **Arabic (`ar`)**, category **Utility**.
3. Set on the API/worker:
   - `WHATSAPP_ACCESS_TOKEN`: the system user token (keep it in the secret store)
   - `WHATSAPP_PHONE_NUMBER_ID`: the phone number's ID (not the number itself)
   - `WHATSAPP_API_VERSION`: optional, defaults to `v21.0`

Both of the first two must be set together, or the API refuses to start.

## Templates

Names must match `notification_templates.provider_template_name`. The body
values go in the order of `notification_templates.provider_params`
(`{{1}}`, `{{2}}`, ...). The text below mirrors our own copy in migrations
`000037_meal_notifications` and `000066_meal_variety_notices`; Meta can ask for wording changes during approval,
but keep the variable order.

| Template | English body | Arabic body |
|---|---|---|
| `meals_tomorrow` | Tomorrow ({{1}}) from {{2}}: {{3}}. Enjoy your meal! | غدًا ({{1}}) من {{2}}: {{3}}. بالهناء والشفاء! |
| `meals_choose_reminder` | Choose your {{1}} for {{2}} from {{3}} before {{4}}: {{5}}. Otherwise you get the first dish. | اختر {{1}} ليوم {{2}} من {{3}} قبل {{4}}: {{5}}. وإلا ستصلك الوجبة الأولى. |
| `meals_renewal_due` | Your {{1}} plan continues on {{2}}. Pay {{3}} in the app to keep your meals coming. | يستمر اشتراكك مع {{1}} في {{2}}. ادفع {{3}} في التطبيق لتستمر وجباتك. |
| `meals_on_the_way` | Your meal from {{1}} is on the way. | وجبتك من {{1}} في الطريق إليك. |
| `meals_delivered` | Your meal from {{1}} has been delivered. Enjoy! | تم توصيل وجبتك من {{1}}. بالهناء والشفاء! |
| `meals_delivery_failed` | We could not deliver your meal from {{1}} today. We will contact you. | لم نتمكن من توصيل وجبتك من {{1}} اليوم. سنتواصل معك. |
| `meals_next_week` | Next week ({{1}}) {{2}} cooks for you: {{3}}. See the whole week in the app. | الأسبوع القادم ({{1}}) يطبخ لك {{2}}: {{3}}. شاهد الأسبوع كاملًا في التطبيق. |

`meals_next_week` (migration `000066_meal_variety_notices`) goes out Thursday from
18:00 in the kitchen's time zone, once per customer per week (the marker is
`meal_variety_notices`), alongside a push that also shows in the in-app inbox.
Its values are dates, kitchen and dishes (`dates_ar`, `kitchen_ar`, `dishes_ar`
in Arabic). The cycle recap (`meals.cycle_recap`) is push + in-app only.

## Failures

- Rejections from Meta (`4xx`: bad number, template not approved, bad token)
  are permanent, so the message is marked failed with Meta's code and reason.
- `429` and `5xx` are retried by the worker.
- The provider's message ID (`wamid...`) is stored in `notifications.provider_ref`.
