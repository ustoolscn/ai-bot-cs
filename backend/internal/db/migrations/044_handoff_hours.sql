ALTER TABLE bot_services ADD COLUMN IF NOT EXISTS handoff_hours jsonb NOT NULL DEFAULT '{"enabled":false,"start":"09:00","end":"18:00"}';
