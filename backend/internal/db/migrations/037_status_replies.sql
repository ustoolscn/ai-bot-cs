DO $$
BEGIN
    IF NOT EXISTS (SELECT FROM pg_attribute WHERE attrelid='messages'::regclass
                   AND attname='contains_site_status' AND NOT attisdropped) THEN
        ALTER TABLE messages ADD COLUMN contains_site_status boolean NOT NULL DEFAULT false;

        -- Recover only the delivery window that can overlap an upgrade. Use
        -- stored attachments and query provenance, never keywords in user text.
        UPDATE messages m SET contains_site_status=true
        FROM outbox_tasks o
        WHERE o.message_id=m.id AND o.updated_at>now()-interval '5 minutes'
          AND o.status IN ('pending','processing','sent') AND m.event_type='AI_RESPONSE'
          AND (m.parts @> '[{"type":"image","filename":"status.png"}]' OR EXISTS (
              SELECT FROM messages i JOIN agent_runs r ON r.message_id=i.id
              WHERE i.conversation_id=m.conversation_id AND i.platform_message_id=m.reply_to_message_id
                AND r.status='completed' AND (
                    EXISTS (SELECT FROM tool_calls t WHERE t.agent_run_id=r.id AND t.error IS NULL
                            AND t.tool_name IN ('get_service_status','get_group_status')
                            AND (t.result ? 'queriedAt' OR t.result ? 'status'))
                    OR EXISTS (SELECT FROM jsonb_array_elements(CASE WHEN jsonb_typeof(r.context_messages)='array' THEN r.context_messages ELSE '[]' END) msg
                               WHERE msg->>'role'='system' AND strpos(msg->>'content','本次查询的全站小时统计：')>0)
                )
          ));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS messages_site_status ON messages(conversation_id,event_at DESC)
    WHERE contains_site_status AND NOT context_excluded;
