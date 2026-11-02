-- Customer support: a ticket per problem, a thread of messages, staff replies
-- from the admin console. Status follows who spoke last: a customer message
-- reopens, a staff reply marks it answered.
CREATE TABLE support_tickets (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users (id),
    subject         text NOT NULL CHECK (length(subject) BETWEEN 1 AND 200),
    status          text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'answered', 'closed')),
    subscription_id uuid REFERENCES subscriptions (id),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX support_tickets_user_idx ON support_tickets (user_id, updated_at DESC);
CREATE INDEX support_tickets_status_idx ON support_tickets (status, updated_at);

CREATE TABLE support_messages (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id  uuid NOT NULL REFERENCES support_tickets (id) ON DELETE CASCADE,
    author_id  uuid REFERENCES users (id),
    from_staff boolean NOT NULL,
    body       text NOT NULL CHECK (length(body) BETWEEN 1 AND 4000),
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX support_messages_ticket_idx ON support_messages (ticket_id, created_at);

INSERT INTO notification_templates (key, channel, locale, category, subject, body) VALUES
    ('support.reply', 'push', 'en', 'transactional', 'Support replied', 'New reply on "{{subject}}".'),
    ('support.reply', 'push', 'ar', 'transactional', 'رد فريق الدعم', 'رد جديد على "{{subject}}".');
