package agent

import (
	"fmt"

	"strings"
)

// Example is a few-shot sample of how the bot should talk.
type Example struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Settings are the global agent settings stored in system_settings.agent_settings.
type Settings struct {
	ToolNames        []string `json:"toolNames"`
	SkillIDs         []string `json:"skillIds"`
	KnowledgeBaseIDs []string `json:"knowledgeBaseIds"`
	// Persona & style
	PersonaName         string    `json:"personaName"`
	StylePrompt         string    `json:"stylePrompt"`
	Examples            []Example `json:"examples"`
	GroupReplyChars     int       `json:"groupReplyChars"`
	PrivateReplyChars   int       `json:"privateReplyChars"`
	SplitLongReplies    bool      `json:"splitLongReplies"`
	MaxReplyParts       int       `json:"maxReplyParts"`
	SanitizeMarkdown    bool      `json:"sanitizeMarkdown"`
	MaxOutputTokens     int       `json:"maxOutputTokens"`
	ReplyQuietSeconds   int       `json:"replyQuietSeconds"`
	ReplyMaxWaitSeconds int       `json:"replyMaxWaitSeconds"`
	// Context & memory
	HistoryTokenBudget int    `json:"historyTokenBudget"`
	GroupThreadMode    bool   `json:"groupThreadMode"`
	RewriteQuery       bool   `json:"rewriteQuery"`
	SummaryEnabled     bool   `json:"summaryEnabled"`
	MemoryEnabled      bool   `json:"memoryEnabled"`
	MaxMemories        int    `json:"maxMemories"`
	DecisionProfileID  string `json:"decisionProfileId"`
	AuxChatProfileID   string `json:"auxChatProfileId"`
	EmbeddingProfileID string `json:"embeddingProfileId"`
	// Tools
	ToolsEnabled        bool `json:"toolsEnabled"`
	MaxToolSteps        int  `json:"maxToolSteps"`
	HandoffPauseMinutes int  `json:"handoffPauseMinutes"`
	// Learning
	EvaluationEnabled        bool    `json:"evaluationEnabled"`
	StaffMiningEnabled       bool    `json:"staffMiningEnabled"`
	AutoPublishStaff         bool    `json:"autoPublishStaff"`
	AutoPublishMinConfidence float64 `json:"autoPublishMinConfidence"`
	LearningKnowledgeBaseID  string  `json:"learningKnowledgeBaseId"`
	GapSimilarity            float64 `json:"gapSimilarity"`
	// Live data (new-api)
	ImageProfileID string           `json:"-"`
	LiveData       LiveDataSettings `json:"liveData"`
}

// LiveDataSettings configures the new-api site integration.
type LiveDataSettings struct {
	StatusImageStyle string `json:"statusImageStyle"`
	StatusPageURL    string `json:"statusPageUrl"`
	Enabled          bool   `json:"enabled"`
	BaseURL          string `json:"baseUrl"`
	AccessTokenEnc   string `json:"accessTokenEnc,omitempty"`
	HasAccessToken   bool   `json:"hasAccessToken"`
	UserID           string `json:"userId"`
	InjectSnapshot   bool   `json:"injectSnapshot"`
	PriceNote        string `json:"priceNote"`
}

const DefaultStylePrompt = `你是平台的在线客服，像一位熟练、友好的真人客服那样在 QQ 里聊天。
说话规则：
1. 先直接给结论，一般 1～3 句话说完；只回答用户问到的那一点。
2. 口语化、自然，不用标题、列表、加粗等格式；需要给配置或代码时才使用代码块。
3. 不说“作为 AI”“希望对您有帮助”“如有其他问题请随时联系”这类套话，不重复用户的问题。
4. 信息不够时，先用一句话问清最关键的一项，不要把所有可能性都列出来。
5. 内容确实较多时，先给关键结论，再主动用临时网页提供详细步骤；需要突出重点时可用图片。
6. 不确定的事情不要编，直说不确定，并建议转人工或查看官网。
7. 价格、分组状态、公告这类实时信息，必须以工具查询结果为准。`

