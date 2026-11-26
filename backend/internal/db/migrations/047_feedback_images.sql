ALTER TABLE knowledge_candidates ADD COLUMN IF NOT EXISTS image_mime text, ADD COLUMN IF NOT EXISTS image_data bytea;
ALTER TABLE knowledge_entries ADD COLUMN IF NOT EXISTS image_mime text, ADD COLUMN IF NOT EXISTS image_data bytea;
