DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_constraint WHERE conrelid='scheduled_posts'::regclass AND conname='scheduled_posts_delivery_format_check') THEN
    ALTER TABLE scheduled_posts DROP CONSTRAINT IF EXISTS scheduled_posts_format_check;
    ALTER TABLE scheduled_posts ADD CONSTRAINT scheduled_posts_delivery_format_check CHECK(format IN ('text','image','link'));
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS scheduled_web_pages (
  id uuid PRIMARY KEY,
  document text NOT NULL,
  expires_at timestamptz NOT NULL DEFAULT (now() + interval '168 hours')
);
CREATE INDEX IF NOT EXISTS scheduled_web_pages_expiry_idx ON scheduled_web_pages(expires_at);
