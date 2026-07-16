ALTER TABLE model_profiles
  ADD COLUMN IF NOT EXISTS web_search_mode text NOT NULL DEFAULT 'disabled',
  ADD COLUMN IF NOT EXISTS extra_body jsonb NOT NULL DEFAULT '{}';

ALTER TABLE agent_runs
  ADD COLUMN IF NOT EXISTS context_latency_ms integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS retrieval_latency_ms integer NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS conversation_members (
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  platform_user_id text NOT NULL,
  display_name text NOT NULL DEFAULT '',
  active boolean NOT NULL DEFAULT true,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(conversation_id, platform_user_id)
);

INSERT INTO conversation_members(conversation_id, platform_user_id, display_name, active, first_seen_at, last_seen_at)
SELECT conversation_id, sender_id, max(sender_name), true, min(event_at), max(event_at)
FROM messages
WHERE direction='inbound' AND sender_id<>''
GROUP BY conversation_id, sender_id
ON CONFLICT(conversation_id, platform_user_id) DO UPDATE SET
  display_name=CASE WHEN EXCLUDED.display_name<>'' THEN EXCLUDED.display_name ELSE conversation_members.display_name END,
  active=true,
  last_seen_at=GREATEST(conversation_members.last_seen_at, EXCLUDED.last_seen_at);

UPDATE conversations
SET name=CASE
  WHEN type='private' THEN 'QQ 用户 · ' || right(platform_id, 6)
  ELSE 'QQ 群聊 · ' || right(platform_id, 6)
END
WHERE name='' OR name=platform_id;
