package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"ai-bot/backend/internal/config"
	"ai-bot/backend/internal/domain"
	"ai-bot/backend/internal/knowledge"
	"ai-bot/backend/internal/openai"
	"ai-bot/backend/internal/qq"
	"ai-bot/backend/internal/secure"
	"ai-bot/backend/internal/storage"
	"ai-bot/backend/internal/webui"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type App struct {
	cfg       config.Config
	db        *pgxpool.Pool
	cipher    *secure.Cipher
	files     *storage.Local
	images    *storage.Local
	imageHTTP *http.Client
	log       *slog.Logger

	events adminEvents
}

func New(ctx context.Context, cfg config.Config, pool *pgxpool.Pool, logger *slog.Logger) (*App, error) {
	c, err := secure.NewCipher(cfg.MasterKey)
	if err != nil {
		return nil, err
	}
	f, err := storage.NewLocal(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	images, err := storage.NewLocal(filepath.Join(cfg.DataDir, "message-images"))
	if err != nil {
		return nil, err
	}
	a := &App{cfg: cfg, db: pool, cipher: c, files: f, images: images, imageHTTP: newSafeImageHTTPClient(), log: logger}
	if err := a.ensureRuntimeSettings(ctx); err != nil {
		return nil, err
	}
	if err := a.recoverTasks(ctx); err != nil {
		return nil, err
	}
	if err := a.ensureAdmin(ctx); err != nil {
		return nil, err
	}
	return a, nil
}
func (a *App) recoverTasks(ctx context.Context) error {
	queries := []string{
		"UPDATE message_stages SET status='failed',detail='服务重启，结束时间未记录' WHERE status='processing'",
		"UPDATE inbox_tasks SET status='pending',locked_at=NULL,next_attempt_at=now(),updated_at=now() WHERE status='processing'",
		"UPDATE outbox_tasks SET status='pending',locked_at=NULL,next_attempt_at=now(),updated_at=now() WHERE status='processing'",
		"UPDATE message_retractions SET status='pending',locked_at=NULL,next_attempt_at=now(),updated_at=now() WHERE status='processing'",
		"UPDATE knowledge_documents SET status='pending',next_attempt_at=now(),updated_at=now() WHERE status='processing'",
		"UPDATE agent_jobs SET status='pending',locked_at=NULL,next_attempt_at=now(),updated_at=now() WHERE status='processing'",
	}
	for _, query := range queries {
		if _, err := a.db.Exec(ctx, query); err != nil {
			return err
		}
	}
	return nil
}
func (a *App) ensureAdmin(ctx context.Context) error {
	var exists bool
	if err := a.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM admin_users)").Scan(&exists); err != nil {
		return err
	}
	if exists {
		return nil
	}
	h, err := bcrypt.GenerateFromPassword([]byte(a.cfg.AdminPassword), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = a.db.Exec(ctx, "INSERT INTO admin_users(username,password_hash) VALUES($1,$2)", a.cfg.AdminUsername, string(h))
	return err
}
func (a *App) Router() http.Handler {
	r := gin.New()
	r.Use(gin.Recovery(), a.requestLog())
	r.GET("/healthz", func(c *gin.Context) { ok(c, gin.H{"status": "ok"}) })
	r.GET("/p/:id", a.scheduledWebPage)
	r.POST("/callbacks/qq/:botId", a.qqWebhook)
	api := r.Group("/api")
	api.POST("/auth/login", a.login)
	api.POST("/auth/logout", a.logout)
	auth := api.Group("")
	auth.Use(a.requireAuth())
	auth.GET("/auth/me", a.me)
	auth.GET("/events", a.adminSocket)
	auth.PUT("/auth/password", a.changePassword)
	a.supportRoutes(auth)
	a.scheduledPostRoutes(auth)
	a.guardRoutes(auth)
	auth.GET("/bots", a.listBots)
	auth.POST("/bots", a.saveBot)
	auth.PUT("/bots/:id", a.saveBot)
	auth.DELETE("/bots/:id", a.deleteBot)
	a.proxyRoutes(auth)
	a.operatorRoutes(auth)
	auth.GET("/model-provider-presets", func(c *gin.Context) { ok(c, modelProviderPresets) })
	auth.GET("/model-profiles", a.listModels)
	auth.POST("/model-profiles", a.saveModel)
	auth.POST("/model-profiles/discover", a.discoverModels)
	auth.PUT("/model-profiles/:id", a.saveModel)
	auth.DELETE("/model-profiles/:id", a.deleteModel)
	auth.POST("/model-profiles/:id/test", a.testModel)
	auth.POST("/mention-names", a.mentionNames)
	auth.GET("/conversations/:id/chat", a.chatHistory)
	auth.GET("/conversations", a.listConversations)
	auth.DELETE("/conversations/:id", a.deleteConversation)
	auth.GET("/conversations/:id", a.getConversation)
	auth.PUT("/conversations/:id", a.updateConversation)
	auth.GET("/messages", a.listMessages)
	auth.GET("/messages/:id", a.getMessage)
	auth.GET("/messages/:id/attachments/:index", a.getMessageAttachment)
	auth.GET("/knowledge-bases", a.listKBs)
	auth.POST("/knowledge-bases", a.createKB)
	auth.POST("/knowledge-bases/import", a.importKnowledge)
	auth.GET("/knowledge-bases/:id/export", a.exportKnowledge)
	auth.GET("/knowledge-bases/:id", a.getKB)
	auth.DELETE("/knowledge-bases/:id", a.deleteKB)
	auth.POST("/knowledge-bases/:id/documents", a.uploadDocument)
	auth.DELETE("/knowledge-bases/:id/documents/:documentId", a.deleteDocument)
	auth.DELETE("/knowledge-bases/:id/documents/:documentId/index", a.deleteDocumentIndex)
	auth.POST("/knowledge-bases/:id/documents/:documentId/retry", a.retryDocument)
	auth.GET("/knowledge-bases/:id/chunks/:chunkId/image", a.knowledgeImage)
	auth.PUT("/knowledge-bases/:id/embedding-profile", a.changeKnowledgeProfile)
	auth.POST("/knowledge-bases/:id/search", a.searchKB)
	auth.GET("/system/overview", a.overview)
	auth.GET("/system/settings", a.getSystemSettings)
	auth.PUT("/system/settings", a.putSystemSettings)
	auth.GET("/system/costs", a.costs)
	auth.POST("/system/dismiss-failed", a.dismissFailed)
	auth.POST("/system/dismiss-failed/:id", a.dismissFailed)
	auth.POST("/system/retry-failed", a.retryFailedTasks)
	auth.GET("/system/alerts", a.listAlerts)
	auth.POST("/system/alerts/ack", a.ackAlerts)
	auth.GET("/agent/settings", a.getAgentSettings)
	auth.PUT("/agent/settings", a.putAgentSettings)
	auth.GET("/agent/live-data", a.getLiveData)
	auth.POST("/agent/live-data/refresh", a.refreshLiveDataNow)
	auth.GET("/skills", a.listSkills)
	auth.POST("/skills", a.saveSkill)
	auth.PUT("/skills/:id", a.saveSkill)
	auth.DELETE("/skills/:id", a.deleteSkill)
	auth.GET("/tools", a.listTools)
	auth.POST("/tools", a.saveTool)
	auth.PUT("/tools/:id", a.saveTool)
	auth.DELETE("/tools/:id", a.deleteTool)
	auth.POST("/tools/:id/test", a.testTool)
	auth.GET("/blocked-users", a.listBlockedUsers)
	auth.POST("/blocked-users", a.addBlockedUser)
	auth.DELETE("/blocked-users/:id", a.deleteBlockedUser)
	auth.GET("/conversations/:id/staff-learning", a.getStaffLearning)
	auth.POST("/conversations/:id/staff-learning", a.startStaffLearning)
	auth.GET("/conversations/:id/delivery/options", a.qqDeliveryOptions)
	auth.GET("/conversations/:id/delivery/assets", a.qqAssets)
	auth.POST("/conversations/:id/delivery/upload", a.qqUpload)
	auth.POST("/conversations/:id/delivery/send", a.qqQueue)
	auth.GET("/conversations/:id/qq/:section", a.qqGroupRead)
	auth.POST("/conversations/:id/qq/:section", a.qqGroupAction)
	auth.POST("/conversations/:id/name/sync", a.syncGroupName)
	auth.POST("/conversations/:id/members/sync", a.syncGroupMembers)
	auth.GET("/conversations/:id/members", a.listMembers)
	auth.PUT("/conversations/:id/members/:userId", a.updateMember)
	auth.GET("/conversations/:id/summaries", a.getSummary)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		auth.Handle(method, "/conversations/:id/user-profiles/:user", a.userProfile)
	}
	auth.GET("/memories/directory", a.memoryDirectory)
	auth.GET("/memories", a.listMemories)
	auth.POST("/memories", a.createMemory)
	auth.PUT("/memories/:id", a.updateMemory)
	auth.DELETE("/memories/:id", a.deleteMemory)
	auth.GET("/handoffs", a.listHandoffs)
	auth.POST("/handoffs/:id/claim", a.claimHandoff)
	auth.POST("/handoffs/:id/resolve", a.resolveHandoff)
	auth.POST("/messages/:id/reply", a.humanReply)
	auth.POST("/messages/:id/feedback", a.createFeedback)
	auth.GET("/feedback", a.listFeedback)
	auth.POST("/feedback/read", a.markFeedbackRead)
	auth.POST("/feedback/:id/read", a.markFeedbackRead)
	auth.POST("/learning/:collection/bulk-delete", a.bulkDeleteLearning)
	auth.GET("/learning/stats", a.learningStats)
	auth.GET("/knowledge/candidates", a.listCandidates)
	auth.POST("/knowledge/candidates/:id/approve", a.approveCandidateAPI)
	auth.POST("/knowledge/candidates/:id/reject", a.rejectCandidate)
	auth.GET("/knowledge/entries", a.listEntries)
	auth.POST("/knowledge/entries", a.createEntryAPI)
	auth.PUT("/knowledge/entries/:id", a.updateEntryAPI)
	auth.DELETE("/knowledge/entries/:id", a.deleteEntryAPI)
	auth.GET("/knowledge/gaps", a.listGaps)
	auth.POST("/knowledge/gaps/:id/answer", a.answerGap)
	auth.POST("/knowledge/gaps/:id/ignore", a.ignoreGap)
	auth.GET("/eval/cases", a.listEvalCases)
	auth.POST("/eval/cases", a.saveEvalCase)
	auth.PUT("/eval/cases/:id", a.saveEvalCase)
	auth.DELETE("/eval/cases/:id", a.deleteEvalCase)
	auth.POST("/eval/cases/from-entries", a.evalCasesFromEntries)
	auth.GET("/eval/runs", a.listEvalRuns)
	auth.POST("/eval/runs", a.createEvalRun)
	auth.GET("/eval/runs/:id/results", a.getEvalRun)
	r.NoRoute(gin.WrapH(webui.Handler()))
	return r
}
func ok(c *gin.Context, data any)      { c.JSON(http.StatusOK, gin.H{"data": data}) }
func created(c *gin.Context, data any) { c.JSON(http.StatusCreated, gin.H{"data": data}) }
func fail(c *gin.Context, status int, code, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{"code": code, "message": msg}})
}
func (a *App) requestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		a.log.Info("http request", "method", c.Request.Method, "path", c.Request.URL.Path, "status", c.Writer.Status(), "latency", time.Since(start))
	}
}

