CREATE OR REPLACE FUNCTION notify_admin_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
  PERFORM pg_notify('admin_change_' || md5(TG_TABLE_SCHEMA), TG_TABLE_NAME);
  RETURN NULL;
END;
$$;

DO $$
DECLARE
  resource text;
BEGIN
  FOREACH resource IN ARRAY ARRAY[
    'messages','conversations','bots','conversation_members','agent_runs','model_calls','tool_calls',
    'inbox_tasks','outbox_tasks','message_retractions','handoffs','system_alerts','blocked_users',
    'blacklist_actions','group_guard_actions','group_reviews','scheduled_posts','knowledge_bases',
    'knowledge_documents','conversation_knowledge_bases','knowledge_entries','knowledge_candidates',
    'knowledge_gaps','message_feedback','user_memories','eval_cases','eval_runs','eval_results',
    'model_profiles','bot_services','support_agents','personality_presets','live_data_snapshots',
    'skills','tool_definitions','conversation_staff_styles','conversation_summaries','agent_jobs','system_settings'
  ] LOOP
    IF NOT EXISTS (SELECT FROM pg_trigger WHERE tgrelid = resource::regclass AND tgname = 'admin_change') THEN
      EXECUTE format('CREATE TRIGGER admin_change AFTER INSERT OR UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION notify_admin_change()',resource);
    END IF;
  END LOOP;
END;
$$;
