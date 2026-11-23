DO $$
BEGIN
IF EXISTS (SELECT FROM pg_attribute WHERE attrelid='handoffs'::regclass AND attname='claimed_at' AND NOT attisdropped) THEN
 RETURN;
END IF;
ALTER TABLE handoffs
 ADD COLUMN claimed_by_id uuid,
 ADD COLUMN claimed_via text CHECK (claimed_via IN ('qq','web')),
 ADD COLUMN claimed_by_name text,
 ADD COLUMN claimed_at timestamptz,
 ADD COLUMN close_method text CHECK (close_method IN ('qq','web','timeout')),
 ADD COLUMN idle_minutes integer NOT NULL DEFAULT 30 CHECK (idle_minutes BETWEEN 1 AND 1440);

UPDATE messages o SET handoff_id=i.handoff_id FROM messages i JOIN handoffs h ON h.id=i.handoff_id
 WHERE o.handoff_id IS NULL AND i.direction='inbound' AND o.direction='outbound'
 AND o.conversation_id=i.conversation_id AND o.reply_to_message_id=i.platform_message_id
 AND (o.event_type='HUMAN_REPLY' OR (o.event_type='AI_RESPONSE' AND i.id=h.message_id));

UPDATE handoffs SET close_method='web' WHERE status='resolved' AND resolved_by IS NOT NULL;
-- Historical automatic closure on delivery had no manual close action.
UPDATE handoffs SET status='resolved',resolved_at=coalesce(resolved_at,updated_at) WHERE status='replied';
UPDATE handoffs SET idle_minutes=greatest(1,least(1440,round(extract(epoch FROM paused_until-created_at)/60)::int)) WHERE status='open' AND paused_until IS NOT NULL;
UPDATE handoffs SET status='resolved',close_method='timeout',resolved_at=coalesce(paused_until,updated_at)
 WHERE status='open' AND coalesce(paused_until,created_at+interval '30 minutes')<=now();
UPDATE handoffs SET paused_until=created_at+interval '30 minutes' WHERE status='open' AND paused_until IS NULL;
WITH ranked AS (SELECT id,row_number() OVER(PARTITION BY conversation_id,platform_user_id ORDER BY created_at DESC,id) AS n FROM handoffs WHERE status='open')
 UPDATE handoffs h SET status='resolved',resolved_at=now() FROM ranked r WHERE h.id=r.id AND r.n>1;
ALTER TABLE handoffs ADD CHECK(status IN ('open','processing','resolved'));
CREATE UNIQUE INDEX handoffs_active_user ON handoffs(conversation_id,platform_user_id) WHERE status IN ('open','processing');
CREATE INDEX handoffs_due ON handoffs(paused_until) WHERE status IN ('open','processing');
END $$;
