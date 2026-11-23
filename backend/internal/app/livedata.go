package app

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/newapi"
	"github.com/gin-gonic/gin"
)

const liveSourceNewAPI = "newapi"

type liveCache struct {
	mu       sync.RWMutex
	loaded   bool
	snapshot newapi.Snapshot
	stale    bool
	lastErr  string
}

func (a *App) invalidateLiveCache() {
	a.live.mu.Lock()
	a.live.loaded = false
	a.live.mu.Unlock()
}

// liveSnapshot returns the cached snapshot, loading it from the database on
// first use. ok is false when live data is disabled or nothing was fetched yet.
func (a *App) liveSnapshot(ctx context.Context, s agent.Settings) (newapi.Snapshot, bool, bool) {
	if !s.LiveData.Enabled {
		return newapi.Snapshot{}, false, false
	}
	a.live.mu.RLock()
	if a.live.loaded {
		snap, stale := a.live.snapshot, a.live.stale
		a.live.mu.RUnlock()
		return snap, stale, !snap.FetchedAt.IsZero()
	}
	a.live.mu.RUnlock()
	var raw []byte
	var lastErr string
	var snap newapi.Snapshot
	if err := a.db.QueryRow(ctx, "SELECT data,last_error FROM live_data_snapshots WHERE source=$1", liveSourceNewAPI).Scan(&raw, &lastErr); err == nil {
		_ = json.Unmarshal(raw, &snap)
	}
	a.live.mu.Lock()
	a.live.snapshot, a.live.stale, a.live.lastErr, a.live.loaded = snap, lastErr != "", lastErr, true
	a.live.mu.Unlock()
	return snap, lastErr != "", !snap.FetchedAt.IsZero()
}

