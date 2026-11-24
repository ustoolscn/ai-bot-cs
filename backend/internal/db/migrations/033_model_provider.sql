ALTER TABLE model_profiles
  ADD COLUMN IF NOT EXISTS provider text NOT NULL DEFAULT 'compatible'
  CHECK (provider IN ('compatible', 'deepseek', 'zhipu'));
