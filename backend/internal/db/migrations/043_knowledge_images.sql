DO $$ BEGIN
IF NOT EXISTS (SELECT FROM pg_constraint WHERE conrelid='model_profiles'::regclass AND conname='google_embedding_provider') THEN
ALTER TABLE model_profiles DROP CONSTRAINT model_profiles_provider_check,
  ADD CONSTRAINT model_profiles_provider_check CHECK (provider IN ('compatible','deepseek','zhipu','typesafe','google')),
  ADD CONSTRAINT google_embedding_provider CHECK (provider <> 'google' OR kind='embedding');
END IF;
END $$;
CREATE TABLE IF NOT EXISTS knowledge_chunk_images (
 chunk_id uuid PRIMARY KEY REFERENCES knowledge_chunks(id) ON DELETE CASCADE,
 mime_type text NOT NULL,
 data bytea NOT NULL
);
