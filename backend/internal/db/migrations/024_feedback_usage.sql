ALTER TABLE message_feedback ADD COLUMN IF NOT EXISTS is_read boolean NOT NULL DEFAULT false;
ALTER TABLE model_profiles
  ADD COLUMN IF NOT EXISTS input_price double precision CHECK (input_price >= 0),
  ADD COLUMN IF NOT EXISTS output_price double precision CHECK (output_price >= 0),
  ADD COLUMN IF NOT EXISTS cache_read_price double precision CHECK (cache_read_price >= 0),
  ADD COLUMN IF NOT EXISTS cache_write_price double precision CHECK (cache_write_price >= 0);
ALTER TABLE model_calls
  ADD COLUMN IF NOT EXISTS message_id uuid REFERENCES messages(id) ON DELETE CASCADE,
  ADD COLUMN IF NOT EXISTS cached_input_tokens integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS cache_write_tokens integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS estimated_cost double precision;

UPDATE model_calls mc SET message_id=r.message_id FROM agent_runs r
WHERE mc.agent_run_id=r.id AND mc.message_id IS NULL;
CREATE INDEX IF NOT EXISTS model_calls_message ON model_calls(message_id);

UPDATE skills SET instructions='报价或推荐模型前查询候选模型的实际分组价格，按用户的分组、输入输出用量比较。分组未指定时说明报价适用的分组。'
WHERE name='价格咨询' AND instructions='价格必须先调用 get_model_price 查询，绝不凭记忆报价。回答给出用户问的那个模型/分组的单价即可，注明“以官网实时价格为准”。用户没说分组时先报默认分组价格，再一句话提示其他分组倍率不同。';
