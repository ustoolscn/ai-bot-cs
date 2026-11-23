ALTER TABLE conversations ADD COLUMN IF NOT EXISTS guard_policy jsonb NOT NULL DEFAULT '{}';
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS guard_revision uuid NOT NULL DEFAULT gen_random_uuid();
CREATE TABLE IF NOT EXISTS group_reviews(
 message_id uuid PRIMARY KEY REFERENCES messages(id) ON DELETE CASCADE,
 queue_reply boolean NOT NULL, status text NOT NULL DEFAULT 'pending', reason text NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS group_guard_actions(
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
 event_key text NOT NULL, member_id text NOT NULL, kind text NOT NULL, reason text NOT NULL,
 policy_revision uuid, expires_at timestamptz, status text NOT NULL DEFAULT 'pending', detail text NOT NULL DEFAULT '',
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(conversation_id,event_key,kind)
);
CREATE INDEX IF NOT EXISTS group_actions_pending ON group_guard_actions(created_at) WHERE status='pending';
