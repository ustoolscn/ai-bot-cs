package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
)

const runtimeSettingsKey = "runtime_defaults"

type RuntimeSettings struct {
	DefaultContextLimit     int        `json:"defaultContextLimit"`
	AIRequestTimeoutSeconds int        `json:"aiRequestTimeoutSeconds"`
	MessageRetentionDays    int        `json:"messageRetentionDays"`
	UpdatedAt               *time.Time `json:"updatedAt,omitempty"`
}

func (a *App) seededRuntimeSettings() RuntimeSettings {
	contextLimit := a.cfg.DefaultContextLimit
	if contextLimit == 0 {
		contextLimit = 20
	}
	timeout := int(a.cfg.AIRequestTimeout / time.Second)
	if timeout == 0 {
		timeout = 90
	}
	retention := a.cfg.MessageRetentionDays
	if retention == 0 {
		retention = 90
	}
	now := time.Now().UTC()
	return RuntimeSettings{DefaultContextLimit: contextLimit, AIRequestTimeoutSeconds: timeout, MessageRetentionDays: retention, UpdatedAt: &now}
}
func validateRuntimeSettings(s RuntimeSettings) error {
	if s.DefaultContextLimit < 1 || s.DefaultContextLimit > 100 {
		return fmt.Errorf("默认上下文条数必须在 1 到 100 之间")
	}
	if s.AIRequestTimeoutSeconds < 10 || s.AIRequestTimeoutSeconds > 600 {
		return fmt.Errorf("AI 请求超时必须在 10 到 600 秒之间")
	}
	if s.MessageRetentionDays < 1 || s.MessageRetentionDays > 3650 {
		return fmt.Errorf("消息保留天数必须在 1 到 3650 天之间")
	}
	return nil
}
func (a *App) ensureRuntimeSettings(ctx context.Context) error {
	s := a.seededRuntimeSettings()
	if err := validateRuntimeSettings(s); err != nil {
		return fmt.Errorf("环境变量中的运行时默认设置无效: %w", err)
	}
	value, _ := json.Marshal(s)
	_, err := a.db.Exec(ctx, "INSERT INTO system_settings(key,value) VALUES($1,$2::jsonb) ON CONFLICT(key) DO NOTHING", runtimeSettingsKey, string(value))
	return err
}
func (a *App) runtimeSettings(ctx context.Context) (RuntimeSettings, error) {
	var raw []byte
	var s RuntimeSettings
	if err := a.db.QueryRow(ctx, "SELECT value FROM system_settings WHERE key=$1", runtimeSettingsKey).Scan(&raw); err != nil {
		return s, err
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, fmt.Errorf("解析数据库运行时设置失败: %w", err)
	}
	if err := validateRuntimeSettings(s); err != nil {
		return s, fmt.Errorf("数据库运行时设置无效: %w", err)
	}
	return s, nil
}
func (a *App) getSystemSettings(c *gin.Context) {
	s, err := a.runtimeSettings(c)
	if err != nil {
		fail(c, 500, "settings_error", err.Error())
		return
	}
	ok(c, s)
}
func (a *App) putSystemSettings(c *gin.Context) {
	var s RuntimeSettings
	if c.ShouldBindJSON(&s) != nil {
		fail(c, 400, "invalid_request", "设置参数格式无效")
		return
	}
	if err := validateRuntimeSettings(s); err != nil {
		fail(c, 400, "invalid_settings", err.Error())
		return
	}
	now := time.Now().UTC()
	s.UpdatedAt = &now
	value, _ := json.Marshal(s)
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "更新设置失败")
		return
	}
	defer tx.Rollback(c)
	if _, err = tx.Exec(c, "INSERT INTO system_settings(key,value,updated_at) VALUES($1,$2::jsonb,now()) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=now()", runtimeSettingsKey, string(value)); err == nil {
		detail, _ := json.Marshal(s)
		_, err = tx.Exec(c, "INSERT INTO audit_logs(user_id,action,target_type,target_id,detail) VALUES($1,'system.settings.update','system_settings',$2,$3::jsonb)", c.GetString("userId"), runtimeSettingsKey, string(detail))
	}
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "更新设置失败")
		return
	}
	ok(c, s)
}
