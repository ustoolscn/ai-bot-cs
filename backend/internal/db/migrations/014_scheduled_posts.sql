CREATE TABLE IF NOT EXISTS scheduled_posts (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  conversation_id uuid NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
  service_id uuid REFERENCES bot_services(id),
  send_time time NOT NULL,
  instruction text NOT NULL,
  format text NOT NULL DEFAULT 'text' CHECK(format IN ('text','image')),
  source text NOT NULL CHECK(source IN ('web','group','instruction')),
  enabled boolean NOT NULL DEFAULT true,
  updated_at timestamptz NOT NULL DEFAULT now(),
  last_run_date date,
  last_job_id uuid,
  last_message_id uuid REFERENCES messages(id) ON DELETE SET NULL
);
CREATE INDEX IF NOT EXISTS scheduled_posts_group ON scheduled_posts(conversation_id);

-- Move previously enabled reports once, retaining their last run date.
WITH legacy AS (
  UPDATE conversations SET daily_report=false WHERE type='group' AND daily_report
  RETURNING id,last_report_date
)
INSERT INTO scheduled_posts(conversation_id,send_time,instruction,source,last_run_date)
SELECT id,'08:00','根据昨天的群消息数、发言人数和客服答复数，整理一份简短群日报。','group',last_report_date FROM legacy;
