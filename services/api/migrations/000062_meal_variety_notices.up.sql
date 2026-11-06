-- "Next week" previews and end-of-cycle recaps. A row here is the sent marker
-- that keeps each notice to one per customer per week (next_week, ref = the
-- week's Sunday) and one per rotation (cycle_recap, ref = rotation id).
CREATE TABLE meal_variety_notices (
    kind     text NOT NULL CHECK (kind IN ('next_week', 'cycle_recap')),
    user_id  uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    ref      text NOT NULL,
    sent_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (kind, user_id, ref)
);

CREATE INDEX meal_variety_notices_recent ON meal_variety_notices (kind, user_id, sent_at);

-- Push rows show in the in-app inbox too (Inbox reads push and in_app).
INSERT INTO notification_templates (key, channel, locale, category, subject, body) VALUES
    ('meals.next_week', 'push', 'en', 'delivery', 'Next week''s kitchen',
     'Next week ({{dates}}) {{kitchen}} cooks for you: {{dishes}}.'),
    ('meals.next_week', 'push', 'ar', 'delivery', 'مطبخ الأسبوع القادم',
     'الأسبوع القادم ({{dates_ar}}) يطبخ لك {{kitchen_ar}}: {{dishes_ar}}.'),
    ('meals.cycle_recap', 'push', 'en', 'delivery', 'Your month of kitchens',
     'Week 4 is here: {{kitchens}} kitchens and {{dishes}} dishes so far. See next month''s kitchens.'),
    ('meals.cycle_recap', 'push', 'ar', 'delivery', 'شهرك مع المطابخ',
     'بدأ الأسبوع الرابع: {{kitchens}} مطابخ و{{dishes}} طبقًا حتى الآن. اطّلع على مطابخ الشهر القادم.');

INSERT INTO notification_templates (key, channel, locale, category, provider_template_name, body, provider_params) VALUES
    ('meals.next_week', 'whatsapp', 'en', 'delivery', 'meals_next_week',
     'Next week ({{dates}}) {{kitchen}} cooks for you: {{dishes}}. See the whole week in the app.',
     '{dates,kitchen,dishes}'::text[]),
    ('meals.next_week', 'whatsapp', 'ar', 'delivery', 'meals_next_week',
     'الأسبوع القادم ({{dates_ar}}) يطبخ لك {{kitchen_ar}}: {{dishes_ar}}. شاهد الأسبوع كاملًا في التطبيق.',
     '{dates_ar,kitchen_ar,dishes_ar}'::text[]);
