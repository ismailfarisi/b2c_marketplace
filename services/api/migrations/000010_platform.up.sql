-- Transactional outbox.
--
-- Events are written in the SAME transaction as the state change that produced
-- them, and relayed afterwards by a poller. This closes the dual-write gap: an
-- event published outside its transaction either fires for an order that rolled
-- back, or fails to fire for one that committed. Both are silent and both
-- corrupt downstream state.
CREATE TABLE outbox_events (
    id           bigserial PRIMARY KEY,
    aggregate_type text NOT NULL,
    aggregate_id   uuid NOT NULL,
    event_type   text NOT NULL,
    payload      jsonb NOT NULL,
    -- Deduplication key for at-least-once relay. Consumers use it to make their
    -- own processing idempotent.
    idempotency_key text NOT NULL,

    published_at timestamptz,
    attempts     int NOT NULL DEFAULT 0,
    last_error   text,
    -- Backoff target. NULL means eligible now.
    next_attempt_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX outbox_idempotency_key ON outbox_events (idempotency_key);
-- The relay poll. Partial index keeps it small regardless of how large the
-- published history grows.
CREATE INDEX outbox_unpublished_idx ON outbox_events (id)
    WHERE published_at IS NULL;
CREATE INDEX outbox_aggregate_idx ON outbox_events (aggregate_type, aggregate_id);


-- Idempotency keys.
--
-- A table and not a cache: the guarantee has to survive an eviction. Mobile
-- clients on flaky networks retry aggressively, and a double-charge is not
-- recoverable by apology.
CREATE TABLE idempotency_keys (
    key          uuid NOT NULL,
    user_id      uuid,
    -- Namespaced by endpoint. The same client-generated UUID sent to two
    -- endpoints is two operations, and a key must never let one user read
    -- another user's stored response.
    endpoint     text NOT NULL,
    request_hash text NOT NULL,

    -- NULL until the request completes. A row with a NULL status is in flight;
    -- a concurrent replay must wait or be rejected, not execute in parallel.
    response_status int,
    response_body   bytea,

    state        text NOT NULL DEFAULT 'in_flight'
                     CHECK (state IN ('in_flight', 'completed')),
    created_at   timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,
    expires_at   timestamptz NOT NULL DEFAULT (now() + interval '24 hours'),

    PRIMARY KEY (key, endpoint)
);

CREATE INDEX idempotency_keys_expiry_idx ON idempotency_keys (expires_at);
CREATE INDEX idempotency_keys_user_idx ON idempotency_keys (user_id) WHERE user_id IS NOT NULL;


-- Audit log for privileged actions. Required for KYC and payout review, and the
-- first thing asked for when money moves unexpectedly.
CREATE TABLE audit_log (
    id          bigserial PRIMARY KEY,
    actor_type  text NOT NULL CHECK (actor_type IN ('user', 'vendor', 'admin', 'system')),
    actor_id    uuid,
    action      text NOT NULL,
    target_type text,
    target_id   uuid,
    -- Before/after for the changed fields only. Storing whole rows makes this
    -- table larger than the data it audits.
    changes     jsonb,
    ip          inet,
    user_agent  text,
    request_id  text,
    occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX audit_log_target_idx ON audit_log (target_type, target_id, occurred_at DESC);
CREATE INDEX audit_log_actor_idx ON audit_log (actor_type, actor_id, occurred_at DESC);
