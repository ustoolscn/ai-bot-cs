ALTER TABLE knowledge_entries ADD COLUMN IF NOT EXISTS material_meta jsonb NOT NULL DEFAULT '[]', ADD COLUMN IF NOT EXISTS material_data bytea[] NOT NULL DEFAULT '{}';
ALTER TABLE knowledge_candidates ADD COLUMN IF NOT EXISTS material_meta jsonb NOT NULL DEFAULT '[]', ADD COLUMN IF NOT EXISTS material_data bytea[] NOT NULL DEFAULT '{}';

-- Preserve single-image writers and archives during the transition.
CREATE OR REPLACE FUNCTION sync_reply_materials() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF (TG_OP='UPDATE' AND (OLD.material_meta,OLD.material_data) IS DISTINCT FROM (NEW.material_meta,NEW.material_data))
       OR (TG_OP='INSERT' AND jsonb_array_length(NEW.material_meta)>0) THEN
        NEW.image_mime := CASE WHEN NEW.material_meta->0->>'kind'='image' THEN NEW.material_meta->0->>'mime' END;
        NEW.image_data := CASE WHEN NEW.image_mime IS NOT NULL THEN NEW.material_data[1] END;
    ELSIF TG_OP='INSERT' OR (NEW.image_data,NEW.image_mime) IS DISTINCT FROM (OLD.image_data,OLD.image_mime)
          OR (NEW.image_data IS NOT NULL AND NEW.material_meta='[]') THEN
        IF NEW.image_data IS NULL THEN
            NEW.material_meta := '[]';
            NEW.material_data := '{}';
        ELSE
            NEW.material_meta := jsonb_build_array(jsonb_build_object('id',gen_random_uuid(),'kind','image','mime',NEW.image_mime,'filename',CASE NEW.image_mime WHEN 'image/png' THEN '演示.png' ELSE '演示.jpg' END,'sizeBytes',octet_length(NEW.image_data)));
            NEW.material_data := ARRAY[NEW.image_data];
        END IF;
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS sync_materials ON knowledge_entries;
CREATE TRIGGER sync_materials BEFORE INSERT OR UPDATE OF material_meta,material_data,image_data,image_mime ON knowledge_entries FOR EACH ROW EXECUTE FUNCTION sync_reply_materials();
DROP TRIGGER IF EXISTS sync_materials ON knowledge_candidates;
CREATE TRIGGER sync_materials BEFORE INSERT OR UPDATE OF material_meta,material_data,image_data,image_mime ON knowledge_candidates FOR EACH ROW EXECUTE FUNCTION sync_reply_materials();
UPDATE knowledge_entries SET image_data=image_data WHERE image_data IS NOT NULL AND material_meta='[]';
UPDATE knowledge_candidates SET image_data=image_data WHERE image_data IS NOT NULL AND material_meta='[]';
ALTER TABLE messages ADD COLUMN IF NOT EXISTS material_batch uuid, ADD COLUMN IF NOT EXISTS material_index int;
CREATE UNIQUE INDEX IF NOT EXISTS messages_material_batch_order ON messages(material_batch,material_index) WHERE material_batch IS NOT NULL;
