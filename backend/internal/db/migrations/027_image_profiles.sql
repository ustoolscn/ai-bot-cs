DO $$
BEGIN
 IF NOT EXISTS (SELECT FROM pg_attribute WHERE attrelid='bot_services'::regclass AND attname='image_profile_id' AND NOT attisdropped) THEN
  ALTER TABLE model_profiles DROP CONSTRAINT model_profiles_kind_check;
  ALTER TABLE model_profiles ADD CHECK(kind IN ('chat','embedding','image'));
  ALTER TABLE bot_services ADD COLUMN image_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL;
 END IF;
END $$;

-- Remove the old key in the same transaction as copying its encrypted value.
WITH legacy AS (
 UPDATE system_settings s SET value=s.value-'imageGeneration'-'imageGenerationApiKey'
 FROM (SELECT key,value->'imageGeneration' AS cfg FROM system_settings WHERE key='agent_settings' AND value ? 'imageGeneration' FOR UPDATE) old
 WHERE s.key=old.key RETURNING old.cfg
)
INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,enabled)
SELECT '原全局生图模型','image',coalesce(cfg->>'baseUrl',''),coalesce(cfg->>'apiKeyEnc',''),
 coalesce(nullif(cfg->>'model',''),'gpt-image-2.6-flare'),coalesce(cfg->>'baseUrl','')<>'' AND coalesce(cfg->>'apiKeyEnc','')<>''
FROM legacy WHERE coalesce(cfg->>'baseUrl','')<>'' OR coalesce(cfg->>'apiKeyEnc','')<>''
 OR coalesce(cfg->>'model','') NOT IN ('','gpt-image-2.6-flare');
