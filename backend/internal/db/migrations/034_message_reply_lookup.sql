CREATE INDEX IF NOT EXISTS messages_conversation_reply_target
    ON messages(conversation_id, reply_to_message_id)
    WHERE reply_to_message_id IS NOT NULL;
