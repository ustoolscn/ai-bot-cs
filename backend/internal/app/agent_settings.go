package app

import (
	"context"
	"encoding/json"
	"strings"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/openai"
	"github.com/gin-gonic/gin"
)

const agentSettingsKey = "agent_settings"

// agentSettings loads the stored agent settings merged over defaults, so keys
// added in later versions get their default values.
func (a *App) agentSettings(ctx context.Context) agent.Settings {
	s := agent.DefaultSettings()
	var raw []byte
	if err := a.db.QueryRow(ctx, "SELECT value FROM system_settings WHERE key=$1", agentSettingsKey).Scan(&raw); err == nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, &s); err != nil {
			a.log.Warn("invalid agent settings, using defaults", "error", err)
			s = agent.DefaultSettings()
		}
	}
	s.Normalize()
	s.LiveData.HasAccessToken = s.LiveData.AccessTokenEnc != ""
	return s
}

func publicAgentSettings(s agent.Settings) agent.Settings {
	s.LiveData.AccessTokenEnc = ""
	return s
}

func (a *App) getAgentSettings(c *gin.Context) {
	ok(c, publicAgentSettings(a.agentSettings(c)))
}

func (a *App) putAgentSettings(c *gin.Context) {
	var in struct {
		agent.Settings
		LiveDataAccessToken *string `json:"liveDataAccessToken"`
	}
	current := a.agentSettings(c)
	in.Settings = current
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "设置参数格式无效")
		return
	}
	s := in.Settings
	s.LiveData.AccessTokenEnc = current.LiveData.AccessTokenEnc
	if strings.TrimRight(strings.TrimSpace(s.LiveData.BaseURL), "/") != current.LiveData.BaseURL {
		s.LiveData.AccessTokenEnc = ""
	}
	if in.LiveDataAccessToken != nil {
		token := strings.TrimSpace(*in.LiveDataAccessToken)
		if token == "" {
			s.LiveData.AccessTokenEnc = ""
		} else {
			enc, err := a.cipher.Encrypt(token)
			if err != nil {
				fail(c, 500, "secret_error", "访问令牌加密失败")
				return
			}
			s.LiveData.AccessTokenEnc = enc
		}
	}
	s.Normalize()
	if err := s.Validate(); err != nil {
		fail(c, 400, "invalid_settings", err.Error())
		return
	}
	value, _ := json.Marshal(s)
	if _, err := a.db.Exec(c, "INSERT INTO system_settings(key,value,updated_at) VALUES($1,$2::jsonb,now()) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=now()", agentSettingsKey, string(value)); err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	a.audit(c, "agent.settings.update", "system_settings", agentSettingsKey, map[string]any{"liveDataEnabled": s.LiveData.Enabled})

	s.LiveData.HasAccessToken = s.LiveData.AccessTokenEnc != ""
	ok(c, publicAgentSettings(s))
}

func (a *App) audit(c *gin.Context, action, targetType, targetID string, detail any) {
	b, _ := json.Marshal(detail)
	var user any
	if id := c.GetString("userId"); id != "" {
		user = id
	}
	_, _ = a.db.Exec(c, "INSERT INTO audit_logs(user_id,action,target_type,target_id,detail) VALUES($1,$2,$3,$4,$5::jsonb)", user, action, targetType, targetID, string(b))
}

func (a *App) defaultChatProfile(ctx context.Context) string {
	var id string
	_ = a.db.QueryRow(ctx, "SELECT id::text FROM model_profiles WHERE kind='chat' AND enabled ORDER BY is_default DESC,created_at LIMIT 1").Scan(&id)
	return id
}

// auxModel returns the model used for background work (rewrite, summary,
// evaluation, mining). Falls back to the given chat profile.
func (a *App) auxModel(ctx context.Context, s agent.Settings, fallbackProfile string) (*openai.Client, string, error) {
	id := s.AuxChatProfileID
	if id == "" {
		id = fallbackProfile
	}
	if id == "" {
		id = a.defaultChatProfile(ctx)
	}
	cl, kind, err := a.loadModel(ctx, id)
	if err == nil && kind != "chat" {
		cl, _, err = a.loadModel(ctx, a.defaultChatProfile(ctx))
	}
	return cl, id, err
}

// embeddingModel returns the profile used for memories and gap clustering.
func (a *App) embeddingModel(ctx context.Context, s agent.Settings) (*openai.Client, string, bool) {
	id := s.EmbeddingProfileID
	if id == "" {
		_ = a.db.QueryRow(ctx, "SELECT id::text FROM model_profiles WHERE kind='embedding' AND enabled ORDER BY is_default DESC,created_at LIMIT 1").Scan(&id)
	}
	if id == "" {
		return nil, "", false
	}
	cl, kind, err := a.loadModel(ctx, id)
	if err != nil || kind != "embedding" {
		return nil, "", false
	}
	return cl, id, true
}
