package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type App struct {
	cfg    config.Config
	db     *pgxpool.Pool
	cipher *secure.Cipher
	files  *storage.Local
	log    *slog.Logger
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
	a := &App{cfg: cfg, db: pool, cipher: c, files: f, log: logger}
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
		"UPDATE inbox_tasks SET status='pending',locked_at=NULL,next_attempt_at=now(),updated_at=now() WHERE status='processing'",
		"UPDATE outbox_tasks SET status='pending',locked_at=NULL,next_attempt_at=now(),updated_at=now() WHERE status='processing'",
		"UPDATE knowledge_documents SET status='pending',next_attempt_at=now(),updated_at=now() WHERE status='processing'",
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
	r.POST("/callbacks/qq/:botId", a.qqWebhook)
	api := r.Group("/api")
	api.POST("/auth/login", a.login)
	api.POST("/auth/logout", a.logout)
	auth := api.Group("")
	auth.Use(a.requireAuth())
	auth.GET("/auth/me", a.me)
	auth.PUT("/auth/password", a.changePassword)
	auth.GET("/bots", a.listBots)
	auth.POST("/bots", a.saveBot)
	auth.PUT("/bots/:id", a.saveBot)
	auth.DELETE("/bots/:id", a.deleteBot)
	auth.GET("/model-profiles", a.listModels)
	auth.POST("/model-profiles", a.saveModel)
	auth.PUT("/model-profiles/:id", a.saveModel)
	auth.DELETE("/model-profiles/:id", a.deleteModel)
	auth.POST("/model-profiles/:id/test", a.testModel)
	auth.GET("/conversations", a.listConversations)
	auth.GET("/conversations/:id", a.getConversation)
	auth.PUT("/conversations/:id", a.updateConversation)
	auth.GET("/messages", a.listMessages)
	auth.GET("/messages/:id", a.getMessage)
	auth.GET("/knowledge-bases", a.listKBs)
	auth.POST("/knowledge-bases", a.createKB)
	auth.GET("/knowledge-bases/:id", a.getKB)
	auth.DELETE("/knowledge-bases/:id", a.deleteKB)
	auth.POST("/knowledge-bases/:id/documents", a.uploadDocument)
	auth.DELETE("/knowledge-bases/:id/documents/:documentId", a.deleteDocument)
	auth.POST("/knowledge-bases/:id/documents/:documentId/retry", a.retryDocument)
	auth.POST("/knowledge-bases/:id/search", a.searchKB)
	auth.GET("/system/overview", a.overview)
	auth.GET("/system/settings", a.getSystemSettings)
	auth.PUT("/system/settings", a.putSystemSettings)
	auth.POST("/system/retry-failed", a.retryFailedTasks)
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
	queryList(c, a, `SELECT id::text,name,channel,app_id AS "appId",enabled,status,last_event_at AS "lastEventAt",created_at AS "createdAt",app_secret_enc<>'' AS "hasSecret",COALESCE(default_chat_profile_id::text,'') AS "defaultChatProfileId" FROM bots ORDER BY created_at DESC`)
}
func (a *App) saveBot(c *gin.Context) {
	var in struct {
		Name           string `json:"name"`
		AppID          string `json:"appId"`
		AppSecret      string `json:"appSecret"`
		ModelProfileID string `json:"modelProfileId"`
		Enabled        *bool  `json:"enabled"`
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
		err := a.db.QueryRow(c, "INSERT INTO bots(name,app_id,app_secret_enc,enabled,default_chat_profile_id) VALUES($1,$2,$3,$4,$5) RETURNING id::text", in.Name, in.AppID, enc, enabled, nullString(in.ModelProfileID)).Scan(&id)
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
		_, err = a.db.Exec(c, "UPDATE bots SET name=$1,app_id=$2,app_secret_enc=$3,enabled=$4,default_chat_profile_id=$5,updated_at=now() WHERE id=$6", in.Name, in.AppID, enc, enabled, nullString(in.ModelProfileID), id)
	} else {
		_, err = a.db.Exec(c, "UPDATE bots SET name=$1,app_id=$2,enabled=$3,default_chat_profile_id=$4,updated_at=now() WHERE id=$5", in.Name, in.AppID, enabled, nullString(in.ModelProfileID), id)
	}
	if err != nil {
		fail(c, 400, "invalid_model_profile", "默认对话模型无效")
		return
	}
	ok(c, gin.H{"id": id})
}
func (a *App) deleteBot(c *gin.Context) {
	_, err := a.db.Exec(c, "DELETE FROM bots WHERE id=$1", c.Param("id"))
	if err != nil {
		fail(c, 409, "delete_failed", "机器人存在关联数据或无法删除")
		return
	}
	ok(c, gin.H{"deleted": true})
}

