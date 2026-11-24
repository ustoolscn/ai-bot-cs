DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_constraint WHERE conrelid='model_profiles'::regclass AND conname='model_profiles_decision_provider') THEN
    ALTER TABLE model_profiles
      DROP CONSTRAINT model_profiles_kind_check,
      DROP CONSTRAINT model_profiles_provider_check,
      ADD CHECK (kind IN ('chat','embedding','image','decision')),
      ADD CHECK (provider IN ('compatible','deepseek','zhipu','typesafe')),
      ADD CONSTRAINT model_profiles_decision_provider CHECK ((kind='decision') = (provider='typesafe'));
  END IF;
END $$;
