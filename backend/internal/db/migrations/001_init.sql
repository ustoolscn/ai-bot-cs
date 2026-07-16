CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS admin_users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), username text NOT NULL UNIQUE,
  password_hash text NOT NULL, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS admin_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
  token_hash bytea NOT NULL UNIQUE, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS bots (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, channel text NOT NULL DEFAULT 'qq', app_id text NOT NULL,
  app_secret_enc text NOT NULL, enabled boolean NOT NULL DEFAULT true, status text NOT NULL DEFAULT 'unknown',
  last_event_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS model_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, kind text NOT NULL CHECK(kind IN ('chat','embedding')),
  base_url text NOT NULL, api_key_enc text NOT NULL, model text NOT NULL, dimension integer,
  enabled boolean NOT NULL DEFAULT true, is_default boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS model_one_default_per_kind ON model_profiles(kind) WHERE is_default;
CREATE TABLE IF NOT EXISTS conversations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), channel text NOT NULL, bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  platform_id text NOT NULL, type text NOT NULL, name text NOT NULL DEFAULT '', enabled boolean NOT NULL DEFAULT true,
  trigger_mode text NOT NULL DEFAULT 'mention_only', context_limit integer NOT NULL DEFAULT 20,
  system_prompt text NOT NULL DEFAULT '你是群聊中的智能助手。请基于上下文和知识库，准确、简洁地回答。',
  chat_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(channel, bot_id, platform_id)
);
CREATE TABLE IF NOT EXISTS messages (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), channel text NOT NULL, bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE, direction text NOT NULL,
  sender_id text NOT NULL DEFAULT '', sender_name text NOT NULL DEFAULT '', platform_message_id text,
  event_type text NOT NULL DEFAULT '', content text NOT NULL DEFAULT '', parts jsonb NOT NULL DEFAULT '[]',
  raw_event jsonb, reply_to_message_id text, status text NOT NULL DEFAULT 'received',
  event_at timestamptz NOT NULL DEFAULT now(), reply_deadline timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS messages_platform_unique ON messages(channel, bot_id, platform_message_id) WHERE platform_message_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS messages_conversation_time ON messages(conversation_id, event_at DESC);
CREATE TABLE IF NOT EXISTS webhook_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), channel text NOT NULL, bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  platform_event_id text NOT NULL, event_type text NOT NULL DEFAULT '', raw_event jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(channel, bot_id, platform_event_id)
);
CREATE TABLE IF NOT EXISTS inbox_tasks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), message_id uuid NOT NULL UNIQUE REFERENCES messages(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'pending', attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
  locked_at timestamptz, last_error text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS inbox_claim_idx ON inbox_tasks(status, next_attempt_at);
CREATE TABLE IF NOT EXISTS agent_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'running', retrieved_chunks jsonb NOT NULL DEFAULT '[]', error text,
  started_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz
);
CREATE TABLE IF NOT EXISTS model_calls (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), agent_run_id uuid REFERENCES agent_runs(id) ON DELETE CASCADE,
  profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL, kind text NOT NULL, input_tokens integer NOT NULL DEFAULT 0,
  output_tokens integer NOT NULL DEFAULT 0, latency_ms integer NOT NULL DEFAULT 0, error text, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS outbox_tasks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), message_id uuid NOT NULL UNIQUE REFERENCES messages(id) ON DELETE CASCADE,
  status text NOT NULL DEFAULT 'pending', attempts integer NOT NULL DEFAULT 0, msg_seq integer NOT NULL DEFAULT 1,
  next_attempt_at timestamptz NOT NULL DEFAULT now(), locked_at timestamptz, platform_message_id text, last_error text,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS outbox_claim_idx ON outbox_tasks(status, next_attempt_at);
CREATE TABLE IF NOT EXISTS knowledge_bases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL, description text NOT NULL DEFAULT '',
  embedding_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL, embedding_model text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS conversation_knowledge_bases (
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  knowledge_base_id uuid NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  PRIMARY KEY(conversation_id, knowledge_base_id)
);
CREATE TABLE IF NOT EXISTS knowledge_documents (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), knowledge_base_id uuid NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  name text NOT NULL, storage_key text NOT NULL, content_type text NOT NULL, size_bytes bigint NOT NULL,
  status text NOT NULL DEFAULT 'pending', attempts integer NOT NULL DEFAULT 0, next_attempt_at timestamptz NOT NULL DEFAULT now(),
  last_error text, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS document_claim_idx ON knowledge_documents(status, next_attempt_at);
CREATE TABLE IF NOT EXISTS knowledge_chunks (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), knowledge_base_id uuid NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  document_id uuid NOT NULL REFERENCES knowledge_documents(id) ON DELETE CASCADE, chunk_index integer NOT NULL,
  content text NOT NULL, embedding vector NOT NULL, metadata jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(document_id, chunk_index)
);
CREATE INDEX IF NOT EXISTS chunks_kb_idx ON knowledge_chunks(knowledge_base_id);
CREATE TABLE IF NOT EXISTS tool_calls (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), agent_run_id uuid REFERENCES agent_runs(id) ON DELETE CASCADE,
  tool_name text NOT NULL, arguments jsonb NOT NULL DEFAULT '{}', result jsonb, error text, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS tool_definitions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), name text NOT NULL UNIQUE, description text NOT NULL DEFAULT '',
  enabled boolean NOT NULL DEFAULT false, config_enc text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS blocked_users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  conversation_id uuid REFERENCES conversations(id) ON DELETE CASCADE, platform_user_id text NOT NULL,
  reason text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(bot_id,conversation_id,platform_user_id)
);
CREATE TABLE IF NOT EXISTS system_settings (
  key text PRIMARY KEY, value jsonb NOT NULL DEFAULT '{}', updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE IF NOT EXISTS audit_logs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(), user_id uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  action text NOT NULL, target_type text NOT NULL DEFAULT '', target_id text NOT NULL DEFAULT '', detail jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now()
);