type modelInput struct {
	Name          string         `json:"name"`
	Kind          string         `json:"kind"`
	BaseURL       string         `json:"baseUrl"`
	APIKey        string         `json:"apiKey"`
	Model         string         `json:"model"`
	Dimension     *int           `json:"dimension"`
	Enabled       *bool          `json:"enabled"`
	IsDefault     bool           `json:"isDefault"`
	WebSearchMode string         `json:"webSearchMode"`
	ExtraBody     map[string]any `json:"extraBody"`
}

func (a *App) listModels(c *gin.Context) {
	queryList(c, a, `SELECT id::text,name,kind,base_url AS "baseUrl",model,dimension,enabled,is_default AS "isDefault",api_key_enc<>'' AS "hasApiKey",web_search_mode AS "webSearchMode",extra_body AS "extraBody",created_at AS "createdAt" FROM model_profiles ORDER BY kind,name`)
}
func (a *App) saveModel(c *gin.Context) {
	var in modelInput
	if c.ShouldBindJSON(&in) != nil || in.Name == "" || in.BaseURL == "" || in.Model == "" || (in.Kind != "chat" && in.Kind != "embedding") {
		fail(c, 400, "invalid_request", "模型配置不完整")
		return
	}
	if in.Kind == "chat" {
		in.Dimension = nil
		if in.WebSearchMode == "" {
			in.WebSearchMode = "disabled"
		}
		if in.WebSearchMode != "disabled" && in.WebSearchMode != "qwen" && in.WebSearchMode != "openai" && in.WebSearchMode != "responses" && in.WebSearchMode != "custom" {
			fail(c, 400, "invalid_request", "联网搜索模式无效")
			return
		}
	} else {
		in.WebSearchMode = "disabled"
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
	if in.IsDefault {
		_, _ = tx.Exec(c, "UPDATE model_profiles SET is_default=false WHERE kind=$1", in.Kind)
	}
	if id == "" {
		if in.APIKey == "" {
			fail(c, 400, "invalid_request", "API Key 必填")
			return
		}
		enc, _ := a.cipher.Encrypt(in.APIKey)
		err = tx.QueryRow(c, "INSERT INTO model_profiles(name,kind,base_url,api_key_enc,model,dimension,enabled,is_default,web_search_mode,extra_body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id::text", in.Name, in.Kind, strings.TrimRight(in.BaseURL, "/"), enc, in.Model, in.Dimension, enabled, in.IsDefault, in.WebSearchMode, in.ExtraBody).Scan(&id)
	} else if in.APIKey != "" {
		enc, _ := a.cipher.Encrypt(in.APIKey)
		_, err = tx.Exec(c, "UPDATE model_profiles SET name=$1,kind=$2,base_url=$3,api_key_enc=$4,model=$5,dimension=$6,enabled=$7,is_default=$8,web_search_mode=$9,extra_body=$10,updated_at=now() WHERE id=$11", in.Name, in.Kind, strings.TrimRight(in.BaseURL, "/"), enc, in.Model, in.Dimension, enabled, in.IsDefault, in.WebSearchMode, in.ExtraBody, id)
	} else {
		_, err = tx.Exec(c, "UPDATE model_profiles SET name=$1,kind=$2,base_url=$3,model=$4,dimension=$5,enabled=$6,is_default=$7,web_search_mode=$8,extra_body=$9,updated_at=now() WHERE id=$10", in.Name, in.Kind, strings.TrimRight(in.BaseURL, "/"), in.Model, in.Dimension, enabled, in.IsDefault, in.WebSearchMode, in.ExtraBody, id)
	}
	if err != nil {
		fail(c, 500, "database_error", "保存模型失败")
		return
	}
	if err = tx.Commit(c); err != nil {
		fail(c, 500, "database_error", "保存模型失败")
		return
	}
	ok(c, gin.H{"id": id})
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
	var base, keyEnc, model, kind, webSearchMode string
	var dimension *int
	var extraBodyRaw []byte
	err := a.db.QueryRow(ctx, "SELECT base_url,api_key_enc,model,kind,dimension,web_search_mode,extra_body FROM model_profiles WHERE id=$1 AND enabled", id).Scan(&base, &keyEnc, &model, &kind, &dimension, &webSearchMode, &extraBodyRaw)
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
		Input        string `json:"input"`
		SystemPrompt string `json:"systemPrompt"`
	}
	if err := c.ShouldBindJSON(&in); err != nil && !errors.Is(err, io.EOF) {
		fail(c, 400, "invalid_request", "测试参数格式无效")
		return
	}
	cl, kind, model, dimension, err := a.loadModelDetails(c, c.Param("id"))
	if err != nil {
		fail(c, 404, "not_found", "模型不存在")
		return
	}
	started := time.Now()
	if kind == "embedding" {
		if strings.TrimSpace(in.Input) == "" {
			in.Input = "连接测试"
		}
		var vectors [][]float32
		vectors, err = cl.Embed(c, []string{in.Input})
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
			in.Input = "请只回复：连接测试成功"
		}
		messages := make([]domain.ChatMessage, 0, 2)
		if strings.TrimSpace(in.SystemPrompt) != "" {
			messages = append(messages, domain.ChatMessage{Role: "system", Content: in.SystemPrompt})
		}
		messages = append(messages, domain.ChatMessage{Role: "user", Content: in.Input})
		var result domain.ChatResult
		result, err = cl.Chat(c, messages)
		if err != nil {
			fail(c, 502, "model_error", err.Error())
			return
		}
		ok(c, gin.H{"kind": kind, "model": model, "content": result.Content, "inputTokens": result.InputTokens, "outputTokens": result.OutputTokens, "latencyMs": time.Since(started).Milliseconds()})
		return
	}
}

