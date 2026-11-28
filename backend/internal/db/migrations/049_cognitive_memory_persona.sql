-- Cognitive memory, cross-bot unified subjects, real-time emotion perception,
-- persona companion state, group episodic memory, and proactive triggers.

CREATE TABLE IF NOT EXISTS memory_subjects (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  display_name text NOT NULL DEFAULT '',
  nicknames jsonb NOT NULL DEFAULT '[]',
  profile_content text NOT NULL DEFAULT '',
  persona jsonb,
  familiarity real NOT NULL DEFAULT 0,
  affinity real NOT NULL DEFAULT 50,
  trust real NOT NULL DEFAULT 50,
  relationship_stage text NOT NULL DEFAULT 'stranger',
  interaction_turns integer NOT NULL DEFAULT 0,
  interaction_days integer NOT NULL DEFAULT 0,
  last_interaction_day date,
  last_interaction_at timestamptz,
  mood_state jsonb NOT NULL DEFAULT '{}',
  turns_since_reflection integer NOT NULL DEFAULT 0,
  last_reflected_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS user_identity_links (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subject_id uuid NOT NULL REFERENCES memory_subjects(id) ON DELETE CASCADE,
  bot_id uuid REFERENCES bots(id) ON DELETE CASCADE,
  identity_type text NOT NULL,
  identity_value text NOT NULL,
  last_conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL,
  display_name text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS user_identity_links_union_uidx
  ON user_identity_links(identity_value) WHERE identity_type = 'qq_union_id';
CREATE UNIQUE INDEX IF NOT EXISTS user_identity_links_bot_uidx
  ON user_identity_links(bot_id, identity_type, identity_value) WHERE bot_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS user_identity_links_subject_idx
  ON user_identity_links(subject_id);

ALTER TABLE user_memories
  ADD COLUMN IF NOT EXISTS subject_id uuid REFERENCES memory_subjects(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS importance smallint NOT NULL DEFAULT 5,
  ADD COLUMN IF NOT EXISTS emotion_tag text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS access_count integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS valid_from timestamptz,
  ADD COLUMN IF NOT EXISTS valid_until timestamptz,
  ADD COLUMN IF NOT EXISTS resolved_at timestamptz,
  ADD COLUMN IF NOT EXISTS source_conversation_name text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS source_bot_name text NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS user_memories_subject_active_idx
  ON user_memories(subject_id, pinned DESC, updated_at DESC) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS group_memories (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  kind text NOT NULL DEFAULT 'topic',
  content text NOT NULL,
  participants jsonb NOT NULL DEFAULT '[]',
  importance smallint NOT NULL DEFAULT 5,
  embedding vector,
  embedding_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL,
  expires_at timestamptz,
  status text NOT NULL DEFAULT 'active',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS group_memories_conv_active_idx
  ON group_memories(conversation_id, updated_at DESC) WHERE status = 'active';

CREATE TABLE IF NOT EXISTS bot_mood_states (
  scope_key text PRIMARY KEY,
  mood text NOT NULL DEFAULT '轻松',
  energy real NOT NULL DEFAULT 0.7,
  valence real NOT NULL DEFAULT 0.3,
  note text NOT NULL DEFAULT '',
  updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS last_chime_in_at timestamptz,
  ADD COLUMN IF NOT EXISTS last_group_digest_at timestamptz,
  ADD COLUMN IF NOT EXISTS group_vibe text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS group_digest_cursor timestamptz;

CREATE TABLE IF NOT EXISTS proactive_triggers (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  subject_id uuid REFERENCES memory_subjects(id) ON DELETE CASCADE,
  bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  kind text NOT NULL,
  topic text NOT NULL,
  source_memory_id uuid REFERENCES user_memories(id) ON DELETE SET NULL,
  scheduled_at timestamptz NOT NULL,
  status text NOT NULL DEFAULT 'pending',
  skip_reason text NOT NULL DEFAULT '',
  sent_message_id uuid REFERENCES messages(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS proactive_triggers_due_idx
  ON proactive_triggers(status, scheduled_at) WHERE status = 'pending';

ALTER TABLE agent_runs
  ADD COLUMN IF NOT EXISTS perception jsonb NOT NULL DEFAULT '{}',
  ADD COLUMN IF NOT EXISTS recalled_memories jsonb NOT NULL DEFAULT '[]',
  ADD COLUMN IF NOT EXISTS director_notes text NOT NULL DEFAULT '';

-- Attach admin_change notification triggers idempotently.
DO $$
DECLARE
  resource text;
BEGIN
  FOREACH resource IN ARRAY ARRAY[
    'memory_subjects', 'user_identity_links', 'group_memories', 'bot_mood_states', 'proactive_triggers'
  ] LOOP
    IF NOT EXISTS (SELECT FROM pg_trigger WHERE tgrelid = resource::regclass AND tgname = 'admin_change') THEN
      EXECUTE format('CREATE TRIGGER admin_change AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION notify_admin_change()', resource);
    END IF;
  END LOOP;
END;
$$;

-- Idempotently backfill existing user_profiles and user_memories into memory_subjects.
DO $$
DECLARE
  rec RECORD;
  v_subject uuid;
  v_id_type text;
  v_union text;
BEGIN
  FOR rec IN
    SELECT DISTINCT
      src.bot_id,
      src.platform_user_id,
      src.conversation_id,
      COALESCE(c.type, 'group') AS conv_type,
      COALESCE(NULLIF(cm.qq_union_id, ''), '') AS union_id,
      COALESCE(NULLIF(src.display_name, ''), NULLIF(cm.display_name, ''), '') AS display_name,
      COALESCE(up.content, '') AS profile_content,
      up.persona AS persona
    FROM (
      SELECT DISTINCT m.bot_id, m.platform_user_id, m.conversation_id, m.display_name
      FROM user_memories m
      WHERE m.subject_id IS NULL
      UNION
      SELECT DISTINCT c2.bot_id, p.platform_user_id, p.conversation_id, p.display_name
      FROM user_profiles p
      JOIN conversations c2 ON c2.id = p.conversation_id
    ) src
    LEFT JOIN conversations c ON c.id = src.conversation_id
    LEFT JOIN conversation_members cm ON cm.conversation_id = src.conversation_id AND cm.platform_user_id = src.platform_user_id
    LEFT JOIN user_profiles up ON up.conversation_id = src.conversation_id AND up.platform_user_id = src.platform_user_id
  LOOP
    v_id_type := CASE WHEN rec.conv_type = 'private' THEN 'user_openid' ELSE 'member_openid' END;
    v_union := rec.union_id;
    v_subject := NULL;

    IF v_union <> '' THEN
      SELECT subject_id INTO v_subject
      FROM user_identity_links
      WHERE identity_type = 'qq_union_id' AND identity_value = v_union
      LIMIT 1;
    END IF;

    IF v_subject IS NULL THEN
      SELECT subject_id INTO v_subject
      FROM user_identity_links
      WHERE bot_id = rec.bot_id AND identity_type = v_id_type AND identity_value = rec.platform_user_id
      LIMIT 1;
    END IF;

    IF v_subject IS NULL THEN
      INSERT INTO memory_subjects(display_name, profile_content, persona)
      VALUES (rec.display_name, rec.profile_content, rec.persona)
      RETURNING id INTO v_subject;
    ELSE
      UPDATE memory_subjects
      SET
        display_name = CASE WHEN display_name = '' THEN rec.display_name ELSE display_name END,
        profile_content = CASE WHEN profile_content = '' THEN rec.profile_content ELSE profile_content END,
        persona = COALESCE(persona, rec.persona),
        updated_at = now()
      WHERE id = v_subject;
    END IF;

    IF v_union <> '' THEN
      INSERT INTO user_identity_links(subject_id, bot_id, identity_type, identity_value, last_conversation_id, display_name)
      VALUES (v_subject, NULL, 'qq_union_id', v_union, rec.conversation_id, rec.display_name)
      ON CONFLICT DO NOTHING;
    END IF;

    INSERT INTO user_identity_links(subject_id, bot_id, identity_type, identity_value, last_conversation_id, display_name)
    VALUES (v_subject, rec.bot_id, v_id_type, rec.platform_user_id, rec.conversation_id, rec.display_name)
    ON CONFLICT DO NOTHING;

    UPDATE user_memories
    SET subject_id = v_subject
    WHERE bot_id = rec.bot_id AND platform_user_id = rec.platform_user_id
      AND conversation_id IS NOT DISTINCT FROM rec.conversation_id
      AND subject_id IS NULL;
  END LOOP;
END;
$$;
