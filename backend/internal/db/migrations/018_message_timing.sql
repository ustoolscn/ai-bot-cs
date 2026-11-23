CREATE TABLE IF NOT EXISTS message_stages (
 id bigserial PRIMARY KEY,
 message_id uuid NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
 label text NOT NULL, started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 finished_at timestamptz, status text NOT NULL DEFAULT 'processing', detail text NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS message_stages_message ON message_stages(message_id,id);
CREATE OR REPLACE TRIGGER admin_change AFTER INSERT OR UPDATE ON message_stages
 FOR EACH ROW EXECUTE FUNCTION notify_admin_change();

-- Cumulative times share the inbound message's database timestamp, including waits and retries.
CREATE OR REPLACE FUNCTION message_timeline(msg messages) RETURNS jsonb LANGUAGE sql STABLE AS $$
 WITH spans AS (SELECT * FROM message_stages WHERE message_id=msg.id),
 task AS (SELECT * FROM inbox_tasks WHERE message_id=msg.id),
 deliveries AS (
   SELECT o.* FROM outbox_tasks o JOIN messages reply ON reply.id=o.message_id
   WHERE reply.conversation_id=msg.conversation_id AND reply.reply_to_message_id=msg.platform_message_id
   AND reply.direction='outbound' AND reply.event_type='AI_RESPONSE'
 ), stages(label,started,ended,status,detail,sort_key,measured_ms) AS (
 SELECT '接收',msg.created_at,msg.created_at,'success','后台收到消息，开始计时',0,NULL::bigint
 UNION ALL
 SELECT '等待处理',t.created_at,coalesce((SELECT min(started_at) FROM spans WHERE label='接单检查'),t.locked_at),
 CASE WHEN t.locked_at IS NULL AND t.status='pending' THEN 'pending' ELSE 'success' END,'进入客服处理队列',1,NULL FROM task t
 UNION ALL
 SELECT label,started_at,finished_at,status,detail,2,NULL FROM spans
 UNION ALL
 SELECT l.label,r.started_at,NULL,'success','历史记录仅保存本步耗时',3,l.ms FROM agent_runs r
 CROSS JOIN LATERAL (VALUES ('上下文构建',r.context_latency_ms),('知识库检索',r.retrieval_latency_ms)) l(label,ms)
 WHERE r.message_id=msg.id AND l.ms>0 AND NOT EXISTS(SELECT FROM spans)
 UNION ALL
 SELECT CASE mc.kind WHEN 'rewrite' THEN '问题改写' ELSE '模型生成' END,
 mc.created_at-mc.latency_ms*interval '1 millisecond',mc.created_at,
 CASE WHEN coalesce(mc.error,'')='' THEN 'success' ELSE 'failed' END,coalesce(mc.error,''),4,mc.latency_ms
 FROM model_calls mc JOIN agent_runs r ON r.id=mc.agent_run_id WHERE r.message_id=msg.id
 AND mc.kind IN ('chat','rewrite') AND NOT EXISTS(SELECT FROM spans)
 UNION ALL
 SELECT '等待投递 · 第 '||msg_seq||' 段',created_at,locked_at,CASE WHEN locked_at IS NULL THEN 'pending' ELSE 'success' END,'包含分段间隔和重试等待',5,NULL FROM deliveries
 UNION ALL
 SELECT '投递第 '||msg_seq||' 段',coalesce(locked_at,created_at),
 CASE WHEN status IN ('sent','failed','expired') THEN updated_at END,
 CASE status WHEN 'sent' THEN 'success' WHEN 'expired' THEN 'failed' ELSE status END,
 coalesce(last_error,''),5,NULL FROM deliveries
 UNION ALL
 SELECT '处理结束',updated_at,updated_at,
 CASE status WHEN 'failed' THEN 'failed' ELSE 'skipped' END,
 CASE status WHEN 'skipped' THEN '跳过回复' WHEN 'handoff' THEN '已转人工' ELSE coalesce(last_error,'') END,6,NULL
 FROM task WHERE status IN ('skipped','handoff','failed') AND NOT EXISTS(SELECT FROM deliveries)
 ), result AS (
 SELECT jsonb_build_object('label',label,'status',status,'detail',detail,
 'ms',coalesce(measured_ms,greatest(0,extract(epoch FROM ended-started)*1000)::bigint)) ||
 CASE WHEN ended IS NULL THEN jsonb_build_object('elapsedMs',NULL,'ms',measured_ms)
 ELSE jsonb_build_object('elapsedMs',greatest(0,extract(epoch FROM ended-msg.created_at)*1000)::bigint) END AS stage,
 started,sort_key,ended FROM stages
 )
 SELECT jsonb_build_object('stages',(SELECT jsonb_agg(stage ORDER BY CASE WHEN ended IS NULL AND stage->>'status' IN ('pending','processing') THEN 'infinity'::timestamptz ELSE coalesce(ended,started) END,sort_key,started) FROM result),
 'totalMs',CASE WHEN EXISTS(SELECT FROM deliveries) AND NOT EXISTS(SELECT FROM deliveries WHERE status NOT IN ('sent','expired','failed'))
 THEN (SELECT greatest(0,extract(epoch FROM max(updated_at)-msg.created_at)*1000)::bigint FROM deliveries)
 WHEN NOT EXISTS(SELECT FROM deliveries) THEN coalesce((SELECT greatest(0,extract(epoch FROM updated_at-msg.created_at)*1000)::bigint FROM task WHERE status IN ('skipped','handoff','failed')), (SELECT greatest(0,extract(epoch FROM max(finished_at)-msg.created_at)*1000)::bigint FROM spans WHERE label='群规则审核' AND EXISTS(SELECT FROM group_reviews r WHERE r.message_id=msg.id AND (r.status='violation' OR (r.status='allowed' AND NOT r.queue_reply))))) END)
$$;