func randomToken() (string, []byte, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	t := hex.EncodeToString(b)
	h := sha256.Sum256([]byte(t))
	return t, h[:], nil
}
func (a *App) login(c *gin.Context) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "请输入用户名和密码")
		return
	}
	var id, hash string
	if err := a.db.QueryRow(c, "SELECT id::text,password_hash FROM admin_users WHERE username=$1", in.Username).Scan(&id, &hash); err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		fail(c, 401, "invalid_credentials", "用户名或密码错误")
		return
	}
	t, h, err := randomToken()
	if err != nil {
		fail(c, 500, "internal_error", "创建会话失败")
		return
	}
	exp := time.Now().Add(7 * 24 * time.Hour)
	if _, err = a.db.Exec(c, "INSERT INTO admin_sessions(user_id,token_hash,expires_at) VALUES($1,$2,$3)", id, h, exp); err != nil {
		fail(c, 500, "internal_error", "创建会话失败")
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "aibot_session", Value: t, Path: "/", HttpOnly: true, Secure: a.cfg.CookieSecure, SameSite: http.SameSiteLaxMode, Expires: exp})
	ok(c, gin.H{"id": id, "username": in.Username})
}
func (a *App) logout(c *gin.Context) {
	if t, err := c.Cookie("aibot_session"); err == nil {
		h := sha256.Sum256([]byte(t))
		_, _ = a.db.Exec(c, "DELETE FROM admin_sessions WHERE token_hash=$1", h[:])
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: "aibot_session", Value: "", Path: "/", HttpOnly: true, MaxAge: -1})
	ok(c, gin.H{"loggedOut": true})
}
func (a *App) requireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		t, err := c.Cookie("aibot_session")
		if err != nil {
			fail(c, 401, "unauthorized", "请先登录")
			return
		}
		h := sha256.Sum256([]byte(t))
		var id, user string
		err = a.db.QueryRow(c, "SELECT u.id::text,u.username FROM admin_sessions s JOIN admin_users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()", h[:]).Scan(&id, &user)
		if err != nil {
			fail(c, 401, "unauthorized", "会话已失效")
			return
		}
		c.Set("userId", id)
		c.Set("username", user)
		c.Next()
	}
}
func (a *App) me(c *gin.Context) {
	ok(c, gin.H{"id": c.GetString("userId"), "username": c.GetString("username")})
}
func (a *App) changePassword(c *gin.Context) {
	var in struct {
		Current string `json:"currentPassword"`
		New     string `json:"newPassword"`
	}
	if c.ShouldBindJSON(&in) != nil || len(in.New) < 8 {
		fail(c, 400, "invalid_request", "新密码至少8位")
		return
	}
	var hash string
	_ = a.db.QueryRow(c, "SELECT password_hash FROM admin_users WHERE id=$1", c.GetString("userId")).Scan(&hash)
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Current)) != nil {
		fail(c, 400, "invalid_password", "当前密码错误")
		return
	}
	h, _ := bcrypt.GenerateFromPassword([]byte(in.New), bcrypt.DefaultCost)
	token, _ := c.Cookie("aibot_session")
	tokenHash := sha256.Sum256([]byte(token))
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "更新密码失败")
		return
	}
	defer tx.Rollback(c)
	if _, err = tx.Exec(c, "UPDATE admin_users SET password_hash=$1,updated_at=now() WHERE id=$2", string(h), c.GetString("userId")); err == nil {
		_, err = tx.Exec(c, "DELETE FROM admin_sessions WHERE user_id=$1 AND token_hash<>$2", c.GetString("userId"), tokenHash[:])
	}
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "更新密码失败")
		return
	}
	ok(c, gin.H{"changed": true})
}

func rowsJSON(rows pgx.Rows) ([]map[string]any, error) {
	defer rows.Close()
	return pgx.CollectRows(rows, pgx.RowToMap)
}
func queryList(c *gin.Context, a *App, sql string, args ...any) {
	rows, err := a.db.Query(c, sql, args...)
	if err != nil {
		fail(c, 500, "database_error", "读取数据失败")
		return
	}
	data, err := rowsJSON(rows)
	if err != nil {
		fail(c, 500, "database_error", "读取数据失败")
		return
	}
	ok(c, data)
}

