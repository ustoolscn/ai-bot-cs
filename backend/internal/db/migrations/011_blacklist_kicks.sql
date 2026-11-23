ALTER TABLE blocked_users ADD COLUMN IF NOT EXISTS auto_kick boolean NOT NULL DEFAULT false;
CREATE INDEX IF NOT EXISTS blocked_users_lookup ON blocked_users(bot_id,platform_user_id);
CREATE TABLE IF NOT EXISTS blacklist_actions (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 blocked_user_id uuid NOT NULL REFERENCES blocked_users(id) ON DELETE CASCADE,
 conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 status text NOT NULL DEFAULT 'pending',
 attempts integer NOT NULL DEFAULT 0,
 last_error text NOT NULL DEFAULT '',
 last_attempt_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(blocked_user_id,conversation_id)
);
