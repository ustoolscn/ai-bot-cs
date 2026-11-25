-- Keep attribution stable when a conversation is rebound after a message arrives.
ALTER TABLE messages ADD COLUMN IF NOT EXISTS knowledge_base_id uuid REFERENCES knowledge_bases ON DELETE SET NULL;
ALTER TABLE knowledge_gaps ADD COLUMN IF NOT EXISTS knowledge_base_id uuid REFERENCES knowledge_bases ON DELETE SET NULL;
ALTER TABLE message_feedback ADD COLUMN IF NOT EXISTS knowledge_base_id uuid REFERENCES knowledge_bases ON DELETE SET NULL;

CREATE OR REPLACE FUNCTION conversation_learning_base(cid uuid) RETURNS uuid LANGUAGE sql STABLE AS $$
 WITH config AS (
   SELECT COALESCE(s.settings->'knowledgeBaseIds', g.value->'knowledgeBaseIds') AS ids
   FROM conversations c LEFT JOIN bot_services s ON s.id=c.service_id
   LEFT JOIN system_settings g ON g.key='agent_settings' WHERE c.id=cid
 ), bindings AS (
   SELECT DISTINCT value AS id FROM config,
     jsonb_array_elements_text(CASE WHEN jsonb_typeof(ids)='array' THEN ids ELSE '[]' END)
   UNION
   SELECT ck.knowledge_base_id::text FROM conversation_knowledge_bases ck,config
   WHERE ck.conversation_id=cid AND jsonb_typeof(ids) IS DISTINCT FROM 'array'
 )
 SELECT id FROM knowledge_bases WHERE id::text=(SELECT min(id) FROM bindings HAVING count(*)=1)
$$;

CREATE OR REPLACE FUNCTION stamp_message_learning_base() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.direction='outbound' AND NEW.reply_to_message_id IS NOT NULL THEN
   SELECT knowledge_base_id INTO NEW.knowledge_base_id FROM messages
   WHERE conversation_id=NEW.conversation_id AND direction='inbound' AND platform_message_id=NEW.reply_to_message_id;
   IF FOUND THEN RETURN NEW; END IF;
 END IF;
 NEW.knowledge_base_id := conversation_learning_base(NEW.conversation_id);
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER stamp_message_base BEFORE INSERT ON messages FOR EACH ROW EXECUTE FUNCTION stamp_message_learning_base();

-- Migrations replay on startup. A NULL owner is a valid permanent snapshot.
DO $$ BEGIN
 IF NOT EXISTS (SELECT FROM pg_trigger WHERE tgrelid='message_feedback'::regclass AND tgname='stamp_feedback_base') THEN
   WITH bindings AS MATERIALIZED (SELECT id,conversation_learning_base(id) AS kb FROM conversations)
   UPDATE messages m SET knowledge_base_id=c.kb FROM bindings c
   WHERE c.id=m.conversation_id AND c.kb IS NOT NULL;

   UPDATE message_feedback f SET knowledge_base_id=m.knowledge_base_id FROM messages m WHERE m.id=f.message_id AND m.knowledge_base_id IS NOT NULL;
   UPDATE knowledge_gaps g SET knowledge_base_id=(
    SELECT min(m.knowledge_base_id::text)::uuid FROM jsonb_array_elements_text(g.sample_message_ids) s
    LEFT JOIN messages m ON m.id::text=s.value
    HAVING count(*)=count(m.knowledge_base_id) AND count(DISTINCT m.knowledge_base_id)=1
   );
 END IF;
END $$;
CREATE OR REPLACE FUNCTION stamp_feedback_learning_base() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT knowledge_base_id INTO NEW.knowledge_base_id FROM messages WHERE id=NEW.message_id;
 RETURN NEW;
END $$;
CREATE OR REPLACE TRIGGER stamp_feedback_base BEFORE INSERT ON message_feedback FOR EACH ROW EXECUTE FUNCTION stamp_feedback_learning_base();
CREATE INDEX IF NOT EXISTS gaps_base_status ON knowledge_gaps(knowledge_base_id,status);
CREATE INDEX IF NOT EXISTS feedback_base_unread ON message_feedback(knowledge_base_id) WHERE NOT is_read AND rating IN ('bad','wrong','unhelpful');

CREATE OR REPLACE FUNCTION message_mention_names(m messages, body text) RETURNS jsonb LANGUAGE sql STABLE AS $$
 WITH ids AS (SELECT DISTINCT hit[1] AS id FROM regexp_matches(body,'<@!?([^!<>[:space:]]+)>','g') hit),
 names AS (
 SELECT id,coalesce(
   (SELECT coalesce(nullif(v->>'member_nickname',''),nullif(v->>'nickname',''),nullif(v->>'nick',''),nullif(v->>'username','')) FROM jsonb_array_elements(
     CASE WHEN jsonb_typeof(m.raw_event#>'{d,mentions}')='array' THEN m.raw_event#>'{d,mentions}' ELSE '[]' END) v
    WHERE id IN (v->>'id',v->>'member_openid',v->>'openid') LIMIT 1),
   (SELECT coalesce(nullif(nullif(display_name,ids.id),''),nullif(nullif(qq_username,ids.id),'')) FROM conversation_members
    WHERE conversation_id=m.conversation_id AND platform_user_id=ids.id),
   (SELECT h.sender_name FROM messages h WHERE h.conversation_id=m.conversation_id AND h.sender_id=ids.id
    AND h.sender_name NOT IN ('',h.sender_id) ORDER BY h.event_at DESC LIMIT 1)
 ) AS name FROM ids
 )
 SELECT coalesce(jsonb_object_agg(id,name) FILTER (WHERE name IS NOT NULL AND name<>id),'{}') FROM names
$$;
