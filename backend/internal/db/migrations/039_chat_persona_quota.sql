ALTER TABLE conversations ADD COLUMN IF NOT EXISTS image_daily_limit integer CHECK(image_daily_limit>=0);
ALTER TABLE user_profiles ADD COLUMN IF NOT EXISTS persona jsonb;
CREATE TABLE IF NOT EXISTS image_daily_usage (
 conversation_id uuid REFERENCES conversations ON DELETE CASCADE, user_id text, day date, used integer NOT NULL DEFAULT 0,
 PRIMARY KEY(conversation_id,user_id,day)
);
CREATE INDEX IF NOT EXISTS chat_history_page ON messages(conversation_id,event_at DESC,id DESC) WHERE event_type<>'PROCESSING_ACK';
CREATE OR REPLACE FUNCTION display_mentions(m messages, body text) RETURNS text LANGUAGE plpgsql STABLE AS $$
DECLARE hit text[]; names jsonb := message_mention_names(m,body);
BEGIN
 FOR hit IN SELECT regexp_matches(body,'(<@!?([^!<>[:space:]]+)>)','g') LOOP
   body := replace(body,hit[1],'@'||coalesce(names->>hit[2],'群成员'));
 END LOOP;
 RETURN body;
END $$;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS delivery_error text;
ALTER TABLE messages ADD COLUMN IF NOT EXISTS persona_applied boolean NOT NULL DEFAULT false;