func (a *App) listConversations(c *gin.Context) {
	queryList(c, a, `SELECT c.id::text,c.channel,c.platform_id AS "platformId",c.type,c.name,c.enabled,c.trigger_mode AS "triggerMode",c.context_limit AS "contextLimit",c.system_prompt AS "systemPrompt",c.chat_profile_id::text AS "chatProfileId",b.name AS "botName",c.updated_at AS "updatedAt",COALESCE(ms.message_count,0) AS "messageCount",CASE WHEN c.type='private' THEN GREATEST(COALESCE(cm.member_count,0),1) ELSE COALESCE(cm.member_count,0) END AS "memberCount",COALESCE(ms.has_full_message_events,false) AS "hasFullMessageEvents" FROM conversations c JOIN bots b ON b.id=c.bot_id LEFT JOIN LATERAL (SELECT count(*) AS message_count,bool_or(event_type='GROUP_MESSAGE_CREATE') AS has_full_message_events FROM messages WHERE conversation_id=c.id) ms ON true LEFT JOIN LATERAL (SELECT count(*) AS member_count FROM conversation_members WHERE conversation_id=c.id AND active) cm ON true ORDER BY c.updated_at DESC`)
}
func (a *App) getConversation(c *gin.Context) {
	rows, err := a.db.Query(c, `SELECT c.id::text,c.channel,c.platform_id AS "platformId",c.type,c.name,c.enabled,c.trigger_mode AS "triggerMode",c.context_limit AS "contextLimit",c.system_prompt AS "systemPrompt",c.chat_profile_id::text AS "chatProfileId",COALESCE(json_agg(ck.knowledge_base_id::text) FILTER(WHERE ck.knowledge_base_id IS NOT NULL),'[]') AS "knowledgeBaseIds" FROM conversations c LEFT JOIN conversation_knowledge_bases ck ON ck.conversation_id=c.id WHERE c.id=$1 GROUP BY c.id`, c.Param("id"))
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
		Name             string   `json:"name"`
		TriggerMode      string   `json:"triggerMode"`
		SystemPrompt     string   `json:"systemPrompt"`
		ChatProfileID    string   `json:"chatProfileId"`
		Enabled          bool     `json:"enabled"`
		ContextLimit     int      `json:"contextLimit"`
		KnowledgeBaseIDs []string `json:"knowledgeBaseIds"`
	}
	if c.ShouldBindJSON(&in) != nil || in.ContextLimit < 1 || in.ContextLimit > 100 {
		fail(c, 400, "invalid_request", "会话配置无效")
		return
	}
	if in.TriggerMode != "mention_only" && in.TriggerMode != "always" {
		fail(c, 400, "invalid_request", "触发模式无效")
		return
	}
	tx, _ := a.db.Begin(c)
	defer tx.Rollback(c)
	var profile any = nil
	if in.ChatProfileID != "" {
		profile = in.ChatProfileID
	}
	_, err := tx.Exec(c, "UPDATE conversations SET name=$1,enabled=$2,trigger_mode=$3,context_limit=$4,system_prompt=$5,chat_profile_id=$6,updated_at=now() WHERE id=$7", in.Name, in.Enabled, in.TriggerMode, in.ContextLimit, in.SystemPrompt, profile, c.Param("id"))
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

