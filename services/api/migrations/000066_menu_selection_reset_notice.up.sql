-- A kitchen shortening its menu cycle can orphan a customer's chosen dish for
-- a future day; the day falls back to the default and the customer hears why.
INSERT INTO notification_templates (key, channel, locale, category, subject, body) VALUES
    ('menu.selections_reset', 'push', 'en', 'transactional', 'Your kitchen changed its menu',
     '{{kitchen}} changed its menu, so {{count}} of your chosen dishes from {{first_date}} went back to the day''s default. Pick again from My meals.'),
    ('menu.selections_reset', 'push', 'ar', 'transactional', 'غيّر مطبخك قائمته',
     'غيّر {{kitchen}} قائمته، لذا عاد {{count}} من أطباقك المختارة ابتداءً من {{first_date}} إلى طبق اليوم الافتراضي. اختر مجددًا من وجباتي.');