// DefaultSettings returns sane defaults.
func DefaultSettings() Settings {
	return Settings{
		PersonaName:       "小助手",
		ReplyQuietSeconds: 6, ReplyMaxWaitSeconds: 20,
		StylePrompt:              DefaultStylePrompt,
		GroupReplyChars:          120,
		PrivateReplyChars:        220,
		SplitLongReplies:         true,
		MaxReplyParts:            3,
		SanitizeMarkdown:         true,
		HistoryTokenBudget:       2500,
		GroupThreadMode:          true,
		RewriteQuery:             true,
		SummaryEnabled:           true,
		MemoryEnabled:            true,
		MaxMemories:              8,
		ToolsEnabled:             true,
		MaxToolSteps:             4,
		HandoffPauseMinutes:      30,
		EvaluationEnabled:        true,
		StaffMiningEnabled:       true,
		AutoPublishStaff:         false,
		AutoPublishMinConfidence: 0.85,
		GapSimilarity:            0.86,

		LiveData: LiveDataSettings{
			StatusImageStyle: "brief",
			InjectSnapshot:   true,
			PriceNote:        "以官网实时价格为准",
		},
	}
}

// Normalize fills zero values with defaults and clamps ranges.
func (s *Settings) Normalize() {
	d := DefaultSettings()
	if strings.TrimSpace(s.StylePrompt) == "" {
		s.StylePrompt = d.StylePrompt
	}
	clamp := func(v *int, def, lo, hi int) {
		if *v == 0 {
			*v = def
		}
		if *v < lo {
			*v = lo
		}
		if *v > hi {
			*v = hi
		}
	}
	clamp(&s.GroupReplyChars, d.GroupReplyChars, 30, 2000)
	clamp(&s.PrivateReplyChars, d.PrivateReplyChars, 30, 4000)
	clamp(&s.MaxReplyParts, d.MaxReplyParts, 1, 4)
	clamp(&s.HistoryTokenBudget, d.HistoryTokenBudget, 200, 32000)
	clamp(&s.MaxMemories, d.MaxMemories, 1, 30)
	clamp(&s.MaxToolSteps, d.MaxToolSteps, 1, 8)
	clamp(&s.HandoffPauseMinutes, d.HandoffPauseMinutes, 1, 1440)
	if s.MaxOutputTokens < 0 {
		s.MaxOutputTokens = 0
	}
	if s.AutoPublishMinConfidence <= 0 || s.AutoPublishMinConfidence > 1 {
		s.AutoPublishMinConfidence = d.AutoPublishMinConfidence
	}
	if s.GapSimilarity <= 0 || s.GapSimilarity >= 1 {
		s.GapSimilarity = d.GapSimilarity
	}
	if len(s.Examples) > 20 {
		s.Examples = s.Examples[:20]
	}
	if s.LiveData.StatusImageStyle == "" {
		s.LiveData.StatusImageStyle = "brief"
	}
	s.LiveData.BaseURL = strings.TrimRight(strings.TrimSpace(s.LiveData.BaseURL), "/")
}

// Validate checks user-supplied settings.
func (s Settings) Validate() error {
	if s.ReplyQuietSeconds < 1 || s.ReplyMaxWaitSeconds < s.ReplyQuietSeconds || s.ReplyMaxWaitSeconds > 120 {
		return fmt.Errorf("合并静默时间和最长等待须为 1 至 120 秒，最长等待须大于或等于静默时间")
	}
	switch s.LiveData.StatusImageStyle {
	case "", "brief", "table", "heatmap", "night", "page", "off":
	default:
		return fmt.Errorf("无效的状态图样式")
	}
	if s.LiveData.Enabled && !strings.HasPrefix(s.LiveData.BaseURL, "http://") && !strings.HasPrefix(s.LiveData.BaseURL, "https://") {
		return fmt.Errorf("网站地址必须以 http:// 或 https:// 开头")
	}
	if len([]rune(s.StylePrompt)) > 6000 {
		return fmt.Errorf("风格提示词过长（最多 6000 字）")
	}
	return nil
}

// ReplyChars returns the target length for a conversation type.
func (s Settings) ReplyChars(conversationType string) int {
	if conversationType == "group" {
		return s.GroupReplyChars
	}
	return s.PrivateReplyChars
}
