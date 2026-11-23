CREATE TABLE IF NOT EXISTS model_proxies (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, address text NOT NULL
);
ALTER TABLE model_profiles ADD COLUMN IF NOT EXISTS proxy_id uuid REFERENCES model_proxies(id) ON DELETE RESTRICT;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS human_operator boolean NOT NULL DEFAULT false CHECK (NOT human_operator OR type='private');
ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS ticket_number bigint GENERATED ALWAYS AS IDENTITY UNIQUE;
ALTER TABLE handoffs ADD COLUMN IF NOT EXISTS reply_id uuid REFERENCES messages(id) ON DELETE SET NULL;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS handoff_id uuid REFERENCES handoffs(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS messages_handoff_idx ON messages(handoff_id) WHERE handoff_id IS NOT NULL;
UPDATE messages m SET handoff_id=(SELECT h.id FROM handoffs h WHERE h.message_id=m.id ORDER BY h.created_at DESC LIMIT 1)
WHERE m.handoff_id IS NULL AND EXISTS(SELECT FROM handoffs h WHERE h.message_id=m.id);
-- Earlier releases recorded paused follow-ups on the inbox task only.
UPDATE messages m SET handoff_id=(
 SELECT h.id FROM handoffs h WHERE h.conversation_id=m.conversation_id
 AND h.platform_user_id=m.sender_id AND m.created_at BETWEEN h.created_at AND h.paused_until
 ORDER BY h.created_at DESC LIMIT 1)
FROM inbox_tasks t WHERE t.message_id=m.id AND t.status='handoff' AND m.handoff_id IS NULL;
UPDATE messages m SET sender_name=u.username
FROM audit_logs a JOIN admin_users u ON u.id=a.user_id
WHERE m.event_type='HUMAN_REPLY' AND m.sender_name='' AND a.action='message.human_reply' AND a.detail->>'replyId'=m.id::text;
-- Recover prior delivered web replies, including replies to paused follow-ups.
UPDATE handoffs h SET reply_id=r.id,status='replied',resolved_at=r.sent_at
FROM (SELECT DISTINCT ON (i.handoff_id) i.handoff_id,o.id,coalesce(t.updated_at,o.event_at) AS sent_at
 FROM messages i JOIN messages o ON o.conversation_id=i.conversation_id AND o.reply_to_message_id=i.platform_message_id
 LEFT JOIN outbox_tasks t ON t.message_id=o.id
 WHERE i.handoff_id IS NOT NULL AND i.direction='inbound' AND o.event_type='HUMAN_REPLY' AND o.status='sent'
 ORDER BY i.handoff_id,o.event_at DESC) r
WHERE h.id=r.handoff_id AND h.reply_id IS NULL;
