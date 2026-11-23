ALTER TABLE conversations ADD COLUMN IF NOT EXISTS roster_synced_at timestamptz;
ALTER TABLE conversation_members ADD COLUMN IF NOT EXISTS qq_role text NOT NULL DEFAULT '';
ALTER TABLE conversation_members ADD COLUMN IF NOT EXISTS is_bot boolean NOT NULL DEFAULT false;
ALTER TABLE conversation_members ADD COLUMN IF NOT EXISTS membership_updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE conversation_members ADD COLUMN IF NOT EXISTS qq_username text NOT NULL DEFAULT '';
