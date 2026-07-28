-- Identity: phone-first, because that is how the GCC authenticates.
-- Email is optional and secondary.

CREATE TABLE users (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    -- E.164, normalised on write. Stored without formatting so +971 50 123 4567
    -- and +971501234567 are the same account.
    phone           text NOT NULL,
    phone_verified_at timestamptz,
    email           citext,
    email_verified_at timestamptz,
    display_name    text,
    locale          locale_code NOT NULL DEFAULT 'en',
    status          user_status NOT NULL DEFAULT 'active',
    country         char(2) NOT NULL DEFAULT 'AE',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,

    CONSTRAINT users_phone_e164 CHECK (phone ~ '^\+[1-9][0-9]{7,14}$')
);

-- Partial unique: a soft-deleted user must not block re-registration of that
-- number, but two live accounts on one number is a support nightmare.
CREATE UNIQUE INDEX users_phone_live_key ON users (phone) WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX users_email_live_key ON users (email) WHERE email IS NOT NULL AND deleted_at IS NULL;

CREATE TRIGGER users_set_updated_at BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();


CREATE TABLE otp_codes (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    phone       text NOT NULL,
    purpose     text NOT NULL DEFAULT 'login',
    -- Never the code itself. An attacker with read access to this table must
    -- not be able to log in as anyone.
    code_hash   bytea NOT NULL,
    attempts    int NOT NULL DEFAULT 0,
    max_attempts int NOT NULL DEFAULT 5,
    -- Rate limiting keys on phone AND request IP. Phone alone lets one host
    -- enumerate numbers; IP alone is defeated by a mobile carrier NAT.
    request_ip  inet,
    consumed_at timestamptz,
    expires_at  timestamptz NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),

    CONSTRAINT otp_attempts_bounded CHECK (attempts <= max_attempts)
);

CREATE INDEX otp_codes_phone_created_idx ON otp_codes (phone, created_at DESC);
CREATE INDEX otp_codes_ip_created_idx ON otp_codes (request_ip, created_at DESC) WHERE request_ip IS NOT NULL;
CREATE INDEX otp_codes_expiry_idx ON otp_codes (expires_at) WHERE consumed_at IS NULL;


CREATE TABLE sessions (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- Hash, not the token. Same reasoning as otp_codes.
    refresh_token_hash bytea NOT NULL,
    device_platform   text,
    device_name       text,
    push_token        text,
    ip                inet,
    user_agent        text,
    last_used_at      timestamptz,
    -- Set on rotation so a replayed old refresh token is detectable as theft
    -- rather than merely rejected.
    rotated_to        uuid REFERENCES sessions (id) ON DELETE SET NULL,
    revoked_at        timestamptz,
    expires_at        timestamptz NOT NULL,
    created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX sessions_refresh_hash_key ON sessions (refresh_token_hash);
CREATE INDEX sessions_user_active_idx ON sessions (user_id) WHERE revoked_at IS NULL;


CREATE TABLE addresses (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    label          text NOT NULL,
    recipient_name text,
    recipient_phone text,

    -- GCC addressing is landmark-based. A postcode-driven form fails here:
    -- "villa behind ADNOC, Al Wasl Road" is a complete address and no schema
    -- built around postal codes can represent it.
    building       text,
    flat           text,
    street         text,
    landmark       text,
    area           text,
    city           text NOT NULL,
    emirate        text,
    country        char(2) NOT NULL DEFAULT 'AE',

    point          geography(Point, 4326) NOT NULL,
    notes          text,
    is_default     boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    deleted_at     timestamptz
);

CREATE INDEX addresses_user_idx ON addresses (user_id) WHERE deleted_at IS NULL;
CREATE INDEX addresses_point_idx ON addresses USING gist (point);
-- One default per user, enforced rather than hoped for.
CREATE UNIQUE INDEX addresses_one_default_per_user ON addresses (user_id)
    WHERE is_default AND deleted_at IS NULL;

CREATE TRIGGER addresses_set_updated_at BEFORE UPDATE ON addresses
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
