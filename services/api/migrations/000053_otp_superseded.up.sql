-- Asking for a new sign-in code retires the earlier ones for that number, so
-- the "earlier codes no longer work" the apps say on resend is true. Its own
-- column rather than consumed_at, so verify can say "use the latest code"
-- instead of "already used", and the log can tell the two apart.
ALTER TABLE otp_codes ADD COLUMN superseded_at timestamptz;
