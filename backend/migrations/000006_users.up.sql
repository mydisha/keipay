-- Console users replace admin_users (expand step: admin_users stays as it is
-- and is dropped in a later contract migration).
CREATE TABLE users (
    id                   BIGSERIAL   PRIMARY KEY,
    public_id            TEXT        NOT NULL UNIQUE,
    -- Canonical form (trimmed, NFC, lower case), so a plain unique index
    -- makes emails unique case-insensitively.
    email                TEXT        NOT NULL UNIQUE,
    name                 TEXT        NOT NULL,
    role                 TEXT        NOT NULL CHECK (role IN ('owner', 'admin', 'viewer')),
    password_hash        TEXT        NOT NULL,
    must_change_password BOOLEAN     NOT NULL DEFAULT false,
    disabled_at          TIMESTAMPTZ,
    last_login_at        TIMESTAMPTZ,
    session_version      INT         NOT NULL DEFAULT 1,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_users_created ON users (created_at DESC, public_id DESC);
CREATE INDEX idx_users_active_role ON users (role) WHERE disabled_at IS NULL;

-- A ULID in Crockford base32: 10 characters of millisecond timestamp, then 16
-- random characters. Public ids are not secrets, so random() is enough.
CREATE OR REPLACE FUNCTION pg_temp.keipay_ulid(ts TIMESTAMPTZ) RETURNS TEXT
LANGUAGE plpgsql VOLATILE AS $$
DECLARE
    alphabet CONSTANT TEXT := '0123456789ABCDEFGHJKMNPQRSTVWXYZ';
    ms BIGINT := floor(extract(epoch FROM ts) * 1000)::BIGINT;
    ulid TEXT := '';
BEGIN
    FOR i IN 1..10 LOOP
        ulid := substr(alphabet, (ms % 32)::INT + 1, 1) || ulid;
        ms := ms / 32;
    END LOOP;
    FOR i IN 1..16 LOOP
        ulid := ulid || substr(alphabet, floor(random() * 32)::INT + 1, 1);
    END LOOP;
    RETURN ulid;
END
$$;

-- Every legacy admin becomes an owner. Usernames without an @ get the
-- reserved .invalid domain, so they can never belong to someone else.
INSERT INTO users (public_id, email, name, role, password_hash, must_change_password, created_at, updated_at)
SELECT 'usr_' || pg_temp.keipay_ulid(created_at),
       CASE WHEN position('@' IN username) > 0 THEN lower(username)
            ELSE lower(username) || '@keipay.invalid' END,
       username,
       'owner',
       password_hash,
       must_change_password,
       created_at,
       updated_at
FROM admin_users
ORDER BY id;

DROP FUNCTION pg_temp.keipay_ulid(TIMESTAMPTZ);
