UPDATE model_profiles
SET web_search_mode='responses', updated_at=now()
WHERE kind='chat' AND web_search_mode='openai';
