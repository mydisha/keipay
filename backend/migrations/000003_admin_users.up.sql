CREATE TABLE IF NOT EXISTS admin_users (
    id                BIGSERIAL PRIMARY KEY,
    username          TEXT UNIQUE NOT NULL,
    password_hash     TEXT NOT NULL,
    must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_admin_users_username ON admin_users(username);