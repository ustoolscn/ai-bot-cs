ALTER TABLE messages ADD COLUMN IF NOT EXISTS reply_batch uuid,
  ADD COLUMN IF NOT EXISTS reply_due_at timestamptz,
  ADD COLUMN IF NOT EXISTS reply_max_at timestamptz;
CREATE INDEX IF NOT EXISTS messages_reply_window ON messages(conversation_id,sender_id,reply_due_at DESC) WHERE reply_batch IS NOT NULL;
CREATE INDEX IF NOT EXISTS messages_reply_batch ON messages(reply_batch) WHERE reply_batch IS NOT NULL;
