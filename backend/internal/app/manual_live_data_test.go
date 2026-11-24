package app

import (
	"ai-bot/backend/internal/domain"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"ai-bot/backend/internal/agent"
	"ai-bot/backend/internal/db"
	"ai-bot/backend/internal/newapi"
)

func TestManualSnapshotMigration(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}
	ctx := context.Background()
	pool := newIntegrationSchema(t, ctx, dsn)
	a := App{db: pool}
	var hits, requests atomic.Int32
	requests.Store(7)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/api/service-status" {
			fmt.Fprintf(w, `{"success":true,"data":{"granularity":%q,"generated_at":123,"overall":{"request_count":%d}}}`, r.URL.Query().Get("granularity"), requests.Load())
			return
		}
		if r.URL.Path == "/api/status" {
			w.Write([]byte(`{"success":true,"data":{"system_name":"new","announcements_enabled":true,"announcements":[{"content":"当前公告"}]}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer site.Close()
	check := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err := pool.Exec(ctx, `WITH kb AS(INSERT INTO knowledge_bases(name) VALUES('kb') RETURNING id),entries AS(INSERT INTO knowledge_entries(knowledge_base_id,source,question,answer) SELECT kb.id,v.source,'q','a' FROM kb,(VALUES('manual'),('announcement')) v(source) RETURNING id,knowledge_base_id), chunks AS(INSERT INTO knowledge_chunks(knowledge_base_id,entry_id,chunk_index,embedding,content) SELECT knowledge_base_id,id,0,'[1,0]'::vector,'q' FROM entries) INSERT INTO agent_jobs(kind,payload) SELECT 'index_entry',jsonb_build_object('entryId',id) FROM entries`)
	check(err)
	_, err = pool.Exec(ctx, `INSERT INTO system_settings(key,value) VALUES('agent_settings', jsonb_build_object('liveData',jsonb_build_object('enabled',true,'baseUrl',$1::text,'announcementKnowledgeBaseId','kb','refreshSeconds',30,'syncAnnouncements',true))) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, site.URL)
	check(err)
	raw, _ := json.Marshal(newapi.Snapshot{SystemName: "old", FetchedAt: time.Now().Add(-25 * time.Hour)})
	_, err = pool.Exec(ctx, `INSERT INTO live_data_snapshots(source,data,fetched_at) VALUES($1,$2,now()-interval '25 hours')`, "newapi:"+hashText(site.URL+"|||kb"), raw)
	check(err)
	for range 2 {
		check(db.Migrate(ctx, pool))
	}
	s := a.agentSettings(ctx)
	snap, stale, ok := a.liveSnapshot(ctx, s)
	if !ok || !stale || snap.SystemName != "old" || hits.Load() != 0 {
		t.Fatal("reading the old snapshot changed it", snap, stale, ok, hits.Load())
	}
	var entries, chunks, jobs int
	check(pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM knowledge_entries WHERE source='manual'),(SELECT count(*) FROM knowledge_chunks),(SELECT count(*) FROM agent_jobs WHERE kind='index_entry')`).Scan(&entries, &chunks, &jobs))
	if entries != 1 || chunks != 1 || jobs != 1 {
		t.Fatal(entries, chunks, jobs)
	}
	check(a.refreshLiveData(ctx, s))
	snap, _, ok = a.liveSnapshot(ctx, s)
	if !ok || snap.SystemName != "new" || len(snap.Announcements) != 1 || hits.Load() == 0 {
		t.Fatal("manual sync missing", snap)
	}
	for _, g := range []string{"hour", "day"} {
		if w := snap.ServiceStatus[g]; w == nil || w.Overall.RequestCount != 7 {
			t.Fatalf("first manual sync %s: %+v", g, w)
		}
	}
	requests.Store(19)
	check(a.refreshLiveData(ctx, s))
	snap, _, _ = a.liveSnapshot(ctx, s)
	for _, g := range []string{"hour", "day"} {
		if w := snap.ServiceStatus[g]; w == nil || w.Overall.RequestCount != 19 {
			t.Fatalf("second manual sync %s: %+v", g, w)
		}
	}
	result, err := a.executeTool(ctx, &turnRuntime{settings: agent.DefaultSettings(), snapshotOK: true, snapshot: snap}, domain.ToolCall{Name: "get_announcements"}, nil)
	check(err)
	if !strings.Contains(result, "当前公告") {
		t.Fatal(result)
	}
	check(pool.QueryRow(ctx, "SELECT count(*) FROM knowledge_entries WHERE source='announcement'").Scan(&entries))
	if entries != 0 {
		t.Fatal("manual sync recreated FAQ entries")
	}
}
