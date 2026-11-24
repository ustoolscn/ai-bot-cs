package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/newapi"
	"github.com/gin-gonic/gin"
)

const liveSourceNewAPI = "newapi"

func liveSource(s agent.Settings) string {
	return liveSourceNewAPI + ":" + hashText(s.LiveData.BaseURL+"|"+s.LiveData.UserID+"|"+s.LiveData.AccessTokenEnc+"|")
}

// Read the snapshot for this site and credential only.
func (a *App) liveSnapshot(ctx context.Context, s agent.Settings) (newapi.Snapshot, bool, bool) {
	if !s.LiveData.Enabled {
		return newapi.Snapshot{}, false, false
	}
	var raw []byte
	var lastErr string
	var snap newapi.Snapshot
	if err := a.db.QueryRow(ctx, "SELECT data,last_error FROM live_data_snapshots WHERE source=$1", liveSource(s)).Scan(&raw, &lastErr); err == nil {
		_ = json.Unmarshal(raw, &snap)
	}
	return snap, snapshotStale(snap, lastErr), !snap.FetchedAt.IsZero()
}

func (a *App) liveClient(s agent.Settings) (*newapi.Client, error) {
	token := ""
	if s.LiveData.AccessTokenEnc != "" {
		t, err := a.cipher.Decrypt(s.LiveData.AccessTokenEnc)
		if err != nil {
			return nil, fmt.Errorf("解密访问令牌失败: %w", err)
		}
		token = t
	}
	return newapi.NewClient(s.LiveData.BaseURL, token, s.LiveData.UserID), nil
}

