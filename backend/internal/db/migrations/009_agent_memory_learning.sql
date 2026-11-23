-- Agent: memory, skills, handoff, live data, learning flywheel, evaluation.

ALTER TABLE conversation_members
  ADD COLUMN IF NOT EXISTS is_staff boolean NOT NULL DEFAULT false;

ALTER TABLE agent_runs
  ADD COLUMN IF NOT EXISTS rewritten_query text NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS steps jsonb NOT NULL DEFAULT '[]',
  ADD COLUMN IF NOT EXISTS evaluation jsonb NOT NULL DEFAULT '{}';

CREATE TABLE IF NOT EXISTS conversation_summaries (
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  scope_key text NOT NULL DEFAULT '',
  summary text NOT NULL DEFAULT '',
  covered_until timestamptz NOT NULL DEFAULT 'epoch',
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(conversation_id, scope_key)
);

CREATE TABLE IF NOT EXISTS user_memories (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  platform_user_id text NOT NULL,
  display_name text NOT NULL DEFAULT '',
  kind text NOT NULL DEFAULT 'fact',
  content text NOT NULL,
  embedding vector,
  embedding_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL,
  confidence real NOT NULL DEFAULT 0.7,
  pinned boolean NOT NULL DEFAULT false,
  source text NOT NULL DEFAULT 'extracted',
  source_message_id uuid REFERENCES messages(id) ON DELETE SET NULL,
  status text NOT NULL DEFAULT 'active',
  expires_at timestamptz,
  last_used_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS user_memories_user_idx ON user_memories(bot_id, platform_user_id) WHERE status='active';

CREATE TABLE IF NOT EXISTS agent_jobs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind text NOT NULL,
  payload jsonb NOT NULL DEFAULT '{}',
  dedupe_key text,
  status text NOT NULL DEFAULT 'pending',
  attempts integer NOT NULL DEFAULT 0,
  next_attempt_at timestamptz NOT NULL DEFAULT now(),
  locked_at timestamptz,
  last_error text,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS agent_jobs_dedupe ON agent_jobs(dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('pending','processing');
CREATE INDEX IF NOT EXISTS agent_jobs_claim_idx ON agent_jobs(status, next_attempt_at);

CREATE TABLE IF NOT EXISTS skills (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL UNIQUE,
  description text NOT NULL DEFAULT '',
  instructions text NOT NULL DEFAULT '',
  tools jsonb NOT NULL DEFAULT '[]',
  enabled boolean NOT NULL DEFAULT true,
  sort_order integer NOT NULL DEFAULT 0,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE tool_definitions
  ADD COLUMN IF NOT EXISTS kind text NOT NULL DEFAULT 'http',
  ADD COLUMN IF NOT EXISTS parameters jsonb NOT NULL DEFAULT '{"type":"object","properties":{}}';

CREATE TABLE IF NOT EXISTS handoffs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  bot_id uuid NOT NULL REFERENCES bots(id) ON DELETE CASCADE,
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  platform_user_id text NOT NULL DEFAULT '',
  display_name text NOT NULL DEFAULT '',
  message_id uuid REFERENCES messages(id) ON DELETE SET NULL,
  reason text NOT NULL DEFAULT '',
  status text NOT NULL DEFAULT 'open',
  paused_until timestamptz,
  resolved_by uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  resolved_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS handoffs_open_idx ON handoffs(conversation_id, platform_user_id) WHERE status='open';

CREATE TABLE IF NOT EXISTS message_feedback (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  source text NOT NULL DEFAULT 'admin',
  rating text NOT NULL,
  comment text NOT NULL DEFAULT '',
  corrected_answer text NOT NULL DEFAULT '',
  created_by uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS message_feedback_message_idx ON message_feedback(message_id);
CREATE INDEX IF NOT EXISTS message_feedback_time_idx ON message_feedback(created_at DESC);

CREATE TABLE IF NOT EXISTS knowledge_entries (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  knowledge_base_id uuid NOT NULL REFERENCES knowledge_bases(id) ON DELETE CASCADE,
  question text NOT NULL,
  aliases jsonb NOT NULL DEFAULT '[]',
  answer text NOT NULL,
  source text NOT NULL DEFAULT 'manual',
  external_key text,
  status text NOT NULL DEFAULT 'published',
  index_status text NOT NULL DEFAULT 'pending',
  index_error text NOT NULL DEFAULT '',
  version integer NOT NULL DEFAULT 1,
  source_refs jsonb NOT NULL DEFAULT '[]',
  hit_count integer NOT NULL DEFAULT 0,
  helpful_count integer NOT NULL DEFAULT 0,
  unhelpful_count integer NOT NULL DEFAULT 0,
  last_hit_at timestamptz,
  created_by uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS knowledge_entries_external_key ON knowledge_entries(external_key) WHERE external_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS knowledge_entries_kb_idx ON knowledge_entries(knowledge_base_id, status);

ALTER TABLE knowledge_chunks ALTER COLUMN document_id DROP NOT NULL;
ALTER TABLE knowledge_chunks ADD COLUMN IF NOT EXISTS entry_id uuid REFERENCES knowledge_entries(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS knowledge_chunks_entry_idx ON knowledge_chunks(entry_id) WHERE entry_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS knowledge_candidates (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  knowledge_base_id uuid REFERENCES knowledge_bases(id) ON DELETE SET NULL,
  question text NOT NULL,
  aliases jsonb NOT NULL DEFAULT '[]',
  answer text NOT NULL,
  source text NOT NULL,
  evidence jsonb NOT NULL DEFAULT '[]',
  confidence real NOT NULL DEFAULT 0.5,
  status text NOT NULL DEFAULT 'pending',
  entry_id uuid REFERENCES knowledge_entries(id) ON DELETE SET NULL,
  review_note text NOT NULL DEFAULT '',
  reviewed_by uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  reviewed_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS knowledge_candidates_status_idx ON knowledge_candidates(status, created_at DESC);

CREATE TABLE IF NOT EXISTS knowledge_gaps (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  question text NOT NULL,
  embedding vector,
  embedding_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL,
  occurrences integer NOT NULL DEFAULT 1,
  sample_message_ids jsonb NOT NULL DEFAULT '[]',
  status text NOT NULL DEFAULT 'open',
  candidate_id uuid REFERENCES knowledge_candidates(id) ON DELETE SET NULL,
  first_seen_at timestamptz NOT NULL DEFAULT now(),
  last_seen_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS knowledge_gaps_status_idx ON knowledge_gaps(status, occurrences DESC);

CREATE TABLE IF NOT EXISTS live_data_snapshots (
  source text PRIMARY KEY,
  data jsonb NOT NULL DEFAULT '{}',
  content_hash text NOT NULL DEFAULT '',
  fetched_at timestamptz,
  last_attempt_at timestamptz,
  last_error text NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS system_alerts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  kind text NOT NULL,
  title text NOT NULL,
  detail jsonb NOT NULL DEFAULT '{}',
  acknowledged boolean NOT NULL DEFAULT false,
  created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS eval_cases (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  question text NOT NULL,
  expected text NOT NULL DEFAULT '',
  source text NOT NULL DEFAULT 'manual',
  entry_id uuid REFERENCES knowledge_entries(id) ON DELETE SET NULL,
  enabled boolean NOT NULL DEFAULT true,
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS eval_cases_entry_unique ON eval_cases(entry_id) WHERE entry_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS eval_runs (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name text NOT NULL DEFAULT '',
  chat_profile_id uuid REFERENCES model_profiles(id) ON DELETE SET NULL,
  knowledge_base_ids jsonb NOT NULL DEFAULT '[]',
  status text NOT NULL DEFAULT 'pending',
  total integer NOT NULL DEFAULT 0,
  completed integer NOT NULL DEFAULT 0,
  avg_score real,
  avg_length real,
  error text,
  created_by uuid REFERENCES admin_users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  finished_at timestamptz
);

CREATE TABLE IF NOT EXISTS eval_results (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  run_id uuid NOT NULL REFERENCES eval_runs(id) ON DELETE CASCADE,
  case_id uuid REFERENCES eval_cases(id) ON DELETE SET NULL,
  question text NOT NULL,
  expected text NOT NULL DEFAULT '',
  answer text NOT NULL DEFAULT '',
  score real,
  verdict text NOT NULL DEFAULT '',
  tools_used jsonb NOT NULL DEFAULT '[]',
  latency_ms integer NOT NULL DEFAULT 0,
  error text,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS eval_results_run_idx ON eval_results(run_id);

INSERT INTO skills(name, description, instructions, tools, sort_order) VALUES
 ('价格咨询', '用户询问模型价格、倍率、计费方式、哪个分组便宜时使用。',
  '价格必须先调用 get_model_price 查询，绝不凭记忆报价。回答给出用户问的那个模型/分组的单价即可，注明“以官网实时价格为准”。用户没说分组时先报默认分组价格，再一句话提示其他分组倍率不同。',
  '["get_model_price"]', 10),
 ('故障排查', '用户反馈报错、无法调用、超时、某个分组/模型用不了时使用。',
  '先调用 get_group_status 看对应分组是否异常；有异常直接告诉用户是平台侧问题和当前状态。没有异常时，一次只让用户提供一项关键信息（报错原文、使用的分组或模型、请求地址），逐步排查，不要一次列出所有可能原因。',
  '["get_group_status","get_announcements","search_knowledge"]', 20),
 ('转人工', '用户明确要求人工、涉及退款/充值到账/账号封禁等需要人工核实的问题、或你连续两次无法解决时使用。',
  '调用 handoff_to_human 并简短告诉用户已经通知人工客服，请稍等。不要承诺具体处理时间。',
  '["handoff_to_human"]', 30)
ON CONFLICT(name) DO NOTHING;
