DELETE FROM notification_templates WHERE key IN (
    'meals.tomorrow', 'meals.choose_reminder', 'meals.renewal_due',
    'meals.on_the_way', 'meals.delivered', 'meals.delivery_failed');
