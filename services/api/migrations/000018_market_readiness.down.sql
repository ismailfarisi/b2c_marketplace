-- Reverse of 000018.

ALTER TABLE users
    DROP COLUMN IF EXISTS notification_locale;

ALTER TABLE fulfillments
    DROP COLUMN IF EXISTS otp_sent_at,
    DROP COLUMN IF EXISTS otp_notification_id;

ALTER TABLE platform_admins
    DROP COLUMN IF EXISTS is_active,
    DROP COLUMN IF EXISTS queues,
    DROP COLUMN IF EXISTS locales;

DROP TABLE IF EXISTS moderation_assignments;
DROP TABLE IF EXISTS content_appeals;
DROP TABLE IF EXISTS author_strikes;

DROP TABLE IF EXISTS payout_accounts;
DROP TABLE IF EXISTS kyc_documents;
DROP TABLE IF EXISTS kyc_cases;

DROP TABLE IF EXISTS zatca_submissions;
DROP TABLE IF EXISTS invoice_lines;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS invoice_series;

DROP TABLE IF EXISTS tax_registrations;
DROP TABLE IF EXISTS tax_rates;

DROP TABLE IF EXISTS whatsapp_sessions;

DROP FUNCTION IF EXISTS create_notification_partition(date);
-- Partitions go with the parent.
DROP TABLE IF EXISTS notifications;

DROP TABLE IF EXISTS notification_preferences;
DROP TABLE IF EXISTS notification_templates;

DROP TABLE IF EXISTS translations;
