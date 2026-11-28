package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ai-bot/backend/internal/config"
)

func TestProjectVectorsPCA2DAndCluster(t *testing.T) {
	if got := projectVectorsPCA2D(nil); len(got) != 0 {
		t.Fatalf("empty PCA = %v, want empty", got)
	}
	if got := projectVectorsPCA3D(nil); len(got) != 0 {
		t.Fatalf("empty 3D PCA = %v, want empty", got)
	}
	if got := projectVectorsPCA2D([][]float64{{1, 2, 3}}); len(got) != 1 || got[0][0] != 0 || got[0][1] != 0 {
		t.Fatalf("single point PCA = %v, want [0,0]", got)
	}
	if got := projectVectorsPCA3D([][]float64{{1, 2, 3}}); len(got) != 1 || got[0][0] != 0 || got[0][1] != 0 || got[0][2] != 0 {
		t.Fatalf("single point 3D PCA = %v, want [0,0,0]", got)
	}
	zeroCoords := projectVectorsPCA3D([][]float64{{0, 0, 0}, {0, 0, 0}, {0, 0, 0}, {0, 0, 0}})
	for i, c := range zeroCoords {
		if math.IsNaN(c[0]) || math.IsInf(c[0], 0) || math.IsNaN(c[1]) || math.IsInf(c[1], 0) || math.IsNaN(c[2]) || math.IsInf(c[2], 0) {
			t.Fatalf("zero vector PCA produced invalid coord at %d: %v", i, c)
		}
	}

	// 两簇明显分离的向量 + 差异显著的主题文本
	vecs := [][]float64{
		{1.0, 0.1, 0.0, 0.0},
		{0.95, 0.15, 0.02, 0.0},
		{0.9, 0.05, 0.0, 0.01},
		{0.0, 0.0, 1.0, 0.1},
		{0.02, 0.0, 0.95, 0.12},
		{0.0, 0.01, 0.9, 0.08},
	}
	texts := []string{
		"退款流程 怎么申请七天退款",
		"退款到账 申请退款一般多久到账",
		"退款说明 订单退款原路退回",
		"API 接口调用 报 401 鉴权错误",
		"API 密钥配置 鉴权失败排查",
		"API 请求限流 接口速率说明",
	}
	coords := projectVectorsPCA3D(vecs)
	if len(coords) != len(vecs) {
		t.Fatalf("coords len = %d, want %d", len(coords), len(vecs))
	}
	for i, c := range coords {
		if math.IsNaN(c[0]) || math.IsNaN(c[1]) || math.IsNaN(c[2]) || math.Abs(c[0]) > 1 || math.Abs(c[1]) > 1 || math.Abs(c[2]) > 1 {
			t.Fatalf("coord[%d] out of bounds: %v", i, c)
		}
	}
	assign, clusters := clusterPoints3D(coords, texts)
	if len(clusters) < 2 {
		t.Fatalf("clusters len = %d, want >= 2", len(clusters))
	}
	if assign[0] == assign[3] {
		t.Fatalf("distinct clusters merged: assign=%v", assign)
	}
	joined := ""
	for _, cl := range clusters {
		joined += cl.Label + "|"
	}
	if !strings.Contains(joined, "退款") && !strings.Contains(joined, "api") {
		t.Fatalf("expected domain keywords in cluster labels, got %q", joined)
	}
}

