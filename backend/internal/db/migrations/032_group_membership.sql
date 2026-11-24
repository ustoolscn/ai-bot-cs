-- Startup replays migrations. Import the legacy membership evidence only once.
DO $$ BEGIN
 IF NOT EXISTS (SELECT FROM pg_attribute WHERE attrelid='conversations'::regclass AND attname='bot_removed' AND NOT attisdropped) THEN
  ALTER TABLE conversations ADD COLUMN bot_removed boolean NOT NULL DEFAULT false,
    ADD COLUMN membership_event_at timestamptz;
  WITH evidence AS (
    SELECT bot_id,raw_event#>>'{d,group_openid}' AS platform_id,event_type='GROUP_DEL_ROBOT' AS removed,created_at,
      CASE WHEN event_type IN ('GROUP_ADD_ROBOT','GROUP_DEL_ROBOT') AND raw_event#>>'{d,timestamp}' ~ '^[1-9][0-9]{0,9}$'
             THEN to_timestamp((raw_event#>>'{d,timestamp}')::bigint)
           WHEN event_type IN ('GROUP_MESSAGE_CREATE','GROUP_AT_MESSAGE_CREATE') AND pg_input_is_valid(raw_event#>>'{d,timestamp}','timestamptz')
             THEN (raw_event#>>'{d,timestamp}')::timestamptz ELSE created_at END AS occurred
    FROM webhook_events WHERE channel='qq' AND (event_type IN ('GROUP_ADD_ROBOT','GROUP_DEL_ROBOT')
      OR (event_type IN ('GROUP_MESSAGE_CREATE','GROUP_AT_MESSAGE_CREATE') AND raw_event#>>'{d,author,bot}' IS DISTINCT FROM 'true'))
    UNION ALL
    SELECT c.bot_id,c.platform_id,false,m.created_at,m.event_at FROM messages m JOIN conversations c ON c.id=m.conversation_id
    WHERE c.channel='qq' AND c.type='group' AND m.direction='inbound' AND m.event_type IN ('GROUP_MESSAGE_CREATE','GROUP_AT_MESSAGE_CREATE')
  ), latest AS (
    SELECT DISTINCT ON(bot_id,platform_id) * FROM evidence ORDER BY bot_id,platform_id,occurred DESC,created_at DESC,removed DESC
  )
  UPDATE conversations c SET bot_removed=l.removed,membership_event_at=l.occurred FROM latest l
    WHERE c.channel='qq' AND c.type='group' AND c.bot_id=l.bot_id AND c.platform_id=l.platform_id;
 END IF;
END $$;

-- A schedule can outlive the bot service that supplied its defaults.
DO $$ BEGIN
 IF EXISTS (SELECT FROM pg_constraint WHERE conrelid='scheduled_posts'::regclass AND conname='scheduled_posts_service_id_fkey' AND confdeltype='a') THEN
  ALTER TABLE scheduled_posts DROP CONSTRAINT scheduled_posts_service_id_fkey,
    ADD FOREIGN KEY(service_id) REFERENCES bot_services(id) ON DELETE SET NULL;
 END IF;
END $$;

CREATE OR REPLACE VIEW schedule_recipients AS
 SELECT s.id AS schedule_id,c.id AS conversation_id,c.bot_id,t.service_id
 FROM scheduled_posts s JOIN conversations c ON c.type='group'
 JOIN bots b ON b.id=c.bot_id
 LEFT JOIN schedule_targets t ON t.schedule_id=s.id AND t.conversation_id=c.id
 WHERE (s.all_groups OR t.conversation_id IS NOT NULL) AND b.enabled AND c.enabled AND NOT c.bot_removed;
