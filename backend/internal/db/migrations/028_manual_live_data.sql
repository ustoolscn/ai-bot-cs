-- Keep the latest successful snapshot per site/credential when dropping the KB suffix.
WITH defaults AS (SELECT coalesce((SELECT value->'liveData' FROM system_settings WHERE key='agent_settings'),'{}') AS cfg),
configs AS (SELECT cfg FROM defaults UNION SELECT cfg || coalesce(settings->'liveData','{}') FROM defaults,bot_services),
keys AS (SELECT rtrim(cfg->>'baseUrl','/')||'|'||coalesce(cfg->>'userId','')||'|'||coalesce(cfg->>'accessTokenEnc','')||'|' AS prefix,
 cfg->>'announcementKnowledgeBaseId' AS kb FROM configs WHERE coalesce(cfg->>'announcementKnowledgeBaseId','')<>''),
old AS (SELECT DISTINCT ON (prefix) 'newapi:'||encode(digest(prefix,'sha1'),'hex') AS dest,s.*
 FROM keys JOIN live_data_snapshots s ON source='newapi:'||encode(digest(prefix||kb,'sha1'),'hex') ORDER BY prefix,fetched_at DESC NULLS LAST,last_attempt_at DESC NULLS LAST)
INSERT INTO live_data_snapshots(source,data,content_hash,fetched_at,last_attempt_at,last_error)
 SELECT dest,data,content_hash,fetched_at,last_attempt_at,last_error FROM old
 ON CONFLICT(source) DO UPDATE SET data=excluded.data,content_hash=excluded.content_hash,fetched_at=excluded.fetched_at,last_attempt_at=excluded.last_attempt_at,last_error=excluded.last_error
 WHERE live_data_snapshots.fetched_at IS NULL OR live_data_snapshots.fetched_at<excluded.fetched_at;

DELETE FROM agent_jobs j USING knowledge_entries e WHERE e.source='announcement' AND j.kind='index_entry' AND j.payload->>'entryId'=e.id::text;
DELETE FROM knowledge_entries WHERE source='announcement';
UPDATE system_settings SET value=jsonb_set(value,'{liveData}',(value->'liveData')-ARRAY['refreshSeconds','syncAnnouncements','announcementKnowledgeBaseId'])
 WHERE key='agent_settings' AND (value->'liveData') ?| ARRAY['refreshSeconds','syncAnnouncements','announcementKnowledgeBaseId'];
UPDATE bot_services SET settings=jsonb_set(settings,'{liveData}',(settings->'liveData')-ARRAY['refreshSeconds','syncAnnouncements','announcementKnowledgeBaseId'])
 WHERE (settings->'liveData') ?| ARRAY['refreshSeconds','syncAnnouncements','announcementKnowledgeBaseId'];
