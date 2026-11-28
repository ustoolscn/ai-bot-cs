package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"

	"ai-bot/backend/internal/config"
)

func TestOverviewSuccessRateBusinessSemantics(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, url)
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	cfg := config.Config{
		DataDir:       t.TempDir(),
		MasterKey:     []byte("0123456789abcdef0123456789abcdef"),
		AdminUsername: "admin",
		AdminPassword: "test-password",
	}
	a, err := New(ctx, cfg, pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	check(err)
	server := httptest.NewServer(a.Router())
	defer server.Close()

	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar}
	req := func(method, path string, in any, status int) []byte {
		t.Helper()
		return requestJSON(t, client, method, server.URL+"/api/"+path, in, status)
	}
	req("POST", "auth/login", map[string]string{"username": "admin", "password": "test-password"}, http.StatusOK)
	botID := createdID(t, req("POST", "bots", map[string]any{"name": "overview-bot", "appId": "qq-overview", "appSecret": "secret", "enabled": true}, http.StatusCreated))

	var convID string
	check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type) VALUES('qq',$1,'group-overview','group') RETURNING id::text`, botID).Scan(&convID))

	insertInbound := func(platformMsgID, inboxStatus string) string {
		t.Helper()
		var msgID string
		check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,platform_message_id,event_type,content) VALUES('qq',$1,$2,'inbound',$3,'GROUP_AT_MESSAGE_CREATE',$3) RETURNING id::text`, botID, convID, platformMsgID).Scan(&msgID))
		_, err := pool.Exec(ctx, `INSERT INTO inbox_tasks(message_id,status) VALUES($1,$2)`, msgID, inboxStatus)
		check(err)
		return msgID
	}
	insertRun := func(msgID, runStatus string) {
		t.Helper()
		_, err := pool.Exec(ctx, `INSERT INTO agent_runs(message_id,status,completed_at) VALUES($1,$2,now())`, msgID, runStatus)
		check(err)
	}
	insertOutbound := func(replyToPlatformID, outStatus, lastError string) string {
		t.Helper()
		var outMsgID, outboxTaskID string
		check(pool.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,event_type,content,reply_to_message_id,status) VALUES('qq',$1,$2,'outbound','AI_RESPONSE','reply:'||$3,$3,$4) RETURNING id::text`, botID, convID, replyToPlatformID, outStatus).Scan(&outMsgID))
		check(pool.QueryRow(ctx, `INSERT INTO outbox_tasks(message_id,status,last_error) VALUES($1,$2,NULLIF($3,'')) RETURNING id::text`, outMsgID, outStatus, lastError).Scan(&outboxTaskID))
		return outboxTaskID
	}

	// 1. 正常回复并送达 -> 成功，且计入 repliedCount
	m1 := insertInbound("replied-ok", "completed")
	insertRun(m1, "completed")
	insertOutbound("replied-ok", "sent", "")

	// 2. 智能判断无需回复 -> 算成功，不计入 repliedCount
	insertInbound("skipped-smart", "skipped")

	// 3. 连续消息合并 -> 算成功
	insertInbound("merged-msg", "merged")

	// 4. 转人工处理 -> 算成功
	insertInbound("handoff-msg", "handoff")

	// 5. 黑名单/主动拦截导致的过期取消 -> 算成功，不进入失败/告警列表
	m5 := insertInbound("blocked-expired", "completed")
	insertRun(m5, "completed")
	blockedOutboxID := insertOutbound("blocked-expired", "expired", "blacklisted or excluded")

	// 6. 入站处理重试耗尽失败 -> 算失败
	insertInbound("inbox-failed", "failed")

	// 7. 模型/Agent 执行失败 -> 算失败
	m7 := insertInbound("model-failed", "completed")
	insertRun(m7, "failed")

	// 8. 出站投递失败 -> 算失败
	m8 := insertInbound("delivery-failed", "completed")
	insertRun(m8, "completed")
	insertOutbound("delivery-failed", "failed", "qq send error")

	// 9. 出站回复超时过期 -> 算失败，且进入告警列表并可被清除
	m9 := insertInbound("delivery-expired", "completed")
	insertRun(m9, "completed")
	expiredOutboxID := insertOutbound("delivery-expired", "expired", "reply deadline exceeded")

	// 10. 入站仍在处理中 -> 暂不计入分母
	insertInbound("inbox-processing", "processing")

	// 11. 出站待投递 -> 暂不计入分母
	m11 := insertInbound("delivery-pending", "completed")
	insertRun(m11, "completed")
	insertOutbound("delivery-pending", "pending", "")

	overview := dataMap(t, req("GET", "system/overview", nil, http.StatusOK))
	assertIntField := func(obj map[string]any, key string, want float64) {
		t.Helper()
		got, ok := obj[key].(float64)
		if !ok || got != want {
			t.Fatalf("obj[%q] = %v, want %v (full=%v)", key, obj[key], want, obj)
		}
	}
	assertIntField(overview, "triggered", 9)
	assertIntField(overview, "successful", 5)
	assertIntField(overview, "failedHandled", 4)
	assertIntField(overview, "repliedCount", 1)
	assertIntField(overview, "failedTasks", 3)

	alerts, ok := overview["alerts"].([]any)
	if !ok || len(alerts) != 3 {
		t.Fatalf("alerts = %v, want 3 alerts", overview["alerts"])
	}
	hasAlertID := func(list []any, targetID string) bool {
		for _, item := range list {
			if m, ok := item.(map[string]any); ok && m["id"] == targetID {
				return true
			}
		}
		return false
	}
	if hasAlertID(alerts, blockedOutboxID) || !hasAlertID(alerts, expiredOutboxID) {
		t.Fatalf("unexpected alert ids (blocked=%s expired=%s): %v", blockedOutboxID, expiredOutboxID, alerts)
	}

	// 单条清除 expired 超时告警
	singleDismiss := dataMap(t, req("POST", "system/dismiss-failed/"+expiredOutboxID, nil, http.StatusOK))
	assertIntField(singleDismiss, "dismissed", 1)
	afterSingle := dataMap(t, req("GET", "system/overview", nil, http.StatusOK))
	assertIntField(afterSingle, "failedTasks", 2)
	afterSingleAlerts, _ := afterSingle["alerts"].([]any)
	if len(afterSingleAlerts) != 2 || hasAlertID(afterSingleAlerts, expiredOutboxID) {
		t.Fatalf("expired alert was not dismissed: %v", afterSingleAlerts)
	}

	// 全部清除剩余告警
	bulkDismiss := dataMap(t, req("POST", "system/dismiss-failed", nil, http.StatusOK))
	assertIntField(bulkDismiss, "dismissed", 2)
	afterBulk := dataMap(t, req("GET", "system/overview", nil, http.StatusOK))
	assertIntField(afterBulk, "failedTasks", 0)
	if afterBulkAlerts, _ := afterBulk["alerts"].([]any); len(afterBulkAlerts) != 0 {
		t.Fatalf("expected all alerts dismissed, got %v", afterBulkAlerts)
	}
	// 清除告警不应抹掉 24 小时真实处理成功率统计
	assertIntField(afterBulk, "triggered", 9)
	assertIntField(afterBulk, "successful", 5)
	assertIntField(afterBulk, "failedHandled", 4)
	assertIntField(afterBulk, "repliedCount", 1)
}
