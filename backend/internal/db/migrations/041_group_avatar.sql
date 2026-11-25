ALTER TABLE conversations ADD COLUMN qq_group_info_checked_at timestamptz;

INSERT INTO agent_jobs(kind,payload,dedupe_key)
SELECT 'qq_group_identity',jsonb_build_object('conversationId',id),'qq_group_identity:'||id::text
FROM conversations WHERE channel='qq' AND type='group' AND enabled AND NOT bot_removed
AND coalesce(qq_group_info->>'group_avatar','')='' ON CONFLICT DO NOTHING;
