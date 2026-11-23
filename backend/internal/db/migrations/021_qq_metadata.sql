ALTER TABLE messages ADD COLUMN IF NOT EXISTS qq_ref_idx text NOT NULL DEFAULT '', ADD COLUMN IF NOT EXISTS qq_sent_at timestamptz;
UPDATE messages m SET qq_ref_idx=COALESCE((SELECT substr(v,9) FROM jsonb_array_elements_text(CASE WHEN jsonb_typeof(m.raw_event#>'{d,message_scene,ext}')='array' THEN m.raw_event#>'{d,message_scene,ext}' ELSE '[]'::jsonb END) v WHERE v LIKE 'msg\_idx=%' LIMIT 1),'') WHERE direction='inbound' AND qq_ref_idx='' AND (raw_event#>'{d,message_scene,ext}') IS NOT NULL;
CREATE INDEX IF NOT EXISTS messages_qq_reference ON messages(conversation_id,qq_ref_idx) WHERE qq_ref_idx<>'';
CREATE TABLE IF NOT EXISTS qq_media_assets(id uuid PRIMARY KEY DEFAULT gen_random_uuid(), conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, filename text NOT NULL, file_uuid text NOT NULL, file_info text NOT NULL,raw_url text NOT NULL,expires_at timestamptz,created_at timestamptz NOT NULL DEFAULT now());
ALTER TABLE messages ADD COLUMN IF NOT EXISTS qq_send_payload jsonb, ADD COLUMN IF NOT EXISTS qq_quote_target uuid REFERENCES messages(id) ON DELETE SET NULL;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS qq_extended_replies boolean NOT NULL DEFAULT false;
ALTER TABLE conversations ADD COLUMN IF NOT EXISTS qq_group_info jsonb NOT NULL DEFAULT '{}';
ALTER TABLE conversation_members ADD COLUMN IF NOT EXISTS qq_avatar text NOT NULL DEFAULT '', ADD COLUMN IF NOT EXISTS qq_joined_at timestamptz, ADD COLUMN IF NOT EXISTS qq_union_id text NOT NULL DEFAULT '';

WITH latest AS (
  SELECT DISTINCT ON (m.conversation_id,m.sender_id) m.conversation_id,m.sender_id,
         m.raw_event#>>'{d,author,avatar}' AS avatar,m.raw_event#>>'{d,author,union_openid}' AS union_id
  FROM messages m JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.platform_user_id=m.sender_id
  WHERE cm.qq_avatar='' AND m.direction='inbound' AND COALESCE(m.raw_event#>>'{d,author,avatar}','')<>''
  ORDER BY m.conversation_id,m.sender_id,m.event_at DESC
)
UPDATE conversation_members cm SET qq_avatar=v.avatar,qq_union_id=COALESCE(NULLIF(cm.qq_union_id,''),v.union_id,'')
FROM latest v WHERE cm.conversation_id=v.conversation_id AND cm.platform_user_id=v.sender_id AND cm.qq_avatar='';
