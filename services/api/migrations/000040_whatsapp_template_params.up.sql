-- WhatsApp Cloud API templates take positional values ({{1}}, {{2}}, ...), not
-- our rendered text. Keep each message's params and each template's order.
ALTER TABLE notifications ADD COLUMN params jsonb;
ALTER TABLE notification_templates ADD COLUMN provider_params text[];

UPDATE notification_templates SET provider_params = CASE
    WHEN key = 'meals.tomorrow' AND locale = 'en' THEN '{day,kitchen,menu}'::text[]
    WHEN key = 'meals.tomorrow' AND locale = 'ar' THEN '{day_ar,kitchen_ar,menu_ar}'::text[]
    WHEN key = 'meals.choose_reminder' AND locale = 'en' THEN '{slot,day,kitchen,cutoff,options}'::text[]
    WHEN key = 'meals.choose_reminder' AND locale = 'ar' THEN '{slot_ar,day_ar,kitchen_ar,cutoff,options_ar}'::text[]
    WHEN key = 'meals.renewal_due' AND locale = 'en' THEN '{kitchen,date,amount}'::text[]
    WHEN key = 'meals.renewal_due' AND locale = 'ar' THEN '{kitchen_ar,date_ar,amount}'::text[]
    WHEN key IN ('meals.on_the_way', 'meals.delivered', 'meals.delivery_failed') AND locale = 'en' THEN '{kitchen}'::text[]
    WHEN key IN ('meals.on_the_way', 'meals.delivered', 'meals.delivery_failed') AND locale = 'ar' THEN '{kitchen_ar}'::text[]
END
WHERE channel = 'whatsapp' AND key LIKE 'meals.%';
