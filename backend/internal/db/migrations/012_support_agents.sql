CREATE TABLE IF NOT EXISTS personality_presets (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  prompt text NOT NULL
);
CREATE TABLE IF NOT EXISTS support_agents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL,
  instructions text NOT NULL DEFAULT '',
  personality_id uuid NOT NULL REFERENCES personality_presets(id)
);
CREATE TABLE IF NOT EXISTS bot_services (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  name text NOT NULL,
  agent_id uuid NOT NULL REFERENCES support_agents(id),
  UNIQUE(bot_id,id)
);
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS service_id uuid;
DO $$ BEGIN
  ALTER TABLE conversations ADD CONSTRAINT conversation_bot_service
    FOREIGN KEY(bot_id,service_id) REFERENCES bot_services(bot_id,id);
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
INSERT INTO personality_presets(id,name,prompt) VALUES
  ('00000000-0000-4000-8000-000000000001','亲切耐心','用自然、温和的口吻回答。先回应用户的困惑，再带对方完成下一步。'),
  ('00000000-0000-4000-8000-000000000002','简洁直接','先给结论，再给必要的操作步骤。句子简短，避免寒暄。'),
  ('00000000-0000-4000-8000-000000000003','严谨细致','区分已知事实和待确认信息。说明判断依据，缺少关键信息时先提问。')
ON CONFLICT(id) DO NOTHING;
