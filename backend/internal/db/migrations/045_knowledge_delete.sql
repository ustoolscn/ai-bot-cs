CREATE TABLE IF NOT EXISTS deleted_knowledge_files (
  knowledge_base_id uuid NOT NULL,
  storage_key text NOT NULL,
  PRIMARY KEY (knowledge_base_id, storage_key)
);

CREATE OR REPLACE FUNCTION detach_knowledge_base(settings jsonb, kb text)
RETURNS jsonb LANGUAGE sql IMMUTABLE AS $$
 SELECT settings || CASE WHEN settings->'knowledgeBaseIds' ? kb
   THEN jsonb_build_object('knowledgeBaseIds',(settings->'knowledgeBaseIds') - kb) ELSE '{}'::jsonb END
   || CASE WHEN settings->>'learningKnowledgeBaseId'=kb
   THEN '{"learningKnowledgeBaseId":""}'::jsonb ELSE '{}'::jsonb END
$$;
