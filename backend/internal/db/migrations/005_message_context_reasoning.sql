ALTER TABLE model_profiles
  ADD COLUMN IF NOT EXISTS reasoning_effort text NOT NULL DEFAULT 'default';

ALTER TABLE agent_runs
  ADD COLUMN IF NOT EXISTS context_messages jsonb NOT NULL DEFAULT '[]';