func (a *App) listMessages(c *gin.Context) {
	limit := 50
	if v, _ := strconv.Atoi(c.DefaultQuery("limit", "50")); v > 0 && v <= 200 {
		limit = v
	}
	query := `SELECT m.id::text,m.direction,m.content,m.sender_name AS "senderName",m.event_type AS "eventType",m.status,m.event_at AS "eventAt",m.platform_message_id AS "platformMessageId",m.reply_to_message_id AS "replyToMessageId",c.name AS "conversationName",b.name AS "botName" FROM messages m JOIN conversations c ON c.id=m.conversation_id JOIN bots b ON b.id=m.bot_id ORDER BY m.event_at DESC LIMIT $1`
	queryList(c, a, query, limit)
}
func (a *App) getMessage(c *gin.Context) {
	var msg map[string]any
	rows, err := a.db.Query(c, `SELECT m.id::text,m.direction,m.content,m.sender_id AS "senderId",m.sender_name AS "senderName",m.event_type AS "eventType",m.status,m.event_at AS "eventAt",m.raw_event AS "rawEvent",m.platform_message_id AS "platformMessageId",m.reply_to_message_id AS "replyToMessageId",c.name AS "conversationName",b.name AS "botName" FROM messages m JOIN conversations c ON c.id=m.conversation_id JOIN bots b ON b.id=m.bot_id WHERE m.id=$1`, c.Param("id"))
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
	runs, _ := a.db.Query(c, `SELECT r.id::text,r.status,r.retrieved_chunks AS "retrievedChunks",r.error,r.started_at AS "startedAt",r.completed_at AS "completedAt",COALESCE(json_agg(json_build_object('kind',mc.kind,'inputTokens',mc.input_tokens,'outputTokens',mc.output_tokens,'latencyMs',mc.latency_ms,'error',mc.error)) FILTER(WHERE mc.id IS NOT NULL),'[]') AS calls FROM agent_runs r LEFT JOIN model_calls mc ON mc.agent_run_id=r.id WHERE r.message_id=$1 GROUP BY r.id`, c.Param("id"))
	rd, _ := rowsJSON(runs)
	msg["agentRuns"] = rd
	ok(c, msg)
}

