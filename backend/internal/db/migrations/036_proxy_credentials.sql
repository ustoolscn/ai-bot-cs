ALTER TABLE model_proxies
    ADD COLUMN IF NOT EXISTS username text NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS password_enc text NOT NULL DEFAULT '';
