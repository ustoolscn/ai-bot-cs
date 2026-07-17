ALTER TABLE webhook_events
  DROP CONSTRAINT IF EXISTS webhook_events_channel_bot_id_platform_event_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS webhook_events_platform_type_unique
  ON webhook_events(channel, bot_id, platform_event_id, event_type);
