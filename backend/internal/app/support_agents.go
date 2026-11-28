package app

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"ai-bot/backend/internal/agent"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (a *App) supportRoutes(r *gin.RouterGroup) {
	r.GET("/personalities", func(c *gin.Context) {
		queryList(c, a, "SELECT id::text,name,prompt FROM personality_presets ORDER BY name,id")
	})
	r.GET("/support-agents", func(c *gin.Context) {
		queryList(c, a, `SELECT id::text,name,instructions,personality_id::text AS "personalityId" FROM support_agents ORDER BY name,id`)
	})
	r.GET("/bots/:id/services/:item/handoff-hours", a.handoffHoursAPI)
	r.PUT("/bots/:id/services/:item/handoff-hours", a.handoffHoursAPI)
	r.GET("/bots/:id/services", a.listServices)
	r.DELETE("/bots/:id/services/:item", a.deleteBotService)
	r.GET("/bots/:id/services/:item/live-data", a.getLiveData)
	r.POST("/bots/:id/services/:item/live-data/refresh", a.refreshLiveDataNow)
	for _, method := range []string{"POST", "PUT"} {
		suffix := ""
		if method == "PUT" {
			suffix = "/:item"
		}
		r.Handle(method, "/personalities"+suffix, a.savePersonality)
		r.Handle(method, "/support-agents"+suffix, a.saveSupportAgent)
		r.Handle(method, "/bots/:id/services"+suffix, a.saveBotService)
	}
}

func saveSupportRecord(c *gin.Context, a *App, insert, update string, args ...any) {
	query := insert
	if c.Request.Method == "PUT" {
		query = update
		args = append(args, c.Param("item"))
	}
	var id string
	err := a.db.QueryRow(c, query, args...).Scan(&id)
	var pg *pgconn.PgError
	switch {
	case err == nil:
		ok(c, gin.H{"id": id})
	case errors.Is(err, pgx.ErrNoRows):
		fail(c, 404, "not_found", "配置不存在")
	case errors.As(err, &pg) && (pg.Code == "23503" || pg.Code == "22P02"):
		fail(c, 400, "invalid_reference", "所选配置不存在，请刷新后重试")
	default:
		fail(c, 500, "database_error", "保存配置失败")
	}
}

func (a *App) savePersonality(c *gin.Context) {
	var in struct{ Name, Prompt string }
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Prompt) == "" {
		fail(c, 400, "invalid_request", "请填写性格名称和说话风格")
		return
	}
	saveSupportRecord(c, a, "INSERT INTO personality_presets(name,prompt) VALUES($1,$2) RETURNING id::text", "UPDATE personality_presets SET name=$1,prompt=$2 WHERE id=$3 RETURNING id::text", strings.TrimSpace(in.Name), strings.TrimSpace(in.Prompt))
}

func (a *App) saveSupportAgent(c *gin.Context) {
	var in struct{ Name, Instructions, PersonalityID string }
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || uuid.Validate(in.PersonalityID) != nil {
		fail(c, 400, "invalid_request", "请填写智能体名称并选择性格")
		return
	}
	saveSupportRecord(c, a, "INSERT INTO support_agents(name,instructions,personality_id) VALUES($1,$2,$3) RETURNING id::text", "UPDATE support_agents SET name=$1,instructions=$2,personality_id=$3 WHERE id=$4 RETURNING id::text", strings.TrimSpace(in.Name), strings.TrimSpace(in.Instructions), in.PersonalityID)
}

func (a *App) deleteBotService(c *gin.Context) {
	botID, serviceID := c.Param("id"), c.Param("item")
	if uuid.Validate(botID) != nil || uuid.Validate(serviceID) != nil {
		fail(c, 404, "not_found", "AI 客服不存在")
		return
	}
	result, err := a.db.Exec(c, "DELETE FROM bot_services WHERE bot_id=$1 AND id=$2", botID, serviceID)
	switch {
	case err != nil:
		fail(c, 500, "database_error", "删除 AI 客服失败")
	case result.RowsAffected() == 0:
		fail(c, 404, "not_found", "AI 客服不存在")
	default:
		ok(c, gin.H{"deleted": true})
	}
}

