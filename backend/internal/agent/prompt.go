package agent

import (
	"fmt"
	"strings"
	"time"
)

// Memory is a long-term fact, preference, issue, or shared experience about a user.
type Memory struct {
	ID          string  `json:"id,omitempty"`
	Kind        string  `json:"kind"`
	Content     string  `json:"content"`
	Importance  int     `json:"importance,omitempty"`
	EmotionTag  string  `json:"emotionTag,omitempty"`
	SourceWhere string  `json:"sourceWhere,omitempty"`
	EventDate   string  `json:"eventDate,omitempty"`
	Resolved    bool    `json:"resolved,omitempty"`
	Score       float64 `json:"score,omitempty"`
}

// GroupMemoryItem is a recent shared topic, event, or meme in a group.
type GroupMemoryItem struct {
	ID         string `json:"id,omitempty"`
	Kind       string `json:"kind"`
	Content    string `json:"content"`
	Importance int    `json:"importance,omitempty"`
	EventDate  string `json:"eventDate,omitempty"`
}

// SkillPrompt is the prompt-relevant part of a skill.
type SkillPrompt struct {
	Name, Description, Instructions string
}

// PromptInput gathers everything that goes into the system prompt.
type PromptInput struct {
	Settings           Settings
	ConversationType   string
	BusinessPrompt     string // per-conversation system prompt
	DirectorNotes      string // dynamic companion / persona / emotion / relationship instructions
	SenderName         string
	SenderIsStaff      bool
	Now                time.Time
	Summary            string
	Memories           []Memory
	GroupMemories      []GroupMemoryItem
	GroupVibe          string
	Knowledge          string
	LiveSnapshot       string
	Skills             []SkillPrompt
	ToolsAvailable     bool
	WebSearchAvailable bool
}

