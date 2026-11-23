-- Migrations run at each startup. Copy legacy assignments only on first upgrade.
DO $$ BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema=current_schema()
                 AND table_name='bot_services' AND column_name='chat_profile_id') THEN
    ALTER TABLE bot_services ADD COLUMN chat_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL;
    UPDATE conversations SET trigger_mode='smart' WHERE type='group' AND trigger_mode IN ('mention_only','always');
    UPDATE bot_services s SET chat_profile_id=COALESCE((
      SELECT min(COALESCE(c.chat_profile_id,b.default_chat_profile_id)::text)::uuid
      FROM conversations c WHERE c.service_id=s.id
      HAVING count(DISTINCT COALESCE(c.chat_profile_id::text,b.default_chat_profile_id::text,''))=1
    ),b.default_chat_profile_id) FROM bots b WHERE b.id=s.bot_id;
  END IF;
END $$;

ALTER TABLE bot_services ADD COLUMN IF NOT EXISTS simple_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL;
ALTER TABLE bot_services ADD COLUMN IF NOT EXISTS decision_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS daily_report boolean NOT NULL DEFAULT false;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS last_report_date date;

CREATE OR REPLACE VIEW conversation_support_config AS
SELECT c.id, s.name AS service_name, p.prompt, a.instructions,
  CASE WHEN s.id IS NULL THEN COALESCE(c.chat_profile_id,b.default_chat_profile_id)
       ELSE s.chat_profile_id END AS chat_profile_id, s.simple_profile_id, s.decision_profile_id
FROM conversations c JOIN bots b ON b.id=c.bot_id
LEFT JOIN bot_services s ON s.id=c.service_id
LEFT JOIN support_agents a ON a.id=s.agent_id
LEFT JOIN personality_presets p ON p.id=a.personality_id;
