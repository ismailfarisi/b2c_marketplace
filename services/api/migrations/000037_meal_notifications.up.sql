-- WhatsApp messages for meal subscribers. Only WhatsApp rows exist for these
-- keys, so the notify ladder sends them on WhatsApp; until a WhatsApp Business
-- account is connected the log provider records them locally.
INSERT INTO notification_templates (key, channel, locale, category, provider_template_name, body) VALUES
    ('meals.tomorrow', 'whatsapp', 'en', 'delivery', 'meals_tomorrow',
     'Tomorrow ({{day}}) from {{kitchen}}: {{menu}}. Enjoy your meal!'),
    ('meals.tomorrow', 'whatsapp', 'ar', 'delivery', 'meals_tomorrow',
     'غدًا ({{day_ar}}) من {{kitchen_ar}}: {{menu_ar}}. بالهناء والشفاء!'),

    ('meals.choose_reminder', 'whatsapp', 'en', 'delivery', 'meals_choose_reminder',
     'Choose your {{slot}} for {{day}} from {{kitchen}} before {{cutoff}}: {{options}}. Otherwise you get the first dish.'),
    ('meals.choose_reminder', 'whatsapp', 'ar', 'delivery', 'meals_choose_reminder',
     'اختر {{slot_ar}} ليوم {{day_ar}} من {{kitchen_ar}} قبل {{cutoff}}: {{options_ar}}. وإلا ستصلك الوجبة الأولى.'),

    ('meals.renewal_due', 'whatsapp', 'en', 'transactional', 'meals_renewal_due',
     'Your {{kitchen}} plan continues on {{date}}. Pay {{amount}} in the app to keep your meals coming.'),
    ('meals.renewal_due', 'whatsapp', 'ar', 'transactional', 'meals_renewal_due',
     'يستمر اشتراكك مع {{kitchen_ar}} في {{date_ar}}. ادفع {{amount}} في التطبيق لتستمر وجباتك.'),

    ('meals.on_the_way', 'whatsapp', 'en', 'delivery', 'meals_on_the_way',
     'Your meal from {{kitchen}} is on the way.'),
    ('meals.on_the_way', 'whatsapp', 'ar', 'delivery', 'meals_on_the_way',
     'وجبتك من {{kitchen_ar}} في الطريق إليك.'),

    ('meals.delivered', 'whatsapp', 'en', 'delivery', 'meals_delivered',
     'Your meal from {{kitchen}} has been delivered. Enjoy!'),
    ('meals.delivered', 'whatsapp', 'ar', 'delivery', 'meals_delivered',
     'تم توصيل وجبتك من {{kitchen_ar}}. بالهناء والشفاء!'),

    ('meals.delivery_failed', 'whatsapp', 'en', 'delivery', 'meals_delivery_failed',
     'We could not deliver your meal from {{kitchen}} today. We will contact you.'),
    ('meals.delivery_failed', 'whatsapp', 'ar', 'delivery', 'meals_delivery_failed',
     'لم نتمكن من توصيل وجبتك من {{kitchen_ar}} اليوم. سنتواصل معك.');