func TestKnowledgeBaseMapAndDocumentChunks(t *testing.T) {
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

	botID := createdID(t, req("POST", "bots", map[string]any{"name": "kb-bot", "appId": "qq-kb", "appSecret": "sec", "enabled": true}, http.StatusCreated))
	secretEnc, _ := a.cipher.Encrypt("key")
	var profileID, kbID, convID string
	check(pool.QueryRow(ctx, `INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,dimension,enabled,is_default) VALUES('emb','embedding','https://emb.example',$1,'text-embedding-3-small',4,true,true) RETURNING id::text`, secretEnc).Scan(&profileID))
	kbID = createdID(t, req("POST", "knowledge-bases", map[string]any{"name": "售后指南", "description": "文档和问答", "embeddingProfileId": profileID}, http.StatusCreated))
	check(pool.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,name) VALUES('qq',$1,'grp-1','group','售后交流群') RETURNING id::text`, botID).Scan(&convID))
	_, err = pool.Exec(ctx, `INSERT INTO conversation_knowledge_bases(conversation_id,knowledge_base_id) VALUES($1,$2)`, convID, kbID)
	check(err)

	var doc1, doc2 string
	check(pool.QueryRow(ctx, `INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes,status) VALUES($1,'refund.md','k1','text/markdown',512,'ready') RETURNING id::text`, kbID).Scan(&doc1))
	check(pool.QueryRow(ctx, `INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes,status) VALUES($1,'api.pdf','k2','application/pdf',2048,'ready') RETURNING id::text`, kbID).Scan(&doc2))

	_, err = pool.Exec(ctx, `
		INSERT INTO knowledge_chunks(knowledge_base_id,document_id,chunk_index,content,embedding) VALUES
		($1,$2,0,'七天无理由退款需要保持商品完好','[1,0.1,0,0]'),
		($1,$2,1,'退款申请提交后 3 个工作日内原路退回','[0.9,0.2,0,0]'),
		($1,$3,0,'API 请求需在请求头携带 Authorization Bearer 令牌','[0,0,1,0.1]')
	`, kbID, doc1, doc2)
	check(err)

	var entry1, entry2 string
	check(pool.QueryRow(ctx, `INSERT INTO knowledge_entries(knowledge_base_id,question,aliases,answer,status,index_status,hit_count,helpful_count) VALUES($1,'怎么申请退款？','["如何退款"]'::jsonb,'在订单详情页点击退款即可。','published','ready',12,5) RETURNING id::text`, kbID).Scan(&entry1))
	check(pool.QueryRow(ctx, `INSERT INTO knowledge_entries(knowledge_base_id,question,answer,status,index_status,hit_count,unhelpful_count) VALUES($1,'API 报错 401 怎么办？','请检查 API Key 是否过期。','published','ready',8,2) RETURNING id::text`, kbID).Scan(&entry2))
	_, err = pool.Exec(ctx, `
		INSERT INTO knowledge_chunks(knowledge_base_id,entry_id,chunk_index,content,embedding) VALUES
		($1,$2,0,'怎么申请退款？ 在订单详情页点击退款即可。','[0.95,0.15,0,0]'),
		($1,$3,0,'API 报错 401 怎么办？ 请检查 API Key 是否过期。','[0,0,0.95,0.15]')
	`, kbID, entry1, entry2)
	check(err)

	_, err = pool.Exec(ctx, `INSERT INTO knowledge_candidates(knowledge_base_id,question,answer,source,confidence) VALUES($1,'支持发票开具吗？','支持开具电子增值税发票。','staff',0.91)`, kbID)
	check(err)
	_, err = pool.Exec(ctx, `INSERT INTO knowledge_gaps(knowledge_base_id,question,occurrences,embedding) VALUES($1,'如何修改绑定手机？',4,'[0.4,0.6,0.2,0.1]')`, kbID)
	check(err)

	mapResp := dataMap(t, req("GET", "knowledge-bases/"+kbID+"/map", nil, http.StatusOK))
	if mapResp["name"] != "售后指南" {
		t.Fatalf("unexpected map name: %v", mapResp["name"])
	}
	if convs, ok := mapResp["boundConversations"].([]any); !ok || len(convs) != 1 {
		t.Fatalf("boundConversations = %v, want 1", mapResp["boundConversations"])
	}
	docs, _ := mapResp["documents"].([]any)
	if len(docs) != 2 {
		t.Fatalf("documents = %d, want 2", len(docs))
	}
	firstDoc := object(t, docs[0])
	if firstDoc["preview"] == "" {
		t.Fatalf("expected non-empty preview on document: %v", firstDoc)
	}
	if entries, _ := mapResp["entries"].([]any); len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	if candidates, _ := mapResp["candidates"].([]any); len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if gaps, _ := mapResp["gaps"].([]any); len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1", len(gaps))
	}
	points, _ := mapResp["points"].([]any)
	if len(points) != 6 {
		t.Fatalf("points = %d, want 6 (5 chunks + 1 gap)", len(points))
	}
	for _, rawPt := range points {
		pt := object(t, rawPt)
		x, okX := pt["x"].(float64)
		y, okY := pt["y"].(float64)
		z, okZ := pt["z"].(float64)
		if !okX || !okY || !okZ || math.IsNaN(x) || math.IsNaN(y) || math.IsNaN(z) || math.Abs(x) > 1 || math.Abs(y) > 1 || math.Abs(z) > 1 {
			t.Fatalf("invalid 3D point coords: %v", pt)
		}
	}
	if clusters, _ := mapResp["clusters"].([]any); len(clusters) < 2 {
		t.Fatalf("clusters = %d, want >= 2", len(clusters))
	}

	var chunksEnvelope struct {
		Data []map[string]any `json:"data"`
	}
	check(json.Unmarshal(req("GET", "knowledge-bases/"+kbID+"/documents/"+doc1+"/chunks", nil, http.StatusOK), &chunksEnvelope))
	chunks := chunksEnvelope.Data
	if len(chunks) != 2 {
		t.Fatalf("doc1 chunks = %d, want 2", len(chunks))
	}
	if chunks[0]["chunkIndex"] != 0.0 || chunks[1]["chunkIndex"] != 1.0 {
		t.Fatalf("unexpected chunk order: %v", chunks)
	}
}
