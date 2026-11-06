DELETE FROM notification_templates WHERE key IN ('meals.next_week', 'meals.cycle_recap');
DROP TABLE IF EXISTS meal_variety_notices;
