-- Keep the conversation and its bot when the assigned service is removed.
DO $$ BEGIN
 IF EXISTS (SELECT FROM pg_constraint WHERE conrelid='conversations'::regclass
            AND conname='conversation_bot_service' AND confdeltype='a') THEN
  ALTER TABLE conversations DROP CONSTRAINT conversation_bot_service,
    ADD CONSTRAINT conversation_bot_service FOREIGN KEY(bot_id,service_id)
      REFERENCES bot_services(bot_id,id) ON DELETE SET NULL (service_id);
 END IF;
END $$;
