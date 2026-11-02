-- Sign-in codes go by SMS through the same outbox as delivery codes, so a
-- provider outage retries instead of dropping the login.
INSERT INTO notification_templates (key, channel, locale, category, subject, body) VALUES
    ('auth.otp', 'sms', 'en', 'account', NULL,
     'Your sign-in code is {{code}}. It expires in {{minutes}} minutes. Do not share it with anyone.'),
    ('auth.otp', 'sms', 'ar', 'account', NULL,
     'رمز تسجيل الدخول الخاص بك هو {{code}}. تنتهي صلاحيته خلال {{minutes}} دقيقة. لا تشاركه مع أي شخص.');