// A turn shares one reading between its status tools and image renderer.
func (a *App) turnStatus(ctx context.Context, rt *turnRuntime) (*newapi.Snapshot, error) {
	if rt.liveStatus != nil {
		return rt.liveStatus, nil
	}
	if !rt.settings.LiveData.Enabled || rt.settings.LiveData.BaseURL == "" {
		return nil, fmt.Errorf("网站实时状态查询未配置")
	}
	client, err := a.liveClient(rt.settings)
	if err != nil {
		return nil, err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status := client.FetchStatus(fetchCtx)
	status.SystemName = rt.snapshot.SystemName
	rt.liveStatus = &status
	return rt.liveStatus, nil
}

func (a *App) refreshLiveData(ctx context.Context, s agent.Settings) error {
	client, err := a.liveClient(s)
	if err != nil {
		return err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snap, err := client.Fetch(fetchCtx)
	if err != nil {
		_, _ = a.db.Exec(ctx, `INSERT INTO live_data_snapshots(source,last_attempt_at,last_error) VALUES($1,now(),$2)
			ON CONFLICT(source) DO UPDATE SET last_attempt_at=now(),last_error=EXCLUDED.last_error`, liveSource(s), err.Error())
		return err
	}
	prev, _, hadPrev := a.liveSnapshot(ctx, s)
	data, _ := json.Marshal(snap)
	partialErr := ""
	if len(snap.Errors) > 0 {
		var parts []string
		for k, v := range snap.Errors {
			parts = append(parts, k+": "+v)
		}
		partialErr = "部分数据获取失败：" + strings.Join(parts, "；")
	}
	if _, err := a.db.Exec(ctx, `INSERT INTO live_data_snapshots(source,data,content_hash,fetched_at,last_attempt_at,last_error) VALUES($1,$2::jsonb,$3,now(),now(),$4)
		ON CONFLICT(source) DO UPDATE SET data=EXCLUDED.data,content_hash=EXCLUDED.content_hash,fetched_at=now(),last_attempt_at=now(),last_error=EXCLUDED.last_error`,
		liveSource(s), string(data), hashText(string(data)), partialErr); err != nil {
		return err
	}
	if hadPrev {
		a.raiseMonitorAlerts(ctx, prev, snap)
	}
	return nil
}

func (a *App) raiseMonitorAlerts(ctx context.Context, prev, cur newapi.Snapshot) {
	was := map[string]int{}
	for _, m := range prev.Monitors {
		was[m.Category+"/"+m.Name] = m.Status
	}
	for _, m := range cur.Monitors {
		key := m.Category + "/" + m.Name
		old, seen := was[key]
		if m.Status == 0 && (!seen || old != 0) {
			detail, _ := json.Marshal(m)
			_, _ = a.db.Exec(ctx, "INSERT INTO system_alerts(kind,title,detail) VALUES('live_status',$1,$2::jsonb)", fmt.Sprintf("%s %s 状态异常", m.Category, m.Name), string(detail))
		}
		if m.Status == 1 && seen && old == 0 {
			detail, _ := json.Marshal(m)
			_, _ = a.db.Exec(ctx, "INSERT INTO system_alerts(kind,title,detail) VALUES('live_status',$1,$2::jsonb)", fmt.Sprintf("%s %s 已恢复", m.Category, m.Name), string(detail))
		}
	}
}

func snapshotStale(snap newapi.Snapshot, lastError string) bool {
	return lastError != "" || (!snap.FetchedAt.IsZero() && time.Since(snap.FetchedAt) >= 24*time.Hour)
}
func hashText(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func (a *App) getLiveData(c *gin.Context) {
	s, okSettings := a.liveRequestSettings(c)
	if !okSettings {
		return
	}
	var raw []byte
	var fetchedAt, attemptAt *time.Time
	var lastErr string
	_ = a.db.QueryRow(c, "SELECT data,fetched_at,last_attempt_at,last_error FROM live_data_snapshots WHERE source=$1", liveSource(s)).Scan(&raw, &fetchedAt, &attemptAt, &lastErr)
	var snap newapi.Snapshot
	_ = json.Unmarshal(raw, &snap)
	stale := snapshotStale(snap, lastErr)
	serviceStatus := map[string]any{}
	for g, w := range snap.ServiceStatus {
		if w != nil && w.Overall != nil {
			serviceStatus[g] = gin.H{"generatedAt": time.Unix(w.GeneratedAt, 0).UTC(), "overall": w.Overall.ServiceMetrics}
		}
	}
	ok(c, gin.H{
		"serviceStatus": serviceStatus, "possiblyStale": stale,
		"enabled":       s.LiveData.Enabled,
		"fetchedAt":     fetchedAt,
		"lastAttemptAt": attemptAt,
		"lastError":     lastErr,
		"systemName":    snap.SystemName,
		"notice":        snap.Notice,
		"announcements": snap.Announcements,
		"monitors":      snap.Monitors,
		"groups":        snap.GroupList(),
		"modelCount":    len(snap.Models),
		"models":        firstModels(snap, 300),
		"promptPreview": snap.PromptSummary(stale),
	})
}

func firstModels(s newapi.Snapshot, n int) []newapi.PriceQuote {
	var out []newapi.PriceQuote
	for i, m := range s.Models {
		if i >= n {
			break
		}
		out = append(out, s.Quote(m, "default"))
	}
	return out
}

func (a *App) refreshLiveDataNow(c *gin.Context) {
	s, okSettings := a.liveRequestSettings(c)
	if !okSettings {
		return
	}
	if !s.LiveData.Enabled || s.LiveData.BaseURL == "" {
		fail(c, 400, "live_data_disabled", "请先启用并填写网站地址")
		return
	}
	if err := a.refreshLiveData(c, s); err != nil {
		fail(c, 502, "live_data_error", err.Error())
		return
	}
	a.getLiveData(c)
}

func (a *App) listAlerts(c *gin.Context) {
	queryList(c, a, `SELECT id::text,kind,title,detail,acknowledged,created_at AS "createdAt" FROM system_alerts ORDER BY created_at DESC LIMIT 50`)
}

func (a *App) ackAlerts(c *gin.Context) {
	_, _ = a.db.Exec(c, "UPDATE system_alerts SET acknowledged=true WHERE acknowledged=false")
	ok(c, gin.H{"acknowledged": true})
}

func (a *App) liveRequestSettings(c *gin.Context) (agent.Settings, bool) {
	s := a.agentSettings(c)
	if id := c.Param("item"); id != "" {
		var raw []byte
		if err := a.db.QueryRow(c, "SELECT settings FROM bot_services WHERE bot_id=$1 AND id=$2", c.Param("id"), id).Scan(&raw); err != nil || json.Unmarshal(raw, &s) != nil {
			fail(c, 404, "not_found", "客服不存在")
			return s, false
		}
	}
	s.Normalize()
	return s, true
}
