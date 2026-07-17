ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS context_excluded boolean NOT NULL DEFAULT false,
  ADD COLUMN IF NOT EXISTS context_exclusion_reason text NOT NULL DEFAULT '';

UPDATE messages m
SET context_excluded = true,
    context_exclusion_reason = '历史模型 HTTP 403，已从后续会话上下文排除'
WHERE m.context_excluded = false
  AND EXISTS (
    SELECT 1
    FROM inbox_tasks t
    WHERE t.message_id = m.id
      AND (
        lower(COALESCE(t.last_error, '')) LIKE '%model api status 403%'
        OR lower(COALESCE(t.last_error, '')) LIKE '%http 403%'
      )
  );

CREATE INDEX IF NOT EXISTS messages_context_history_idx
  ON messages(conversation_id, event_at DESC)
  WHERE context_excluded = false;
