-- Include soft-deleted rows when checking the per-account storage quota.
CREATE INDEX IF NOT EXISTS groups_owner_all ON groups(owner_id);
CREATE INDEX IF NOT EXISTS sites_owner_all ON sites(owner_id);
