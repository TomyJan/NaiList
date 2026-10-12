CREATE TABLE IF NOT EXISTS identities (
 id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 provider_id TEXT NOT NULL,
 issuer TEXT NOT NULL,
 subject TEXT NOT NULL,
 qq_unionid TEXT,
 nickname TEXT NOT NULL,
 avatar_url TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
 UNIQUE(provider_id, issuer, subject),
 UNIQUE(user_id, provider_id)
);
CREATE UNIQUE INDEX IF NOT EXISTS identities_qq_unionid
 ON identities(qq_unionid) WHERE provider_id='qq' AND qq_unionid IS NOT NULL;
-- Legacy OpenIDs are claimed by the original QQ app on the next successful login.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM information_schema.columns
   WHERE table_schema=current_schema() AND table_name='users' AND column_name='qq_openid') THEN
  INSERT INTO identities(user_id,provider_id,issuer,subject,nickname,avatar_url)
   SELECT id,'qq','',qq_openid,nickname,avatar_url FROM users WHERE qq_openid IS NOT NULL
   ON CONFLICT DO NOTHING;
  ALTER TABLE users DROP COLUMN qq_openid;
 END IF;
END $$;
CREATE TABLE IF NOT EXISTS auth_flows (
 state_hash TEXT PRIMARY KEY,
 provider_id TEXT NOT NULL,
 bind_user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
 session_hash TEXT NOT NULL DEFAULT '',
 nonce TEXT NOT NULL,
 verifier TEXT NOT NULL,
 expires_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS auth_flows_expiry ON auth_flows(expires_at);
