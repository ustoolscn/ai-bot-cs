ALTER TABLE bots
  ADD COLUMN IF NOT EXISTS default_chat_profile_id uuid
  REFERENCES model_profiles(id) ON DELETE SET NULL;