func (a *App) listBots(c *gin.Context) {
	queryList(c, a, `SELECT coalesce((SELECT value->>'publicBaseUrl' FROM system_settings WHERE key='runtime_defaults'),'') || '/callbacks/qq/' || id::text AS "callbackUrl", id::text,name,channel,app_id AS "appId",enabled,status,last_event_at AS "lastEventAt",created_at AS "createdAt",app_secret_enc<>'' AS "hasSecret" FROM bots ORDER BY created_at DESC`)
}
func (a *App) saveBot(c *gin.Context) {
	var in struct {
		Name      string `json:"name"`
		AppID     string `json:"appId"`
		AppSecret string `json:"appSecret"`

		Enabled *bool `json:"enabled"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Name == "" || in.AppID == "" {
		fail(c, 400, "invalid_request", "名称和 AppID 必填")
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	id := c.Param("id")
	if id == "" {
		if in.AppSecret == "" {
			fail(c, 400, "invalid_request", "AppSecret 必填")
			return
		}
		enc, _ := a.cipher.Encrypt(in.AppSecret)
		err := a.db.QueryRow(c, "INSERT INTO bots(name,app_id,app_secret_enc,enabled) VALUES($1,$2,$3,$4) RETURNING id::text", in.Name, in.AppID, enc, enabled).Scan(&id)
		if err != nil {
			fail(c, 500, "database_error", "保存机器人失败")
			return
		}
		created(c, gin.H{"id": id})
		return
	}
	var err error
	if in.AppSecret != "" {
		enc, _ := a.cipher.Encrypt(in.AppSecret)
		_, err = a.db.Exec(c, "UPDATE bots SET name=$1,app_id=$2,app_secret_enc=$3,enabled=$4,updated_at=now() WHERE id=$5", in.Name, in.AppID, enc, enabled, id)
	} else {
		_, err = a.db.Exec(c, "UPDATE bots SET name=$1,app_id=$2,enabled=$3,updated_at=now() WHERE id=$4", in.Name, in.AppID, enabled, id)
	}
	if err != nil {
		fail(c, 500, "database_error", "保存机器人失败")
		return
	}
	ok(c, gin.H{"id": id})
}
func (a *App) deleteBot(c *gin.Context) {
	a.deleteRecord(c, "DELETE FROM bots WHERE id=$1")
}

func (a *App) deleteConversation(c *gin.Context) {
	a.deleteRecord(c, "DELETE FROM conversations WHERE id=$1")
}

func (a *App) deleteRecord(c *gin.Context, statement string) {
	if parseUUID(c.Param("id")) != nil {
		fail(c, 400, "invalid_id", "ID 格式无效")
		return
	}
	tag, err := a.db.Exec(c, statement, c.Param("id"))
	if err != nil {
		a.log.Error("delete record", "error", err)
		fail(c, 500, "database_error", "删除失败")
		return
	}
	if tag.RowsAffected() == 0 {
		fail(c, 404, "not_found", "记录不存在")
		return
	}
	ok(c, gin.H{"deleted": true})
}

type modelInput struct {
	Provider                  string          `json:"provider"`
	ProxyID                   json.RawMessage `json:"proxyId"`
	PricePerImage             json.RawMessage `json:"pricePerImage"`
	InputPricePerMillion      json.RawMessage `json:"inputPricePerMillion"`
	OutputPricePerMillion     json.RawMessage `json:"outputPricePerMillion"`
	CacheReadPricePerMillion  json.RawMessage `json:"cacheReadPricePerMillion"`
	CacheWritePricePerMillion json.RawMessage `json:"cacheWritePricePerMillion"`
	Name                      string          `json:"name"`
	Kind                      string          `json:"kind"`
	BaseURL                   string          `json:"baseUrl"`
	APIKey                    string          `json:"apiKey"`
	Model                     string          `json:"model"`
	Dimension                 *int            `json:"dimension"`
	Enabled                   *bool           `json:"enabled"`
	IsDefault                 bool            `json:"isDefault"`
	WebSearchMode             string          `json:"webSearchMode"`
	ReasoningEffort           string          `json:"reasoningEffort"`
	ExtraBody                 map[string]any  `json:"extraBody"`
}

func (a *App) listModels(c *gin.Context) {
	queryList(c, a, `SELECT id::text,proxy_id::text AS "proxyId",provider,name,kind,base_url AS "baseUrl",model,dimension,enabled,is_default AS "isDefault",api_key_enc<>'' AS "hasApiKey",web_search_mode AS "webSearchMode",reasoning_effort AS "reasoningEffort",extra_body AS "extraBody",price_per_image AS "pricePerImage",input_price AS "inputPricePerMillion",output_price AS "outputPricePerMillion",cache_read_price AS "cacheReadPricePerMillion",cache_write_price AS "cacheWritePricePerMillion",created_at AS "createdAt" FROM model_profiles ORDER BY kind,name`)
}
func (a *App) saveModel(c *gin.Context) {
	var in modelInput
	if c.ShouldBindJSON(&in) != nil {
		fail(c, 400, "invalid_request", "模型配置格式无效")
		return
	}
	if err := in.applyProvider(); err != nil {
		fail(c, 400, "invalid_request", err.Error())
		return
	}
	if in.Name == "" || in.BaseURL == "" || in.Model == "" || (in.Kind != "chat" && in.Kind != "embedding" && in.Kind != "image" && in.Kind != "decision") {
		fail(c, 400, "invalid_request", "模型配置不完整")
		return
	}
	if in.Provider == "google" && (in.Model != "gemini-embedding-2" || (in.Dimension != nil && (*in.Dimension < 128 || *in.Dimension > 3072))) {
		fail(c, 400, "invalid_request", "Google 渠道支持 gemini-embedding-2，维度范围为 128 至 3072")
		return
	}
	if in.Kind == "image" || in.Kind == "decision" {
		in.Dimension = nil
	}
	if in.Kind == "decision" {
		// TypeSafe reports output tokens for usage but charges only for input.
		in.OutputPricePerMillion = json.RawMessage("0")
	}
	rates := []json.RawMessage{in.InputPricePerMillion, in.OutputPricePerMillion, in.CacheReadPricePerMillion, in.CacheWritePricePerMillion, in.PricePerImage}
	for _, raw := range rates {
		var price *float64
		if raw != nil && (json.Unmarshal(raw, &price) != nil || (price != nil && *price < 0)) {
			fail(c, 400, "invalid_price", "单价必须是非负数或 null")
			return
		}
	}
	if in.Kind == "chat" {
		in.Dimension = nil
		if in.WebSearchMode == "" {
			in.WebSearchMode = "disabled"
		}
		if in.ReasoningEffort == "" {
			in.ReasoningEffort = "default"
		}
		switch in.ReasoningEffort {
		case "default", "none", "minimal", "low", "medium", "high":
		default:
			fail(c, 400, "invalid_request", "思考等级无效")
			return
		}
	} else {
		in.WebSearchMode = "disabled"
		in.ReasoningEffort = "default"
		in.ExtraBody = nil
	}
	if in.ExtraBody == nil {
		in.ExtraBody = map[string]any{}
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	id := c.Param("id")
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "保存失败")
		return
	}
	defer tx.Rollback(c)
	if id != "" {
		var bound bool
		err = tx.QueryRow(c, `SELECT EXISTS(SELECT FROM knowledge_bases k WHERE k.embedding_profile_id=p.id) AND (provider<>$2 OR model<>$3 OR dimension IS DISTINCT FROM $4::int OR kind<>$5 OR base_url<>$6) FROM model_profiles p WHERE id=$1 FOR UPDATE`, id, in.Provider, in.Model, in.Dimension, in.Kind, strings.TrimRight(in.BaseURL, "/")).Scan(&bound)
		if err != nil {
			fail(c, 400, "invalid_model", "无法读取要更新的模型配置")
			return
		}
		if bound {
			fail(c, 409, "profile_in_use", "该配置已绑定知识库，请新建模型配置并切换知识库以重建索引")
			return
		}
	}
	if id != "" && in.APIKey == "" {
		var savedBase string
		if err := tx.QueryRow(c, "SELECT base_url FROM model_profiles WHERE id=$1 FOR UPDATE", id).Scan(&savedBase); err != nil {
			fail(c, 400, "invalid_model", "无法读取要更新的模型配置")
			return
		}
		if strings.TrimRight(in.BaseURL, "/") != strings.TrimRight(savedBase, "/") {
			fail(c, 400, "invalid_request", "更换服务地址时请填写对应的 API Key")
			return
		}
	}
	if in.IsDefault {
		_, _ = tx.Exec(c, "UPDATE model_profiles SET is_default=false WHERE kind=$1", in.Kind)
	}
	if id == "" {
		if in.APIKey == "" {
			fail(c, 400, "invalid_request", "API Key 必填")
			return
		}
		enc, _ := a.cipher.Encrypt(in.APIKey)
		err = tx.QueryRow(c, "INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,dimension,enabled,is_default,web_search_mode,reasoning_effort,extra_body,provider) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING id::text", in.Name, in.Kind, strings.TrimRight(in.BaseURL, "/"), enc, in.Model, in.Dimension, enabled, in.IsDefault, in.WebSearchMode, in.ReasoningEffort, in.ExtraBody, in.Provider).Scan(&id)
	} else if in.APIKey != "" {
		enc, _ := a.cipher.Encrypt(in.APIKey)
		_, err = tx.Exec(c, "UPDATE model_profiles SET name=$1,kind=$2,base_url=$3,api_key_enc=$4,model=$5,dimension=$6,enabled=$7,is_default=$8,web_search_mode=$9,reasoning_effort=$10,extra_body=$11,provider=$13,updated_at=now() WHERE id=$12", in.Name, in.Kind, strings.TrimRight(in.BaseURL, "/"), enc, in.Model, in.Dimension, enabled, in.IsDefault, in.WebSearchMode, in.ReasoningEffort, in.ExtraBody, id, in.Provider)
	} else {
		_, err = tx.Exec(c, "UPDATE model_profiles SET name=$1,kind=$2,base_url=$3,model=$4,dimension=$5,enabled=$6,is_default=$7,web_search_mode=$8,reasoning_effort=$9,extra_body=$10,provider=$12,updated_at=now() WHERE id=$11", in.Name, in.Kind, strings.TrimRight(in.BaseURL, "/"), in.Model, in.Dimension, enabled, in.IsDefault, in.WebSearchMode, in.ReasoningEffort, in.ExtraBody, id, in.Provider)
	}
	if err == nil && in.ProxyID != nil {
		var proxy string
		if json.Unmarshal(in.ProxyID, &proxy) != nil || (proxy != "" && uuid.Validate(proxy) != nil) {
			fail(c, 400, "invalid_proxy", "代理 ID 无效")
			return
		}
		if _, err = tx.Exec(c, "UPDATE model_profiles SET proxy_id=NULLIF($2::text,'')::uuid WHERE id=$1", id, proxy); err != nil {
			fail(c, 400, "invalid_proxy", "所选代理不存在")
			return
		}
	}
	var savedRates [5]*float64
	if err == nil {
		err = tx.QueryRow(c, `UPDATE model_profiles SET
			input_price=(coalesce($2::jsonb,to_jsonb(input_price)) #>> '{}')::float8,
			output_price=(coalesce($3::jsonb,to_jsonb(output_price)) #>> '{}')::float8,
			cache_read_price=(coalesce($4::jsonb,to_jsonb(cache_read_price)) #>> '{}')::float8,
			cache_write_price=(coalesce($5::jsonb,to_jsonb(cache_write_price)) #>> '{}')::float8,
			price_per_image=(coalesce($6::jsonb,to_jsonb(price_per_image)) #>> '{}')::float8
			WHERE id=$1 RETURNING input_price,output_price,cache_read_price,cache_write_price,price_per_image`, id, rates[0], rates[1], rates[2], rates[3], rates[4]).Scan(&savedRates[0], &savedRates[1], &savedRates[2], &savedRates[3], &savedRates[4])
	}
	if err != nil {
		fail(c, 500, "database_error", "保存模型失败")
		return
	}
	if err = tx.Commit(c); err != nil {
		fail(c, 500, "database_error", "保存模型失败")
		return
	}
	ok(c, gin.H{"id": id, "inputPricePerMillion": savedRates[0], "outputPricePerMillion": savedRates[1], "cacheReadPricePerMillion": savedRates[2], "cacheWritePricePerMillion": savedRates[3], "pricePerImage": savedRates[4]})
}
func (a *App) deleteModel(c *gin.Context) {
	_, err := a.db.Exec(c, "DELETE FROM model_profiles WHERE id=$1", c.Param("id"))
	if err != nil {
		fail(c, 409, "delete_failed", "模型正在使用中")
		return
	}
	ok(c, gin.H{"deleted": true})
}
func (a *App) loadModel(ctx context.Context, id string) (*openai.Client, string, error) {
	cl, kind, _, _, err := a.loadModelDetails(ctx, id)
	return cl, kind, err
}
func (a *App) loadModelDetails(ctx context.Context, id string) (*openai.Client, string, string, *int, error) {
	return a.readModelProfile(ctx, id, false)
}

func (a *App) readModelProfile(ctx context.Context, id string, includeDisabled bool) (*openai.Client, string, string, *int, error) {
	var base, keyEnc, model, kind, webSearchMode, reasoningEffort string
	var dimension *int
	var proxyID, provider string
	var extraBodyRaw []byte
	err := a.db.QueryRow(ctx, "SELECT base_url,api_key_enc,model,kind,dimension,web_search_mode,reasoning_effort,extra_body,provider,COALESCE(proxy_id::text,'') FROM model_profiles WHERE id=$1 AND (enabled OR $2)", id, includeDisabled).Scan(&base, &keyEnc, &model, &kind, &dimension, &webSearchMode, &reasoningEffort, &extraBodyRaw, &provider, &proxyID)
	if err != nil {
		return nil, "", "", nil, err
	}
	key, err := a.cipher.Decrypt(keyEnc)
	if err != nil {
		return nil, "", "", nil, err
	}
	settings, err := a.runtimeSettings(ctx)
	if err != nil {
		return nil, "", "", nil, fmt.Errorf("读取运行时设置失败: %w", err)
	}
	client := openai.New(base, key, model, time.Duration(settings.AIRequestTimeoutSeconds)*time.Second)
	client.HTTP.Transport, err = a.proxyTransport(ctx, proxyID)
	if err != nil {
		return nil, "", "", nil, err
	}
	if proxy, ok := client.HTTP.Transport.(authenticatedProxyTransport); ok {
		client.RedactProxyCredentials = proxy.redactor
	}
	client.Provider = provider
	client.WebSearchMode = webSearchMode
	client.ReasoningEffort = reasoningEffort
	if len(extraBodyRaw) > 0 {
		_ = json.Unmarshal(extraBodyRaw, &client.ExtraBody)
	}
	if client.ExtraBody == nil {
		client.ExtraBody = map[string]any{}
	}
	if kind == "chat" {
		switch webSearchMode {
		case "qwen":
			client.ExtraBody["enable_search"] = true
		case "openai":
			client.ExtraBody["web_search_options"] = map[string]any{}
		case "responses":
			client.UseResponses = true
		}
	}
	if dimension != nil && *dimension > 0 {
		client.Dimensions = *dimension
	}
	return client, kind, model, dimension, nil
}
func (a *App) testModel(c *gin.Context) {
	var in struct {
		WebSearch    *bool  `json:"webSearch"`
		Input        string `json:"input"`
		SystemPrompt string `json:"systemPrompt"`
	}
	if err := c.ShouldBindJSON(&in); err != nil && !errors.Is(err, io.EOF) {
		fail(c, 400, "invalid_request", "测试参数格式无效")
		return
	}
	searchTest := in.WebSearch != nil && *in.WebSearch
	cl, kind, model, dimension, err := a.readModelProfile(c, c.Param("id"), searchTest)
	if err != nil {
		fail(c, 404, "not_found", "模型不存在")
		return
	}
	if searchTest && (kind != "chat" || cl.WebSearchMode == "disabled") {
		fail(c, 400, "invalid_request", "请为对话模型开启联网搜索")
		return
	}
	// The Qwen compatible endpoint only includes source evidence when requested.
	if searchTest && cl.WebSearchMode == "qwen" {
		options, _ := cl.ExtraBody["search_options"].(map[string]any)
		if options == nil {
			options = map[string]any{}
		}
		options["enable_source"] = true
		cl.ExtraBody["search_options"] = options
	}
	started := time.Now()
	if kind == "decision" {
		state := map[string]string{"当前消息": firstNonEmpty(strings.TrimSpace(in.Input), "客服你好，我的订单支付失败了，请帮忙看看。")}
		res, err := cl.TypeSafeDecision(c, state, replyQuestions)
		elapsed := time.Since(started).Milliseconds()
		a.recordModelCall(c, "", "", c.Param("id"), "decision_test", res, elapsed, err)
		if err != nil {
			fail(c, 502, "model_error", err.Error())
			return
		}
		ok(c, gin.H{"kind": kind, "model": model, "content": res.Content, "decision": json.RawMessage(res.Content), "inputTokens": res.InputTokens, "outputTokens": res.OutputTokens, "latencyMs": elapsed})
		return
	}
	if kind == "image" {
		data, typ, err := a.requestImage(c, cl, firstNonEmpty(strings.TrimSpace(in.Input), "画一个蓝色圆形，白色背景。"))
		if err != nil {
			fail(c, 502, "model_error", err.Error())
			return
		}
		a.recordModelCall(c, "", "", c.Param("id"), "image", domain.ChatResult{}, time.Since(started).Milliseconds(), nil)
		ok(c, gin.H{"kind": kind, "model": model, "content": "已生成一张测试图片。", "contentType": typ, "imageUrl": "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(data), "sizeBytes": len(data), "latencyMs": time.Since(started).Milliseconds()})
		return
	}
	if kind == "embedding" {
		if strings.TrimSpace(in.Input) == "" {
			in.Input = "连接测试"
		}
		var vectors [][]float32
		vectors, err = a.embed(c, cl, c.Param("id"), "embedding", []string{in.Input})
		if err == nil && len(vectors) != 1 {
			err = fmt.Errorf("Embedding 接口返回向量数量异常：期望 1，实际 %d", len(vectors))
		}
		if err == nil && dimension != nil && len(vectors[0]) != *dimension {
			err = fmt.Errorf("Embedding 维度不匹配：配置为 %d，实际返回 %d", *dimension, len(vectors[0]))
		}
		if err != nil {
			fail(c, 502, "model_error", err.Error())
			return
		}
		preview := vectors[0]
		if len(preview) > 12 {
			preview = preview[:12]
		}
		ok(c, gin.H{"kind": kind, "model": model, "dimensions": len(vectors[0]), "vectorPreview": preview, "latencyMs": time.Since(started).Milliseconds()})
		return
	} else {
		if strings.TrimSpace(in.Input) == "" {
			if searchTest {
				in.Input = "请联网搜索最近一次 DeepSeek 官方模型更新，并引用官方来源。"
			} else {
				in.Input = "请只回复：连接测试成功"
			}
		}
		messages := make([]domain.ChatMessage, 0, 2)
		if strings.TrimSpace(in.SystemPrompt) != "" {
			messages = append(messages, domain.ChatMessage{Role: "system", Content: in.SystemPrompt})
		}
		messages = append(messages, domain.ChatMessage{Role: "user", Content: in.Input})
		var result domain.ChatResult
		result, err = cl.ChatWith(c, messages, domain.ChatOptions{DisableWebSearch: in.WebSearch != nil && !*in.WebSearch})
		a.recordModelCall(c, "", "", c.Param("id"), "chat_test", result, time.Since(started).Milliseconds(), err)
		if err != nil {
			fail(c, 502, "model_error", err.Error())
			return
		}
		ok(c, gin.H{"kind": kind, "model": model, "content": result.Content, "searched": result.Searched, "searchQueries": result.SearchQueries, "sources": result.Sources, "inputTokens": result.InputTokens, "outputTokens": result.OutputTokens, "latencyMs": time.Since(started).Milliseconds()})
		return
	}
}

const conversationIdentity = `coalesce(
 CASE WHEN c.type='private' THEN (SELECT coalesce(nullif(nullif(display_name,''),platform_user_id),nullif(nullif(qq_username,''),platform_user_id)) FROM conversation_members WHERE conversation_id=c.id AND platform_user_id=c.platform_id) END,
 CASE WHEN c.name NOT IN ('',c.platform_id) AND c.name NOT LIKE 'QQ 群聊 · %' AND c.name NOT LIKE 'QQ 用户 · %' THEN c.name END,
 nullif(c.qq_group_info->>'group_name',''),CASE WHEN c.type='group' THEN 'QQ 群聊' ELSE 'QQ 用户' END) AS name,
 coalesce(CASE WHEN c.type='private' THEN (SELECT qq_avatar FROM conversation_members WHERE conversation_id=c.id AND platform_user_id=c.platform_id) ELSE c.qq_group_info->>'group_avatar' END,'') AS avatar`

func (a *App) listConversations(c *gin.Context) {
	queryList(c, a, `SELECT c.image_daily_limit AS "imageDailyLimit",c.id::text,c.bot_id::text AS "botId",c.service_id::text AS "serviceId",c.channel,c.platform_id AS "platformId",c.type,`+conversationIdentity+`,c.enabled,c.trigger_mode AS "triggerMode",c.context_limit AS "contextLimit",c.system_prompt AS "systemPrompt",c.chat_profile_id::text AS "chatProfileId",b.name AS "botName",ARRAY(SELECT ck.knowledge_base_id::text FROM conversation_knowledge_bases ck WHERE ck.conversation_id=c.id) AS "knowledgeBaseIds",c.updated_at AS "updatedAt",latest.preview AS "lastMessage",COALESCE(ms.message_count,0) AS "messageCount",CASE WHEN c.type='private' THEN GREATEST(COALESCE(cm.member_count,0),1) ELSE COALESCE(cm.member_count,0) END AS "memberCount",COALESCE(ms.has_full_message_events,false) AS "hasFullMessageEvents" FROM conversations c JOIN bots b ON b.id=c.bot_id LEFT JOIN LATERAL (SELECT count(*) FILTER(WHERE event_type<>'PROCESSING_ACK') AS message_count,bool_or(event_type='GROUP_MESSAGE_CREATE') AS has_full_message_events FROM messages WHERE conversation_id=c.id) ms ON true LEFT JOIN LATERAL (SELECT count(*) AS member_count FROM conversation_members WHERE conversation_id=c.id AND active) cm ON true LEFT JOIN LATERAL (SELECT m.event_at,jsonb_build_object('content',left(m.content,200),'direction',m.direction,'senderName',coalesce(nullif(m.sender_name,''),CASE WHEN m.direction='outbound' THEN b.name ELSE 'QQ用户' END),'at',m.event_at,'mentionNames',message_mention_names(m,left(m.content,200))) AS preview FROM messages m WHERE m.conversation_id=c.id AND m.event_type<>'PROCESSING_ACK' ORDER BY m.event_at DESC,m.id DESC LIMIT 1) latest ON true WHERE NOT c.bot_removed ORDER BY latest.event_at DESC NULLS LAST,c.updated_at DESC`)
}
func (a *App) getConversation(c *gin.Context) {
	rows, err := a.db.Query(c, `SELECT c.image_daily_limit AS "imageDailyLimit",c.id::text,c.bot_id::text AS "botId",c.service_id::text AS "serviceId",c.channel,c.platform_id AS "platformId",c.type,`+conversationIdentity+`,c.enabled,c.trigger_mode AS "triggerMode",c.context_limit AS "contextLimit",c.system_prompt AS "systemPrompt",c.chat_profile_id::text AS "chatProfileId",COALESCE(json_agg(ck.knowledge_base_id::text) FILTER(WHERE ck.knowledge_base_id IS NOT NULL),'[]') AS "knowledgeBaseIds" FROM conversations c LEFT JOIN conversation_knowledge_bases ck ON ck.conversation_id=c.id WHERE c.id=$1 GROUP BY c.id`, c.Param("id"))
	if err != nil {
		fail(c, 500, "database_error", "读取失败")
		return
	}
	data, _ := rowsJSON(rows)
	if len(data) == 0 {
		fail(c, 404, "not_found", "会话不存在")
		return
	}
	ok(c, data[0])
}
func (a *App) updateConversation(c *gin.Context) {
	var in struct {
		ImageDailyLimit  json.RawMessage `json:"imageDailyLimit"`
		Name             string          `json:"name"`
		TriggerMode      string          `json:"triggerMode"`
		SystemPrompt     string          `json:"systemPrompt"`
		ChatProfileID    string          `json:"chatProfileId"`
		Enabled          bool            `json:"enabled"`
		ContextLimit     int             `json:"contextLimit"`
		KnowledgeBaseIDs []string        `json:"knowledgeBaseIds"`
		ServiceID        *string         `json:"serviceId"`
	}
	if c.ShouldBindJSON(&in) != nil || in.ContextLimit < 1 || in.ContextLimit > 100 {
		fail(c, 400, "invalid_request", "会话配置无效")
		return
	}
	var limit *int32
	if in.ImageDailyLimit != nil && (json.Unmarshal(in.ImageDailyLimit, &limit) != nil || (limit != nil && *limit < 0)) {
		fail(c, 400, "invalid_request", "生图限额无效")
		return
	}
	if in.TriggerMode != "mention_only" && in.TriggerMode != "always" && in.TriggerMode != "smart" {
		fail(c, 400, "invalid_request", "触发模式无效")
		return
	}
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "更新失败")
		return
	}
	defer tx.Rollback(c)
	var profile any = nil
	if in.ChatProfileID != "" {
		profile = in.ChatProfileID
	}
	_, err = tx.Exec(c, "UPDATE conversations SET image_daily_limit=CASE WHEN $9 THEN $10::int ELSE image_daily_limit END,name=$1,enabled=$2,trigger_mode=$3,context_limit=$4,system_prompt=$5,chat_profile_id=$6,service_id=CASE WHEN $8::text IS NULL THEN service_id ELSE NULLIF($8,'')::uuid END,updated_at=now() WHERE id=$7", in.Name, in.Enabled, in.TriggerMode, in.ContextLimit, in.SystemPrompt, profile, c.Param("id"), in.ServiceID, in.ImageDailyLimit != nil, limit)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.ConstraintName == "conversation_bot_service" || pgErr.Code == "22P02") {
		fail(c, 400, "invalid_reference", "请选择当前机器人的客服")
		return
	}
	if err == nil {
		_, err = tx.Exec(c, "DELETE FROM conversation_knowledge_bases WHERE conversation_id=$1", c.Param("id"))
	}
	for _, id := range in.KnowledgeBaseIDs {
		if err == nil {
			_, err = tx.Exec(c, "INSERT INTO conversation_knowledge_bases VALUES($1,$2)", c.Param("id"), id)
		}
	}
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "更新失败")
		return
	}
	ok(c, gin.H{"id": c.Param("id")})
}

const messageUsageJoin = `LEFT JOIN LATERAL (
	SELECT sum(input_tokens) AS inputs,sum(output_tokens) AS outputs,sum(cached_input_tokens) AS reads,sum(cache_write_tokens) AS writes,
	sum(cost) AS cost
	FROM priced_model_calls WHERE message_id=m.id) usage ON true`
const messageUsageColumns = `coalesce(usage.inputs,0) AS "inputTokens",coalesce(usage.outputs,0) AS "outputTokens",
	coalesce(usage.reads,0) AS "cachedInputTokens",coalesce(usage.writes,0) AS "cacheWriteTokens",
	coalesce(usage.reads::float8/nullif(usage.inputs,0),0) AS "cacheHitRate",usage.cost AS "estimatedCost"`

const messageMentionText = `concat_ws(' ',m.content,(SELECT string_agg(x.content,' ') FROM messages x WHERE x.conversation_id=m.conversation_id AND x.direction='outbound' AND x.reply_to_message_id=m.platform_message_id))`

const messageSenderColumns = `m.bot_id::text AS "botId",m.conversation_id::text AS "conversationId",
 c.type AS "conversationType",m.sender_id AS "senderId",
 COALESCE((SELECT jsonb_agg(bu.id::text ORDER BY bu.id) FROM blocked_users bu
 WHERE bu.bot_id=m.bot_id AND bu.platform_user_id=m.sender_id
 AND (bu.conversation_id IS NULL OR bu.conversation_id=m.conversation_id)), '[]'::jsonb) AS "senderBlockedIds"`

func (a *App) listMessages(c *gin.Context) {
	limit := 50
	if v, _ := strconv.Atoi(c.DefaultQuery("limit", "50")); v > 0 && v <= 200 {
		limit = v
	}
	query := `SELECT message_mention_names(m,` + messageMentionText + `) AS "mentionNames",` + messageSenderColumns + `,m.id::text,m.content AS question,m.parts,m.sender_name AS "senderName",m.event_type AS "eventType",m.event_at AS "eventAt",
		m.platform_message_id AS "platformMessageId",c.name AS "conversationName",b.name AS "botName",
		COALESCE((SELECT string_agg(x.content, E'\n\n' ORDER BY x.event_at) FROM messages x WHERE x.conversation_id=m.conversation_id AND x.direction='outbound' AND x.event_type IN ('AI_RESPONSE','HUMAN_REPLY') AND x.status<>'merged' AND x.reply_to_message_id=m.platform_message_id),'') AS answer,om.id::text AS "answerId",COALESCE(om.parts,'[]'::jsonb) AS "answerParts",` + handoffStatusColumns + `, COALESCE(CASE WHEN t.status='merged' THEN t.status END,hs.state,ar.status,t.status,(SELECT status FROM group_reviews WHERE message_id=m.id)) AS status,
		COALESCE(mp.name,mp.model,'') AS model,` + messageUsageColumns + `,
		COALESCE(mc.latency_ms,0) AS "modelLatencyMs",COALESCE(ar.context_latency_ms,0) AS "contextLatencyMs",
		COALESCE(ar.retrieval_latency_ms,0) AS "retrievalLatencyMs",CASE WHEN jsonb_typeof(ar.retrieved_chunks)='array' THEN jsonb_array_length(ar.retrieved_chunks) ELSE 0 END AS "knowledgeHits",
		COALESCE(ot.status,CASE WHEN om.id IS NULL THEN 'pending' ELSE om.status END,'pending') AS "deliveryStatus",
		COALESCE(ot.last_error,'') AS "deliveryError",
		COALESCE(GREATEST(EXTRACT(EPOCH FROM (ot.updated_at-ot.created_at))*1000,0),0)::bigint AS "deliveryLatencyMs",
		COALESCE(GREATEST(EXTRACT(EPOCH FROM (COALESCE(ot.updated_at,ar.completed_at,t.updated_at)-t.created_at))*1000,0),0)::bigint AS "latencyMs",
		m.id::text AS "traceId"
	FROM messages m LEFT JOIN inbox_tasks t ON t.message_id=m.id
	JOIN conversations c ON c.id=m.conversation_id
	JOIN bots b ON b.id=m.bot_id
 ` + handoffStatusJoin + `
 LEFT JOIN LATERAL (SELECT * FROM agent_runs WHERE message_id=m.id ORDER BY started_at DESC LIMIT 1) ar ON true
	` + messageUsageJoin + `
 LEFT JOIN LATERAL (SELECT * FROM model_calls WHERE agent_run_id=ar.id ORDER BY (kind='chat') DESC,created_at DESC LIMIT 1) mc ON true
	LEFT JOIN model_profiles mp ON mp.id=mc.profile_id
	LEFT JOIN LATERAL (SELECT * FROM messages WHERE conversation_id=m.conversation_id AND direction='outbound' AND event_type<>'PROCESSING_ACK' AND status<>'merged' AND reply_to_message_id=m.platform_message_id ORDER BY created_at ASC LIMIT 1) om ON true
	LEFT JOIN outbox_tasks ot ON ot.message_id=om.id
	WHERE t.id IS NOT NULL OR EXISTS(SELECT FROM group_reviews WHERE message_id=m.id)
 ORDER BY m.event_at DESC LIMIT $1`
	rows, err := a.db.Query(c, query, limit)
	if err != nil {
		fail(c, 500, "database_error", "读取消息记录失败")
		return
	}
	items, err := rowsJSON(rows)
	if err != nil {
		fail(c, 500, "database_error", "读取消息记录失败")
		return
	}
	for _, item := range items {
		decorateMessageParts(item)
	}
	ok(c, items)
}
func (a *App) getMessage(c *gin.Context) {
	var msg map[string]any
	rows, err := a.db.Query(c, `SELECT message_mention_names(m,concat_ws(' ',`+messageMentionText+`,(SELECT string_agg(r.context_messages::text,' ') FROM agent_runs r WHERE r.message_id=m.id))) AS "mentionNames",`+messageSenderColumns+`,message_timeline(m) AS timeline,m.id::text,m.content AS question,m.parts,m.sender_name AS "senderName",m.event_type AS "eventType",m.event_at AS "eventAt",m.raw_event AS "rawEvent",
		m.platform_message_id AS "platformMessageId",c.name AS "conversationName",b.name AS "botName",
		COALESCE((SELECT string_agg(x.content, E'\n\n' ORDER BY x.event_at) FROM messages x WHERE x.conversation_id=m.conversation_id AND x.direction='outbound' AND x.event_type IN ('AI_RESPONSE','HUMAN_REPLY') AND x.status<>'merged' AND x.reply_to_message_id=m.platform_message_id),'') AS answer,om.id::text AS "answerId",COALESCE(om.parts,'[]'::jsonb) AS "answerParts",`+handoffStatusColumns+`, COALESCE(CASE WHEN t.status='merged' THEN t.status END,hs.state,ar.status,t.status,(SELECT status FROM group_reviews WHERE message_id=m.id)) AS status,
		COALESCE(mp.name,mp.model,'') AS model,`+messageUsageColumns+`,
		COALESCE(mc.latency_ms,0) AS "modelLatencyMs",COALESCE(ar.context_latency_ms,0) AS "contextLatencyMs",
		COALESCE(ar.retrieval_latency_ms,0) AS "retrievalLatencyMs",CASE WHEN jsonb_typeof(ar.retrieved_chunks)='array' THEN jsonb_array_length(ar.retrieved_chunks) ELSE 0 END AS "knowledgeHits",
		COALESCE(ot.status,CASE WHEN om.id IS NULL THEN 'pending' ELSE om.status END,'pending') AS "deliveryStatus",
		COALESCE(ot.last_error,'') AS "deliveryError",
		COALESCE(GREATEST(EXTRACT(EPOCH FROM (ot.updated_at-ot.created_at))*1000,0),0)::bigint AS "deliveryLatencyMs",
		COALESCE(GREATEST(EXTRACT(EPOCH FROM (COALESCE(ot.updated_at,ar.completed_at,t.updated_at)-t.created_at))*1000,0),0)::bigint AS "latencyMs",
		m.id::text AS "traceId"
	FROM messages m LEFT JOIN inbox_tasks t ON t.message_id=m.id
	JOIN conversations c ON c.id=m.conversation_id
	JOIN bots b ON b.id=m.bot_id
 `+handoffStatusJoin+`
 LEFT JOIN LATERAL (SELECT * FROM agent_runs WHERE message_id=m.id ORDER BY started_at DESC LIMIT 1) ar ON true
	`+messageUsageJoin+`
 LEFT JOIN LATERAL (SELECT * FROM model_calls WHERE agent_run_id=ar.id ORDER BY (kind='chat') DESC,created_at DESC LIMIT 1) mc ON true
	LEFT JOIN model_profiles mp ON mp.id=mc.profile_id
	LEFT JOIN LATERAL (SELECT * FROM messages WHERE conversation_id=m.conversation_id AND direction='outbound' AND event_type<>'PROCESSING_ACK' AND status<>'merged' AND reply_to_message_id=m.platform_message_id ORDER BY created_at ASC LIMIT 1) om ON true
	LEFT JOIN outbox_tasks ot ON ot.message_id=om.id
	WHERE m.id=$1`, c.Param("id"))
	if err == nil {
		d, _ := rowsJSON(rows)
		if len(d) > 0 {
			msg = d[0]
		}
	}
	if msg == nil {
		fail(c, 404, "not_found", "消息不存在")
		return
	}
	decorateMessageParts(msg)
	runs, _ := a.db.Query(c, `SELECT r.id::text,r.status,r.context_messages AS "contextMessages",r.rewritten_query AS "rewrittenQuery",r.steps,r.evaluation,r.retrieved_chunks AS "retrievedChunks",
		r.context_latency_ms AS "contextLatencyMs",r.retrieval_latency_ms AS "retrievalLatencyMs",r.error,r.started_at AS "startedAt",r.completed_at AS "completedAt",
		COALESCE(json_agg(json_build_object('kind',mc.kind,'profileName',COALESCE(mp.name,mp.model,''),'model',COALESCE(mp.model,''),'inputTokens',mc.input_tokens,'outputTokens',mc.output_tokens,'cachedInputTokens',mc.cached_input_tokens,'cacheWriteTokens',mc.cache_write_tokens,'cacheHitRate',coalesce(mc.cached_input_tokens::float8/nullif(mc.input_tokens,0),0),'images',mc.images,'estimatedCost',mc.cost,'latencyMs',mc.latency_ms,'error',mc.error) ORDER BY mc.created_at) FILTER(WHERE mc.id IS NOT NULL),'[]') AS calls
	FROM agent_runs r LEFT JOIN priced_model_calls mc ON mc.agent_run_id=r.id LEFT JOIN model_profiles mp ON mp.id=mc.profile_id
	WHERE r.message_id=$1 GROUP BY r.id ORDER BY r.started_at DESC`, c.Param("id"))
	rd, _ := rowsJSON(runs)
	msg["agentRuns"] = rd
	fb, _ := a.db.Query(c, `SELECT f.id::text,f.is_read AS "read",f.source,f.rating,f.comment,f.corrected_answer AS "correctedAnswer",f.created_at AS "createdAt" FROM message_feedback f JOIN messages o ON o.id=f.message_id
		JOIN messages i ON i.conversation_id=o.conversation_id AND i.platform_message_id=o.reply_to_message_id WHERE i.id=$1 ORDER BY f.created_at DESC`, c.Param("id"))
	fd, _ := rowsJSON(fb)
	msg["feedback"] = fd
	var replies []map[string]any
	if rr, err := a.db.Query(c, `SELECT x.id::text,x.event_type AS "eventType",x.content,x.status,x.event_at AS "eventAt" FROM messages i JOIN messages x ON x.conversation_id=i.conversation_id AND x.reply_to_message_id=i.platform_message_id AND x.direction='outbound' AND x.event_type<>'PROCESSING_ACK' WHERE i.id=$1 ORDER BY x.event_at`, c.Param("id")); err == nil {
		replies, _ = rowsJSON(rr)
	}
	msg["replies"] = replies
	var senderStaff bool
	_ = a.db.QueryRow(c, `SELECT COALESCE(cm.is_staff,false) FROM messages m LEFT JOIN conversation_members cm ON cm.conversation_id=m.conversation_id AND cm.platform_user_id=m.sender_id WHERE m.id=$1`, c.Param("id")).Scan(&senderStaff)
	msg["senderIsStaff"] = senderStaff
	ok(c, msg)
}

func (a *App) listKBs(c *gin.Context) {
	queryList(c, a, `SELECT k.id::text,(SELECT count(*) FROM knowledge_gaps g WHERE g.knowledge_base_id=k.id AND g.status='open') AS "openGaps",(SELECT count(*) FROM message_feedback f WHERE f.knowledge_base_id=k.id AND NOT f.is_read AND f.rating IN ('bad','wrong','unhelpful')) AS "unreadNegativeFeedback",k.name,k.description,k.embedding_profile_id::text AS "embeddingProfileId",k.embedding_model AS "embeddingModel",(SELECT count(*) FROM knowledge_entries e WHERE e.knowledge_base_id=k.id AND e.status='published') AS "entryCount",COUNT(DISTINCT d.id) AS "documentCount",COUNT(ch.id) AS "chunkCount",k.created_at AS "createdAt" FROM knowledge_bases k LEFT JOIN knowledge_documents d ON d.knowledge_base_id=k.id LEFT JOIN knowledge_chunks ch ON ch.document_id=d.id GROUP BY k.id ORDER BY k.created_at DESC`)
}
func (a *App) createKB(c *gin.Context) {
	var in struct {
		Name               string `json:"name"`
		Description        string `json:"description"`
		EmbeddingProfileID string `json:"embeddingProfileId"`
	}
	if c.ShouldBindJSON(&in) != nil || in.Name == "" || in.EmbeddingProfileID == "" {
		fail(c, 400, "invalid_request", "名称和 Embedding 配置必填")
		return
	}
	var model, id string
	err := a.db.QueryRow(c, "SELECT model FROM model_profiles WHERE id=$1 AND kind='embedding'", in.EmbeddingProfileID).Scan(&model)
	if err == nil {
		err = a.db.QueryRow(c, "INSERT INTO knowledge_bases(name,description,embedding_profile_id,embedding_model) VALUES($1,$2,$3,$4) RETURNING id::text", in.Name, in.Description, in.EmbeddingProfileID, model).Scan(&id)
	}
	if err != nil {
		fail(c, 400, "invalid_embedding_profile", "Embedding 配置无效")
		return
	}
	created(c, gin.H{"id": id})
}
func (a *App) getKB(c *gin.Context) {
	rows, err := a.db.Query(c, `SELECT k.id::text,k.name,k.description,k.embedding_profile_id::text AS "embeddingProfileId",k.embedding_model AS "embeddingModel",
		COALESCE((SELECT json_agg(json_build_object('id',s.id,'name',s.name) ORDER BY s.name,s.id)
        FROM bot_services s LEFT JOIN system_settings g ON g.key='agent_settings'
        WHERE COALESCE(s.settings->'knowledgeBaseIds',g.value->'knowledgeBaseIds') ? k.id::text
           OR COALESCE(s.settings->>'learningKnowledgeBaseId',g.value->>'learningKnowledgeBaseId')=k.id::text),'[]') AS "boundAgents",
        COALESCE((SELECT json_agg(json_build_object('id',c.id,'name',COALESCE(NULLIF(c.name,''),c.platform_id)) ORDER BY c.name,c.id)
        FROM conversations c LEFT JOIN bot_services s ON s.id=c.service_id LEFT JOIN system_settings g ON g.key='agent_settings'
        WHERE COALESCE(s.settings->'knowledgeBaseIds',g.value->'knowledgeBaseIds') ? k.id::text
           OR COALESCE(s.settings->>'learningKnowledgeBaseId',g.value->>'learningKnowledgeBaseId')=k.id::text
           OR EXISTS (SELECT FROM conversation_knowledge_bases b WHERE b.conversation_id=c.id AND b.knowledge_base_id=k.id)),'[]') AS "boundConversations",
        count(d.id) AS "documentCount",(SELECT count(*) FROM knowledge_entries e WHERE e.knowledge_base_id=k.id AND e.status='published') AS "entryCount",COALESCE(json_agg(json_build_object('id',d.id::text,'name',d.name,'status',d.status,'sizeBytes',d.size_bytes,'chunkCount',COALESCE(dc.chunk_count,0),'imageCount',dc.image_count,'lastError',d.last_error,'createdAt',d.created_at,'updatedAt',d.updated_at) ORDER BY d.created_at DESC) FILTER(WHERE d.id IS NOT NULL),'[]') AS documents
	FROM knowledge_bases k LEFT JOIN knowledge_documents d ON d.knowledge_base_id=k.id
	LEFT JOIN LATERAL (SELECT count(*) AS chunk_count,count(ci.chunk_id) AS image_count FROM knowledge_chunks c LEFT JOIN knowledge_chunk_images ci ON ci.chunk_id=c.id WHERE c.document_id=d.id) dc ON true
	WHERE k.id=$1 GROUP BY k.id`, c.Param("id"))
	if err != nil {
		fail(c, 500, "database_error", "读取失败")
		return
	}
	d, err := rowsJSON(rows)
	if err != nil {
		fail(c, 500, "database_error", "读取知识库详情失败")
		return
	}
	if len(d) == 0 {
		fail(c, 404, "not_found", "知识库不存在")
		return
	}
	ok(c, d[0])
}
func (a *App) uploadDocument(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 11<<20)
	file, err := c.FormFile("file")
	if err != nil || file.Size > 10<<20 || !knowledge.Supported(file.Filename) {
		fail(c, 400, "invalid_file", "请选择不超过 10 MiB 的文件，支持 "+knowledge.Formats)
		return
	}
	if _, present := c.GetPostForm("description"); present {
		fail(c, 400, "invalid_description", "上传接口不接受图片说明")
		return
	}
	if knowledge.IsImage(file.Filename) {
		var supported bool
		err = a.db.QueryRow(c, `SELECT EXISTS(SELECT FROM model_profiles p WHERE p.id=k.embedding_profile_id AND p.kind='embedding' AND p.provider='google' AND p.model='gemini-embedding-2') FROM knowledge_bases k WHERE k.id=$1`, c.Param("id")).Scan(&supported)
		if errors.Is(err, pgx.ErrNoRows) {
			fail(c, 404, "not_found", "知识库不存在")
			return
		}
		if err != nil {
			fail(c, 500, "database_error", "读取知识库模型配置失败")
			return
		}
		if !supported {
			fail(c, 400, "image_model_required", knowledge.ImageModelRequired)
			return
		}
	}
	src, err := file.Open()
	if err != nil {
		fail(c, 400, "invalid_file", "读取文件失败")
		return
	}
	defer src.Close()
	data, err := io.ReadAll(io.LimitReader(src, (10<<20)+1))
	if err != nil || len(data) > 10<<20 || int64(len(data)) != file.Size {
		fail(c, 400, "invalid_file", "文件读取不完整或超过10MB")
		return
	}
	key, err := a.files.Save(c, file.Filename, data)
	if err != nil {
		fail(c, 400, "invalid_file", err.Error())
		return
	}
	var id string
	err = a.db.QueryRow(c, "INSERT INTO knowledge_documents(knowledge_base_id,name,storage_key,content_type,size_bytes) VALUES($1,$2,$3,$4,$5) RETURNING id::text", c.Param("id"), file.Filename, key, file.Header.Get("Content-Type"), len(data)).Scan(&id)
	if err != nil {
		_ = a.files.Delete(c, key)
		fail(c, 500, "database_error", "保存文档失败")
		return
	}
	created(c, gin.H{"id": id, "status": "pending"})
}
func (a *App) retryDocument(c *gin.Context) {
	_, err := a.db.Exec(c, "UPDATE knowledge_documents SET dismissed_at=NULL,status='pending',attempts=0,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE id=$1 AND knowledge_base_id=$2", c.Param("documentId"), c.Param("id"))
	if err != nil {
		fail(c, 500, "database_error", "重试失败")
		return
	}
	ok(c, gin.H{"queued": true})
}
func (a *App) deleteDocument(c *gin.Context) {
	var key string
	err := a.db.QueryRow(c, "DELETE FROM knowledge_documents WHERE id=$1 AND knowledge_base_id=$2 RETURNING storage_key", c.Param("documentId"), c.Param("id")).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "文档不存在")
		return
	}
	if err != nil {
		fail(c, 500, "database_error", "删除文档失败")
		return
	}
	if err := a.files.Delete(c, key); err != nil {
		a.log.Warn("delete document file", "key", key, "error", err)
	}
	ok(c, gin.H{"deleted": true})
}
func (a *App) deleteDocumentIndex(c *gin.Context) {
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "删除索引失败")
		return
	}
	defer tx.Rollback(c)
	var status string
	if err = tx.QueryRow(c, "SELECT status FROM knowledge_documents WHERE id=$1 AND knowledge_base_id=$2 FOR UPDATE", c.Param("documentId"), c.Param("id")).Scan(&status); errors.Is(err, pgx.ErrNoRows) {
		fail(c, 404, "not_found", "文档不存在")
		return
	} else if err != nil {
		fail(c, 500, "database_error", "删除索引失败")
		return
	}
	if status == "processing" {
		fail(c, 409, "document_processing", "文档正在索引，请稍后再删除索引")
		return
	}
	if _, err = tx.Exec(c, "DELETE FROM knowledge_chunks WHERE document_id=$1", c.Param("documentId")); err == nil {
		_, err = tx.Exec(c, "UPDATE knowledge_documents SET status='unindexed',attempts=0,last_error=NULL,updated_at=now() WHERE id=$1", c.Param("documentId"))
	}
	if err != nil || tx.Commit(c) != nil {
		fail(c, 500, "database_error", "删除索引失败")
		return
	}
	ok(c, gin.H{"deleted": true})
}
func vectorLiteral(v []float32) string {
	p := make([]string, len(v))
	for i, x := range v {
		p[i] = strconv.FormatFloat(float64(x), 'g', -1, 32)
	}
	return "[" + strings.Join(p, ",") + "]"
}
func (a *App) searchKB(c *gin.Context) {
	var in struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Query) == "" {
		fail(c, 400, "invalid_request", "查询内容必填")
		return
	}
	if in.Limit <= 0 || in.Limit > 20 {
		in.Limit = 8
	}
	var pid string
	if err := a.db.QueryRow(c, "SELECT embedding_profile_id::text FROM knowledge_bases WHERE id=$1", c.Param("id")).Scan(&pid); err != nil {
		fail(c, 404, "not_found", "知识库不存在")
		return
	}
	cl, _, err := a.loadModel(c, pid)
	if err != nil {
		fail(c, 400, "model_error", "Embedding 配置不可用")
		return
	}
	vs, err := a.embed(c, cl, pid, "embedding", []string{in.Query})
	if err != nil {
		fail(c, 502, "model_error", err.Error())
		return
	}
	rows, err := a.db.Query(c, `SELECT id::text,document_id::text,content,EXISTS(SELECT FROM knowledge_chunk_images ci WHERE ci.chunk_id=knowledge_chunks.id) AS "hasImage",1-(embedding <=> $1::vector) AS score FROM knowledge_chunks WHERE knowledge_base_id=$2 AND EXISTS(SELECT FROM knowledge_bases WHERE id=$2 AND embedding_profile_id=$4) ORDER BY embedding <=> $1::vector LIMIT $3`, vectorLiteral(vs[0]), c.Param("id"), in.Limit, pid)
	if err != nil {
		fail(c, 500, "search_error", "检索失败")
		return
	}
	d, _ := rowsJSON(rows)
	ok(c, d)
}
func (a *App) retryFailedTasks(c *gin.Context) {
	tx, err := a.db.Begin(c)
	if err != nil {
		fail(c, 500, "database_error", "重新入队失败")
		return
	}
	defer tx.Rollback(c)
	inbox, err := tx.Exec(c, `UPDATE inbox_tasks t SET status='pending',attempts=0,next_attempt_at=now(),locked_at=NULL,last_error=NULL,updated_at=now()
		FROM messages m WHERE t.message_id=m.id AND t.status='failed' AND t.dismissed_at IS NULL AND m.context_excluded=false`)
	if err != nil {
		fail(c, 500, "database_error", "重新入队失败")
		return
	}
	outbox, err := tx.Exec(c, `UPDATE outbox_tasks t SET status='pending',attempts=0,next_attempt_at=now(),locked_at=NULL,last_error=NULL,updated_at=now()
		FROM messages m WHERE t.message_id=m.id AND t.status='failed' AND t.dismissed_at IS NULL AND m.event_type<>'PROCESSING_ACK' AND (m.event_type<>'HUMAN_REPLY' OR m.handoff_id IS NULL OR EXISTS(SELECT FROM handoffs h WHERE h.id=m.handoff_id AND h.status='processing'))`)
	if err != nil {
		fail(c, 500, "database_error", "重新入队失败")
		return
	}
	documents, err := tx.Exec(c, "UPDATE knowledge_documents SET status='pending',attempts=0,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE status='failed' AND dismissed_at IS NULL")
	if err == nil && tx.Commit(c) == nil {
		ok(c, gin.H{"queued": inbox.RowsAffected() + outbox.RowsAffected() + documents.RowsAffected()})
		return
	}
	fail(c, 500, "database_error", "重新入队失败")
}

func (a *App) qqWebhook(c *gin.Context) {
	botID := c.Param("botId")
	body, err := c.GetRawData()
	if err != nil {
		fail(c, 400, "invalid_body", "无法读取请求")
		return
	}
	var appID, secretEnc string
	if err = a.db.QueryRow(c, "SELECT app_id,app_secret_enc FROM bots WHERE id=$1 AND enabled", botID).Scan(&appID, &secretEnc); err != nil {
		fail(c, 404, "bot_not_found", "机器人不存在或已停用")
		return
	}
	secret, err := a.cipher.Decrypt(secretEnc)
	if err != nil {
		fail(c, 500, "secret_error", "机器人凭证不可用")
		return
	}
	env, msg, err := qq.Parse(body, botID)
	if err != nil {
		fail(c, 400, "invalid_event", err.Error())
		return
	}
	if env.Op == 13 {
		if headerAppID := c.GetHeader("X-Bot-Appid"); headerAppID != "" && headerAppID != appID {
			fail(c, 401, "invalid_app_id", "机器人 AppID 不匹配")
			return
		}
		var d qq.Validation
		_ = json.Unmarshal(env.D, &d)
		c.JSON(200, qq.ValidationResponse(secret, d))
		return
	}
	ts := c.GetHeader("X-Signature-Timestamp")
	sig := c.GetHeader("X-Signature-Ed25519")
	if !qq.VerifySignature(secret, ts, sig, body) {
		fail(c, 401, "invalid_signature", "签名校验失败")
		return
	}
	if err = a.persistInbound(c, botID, env, msg, body); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		a.log.Error("persist QQ event", "error", err)
		fail(c, 500, "database_error", "事件入库失败")
		return
	}
	c.JSON(200, gin.H{"op": 12})
}
func (a *App) persistInbound(ctx context.Context, botID string, env qq.Envelope, msg *domain.InboundMessage, raw []byte) error {
	settings, err := a.runtimeSettings(ctx)
	if err != nil {
		return fmt.Errorf("读取运行时设置失败: %w", err)
	}
	tx, err := a.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, "INSERT INTO webhook_events(channel,bot_id,platform_event_id,event_type,raw_event) VALUES('qq',$1,$2,$3,$4::jsonb) ON CONFLICT DO NOTHING", botID, env.ID, env.T, string(raw))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tx.Commit(ctx)
	}
	_, _ = tx.Exec(ctx, "UPDATE bots SET last_event_at=now(),status='online' WHERE id=$1", botID)
	if msg == nil {
		if env.T == "INTERACTION_CREATE" {
			var interaction struct {
				ID   string
				Type int
			}
			if json.Unmarshal(env.D, &interaction) == nil && interaction.ID != "" && (interaction.Type == 11 || interaction.Type == 12) {
				if _, err = tx.Exec(ctx, `INSERT INTO agent_jobs(kind,payload,dedupe_key) VALUES('qq_interaction_ack',jsonb_build_object('bot',$1::text,'interaction',$2::text),$3) ON CONFLICT DO NOTHING`, botID, interaction.ID, "qq_ack:"+botID+":"+interaction.ID); err != nil {
					return err
				}
			}
		}
		if err := a.persistQQGroupEvent(ctx, tx, botID, env, settings.DefaultContextLimit); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	conversationName := inboundConversationName(msg)
	var conversationEnabled, guardEnabled bool
	var cid, triggerMode string
	err = tx.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,name,context_limit,membership_event_at,trigger_mode) VALUES('qq',$1,$2,$3,$4,$5,CASE WHEN $3='group' THEN $6::timestamptz END,CASE WHEN $3='group' THEN 'smart' ELSE 'mention_only' END) ON CONFLICT(channel,bot_id,platform_id) DO UPDATE SET updated_at=now(),
		bot_removed=conversations.bot_removed AND conversations.membership_event_at>EXCLUDED.membership_event_at,
		membership_event_at=GREATEST(conversations.membership_event_at,EXCLUDED.membership_event_at),
		name=CASE WHEN conversations.name='' OR conversations.name=conversations.platform_id OR conversations.name LIKE 'QQ 群聊 · %' OR conversations.name LIKE 'QQ 用户 · %' THEN EXCLUDED.name ELSE conversations.name END RETURNING id::text,trigger_mode,enabled AND NOT bot_removed,enabled AND NOT bot_removed AND COALESCE((guard_policy->>'enabled')::bool,false)`, msg.BotID, msg.ConversationID, msg.ConversationType, conversationName, settings.DefaultContextLimit, msg.EventTime).Scan(&cid, &triggerMode, &conversationEnabled, &guardEnabled)
	if err != nil {
		return err
	}
	if msg.ConversationType == "group" {
		_, err = tx.Exec(ctx, `INSERT INTO agent_jobs(kind,payload,dedupe_key) SELECT 'qq_group_identity',jsonb_build_object('conversationId',c.id),'qq_group_identity:'||c.id::text FROM conversations c WHERE c.id=$1 AND NOT c.bot_removed AND `+needsGroupInfo+` ON CONFLICT DO NOTHING`, cid)
		if err != nil {
			return err
		}
	}
	if msg.SenderID != "" {
		_, err = tx.Exec(ctx, `INSERT INTO conversation_members(conversation_id,platform_user_id,display_name,active,last_seen_at,qq_avatar,qq_union_id) VALUES($1,$2,$3,true,$4,COALESCE($5::jsonb#>>'{d,author,avatar}',''),COALESCE($5::jsonb#>>'{d,author,union_openid}','')) ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET qq_avatar=COALESCE(NULLIF(EXCLUDED.qq_avatar,''),conversation_members.qq_avatar),qq_union_id=COALESCE(NULLIF(EXCLUDED.qq_union_id,''),conversation_members.qq_union_id),display_name=CASE WHEN EXCLUDED.display_name<>'' THEN EXCLUDED.display_name ELSE conversation_members.display_name END,active=true,membership_updated_at=clock_timestamp(),last_seen_at=EXCLUDED.last_seen_at`, cid, msg.SenderID, msg.SenderName, msg.EventTime, string(msg.Raw))
		if err != nil {
			return err
		}
	}
	parts, _ := json.Marshal(msg.Parts)
	var mid string
	err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,sender_id,sender_name,platform_message_id,event_type,content,parts,raw_event,event_at,reply_deadline)
		VALUES('qq',$1,$2,'inbound',$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11)
		ON CONFLICT(channel,bot_id,platform_message_id) WHERE platform_message_id IS NOT NULL DO UPDATE SET
			event_type=CASE WHEN EXCLUDED.event_type='GROUP_AT_MESSAGE_CREATE' OR ($12 AND messages.event_type<>'GROUP_AT_MESSAGE_CREATE') THEN EXCLUDED.event_type ELSE messages.event_type END,
			content=CASE WHEN EXCLUDED.event_type='GROUP_AT_MESSAGE_CREATE' OR ($12 AND messages.event_type<>'GROUP_AT_MESSAGE_CREATE') THEN EXCLUDED.content ELSE messages.content END,
			parts=CASE
                WHEN messages.parts @> '[{"type":"image"}]'::jsonb THEN messages.parts
                WHEN EXCLUDED.parts @> '[{"type":"image"}]'::jsonb OR EXCLUDED.event_type='GROUP_AT_MESSAGE_CREATE' OR ($12 AND messages.event_type<>'GROUP_AT_MESSAGE_CREATE') THEN EXCLUDED.parts
                ELSE messages.parts END,
			raw_event=CASE WHEN EXCLUDED.event_type='GROUP_AT_MESSAGE_CREATE' OR ($12 AND messages.event_type<>'GROUP_AT_MESSAGE_CREATE') THEN EXCLUDED.raw_event ELSE messages.raw_event END,
			reply_deadline=CASE WHEN EXCLUDED.event_type='GROUP_AT_MESSAGE_CREATE' OR ($12 AND messages.event_type<>'GROUP_AT_MESSAGE_CREATE') THEN EXCLUDED.reply_deadline ELSE messages.reply_deadline END
		RETURNING id::text`, msg.BotID, cid, msg.SenderID, msg.SenderName, msg.PlatformMessageID, msg.EventType, msg.Text, string(parts), string(msg.Raw), msg.EventTime, msg.ReplyDeadline, msg.MentionsBot).Scan(&mid)
	if err != nil {
		return err
	}
	if index := qq.ReferenceIndex(msg.Raw); index != "" {
		if _, err = tx.Exec(ctx, "UPDATE messages SET qq_ref_idx=$2 WHERE id=$1 AND qq_ref_idx=''", mid, index); err != nil {
			return err
		}
	}
	var senderIsStaff bool
	if msg.ConversationType == "group" && msg.SenderID != "" {
		_ = tx.QueryRow(ctx, "SELECT is_staff FROM conversation_members WHERE conversation_id=$1 AND platform_user_id=$2", cid, msg.SenderID).Scan(&senderIsStaff)
	}
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blocked_users WHERE bot_id=$1 AND platform_user_id=$2 AND (conversation_id IS NULL OR conversation_id=$3))`, msg.BotID, msg.SenderID, cid).Scan(&blocked); err != nil {
		return err
	}
	if blocked {
		if _, err = tx.Exec(ctx, `UPDATE messages SET context_excluded=true,context_exclusion_reason='blacklist' WHERE id=$1`, mid); err != nil {
			return err
		}
		if msg.ConversationType == "group" {
			if err = a.queueBlockedKick(ctx, tx, msg.BotID, cid, msg.SenderID); err != nil {
				return err
			}
		}
	}
	if !blocked {
		handled, e := a.operatorCommand(ctx, tx, msg, cid, mid)
		if e != nil {
			return e
		}
		if handled {
			return tx.Commit(ctx)
		}
	}
	needsReview := !blocked && guardEnabled && msg.ConversationType == "group"
	queue := qq.ShouldQueue(msg.EventType, triggerMode, msg.MentionsBot)
	if conversationEnabled && !blocked && !needsReview {
		linked, e := linkHandoffTurn(ctx, tx, msg.BotID, cid, msg.SenderID, msg.SenderName, mid, "", 0)
		if e != nil {
			return e
		}
		if linked {
			_, e = tx.Exec(ctx, "INSERT INTO inbox_tasks(message_id,status) VALUES($1,'handoff') ON CONFLICT DO NOTHING", mid)
			if e != nil {
				return e
			}
			return tx.Commit(ctx)
		}
	}
	if !blocked && conversationEnabled {
		queue, err = joinReplyWindow(ctx, tx, cid, msg.SenderID, mid, queue)
		if err != nil {
			return err
		}
	}
	if needsReview {
		err = a.queueReview(ctx, tx, mid, queue)
		if err == nil {
			_, err = tx.Exec(ctx, "UPDATE messages SET context_excluded=true,context_exclusion_reason='pending_review' WHERE id=$1 AND NOT context_excluded AND EXISTS(SELECT 1 FROM group_reviews WHERE message_id=$1 AND status='pending')", mid)
		}
	} else if !blocked && queue {
		_, err = tx.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1) ON CONFLICT DO NOTHING", mid)
	}
	if err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	if !blocked && !needsReview && senderIsStaff && strings.TrimSpace(msg.Text) != "" {
		a.enqueueJob(ctx, "staff_answer", map[string]any{"messageId": mid}, "staff_answer:"+mid)
	}
	return nil
}

func inboundConversationName(msg *domain.InboundMessage) string {
	if name := strings.TrimSpace(msg.ConversationName); name != "" {
		return name
	}
	prefix := "QQ 群聊 · "
	if msg.ConversationType == "private" {
		prefix = "QQ 用户 · "
	}
	return prefix + shortPlatformID(msg.ConversationID)
}

func shortPlatformID(id string) string {
	runes := []rune(id)
	if len(runes) > 6 {
		runes = runes[len(runes)-6:]
	}
	return string(runes)
}

func (a *App) persistQQGroupEvent(ctx context.Context, tx pgx.Tx, botID string, env qq.Envelope, contextLimit int) error {
	if env.T == "GROUP_JOIN_REQUEST" {
		return a.queueJoinApproval(ctx, tx, botID, env.D)
	}
	if env.T != "GROUP_ADD_ROBOT" && env.T != "GROUP_DEL_ROBOT" && env.T != "GROUP_MEMBER_ADD" && env.T != "GROUP_MEMBER_REMOVE" && env.T != "GROUP_MSG_RECEIVE" && env.T != "GROUP_MSG_REJECT" {
		return nil
	}
	var event struct {
		Timestamp    int64  `json:"timestamp"`
		Union        string `json:"union_group_openid"`
		User         string `json:"user_openid"`
		GroupOpenID  string `json:"group_openid"`
		MemberOpenID string `json:"member_openid"`
		OpMemberID   string `json:"op_member_openid"`
	}
	if err := json.Unmarshal(env.D, &event); err != nil || event.GroupOpenID == "" {
		return err
	}
	var conversationID string
	err := tx.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,name,context_limit,trigger_mode) VALUES('qq',$1,$2,'group',$3,$4,'smart') ON CONFLICT(channel,bot_id,platform_id) DO UPDATE SET updated_at=now() RETURNING id::text`, botID, event.GroupOpenID, "QQ 群聊 · "+shortPlatformID(event.GroupOpenID), contextLimit).Scan(&conversationID)
	if err != nil {
		return err
	}
	occurred := time.Now()
	if event.Timestamp > 0 {
		occurred = time.Unix(event.Timestamp, 0)
	}
	if event.Union != "" {
		if _, err = tx.Exec(ctx, "UPDATE conversations SET qq_group_info=qq_group_info||jsonb_build_object('union_group_openid',$2::text) WHERE id=$1", conversationID, event.Union); err != nil {
			return err
		}
	}
	if env.T == "GROUP_DEL_ROBOT" || env.T == "GROUP_ADD_ROBOT" {
		removed := env.T == "GROUP_DEL_ROBOT"
		_, err = tx.Exec(ctx, `UPDATE conversations SET bot_removed=$2,membership_event_at=$3 WHERE id=$1
			AND (membership_event_at IS NULL OR membership_event_at<=$3)`, conversationID, removed, occurred)
		return err
	}
	memberID := event.MemberOpenID
	if memberID == "" && (env.T == "GROUP_MEMBER_ADD" || env.T == "GROUP_MEMBER_REMOVE") {
		memberID = event.User
		if memberID == "" {
			memberID = event.OpMemberID
		}
	}
	if memberID != "" {
		active := env.T != "GROUP_MEMBER_REMOVE"
		_, err = tx.Exec(ctx, `INSERT INTO conversation_members(conversation_id,platform_user_id,active,last_seen_at,membership_updated_at,qq_joined_at) VALUES($1,$2,$3,$4,$4,CASE WHEN $5 THEN $4::timestamptz END) ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET active=EXCLUDED.active,membership_updated_at=EXCLUDED.membership_updated_at,last_seen_at=GREATEST(conversation_members.last_seen_at,EXCLUDED.last_seen_at),qq_joined_at=COALESCE(EXCLUDED.qq_joined_at,conversation_members.qq_joined_at) WHERE conversation_members.membership_updated_at<=EXCLUDED.membership_updated_at`, conversationID, memberID, active, occurred, env.T == "GROUP_MEMBER_ADD")
	}
	if err == nil && env.T == "GROUP_MEMBER_ADD" && event.MemberOpenID != "" {
		err = a.queueBlockedKick(ctx, tx, botID, conversationID, memberID)
	}
	return err
}

func nullString(v string) any {
	if v == "" {
		return nil
	}
	return v
}
func parseUUID(v string) error { _, err := uuid.Parse(v); return err }
func jsonValue(v any) []byte   { b, _ := json.Marshal(v); return b }

var _ = fmt.Sprintf
var _ = knowledge.Chunk
