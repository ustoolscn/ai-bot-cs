CREATE TABLE IF NOT EXISTS conversation_staff_styles (
 conversation_id uuid PRIMARY KEY REFERENCES conversations(id) ON DELETE CASCADE,
 staff_ids text[] NOT NULL DEFAULT '{}',
 style text NOT NULL DEFAULT '',
 sample_count integer NOT NULL DEFAULT 0,
 source_message_ids jsonb NOT NULL DEFAULT '[]',
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE conversation_staff_styles ADD COLUMN IF NOT EXISTS staff_ids text[] NOT NULL DEFAULT '{}';
-- Never send queued processing placeholders after upgrading.
UPDATE outbox_tasks t SET status='expired',updated_at=now() FROM messages m
 WHERE t.message_id=m.id AND m.event_type='PROCESSING_ACK' AND t.status IN ('pending','processing');