// BuildSystemPrompt assembles the system messages in priority order.
func BuildSystemPrompt(in PromptInput) []string {
	s := in.Settings
	var out []string
	var b strings.Builder
	if name := strings.TrimSpace(s.PersonaName); name != "" {
		fmt.Fprintf(&b, "你的名字是“%s”。\n", name)
	}
	b.WriteString(strings.TrimSpace(s.StylePrompt))
	limit := s.ReplyChars(in.ConversationType)
	scene := "私聊"
	if in.ConversationType == "group" {
		scene = "群聊"
	}
	fmt.Fprintf(&b, "\n\n当前场景：QQ%s。回复尽量控制在 %d 字以内。", scene, limit)
	if in.ConversationType == "group" {
		b.WriteString("群里有多人发言，仅回答当前消息的意图。历史用于理解指代或明确的追问；当前用户换了话题或在反馈之前的回答时，回应新话题或反馈，不重答历史问题。")
	}
	if !in.Now.IsZero() {
		fmt.Fprintf(&b, "\n当前时间：%s。", in.Now.Format("2006-01-02 15:04"))
	}
	b.WriteString("\n链接直接写完整地址，地址中不要插入空格或换行。需要发送已有图片时使用 [IMAGE]https://公开图片地址[/IMAGE]，每次最多一张。")
	if bp := strings.TrimSpace(in.BusinessPrompt); bp != "" {
		b.WriteString("\n\n业务说明：\n" + bp)
	}
	out = append(out, b.String())

	if len(s.Examples) > 0 {
		var e strings.Builder
		e.WriteString("以下是本平台客服的真实回复示例，请模仿这种语气和长度（不要照抄内容）：")
		for _, ex := range s.Examples {
			if strings.TrimSpace(ex.Question) == "" || strings.TrimSpace(ex.Answer) == "" {
				continue
			}
			fmt.Fprintf(&e, "\n用户：%s\n客服：%s", strings.TrimSpace(ex.Question), strings.TrimSpace(ex.Answer))
		}
		out = append(out, e.String())
	}

	if len(in.Skills) > 0 {
		var k strings.Builder
		k.WriteString("你掌握以下技能，遇到对应情况时按技能说明处理：")
		for _, sk := range in.Skills {
			fmt.Fprintf(&k, "\n\n【%s】适用：%s\n%s", sk.Name, strings.TrimSpace(sk.Description), strings.TrimSpace(sk.Instructions))
		}
		out = append(out, k.String())
	}

	if in.LiveSnapshot != "" {
		out = append(out, "网站实时信息（自动同步，仅在相关时使用）：\n"+in.LiveSnapshot)
	}

	if dn := strings.TrimSpace(in.DirectorNotes); dn != "" {
		out = append(out, dn)
	}

	if in.ConversationType == "group" && (len(in.GroupMemories) > 0 || strings.TrimSpace(in.GroupVibe) != "") {
		var gm strings.Builder
		gm.WriteString("本群近期氛围与共同记忆（供你自然融入群聊语境，不要生硬背诵）：")
		if v := strings.TrimSpace(in.GroupVibe); v != "" {
			fmt.Fprintf(&gm, "\n- 当前群氛围：%s", v)
		}
		for _, item := range in.GroupMemories {
			prefix := ""
			if item.EventDate != "" {
				prefix = item.EventDate + " · "
			}
			fmt.Fprintf(&gm, "\n- %s%s", prefix, item.Content)
		}
		out = append(out, gm.String())
	}

	var u strings.Builder
	if in.SenderName != "" {
		fmt.Fprintf(&u, "当前提问用户：%s。", in.SenderName)
	}
	if in.SenderIsStaff {
		u.WriteString("该用户是平台官方客服/管理员。")
	}
	if len(in.Memories) > 0 {
		u.WriteString("\n关于这位群友你记住的信息（跨群/私聊共享，可能随时间变化；像朋友自然想起一样带出，不要说“根据记录”）：")
		for _, m := range in.Memories {
			if m.Kind == "profile" {
				fmt.Fprintf(&u, "\n- %s", m.Content)
				continue
			}
			var tags []string
			switch m.Kind {
			case "episode":
				tags = append(tags, "共同经历")
			case "inside_joke":
				tags = append(tags, "老梗默契")
			case "social":
				tags = append(tags, "社交关系")
			case "preference":
				tags = append(tags, "偏好")
			case "issue":
				if m.Resolved {
					tags = append(tags, "已解决旧问题")
				} else {
					tags = append(tags, "待关注问题")
				}
			}
			if m.EmotionTag != "" {
				tags = append(tags, m.EmotionTag)
			}
			if m.SourceWhere != "" {
				tags = append(tags, m.SourceWhere)
			}
			if len(tags) > 0 {
				fmt.Fprintf(&u, "\n- [%s] %s", strings.Join(tags, "/"), m.Content)
			} else {
				fmt.Fprintf(&u, "\n- %s", m.Content)
			}
		}
	}
	if in.Summary != "" {
		u.WriteString("\n此前对话摘要：\n" + in.Summary)
	}
	if u.Len() > 0 {
		out = append(out, strings.TrimSpace(u.String()))
	}

	if in.Knowledge != "" {
		out = append(out, "知识库检索结果（仅在相关时使用；只挑与问题直接相关的一两点回答，不要复述整段资料）：\n"+in.Knowledge)
	}
	if in.WebSearchAvailable {
		out = append(out, "本轮已开启模型的内置联网搜索。涉及最新新闻、天气、公开公告等时效信息，或用户要求上网核实时，先联网查询，再根据检索结果回答并提供来源。未查到时说明具体情况。")
	}
	if in.ToolsAvailable {
		out = append(out, "你可以调用工具获取实时数据或执行操作。复杂说明主动优先用临时网页并发回完整链接，需要突出重点时可生成图片。分组或服务状态必须调用状态工具，并按客服配置附状态图。推荐模型或比较性价比前，逐一查询适合任务的候选模型，按适用分组的最终输入、输出单价和预期用量比较。报价必须说明分组，实际美元价格=基础价×分组倍率，工具中已乘倍率的数字不能重复乘。英文提问报美元，中文提问按1美元=7元人民币换算。需要时先调用工具，拿到结果后再用自己的话简短回答。")
	}
	return out
}

// RewritePrompt asks a model to rewrite the latest message into a standalone query.
const RewritePrompt = `你负责把客服对话中用户的最新一句话改写成一个可以独立理解、适合检索知识库的问题。
要求：补全省略的主语/对象（如模型名、分组、报错），保持原意，不要回答问题。
如果最新一句本身已经完整，或只是寒暄，原样输出。只输出改写后的这一句话。`

// SummaryPrompt asks a model to update a rolling conversation summary.
const SummaryPrompt = `你负责维护客服对话的滚动摘要。根据“已有摘要”和“新增对话”，输出更新后的摘要。
要求：200 字以内；保留用户的问题、已确认的关键信息（模型、分组、报错、订单等）、已给出的结论和尚未解决的事项；去掉寒暄。只输出摘要正文。`

