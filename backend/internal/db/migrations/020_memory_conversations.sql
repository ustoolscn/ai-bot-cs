ALTER TABLE user_memories ADD COLUMN IF NOT EXISTS conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE;

UPDATE user_memories m SET conversation_id=msg.conversation_id
FROM messages msg WHERE m.conversation_id IS NULL AND m.source_message_id=msg.id
AND m.bot_id=msg.bot_id AND m.platform_user_id=msg.sender_id;

CREATE INDEX IF NOT EXISTS user_memories_conversation_user_idx
ON user_memories(conversation_id,platform_user_id,updated_at DESC) WHERE status='active';