func (a *App) liveDataLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		s := a.agentSettings(ctx)
		if s.LiveData.Enabled && s.LiveData.BaseURL != "" {
			var last *time.Time
			_ = a.db.QueryRow(ctx, "SELECT last_attempt_at FROM live_data_snapshots WHERE source=$1", liveSourceNewAPI).Scan(&last)
			if last == nil || time.Since(*last) >= time.Duration(s.LiveData.RefreshSeconds)*time.Second {
				if err := a.refreshLiveData(ctx, s); err != nil && ctx.Err() == nil {
					a.log.Warn("live data refresh failed", "error", err)
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// refreshLiveData fetches the site, stores the snapshot, syncs announcements
// into the knowledge base and raises alerts for newly abnormal monitors.
func (a *App) refreshLiveData(ctx context.Context, s agent.Settings) error {
	token := ""
	if s.LiveData.AccessTokenEnc != "" {
		t, err := a.cipher.Decrypt(s.LiveData.AccessTokenEnc)
		if err != nil {
			return fmt.Errorf("解密访问令牌失败: %w", err)
		}
		token = t
	}
	fetchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	snap, err := newapi.NewClient(s.LiveData.BaseURL, token, s.LiveData.UserID).Fetch(fetchCtx)
	if err != nil {
		_, _ = a.db.Exec(ctx, `INSERT INTO live_data_snapshots(source,last_attempt_at,last_error) VALUES($1,now(),$2)
			ON CONFLICT(source) DO UPDATE SET last_attempt_at=now(),last_error=EXCLUDED.last_error`, liveSourceNewAPI, err.Error())
		a.live.mu.Lock()
		a.live.stale, a.live.lastErr = true, err.Error()
		a.live.mu.Unlock()
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
		liveSourceNewAPI, string(data), hashText(string(data)), partialErr); err != nil {
		return err
	}
	a.live.mu.Lock()
	a.live.snapshot, a.live.stale, a.live.lastErr, a.live.loaded = snap, false, partialErr, true
	a.live.mu.Unlock()
	if hadPrev {
		a.raiseMonitorAlerts(ctx, prev, snap)
	}
	if s.LiveData.SyncAnnouncements && s.LiveData.AnnouncementKBID != "" {
		if err := a.syncAnnouncements(ctx, s.LiveData.AnnouncementKBID, snap); err != nil {
			a.log.Warn("announcement sync failed", "error", err)
		}
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

// syncAnnouncements mirrors site announcements into FAQ entries. Entries for
// announcements no longer on the site are archived.
func (a *App) syncAnnouncements(ctx context.Context, kbID string, snap newapi.Snapshot) error {
	type ann struct{ key, question, answer string }
	var items []ann
	for _, an := range snap.Announcements {
		content := strings.TrimSpace(an.Content)
		if content == "" {
			continue
		}
		date := an.PublishDate
		if len(date) > 10 {
			date = date[:10]
		}
		items = append(items, ann{key: "newapi:announcement:" + hashText(an.PublishDate + content)[:16], question: "网站公告（" + date + "）：" + agent.TruncateRunes(firstLine(content), 40), answer: content})
	}
	if n := strings.TrimSpace(snap.Notice); n != "" {
		items = append(items, ann{key: "newapi:notice", question: "网站当前通知：" + agent.TruncateRunes(firstLine(n), 40), answer: n})
	}
	keys := make([]string, 0, len(items))
	for _, it := range items {
		keys = append(keys, it.key)
		var id, oldAnswer, status string
		err := a.db.QueryRow(ctx, "SELECT id::text,answer,status FROM knowledge_entries WHERE external_key=$1", it.key).Scan(&id, &oldAnswer, &status)
		if err != nil {
			if err := a.db.QueryRow(ctx, `INSERT INTO knowledge_entries(knowledge_base_id,question,answer,source,external_key,status) VALUES($1,$2,$3,'announcement',$4,'published') RETURNING id::text`, kbID, it.question, it.answer, it.key).Scan(&id); err != nil {
				return err
			}
			a.enqueueJob(ctx, "index_entry", map[string]any{"entryId": id}, "index_entry:"+id)
			continue
		}
		if oldAnswer != it.answer || status != "published" {
			if _, err := a.db.Exec(ctx, "UPDATE knowledge_entries SET question=$1,answer=$2,status='published',version=version+1,index_status='pending',updated_at=now() WHERE id=$3", it.question, it.answer, id); err != nil {
				return err
			}
			a.enqueueJob(ctx, "index_entry", map[string]any{"entryId": id}, "index_entry:"+id)
		}
	}
	rows, err := a.db.Query(ctx, "UPDATE knowledge_entries SET status='archived',updated_at=now() WHERE source='announcement' AND status='published' AND external_key LIKE 'newapi:%' AND NOT (external_key = ANY($1)) RETURNING id::text", keys)
	if err != nil {
		return err
	}
	var archived []string
	for rows.Next() {
		var id string
		_ = rows.Scan(&id)
		archived = append(archived, id)
	}
	rows.Close()
	for _, id := range archived {
		_, _ = a.db.Exec(ctx, "DELETE FROM knowledge_chunks WHERE entry_id=$1", id)
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(strings.NewReplacer("#", "", "*", "").Replace(s))
	if i := strings.IndexAny(s, "\n"); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func hashText(s string) string {
	h := sha1.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}

func (a *App) getLiveData(c *gin.Context) {
	s := a.agentSettings(c)
	var raw []byte
	var fetchedAt, attemptAt *time.Time
	var lastErr string
	_ = a.db.QueryRow(c, "SELECT data,fetched_at,last_attempt_at,last_error FROM live_data_snapshots WHERE source=$1", liveSourceNewAPI).Scan(&raw, &fetchedAt, &attemptAt, &lastErr)
	var snap newapi.Snapshot
	_ = json.Unmarshal(raw, &snap)
	stale := lastErr != "" && !strings.HasPrefix(lastErr, "部分")
	serviceStatus := map[string]any{}
	for g, w := range snap.ServiceStatus {
		if w != nil && w.Overall != nil {
			serviceStatus[g] = gin.H{"generatedAt": time.Unix(w.GeneratedAt, 0).UTC(), "overall": w.Overall.ServiceMetrics}
		}
	}
	ok(c, gin.H{
		"serviceStatus": serviceStatus,
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
	s := a.agentSettings(c)
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
