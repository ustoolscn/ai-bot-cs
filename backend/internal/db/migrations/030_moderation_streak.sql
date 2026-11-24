-- Migrations replay on startup. Adopt legacy exclusions only once.
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_attribute WHERE attrelid='conversations'::regclass AND attname='moderation_revision') THEN
    ALTER TABLE conversations
      ADD COLUMN moderation_pending_at timestamptz,
      ADD COLUMN moderation_pending_event_at timestamptz,
      ADD COLUMN moderation_revision bigint NOT NULL DEFAULT 0;
    UPDATE messages o SET context_excluded=true,context_exclusion_reason=i.context_exclusion_reason
      FROM messages i WHERE i.context_excluded AND i.context_exclusion_reason LIKE '%403%' AND i.direction='inbound'
      AND o.conversation_id=i.conversation_id AND o.reply_to_message_id=i.platform_message_id
      AND o.direction='outbound' AND o.event_type='AI_RESPONSE' AND NOT o.context_excluded;
  END IF;
END $$;