func (a *App) saveBotService(c *gin.Context) {
	var in struct {
		Name, AgentID                                                     string
		Instructions                                                      *string
		Settings                                                          map[string]json.RawMessage
		LiveDataAccessToken                                               *string
		ChatProfileID, SimpleProfileID, DecisionProfileID, ImageProfileID *string
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || (in.AgentID != "" && uuid.Validate(in.AgentID) != nil) {
		fail(c, 400, "invalid_request", "请填写客服名称或检查旧智能体 ID")
		return
	}
	for _, pid := range []*string{in.ChatProfileID, in.SimpleProfileID, in.DecisionProfileID, in.ImageProfileID} {
		if pid == nil || *pid == "" {
			continue
		}
		kind := "chat"
		if pid == in.ImageProfileID {
			kind = "image"
		}
		var valid bool
		if uuid.Validate(*pid) != nil {
			fail(c, 400, "invalid_model", "请选择有效的模型")
			return
		}
		if err := a.db.QueryRow(c, "SELECT EXISTS(SELECT 1 FROM model_profiles WHERE id=$1 AND enabled AND (kind=$2 OR ($3 AND kind='decision')))", *pid, kind, pid == in.DecisionProfileID).Scan(&valid); err != nil {
			fail(c, 500, "database_error", "读取模型失败")
			return
		}
		if !valid {
			fail(c, 400, "invalid_model", "请选择类型匹配且已启用的模型")
			return
		}
	}
	patch, err := a.servicePatch(c, in.Settings, in.LiveDataAccessToken)
	if err != nil {
		fail(c, 400, "invalid_settings", err.Error())
		return
	}
	saveSupportRecord(c, a,
		"INSERT INTO bot_services(bot_id,name,agent_id,chat_profile_id,simple_profile_id,decision_profile_id,instructions,settings,image_profile_id) VALUES($1,$2,$3,NULLIF($4::text,'')::uuid,NULLIF($5::text,'')::uuid,NULLIF($6::text,'')::uuid,$7,$8::jsonb,NULLIF($9::text,'')::uuid) RETURNING id::text",
		`UPDATE bot_services SET name=$2,agent_id=COALESCE($3,agent_id),chat_profile_id=CASE WHEN $4::text IS NULL THEN chat_profile_id ELSE NULLIF($4,'')::uuid END,simple_profile_id=CASE WHEN $5::text IS NULL THEN simple_profile_id ELSE NULLIF($5,'')::uuid END,decision_profile_id=CASE WHEN $6::text IS NULL THEN decision_profile_id ELSE NULLIF($6,'')::uuid END,image_profile_id=CASE WHEN $9::text IS NULL THEN image_profile_id ELSE NULLIF($9,'')::uuid END,instructions=COALESCE($7,instructions),settings=settings || $8::jsonb || CASE WHEN $8::jsonb ? 'liveData' THEN jsonb_build_object('liveData',COALESCE(settings->'liveData','{}') || ($8::jsonb->'liveData')) ELSE '{}'::jsonb END WHERE bot_id=$1 AND id=$10 RETURNING id::text`,
		c.Param("id"), strings.TrimSpace(in.Name), nullString(in.AgentID), in.ChatProfileID, in.SimpleProfileID, in.DecisionProfileID, in.Instructions, string(patch), in.ImageProfileID)
}

func (a *App) conversationAgent(ctx context.Context, id string, s agent.Settings) (agent.Settings, string, string, error) {
	var name, style, instructions, profile, simple, decision string
	var raw []byte
	err := a.db.QueryRow(ctx, "SELECT COALESCE(service_name,''),COALESCE(prompt,''),COALESCE(svc.instructions,cfg.instructions,''),COALESCE(cfg.chat_profile_id::text,''),COALESCE(cfg.simple_profile_id::text,''),COALESCE(cfg.decision_profile_id::text,'') ,coalesce((SELECT id::text FROM model_profiles WHERE id=svc.image_profile_id AND enabled AND kind='image'),''),coalesce(svc.settings,'{}') FROM conversation_support_config cfg JOIN conversations c ON c.id=cfg.id LEFT JOIN bot_services svc ON svc.id=c.service_id WHERE c.id=$1", id).Scan(&name, &style, &instructions, &profile, &simple, &decision, &s.ImageProfileID, &raw)
	if err != nil {
		return s, "", "", err
	}
	if name != "" {
		s.PersonaName = name
		if style != "" {
			s.StylePrompt = style
		}
	}
	if err = json.Unmarshal(raw, &s); err != nil {
		return s, "", "", err
	}
	s.Normalize()
	if simple != "" {
		s.AuxChatProfileID = simple
	}
	if decision != "" {
		s.DecisionProfileID = decision
	}
	if profile == "" {
		profile = a.defaultChatProfile(ctx)
	}
	return s, instructions, profile, nil
}

func (a *App) servicePatch(c *gin.Context, patch map[string]json.RawMessage, token *string) ([]byte, error) {
	if patch == nil {
		patch = map[string]json.RawMessage{}
	}
	for k := range patch {
		if !serviceField(k) {
			return nil, errors.New("未知客服设置：" + k)
		}
	}
	var err error
	s := a.agentSettings(c)
	if c.Request.Method == "PUT" {
		var stored []byte
		if err = a.db.QueryRow(c, "SELECT settings FROM bot_services WHERE bot_id=$1 AND id=$2", c.Param("id"), c.Param("item")).Scan(&stored); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(stored, &s); err != nil {
			return nil, err
		}
	}
	if patch["liveData"] != nil || token != nil {
		live := map[string]json.RawMessage{}
		if raw := patch["liveData"]; raw != nil {
			if err := json.Unmarshal(raw, &live); err != nil || live == nil {
				return nil, errors.New("网站配置格式错误")
			}
		}
		// A credential belongs to the configured origin. Preserve it only for that site.
		if raw, ok := live["baseUrl"]; ok && token == nil {
			var base string
			if err = json.Unmarshal(raw, &base); err != nil {
				return nil, err
			}
			if strings.TrimRight(strings.TrimSpace(base), "/") != s.LiveData.BaseURL {
				blank := ""
				token = &blank
			}
		}
		if _, ok := live["baseUrl"]; !ok {
			live["baseUrl"], _ = json.Marshal(s.LiveData.BaseURL)
		}
		if token == nil {
			live["accessTokenEnc"], _ = json.Marshal(s.LiveData.AccessTokenEnc)
		} else {
			delete(live, "accessTokenEnc")
		}
		for _, obsolete := range []string{"refreshSeconds", "syncAnnouncements", "announcementKnowledgeBaseId"} {
			delete(live, obsolete)
		}
		delete(live, "hasAccessToken")
		if token != nil {
			enc := ""
			var err error
			if strings.TrimSpace(*token) != "" {
				enc, err = a.cipher.Encrypt(strings.TrimSpace(*token))
				if err != nil {
					return nil, err
				}
			}
			live["accessTokenEnc"], _ = json.Marshal(enc)
		}
		patch["liveData"], _ = json.Marshal(live)
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	if s.LiveData.StatusPageURL != "" {
		if err := validateStatusURL(s.LiveData.StatusPageURL); err != nil {
			return nil, errors.New("状态页需要公网 HTTP(S) 地址")
		}
	}
	for _, ids := range [][]string{s.SkillIDs, s.KnowledgeBaseIDs} {
		for _, id := range ids {
			if uuid.Validate(id) != nil {
				return nil, errors.New("技能或知识库 ID 无效")
			}
		}
	}
	return raw, s.Validate()
}

func (a *App) listServices(c *gin.Context) {
	defaults := a.agentSettings(c)
	rows, err := a.db.Query(c, `SELECT s.id::text,s.name,coalesce(s.agent_id::text,''),coalesce(s.chat_profile_id::text,''),coalesce(s.simple_profile_id::text,''),coalesce(s.decision_profile_id::text,''),coalesce(s.instructions,a.instructions,''),coalesce(p.prompt,''),coalesce(s.image_profile_id::text,''),s.settings FROM bot_services s LEFT JOIN support_agents a ON a.id=s.agent_id LEFT JOIN personality_presets p ON p.id=a.personality_id WHERE s.bot_id=$1 ORDER BY s.name,s.id`, c.Param("id"))
	if err != nil {
		fail(c, 500, "database_error", "读取客服失败")
		return
	}
	defer rows.Close()
	out := []gin.H{}
	for rows.Next() {
		var id, name, aid, chat, simple, decision, role, style, imageID string
		var raw []byte
		if err = rows.Scan(&id, &name, &aid, &chat, &simple, &decision, &role, &style, &imageID, &raw); err != nil {
			break
		}
		s := defaults
		s.Examples = slices.Clone(defaults.Examples)
		s.SkillIDs = slices.Clone(s.SkillIDs)
		s.ToolNames = slices.Clone(s.ToolNames)
		s.KnowledgeBaseIDs = slices.Clone(s.KnowledgeBaseIDs)
		s.Companion.PersonalityTraits = slices.Clone(defaults.Companion.PersonalityTraits)
		s.Companion.Catchphrases = slices.Clone(defaults.Companion.Catchphrases)
		if style != "" {
			s.StylePrompt = style
		}
		if err = json.Unmarshal(raw, &s); err != nil {
			break
		}
		s.Normalize()
		s.LiveData.HasAccessToken = s.LiveData.AccessTokenEnc != ""
		out = append(out, gin.H{"id": id, "name": name, "agentId": aid, "instructions": role, "chatProfileId": chat, "imageProfileId": imageID, "simpleProfileId": simple, "decisionProfileId": decision, "settings": serviceFields(s)})
	}
	if err != nil || rows.Err() != nil {
		fail(c, 500, "database_error", "读取客服失败")
		return
	}
	ok(c, out)
}

func serviceField(key string) bool {
	switch key {
	case "replyQuietSeconds", "replyMaxWaitSeconds", "stylePrompt", "examples", "companion", "liveData", "toolsEnabled", "toolNames", "skillIds", "knowledgeBaseIds", "maxToolSteps", "handoffPauseMinutes", "evaluationEnabled", "staffMiningEnabled", "autoPublishStaff", "autoPublishMinConfidence", "learningKnowledgeBaseId":
		return true
	}
	return false
}
func serviceFields(s agent.Settings) map[string]json.RawMessage {
	raw, _ := json.Marshal(publicAgentSettings(s))
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for key := range fields {
		if !serviceField(key) {
			delete(fields, key)
		}
	}
	return fields
}
