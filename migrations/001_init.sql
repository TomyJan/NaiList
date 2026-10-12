CREATE TABLE IF NOT EXISTS users (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 qq_openid TEXT NOT NULL UNIQUE,
 nickname TEXT NOT NULL,
 avatar_url TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS sessions (
 token_hash TEXT PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id),
 csrf_token TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS sessions_expiry ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS groups (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 owner_id BIGINT NOT NULL REFERENCES users(id),
 organization VARCHAR(100) NOT NULL,
 keywords TEXT[] NOT NULL DEFAULT '{}',
 qq_number VARCHAR(12) NOT NULL,
 join_url TEXT NOT NULL,
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 deleted_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS groups_active_number ON groups(qq_number) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS groups_owner ON groups(owner_id) WHERE deleted_at IS NULL;
