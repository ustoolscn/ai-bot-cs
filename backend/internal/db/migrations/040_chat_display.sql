ALTER TABLE messages ADD COLUMN IF NOT EXISTS qq_delivered_text text;

-- Historical message content and multipart text are also consumed by model jobs.
CREATE OR REPLACE FUNCTION pg_temp.chat_face_names(body text) RETURNS text LANGUAGE plpgsql AS $$
DECLARE hit text[]; label text;
BEGIN
 FOR hit IN SELECT regexp_matches(body,'(<faceType=[^<>]*>)','g') LOOP
  BEGIN
   label := convert_from(decode(substring(hit[1] from 'ext="([^"]+)"'),'base64'),'UTF8')::jsonb->>'text';
  EXCEPTION WHEN data_exception THEN label := NULL;
  END;
  label := trim(label);
  IF label <> '' AND label NOT LIKE '[%]' THEN label := '['||label||']'; END IF;
  body := replace(body,hit[1],coalesce(nullif(label,''),'[表情]'));
 END LOOP;
 RETURN body;
END $$;
UPDATE messages SET content=pg_temp.chat_face_names(content),
 parts=CASE WHEN jsonb_typeof(parts)='array' THEN
 (SELECT coalesce(jsonb_agg(CASE WHEN p->>'type'='text' AND p->>'text' LIKE '%<faceType=%'
 THEN jsonb_set(p,'{text}',to_jsonb(pg_temp.chat_face_names(p->>'text'))) ELSE p END ORDER BY n),'[]')
 FROM jsonb_array_elements(parts) WITH ORDINALITY AS part(p,n)) ELSE parts END
WHERE direction='inbound' AND (content LIKE '%<faceType=%' OR parts::text LIKE '%<faceType=%');

INSERT INTO agent_jobs(kind,payload,dedupe_key)
SELECT 'qq_group_identity',jsonb_build_object('conversationId',id),'qq_group_identity:'||id::text
FROM conversations WHERE channel='qq' AND type='group' AND enabled AND NOT bot_removed
AND (name='' OR name=platform_id OR name LIKE 'QQ 群聊 · %') ON CONFLICT DO NOTHING;
