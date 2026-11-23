ALTER TABLE scheduled_posts ALTER COLUMN conversation_id DROP NOT NULL;
ALTER TABLE scheduled_posts ADD COLUMN IF NOT EXISTS name text NOT NULL DEFAULT '';
ALTER TABLE scheduled_posts ADD COLUMN IF NOT EXISTS all_groups boolean NOT NULL DEFAULT false;
ALTER TABLE scheduled_posts ADD COLUMN IF NOT EXISTS revision uuid NOT NULL DEFAULT gen_random_uuid();

CREATE TABLE IF NOT EXISTS schedule_targets (
 schedule_id uuid REFERENCES scheduled_posts ON DELETE CASCADE,
 conversation_id uuid REFERENCES conversations ON DELETE CASCADE,
 service_id uuid REFERENCES bot_services ON DELETE SET NULL,
 PRIMARY KEY(schedule_id,conversation_id)
);
CREATE TABLE IF NOT EXISTS schedule_runs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 schedule_id uuid NOT NULL REFERENCES scheduled_posts ON DELETE CASCADE,
 revision uuid NOT NULL, run_date date NOT NULL, deadline timestamptz NOT NULL,
 is_test boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS schedule_daily_once ON schedule_runs(schedule_id,run_date) WHERE NOT is_test;
CREATE INDEX IF NOT EXISTS schedule_runs_recent ON schedule_runs(schedule_id,created_at DESC);
CREATE TABLE IF NOT EXISTS schedule_deliveries (
 run_id uuid REFERENCES schedule_runs ON DELETE CASCADE,
 conversation_id uuid REFERENCES conversations ON DELETE CASCADE,
 job_id uuid NOT NULL, message_id uuid UNIQUE REFERENCES messages ON DELETE SET NULL,
 PRIMARY KEY(run_id,conversation_id)
);
CREATE INDEX IF NOT EXISTS schedule_delivery_job ON schedule_deliveries(job_id);
INSERT INTO schedule_targets SELECT id,conversation_id,service_id FROM scheduled_posts WHERE conversation_id IS NOT NULL ON CONFLICT DO NOTHING;
INSERT INTO schedule_runs(id,schedule_id,revision,run_date,deadline)
 SELECT last_job_id,id,revision,last_run_date,((last_run_date+send_time) AT TIME ZONE 'Asia/Shanghai')+interval '1 hour'
 FROM scheduled_posts WHERE conversation_id IS NOT NULL AND last_job_id IS NOT NULL AND last_run_date IS NOT NULL ON CONFLICT DO NOTHING;
INSERT INTO schedule_deliveries SELECT r.id,s.conversation_id,s.last_job_id,s.last_message_id FROM scheduled_posts s
 JOIN schedule_runs r ON r.id=s.last_job_id WHERE s.conversation_id IS NOT NULL ON CONFLICT DO NOTHING;
UPDATE scheduled_posts SET name=left(instruction,40),conversation_id=NULL,service_id=NULL WHERE conversation_id IS NOT NULL;

CREATE OR REPLACE VIEW schedule_recipients AS
 SELECT s.id AS schedule_id,c.id AS conversation_id,c.bot_id,t.service_id
 FROM scheduled_posts s JOIN conversations c ON c.type='group'
 JOIN bots b ON b.id=c.bot_id
 LEFT JOIN schedule_targets t ON t.schedule_id=s.id AND t.conversation_id=c.id
 WHERE (s.all_groups OR t.conversation_id IS NOT NULL) AND b.enabled AND c.enabled;

CREATE OR REPLACE TRIGGER admin_change AFTER INSERT OR UPDATE OR DELETE ON schedule_runs
 FOR EACH ROW EXECUTE FUNCTION notify_admin_change();
CREATE OR REPLACE TRIGGER admin_change AFTER INSERT OR UPDATE OR DELETE ON schedule_deliveries
 FOR EACH ROW EXECUTE FUNCTION notify_admin_change();