func (a *App) listKBs(c *gin.Context) {
	queryList(c, a, `SELECT k.id::text,k.name,k.description,k.embedding_profile_id::text AS "embeddingProfileId",k.embedding_model AS "embeddingModel",COUNT(DISTINCT d.id) AS "documentCount",COUNT(ch.id) AS "chunkCount",k.created_at AS "createdAt" FROM knowledge_bases k LEFT JOIN knowledge_documents d ON d.knowledge_base_id=k.id LEFT JOIN knowledge_chunks ch ON ch.document_id=d.id GROUP BY k.id ORDER BY k.created_at DESC`)
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
	rows, err := a.db.Query(c, `SELECT k.id::text,k.name,k.description,k.embedding_profile_id::text AS "embeddingProfileId",k.embedding_model AS "embeddingModel",COALESCE(json_agg(json_build_object('id',d.id::text,'name',d.name,'status',d.status,'sizeBytes',d.size_bytes,'lastError',d.last_error,'createdAt',d.created_at) ORDER BY d.created_at DESC) FILTER(WHERE d.id IS NOT NULL),'[]') AS documents FROM knowledge_bases k LEFT JOIN knowledge_documents d ON d.knowledge_base_id=k.id WHERE k.id=$1 GROUP BY k.id`, c.Param("id"))
	if err != nil {
		fail(c, 500, "database_error", "读取失败")
		return
	}
	d, _ := rowsJSON(rows)
	if len(d) == 0 {
		fail(c, 404, "not_found", "知识库不存在")
		return
	}
	ok(c, d[0])
}
func (a *App) deleteKB(c *gin.Context) {
	_, err := a.db.Exec(c, "DELETE FROM knowledge_bases WHERE id=$1", c.Param("id"))
	if err != nil {
		fail(c, 409, "delete_failed", "知识库正在使用中")
		return
	}
	ok(c, gin.H{"deleted": true})
}
func (a *App) uploadDocument(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil || file.Size > 10<<20 {
		fail(c, 400, "invalid_file", "请选择不超过10MB的 TXT/Markdown 文件")
		return
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
	_, err := a.db.Exec(c, "UPDATE knowledge_documents SET status='pending',attempts=0,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE id=$1 AND knowledge_base_id=$2", c.Param("documentId"), c.Param("id"))
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
	vs, err := cl.Embed(c, []string{in.Query})
	if err != nil {
		fail(c, 502, "model_error", err.Error())
		return
	}
	rows, err := a.db.Query(c, `SELECT id::text,document_id::text,content,1-(embedding <=> $1::vector) AS score FROM knowledge_chunks WHERE knowledge_base_id=$2 ORDER BY embedding <=> $1::vector LIMIT $3`, vectorLiteral(vs[0]), c.Param("id"), in.Limit)
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
	inbox, err := tx.Exec(c, "UPDATE inbox_tasks SET status='pending',attempts=0,next_attempt_at=now(),locked_at=NULL,last_error=NULL,updated_at=now() WHERE status='failed'")
	if err != nil {
		fail(c, 500, "database_error", "重新入队失败")
		return
	}
	outbox, err := tx.Exec(c, "UPDATE outbox_tasks SET status='pending',attempts=0,next_attempt_at=now(),locked_at=NULL,last_error=NULL,updated_at=now() WHERE status='failed'")
	if err != nil {
		fail(c, 500, "database_error", "重新入队失败")
		return
	}
	documents, err := tx.Exec(c, "UPDATE knowledge_documents SET status='pending',attempts=0,next_attempt_at=now(),last_error=NULL,updated_at=now() WHERE status='failed'")
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
		if err := a.persistQQGroupEvent(ctx, tx, botID, env, settings.DefaultContextLimit); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	conversationName := inboundConversationName(msg)
	var cid, triggerMode string
	err = tx.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,name,context_limit) VALUES('qq',$1,$2,$3,$4,$5) ON CONFLICT(channel,bot_id,platform_id) DO UPDATE SET updated_at=now(),name=CASE WHEN conversations.name='' OR conversations.name=conversations.platform_id OR conversations.name LIKE 'QQ 群聊 · %' OR conversations.name LIKE 'QQ 用户 · %' THEN EXCLUDED.name ELSE conversations.name END RETURNING id::text,trigger_mode`, msg.BotID, msg.ConversationID, msg.ConversationType, conversationName, settings.DefaultContextLimit).Scan(&cid, &triggerMode)
	if err != nil {
		return err
	}
	if msg.SenderID != "" {
		_, err = tx.Exec(ctx, `INSERT INTO conversation_members(conversation_id,platform_user_id,display_name,active,last_seen_at) VALUES($1,$2,$3,true,$4) ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET display_name=CASE WHEN EXCLUDED.display_name<>'' THEN EXCLUDED.display_name ELSE conversation_members.display_name END,active=true,last_seen_at=EXCLUDED.last_seen_at`, cid, msg.SenderID, msg.SenderName, msg.EventTime)
		if err != nil {
			return err
		}
	}
	parts, _ := json.Marshal(msg.Parts)
	var mid string
	err = tx.QueryRow(ctx, `INSERT INTO messages(channel,bot_id,conversation_id,direction,sender_id,sender_name,platform_message_id,event_type,content,parts,raw_event,event_at,reply_deadline) VALUES('qq',$1,$2,'inbound',$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10,$11) ON CONFLICT DO NOTHING RETURNING id::text`, msg.BotID, cid, msg.SenderID, msg.SenderName, msg.PlatformMessageID, msg.EventType, msg.Text, string(parts), string(msg.Raw), msg.EventTime, msg.ReplyDeadline).Scan(&mid)
	if errors.Is(err, pgx.ErrNoRows) {
		return tx.Commit(ctx)
	}
	if err != nil {
		return err
	}
	var blocked bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM blocked_users WHERE bot_id=$1 AND platform_user_id=$2 AND (conversation_id IS NULL OR conversation_id=$3))`, msg.BotID, msg.SenderID, cid).Scan(&blocked); err != nil {
		return err
	}
	if !blocked && qq.ShouldQueue(msg.EventType, triggerMode) {
		_, err = tx.Exec(ctx, "INSERT INTO inbox_tasks(message_id) VALUES($1) ON CONFLICT DO NOTHING", mid)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
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
	if env.T != "GROUP_ADD_ROBOT" && env.T != "GROUP_DEL_ROBOT" && env.T != "GROUP_MEMBER_ADD" && env.T != "GROUP_MEMBER_REMOVE" && env.T != "GROUP_MSG_RECEIVE" && env.T != "GROUP_MSG_REJECT" {
		return nil
	}
	var event struct {
		GroupOpenID  string `json:"group_openid"`
		MemberOpenID string `json:"member_openid"`
		OpMemberID   string `json:"op_member_openid"`
	}
	if err := json.Unmarshal(env.D, &event); err != nil || event.GroupOpenID == "" {
		return err
	}
	var conversationID string
	err := tx.QueryRow(ctx, `INSERT INTO conversations(channel,bot_id,platform_id,type,name,context_limit) VALUES('qq',$1,$2,'group',$3,$4) ON CONFLICT(channel,bot_id,platform_id) DO UPDATE SET updated_at=now() RETURNING id::text`, botID, event.GroupOpenID, "QQ 群聊 · "+shortPlatformID(event.GroupOpenID), contextLimit).Scan(&conversationID)
	if err != nil {
		return err
	}
	if env.T == "GROUP_DEL_ROBOT" {
		_, err = tx.Exec(ctx, "UPDATE conversations SET enabled=false,updated_at=now() WHERE id=$1", conversationID)
		return err
	}
	memberID := event.MemberOpenID
	if memberID == "" && (env.T == "GROUP_MEMBER_ADD" || env.T == "GROUP_MEMBER_REMOVE") {
		memberID = event.OpMemberID
	}
	if memberID != "" {
		active := env.T != "GROUP_MEMBER_REMOVE"
		_, err = tx.Exec(ctx, `INSERT INTO conversation_members(conversation_id,platform_user_id,active,last_seen_at) VALUES($1,$2,$3,now()) ON CONFLICT(conversation_id,platform_user_id) DO UPDATE SET active=EXCLUDED.active,last_seen_at=now()`, conversationID, memberID, active)
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
