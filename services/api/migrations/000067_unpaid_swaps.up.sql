-- A swapped-in week whose dearer kitchen's difference was never paid is voided
-- by the worker once the swap cutoff has passed; voided_at says it happened,
-- so the sweep does not pick it up again and the rotation shows why the week
-- is empty.
ALTER TABLE meal_rotation_swaps ADD COLUMN voided_at timestamptz;

INSERT INTO notification_templates (key, channel, locale, category, subject, body) VALUES
    ('meals.swap_unpaid', 'push', 'en', 'transactional', 'Week {{week}} is not booked',
     'The change to {{kitchen}} for week {{week}} was not paid for in time, so that week has no kitchen. {{amount}} is back in your wallet.'),
    ('meals.swap_unpaid', 'push', 'ar', 'transactional', 'الأسبوع {{week}} غير محجوز',
     'لم يُدفع فرق تغيير مطبخ الأسبوع {{week}} إلى {{kitchen_ar}} في الوقت المحدد، فلم يُحجز ذلك الأسبوع. عاد {{amount}} إلى محفظتك.');
