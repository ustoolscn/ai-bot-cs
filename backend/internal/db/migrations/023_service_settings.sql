-- Earlier migrations are replayed at startup. Only copy legacy defaults once.
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_attribute WHERE attrelid='bot_services'::regclass AND attname='settings') THEN
    ALTER TABLE bot_services ADD COLUMN settings jsonb NOT NULL DEFAULT '{}',
      ADD COLUMN instructions text, ALTER COLUMN agent_id DROP NOT NULL;
    UPDATE bot_services s SET settings=coalesce((
      SELECT jsonb_object_agg(k,v) FROM system_settings, jsonb_each(value) AS field(k,v)
      WHERE key='agent_settings' AND k IN ('examples','liveData','toolsEnabled','maxToolSteps','handoffPauseMinutes','evaluationEnabled','staffMiningEnabled','autoPublishStaff','autoPublishMinConfidence','learningKnowledgeBaseId')
    ),'{}') || jsonb_build_object('stylePrompt',concat_ws(E'\n',nullif((SELECT value->>'stylePrompt' FROM system_settings WHERE key='agent_settings'),''),p.prompt)),
      instructions=a.instructions
    FROM support_agents a JOIN personality_presets p ON p.id=a.personality_id WHERE a.id=s.agent_id;
    -- Pin even an empty credential to its original site.
    UPDATE bot_services SET settings=jsonb_set(settings,'{liveData,accessTokenEnc}','""')
    WHERE jsonb_typeof(settings->'liveData')='object' AND NOT (settings->'liveData' ? 'accessTokenEnc');
  END IF;
END $$;
CREATE TABLE IF NOT EXISTS user_profiles (
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  platform_user_id text NOT NULL, content text NOT NULL DEFAULT '', display_name text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(conversation_id,platform_user_id)
);
ALTER TABLE user_memories ADD COLUMN IF NOT EXISTS event_at timestamptz;
DO $$ BEGIN
 IF NOT EXISTS(SELECT FROM pg_attribute WHERE attrelid='conversations'::regclass AND attname='moderation_cutoff') THEN
   ALTER TABLE conversations ADD COLUMN moderation_cutoff timestamptz;
   UPDATE conversations c SET moderation_cutoff=m.cutoff FROM (
     SELECT conversation_id,max(event_at) AS cutoff FROM messages WHERE context_excluded AND context_exclusion_reason LIKE '%403%' GROUP BY conversation_id
   ) m WHERE c.id=m.conversation_id;
   DELETE FROM conversation_summaries s USING conversations c WHERE s.conversation_id=c.id AND c.moderation_cutoff IS NOT NULL;
 END IF;
END $$;

-- Adopt the old site's snapshot and announcement keys atomically on upgrade.
DO $$ DECLARE live jsonb; site text; kb text;
BEGIN
 SELECT value->'liveData' INTO live FROM system_settings WHERE key='agent_settings';
 site:=rtrim(coalesce(live->>'baseUrl',''),'/'); kb:=coalesce(live->>'announcementKnowledgeBaseId','');
 IF site<>'' THEN
   UPDATE live_data_snapshots SET source='newapi:'||encode(digest(site||'|'||coalesce(live->>'userId','')||'|'||coalesce(live->>'accessTokenEnc','')||'|'||kb,'sha1'),'hex') WHERE source='newapi';
   UPDATE knowledge_entries SET external_key='site:'||encode(digest(site||'|'||kb,'sha1'),'hex')||':'||substr(external_key,8)
   WHERE source='announcement' AND knowledge_base_id::text=kb AND external_key LIKE 'newapi:%';
 END IF;
END $$;