// EvaluationPrompt asks the auxiliary model to analyse one completed turn (fast path).
const EvaluationPrompt = `你是客服质检与记忆整理助手。阅读下面一轮对话，输出 JSON（不要输出其它内容）：
{
  "answered": true/false,             // 本轮回应是否妥当或解决了用户问题（日常闲聊/搭茬视为 true）
  "knowledge_gap": "",                // 若因缺少业务资料没能回答，写出概括后的标准问题，否则为空字符串
  "previous_feedback": "none",        // 用户本轮发言是否表明“上一条AI回答”没帮上忙：helpful / unhelpful / none
  "too_long": true/false,             // 回复是否明显啰嗦
  "mood_delta": "neutral",            // 本轮结束时用户的情绪/满意度倾向：positive / neutral / negative
  "memories": [                       // 值得跨会话长期记住的原子记忆（最多 3 条，没有则为空数组）
    {
      "action": "add|update|resolve|delete",
      "id": "更新、解决或删除时填已有记忆id",
      "kind": "fact|preference|issue|episode|inside_joke|social",
      "content": "一句话描述（40字以内最好）",
      "importance": 5,                // 1-10 重要程度（普通闲聊3-4，稳定偏好6-7，重要经历/核心故障8-9）
      "emotionTag": "",               // 如 开心/委屈/吐槽/感动，无则留空
      "eventAt": "",                  // 用户明确提及的事件时间 RFC3339，未知留空
      "confidence": 0.0-1.0
    }
  ],
  "proactive_candidate": null         // 可选：仅当用户明确提到近期将要发生的个人重要事件或待跟进事项（如“明天答辩/今晚发版/等下看结果”）时填 {"kind":"event_care|issue_followup","topic":"一句话事由","delayHours":4}，否则填 null
}
规则：只记录对以后互动有价值的事实、偏好、共同经历（episode）、默契老梗（inside_joke）、社交关系（social）或待解决事项（issue）；若旧 issue 在本轮已确认解决，使用 action="resolve" 并引用其 id；严禁保存手机号、证件号、密钥、密码、令牌或一次性无意义寒暄。`

// ReflectionPrompt asks the auxiliary model to consolidate a user's long-term
// memories, refine their narrative profile, and incrementally update persona traits.
const ReflectionPrompt = `你是人物记忆巩固与画像反思专家。阅读关于同一个人的“已有档案”“已有结构化画像”“互动数据”和“多条零散记忆”，进行慢思考整合，输出 JSON（不要输出其它内容）：
{
  "profile": "整合去重后的跨会话人物档案（自然语言，保留核心身份、习惯、经历、相处模式，不超过 1200 字，无敏感隐私；若无明显新信息可保持原样）",
  "nicknames": ["可选：仅在互动中自然形成的亲切称呼，最多 3 个，没有则为空数组"],
  "archive_ids": ["过时、重复、已解决且低价值的琐碎记忆 id（用于自然遗忘归档）"],
  "merged_memories": [
    {"kind": "fact|preference|issue|episode|inside_joke|social", "content": "由多条碎片提炼出的高阶洞察或长期经历", "importance": 7, "confidence": 0.85}
  ],
  "persona": {}
}
`

// GroupDigestPrompt asks the auxiliary model to summarize recent group chat vibes and shared topics.
const GroupDigestPrompt = `你是社群群聊记忆整理助手。阅读最近一段时间的群聊记录与已有群记忆，提炼出本群近期最重要的 1～3 条公共情景记忆和当前群氛围，输出 JSON（不要输出其它内容）：
{
  "group_vibe": "一句话描述当前群气氛（如：晚上大家都在热烈折腾新模型，氛围轻松）",
  "memories": [
    {"kind": "topic|event|meme|incident", "content": "一句话概括群里最近发生的共同话题、梗或大事", "importance": 6, "ttlDays": 7}
  ]
}
规则：不记录个人隐私、密钥或无意义刷屏；如无值得沉淀的话题，memories 返回空数组。`

// ProactiveGatePrompt decides whether a scheduled follow-up message is natural and non-intrusive right now.
const ProactiveGatePrompt = `你是社群防打扰自检器。判断现在主动向用户发一条关怀/跟进消息是否自然、不突兀、不打扰。
阅读“计划事由”“用户近期记忆与情绪”“最近对话记录”和“机器人人设”，输出 JSON（不要输出其它内容）：
{
  "shouldSend": true/false,
  "reason": "一句话说明原因",
  "draft": "若 shouldSend 为 true，写一句像真人朋友一样轻松自然、低压力的短消息（1～2 句，35 字以内，不带客服腔）"
}`

// StaffMiningPrompt asks a model to turn a staff answer into an FAQ candidate.
const StaffMiningPrompt = `下面是一段 QQ 群聊记录，最后一条是官方客服的发言。
请判断这条客服发言是否在回答某位用户的问题，并且其中包含可复用的通用知识（而不是只针对个人账号的处理）。
输出 JSON（不要输出其它内容）：
{"reusable": true/false, "question": "概括后的标准问题", "aliases": ["其他问法"], "answer": "整理后的标准答案（保留客服原意，简洁口语化）", "confidence": 0.0-1.0}
不可复用时 reusable 为 false，其余字段可为空。`

// JudgePrompt grades an answer in evaluation runs.
const JudgePrompt = `你是客服回答评审。根据“问题”“期望要点”和“实际回答”，输出 JSON（不要输出其它内容）：
{"score": 0-10, "verdict": "一句话评语"}
评分标准：要点是否正确覆盖（最重要）、是否有错误信息、是否简洁自然像真人客服。`
