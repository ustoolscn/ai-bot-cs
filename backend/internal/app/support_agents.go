package app

import (
	"context"
	"errors"
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
	r.GET("/bots/:id/services", func(c *gin.Context) {
		queryList(c, a, `SELECT id::text,name,agent_id::text AS "agentId",COALESCE(chat_profile_id::text,'') AS "chatProfileId",COALESCE(simple_profile_id::text,'') AS "simpleProfileId",COALESCE(decision_profile_id::text,'') AS "decisionProfileId" FROM bot_services WHERE bot_id=$1 ORDER BY name,id`, c.Param("id"))
	})
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

func (a *App) saveBotService(c *gin.Context) {
	var in struct {
		Name, AgentID                                     string
		ChatProfileID, SimpleProfileID, DecisionProfileID *string
	}
	if c.ShouldBindJSON(&in) != nil || strings.TrimSpace(in.Name) == "" || uuid.Validate(in.AgentID) != nil {
		fail(c, 400, "invalid_request", "请填写客服名称并选择智能体")
		return
	}
	for _, pid := range []*string{in.ChatProfileID, in.SimpleProfileID, in.DecisionProfileID} {
		if pid == nil || *pid == "" {
			continue
		}
		var valid bool
		if uuid.Validate(*pid) != nil {
			fail(c, 400, "invalid_model", "请选择有效的对话模型")
			return
		}
		if err := a.db.QueryRow(c, "SELECT EXISTS(SELECT 1 FROM model_profiles WHERE id=$1 AND enabled AND kind='chat')", *pid).Scan(&valid); err != nil {
			fail(c, 500, "database_error", "读取模型失败")
			return
		}
		if !valid {
			fail(c, 400, "invalid_model", "请选择已启用的对话模型")
			return
		}
	}
	saveSupportRecord(c, a,
		"INSERT INTO bot_services(bot_id,name,agent_id,chat_profile_id,simple_profile_id,decision_profile_id) VALUES($1,$2,$3,NULLIF($4::text,'')::uuid,NULLIF($5::text,'')::uuid,NULLIF($6::text,'')::uuid) RETURNING id::text",
		"UPDATE bot_services SET name=$2,agent_id=$3,chat_profile_id=CASE WHEN $4::text IS NULL THEN chat_profile_id ELSE NULLIF($4,'')::uuid END,simple_profile_id=CASE WHEN $5::text IS NULL THEN simple_profile_id ELSE NULLIF($5,'')::uuid END,decision_profile_id=CASE WHEN $6::text IS NULL THEN decision_profile_id ELSE NULLIF($6,'')::uuid END WHERE bot_id=$1 AND id=$7 RETURNING id::text",
		c.Param("id"), strings.TrimSpace(in.Name), in.AgentID, in.ChatProfileID, in.SimpleProfileID, in.DecisionProfileID)
}

func (a *App) conversationAgent(ctx context.Context, id string, s agent.Settings) (agent.Settings, string, string, error) {
	var name, style, instructions, profile, simple, decision string
	err := a.db.QueryRow(ctx, "SELECT COALESCE(service_name,''),COALESCE(prompt,''),COALESCE(instructions,''),COALESCE(chat_profile_id::text,''),COALESCE(simple_profile_id::text,''),COALESCE(decision_profile_id::text,'') FROM conversation_support_config WHERE id=$1", id).Scan(&name, &style, &instructions, &profile, &simple, &decision)
	if err != nil {
		return s, "", "", err
	}
	if name != "" {
		s.PersonaName = name
		s.StylePrompt = style
	}
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
