ALTER TABLE model_profiles ADD COLUMN IF NOT EXISTS price_per_image double precision CHECK (price_per_image >= 0);
ALTER TABLE model_calls
  ADD COLUMN IF NOT EXISTS images integer NOT NULL DEFAULT 0 CHECK (images >= 0),
  ADD COLUMN IF NOT EXISTS conversation_id uuid REFERENCES conversations(id) ON DELETE SET NULL;
ALTER TABLE inbox_tasks ADD COLUMN IF NOT EXISTS dismissed_at timestamptz;
ALTER TABLE outbox_tasks ADD COLUMN IF NOT EXISTS dismissed_at timestamptz;
ALTER TABLE knowledge_documents ADD COLUMN IF NOT EXISTS dismissed_at timestamptz;

-- Keep one pricing formula for message details and the dashboard. Existing chat
-- calls retain their operation name (rewrite, evaluate, etc.) in mc.kind.
CREATE OR REPLACE VIEW priced_model_calls AS
SELECT mc.id, mc.agent_run_id, mc.message_id, mc.profile_id, mc.kind,
       mc.input_tokens, mc.output_tokens, mc.cached_input_tokens, mc.cache_write_tokens,
       mc.images, mc.created_at, mc.error, mc.latency_ms, mc.conversation_id,
       CASE WHEN mc.kind='image' THEN 'image' WHEN mc.kind IN ('embedding','embedding_index') THEN 'embedding' ELSE 'chat' END AS billing_kind,
       coalesce(mp.name,'已删除模型') AS profile_name,
       (greatest(mc.input_tokens-mc.cached_input_tokens-mc.cache_write_tokens,0)*coalesce(mp.input_price,0)
        +mc.output_tokens*coalesce(mp.output_price,0)+mc.cached_input_tokens*coalesce(mp.cache_read_price,0)
        +mc.cache_write_tokens*coalesce(mp.cache_write_price,0))/1000000
        +mc.images*coalesce(mp.price_per_image,0) AS cost,
       CASE WHEN mc.kind='image' THEN mp.price_per_image IS NULL
            ELSE (mp.input_price IS NULL AND (mc.input_tokens>mc.cached_input_tokens+mc.cache_write_tokens OR
                    num_nonnulls(mp.output_price,mp.cache_read_price,mp.cache_write_price)=0))
              OR (mc.output_tokens>0 AND mp.output_price IS NULL)
              OR (mc.cached_input_tokens>0 AND mp.cache_read_price IS NULL)
              OR (mc.cache_write_tokens>0 AND mp.cache_write_price IS NULL) END AS unpriced
FROM model_calls mc LEFT JOIN model_profiles mp ON mp.id=mc.profile_id;
