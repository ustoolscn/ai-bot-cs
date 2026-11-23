package agent

import (
	"fmt"
	"strings"
	"time"
)

// Memory is a long-term fact about a user.
type Memory struct {
	Kind    string
	Content string
}

// SkillPrompt is the prompt-relevant part of a skill.
type SkillPrompt struct {
	Name, Description, Instructions string
}

// PromptInput gathers everything that goes into the system prompt.
type PromptInput struct {
	Settings         Settings
	ConversationType string
	BusinessPrompt   string // per-conversation system prompt
	SenderName       string
	SenderIsStaff    bool
	Now              time.Time
	Summary          string
	Memories         []Memory
	Knowledge        string
	LiveSnapshot     string
	Skills           []SkillPrompt
	ToolsAvailable   bool
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
		b.WriteString("群里有多人发言，只回应当前提问的这位用户。")
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

	var u strings.Builder
	if in.SenderName != "" {
		fmt.Fprintf(&u, "当前提问用户：%s。", in.SenderName)
	}
	if in.SenderIsStaff {
		u.WriteString("该用户是平台官方客服/管理员。")
	}
	if len(in.Memories) > 0 {
		u.WriteString("\n关于这位用户你已知的信息（来自以往对话，可能过时，必要时向用户确认）：")
		for _, m := range in.Memories {
			fmt.Fprintf(&u, "\n- %s", m.Content)
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
	if in.ToolsAvailable {
		out = append(out, "你可以调用工具获取实时数据或执行操作。复杂说明主动优先用临时网页并发回完整链接，需要突出重点时可生成图片。分组或服务状态必须调用状态工具以附上配置的状态页截图。推荐模型或比较性价比前，逐一查询适合任务的候选模型，按适用分组的最终输入、输出单价和预期用量比较。报价必须说明分组，实际美元价格=基础价×分组倍率，工具中已乘倍率的数字不能重复乘。英文提问报美元，中文提问按1美元=7元人民币换算。需要时先调用工具，拿到结果后再用自己的话简短回答。")
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

// EvaluationPrompt asks the auxiliary model to analyse one completed turn.
const EvaluationPrompt = `你是客服质检与记忆整理助手。阅读下面一轮客服对话，输出 JSON（不要输出其它内容）：
{
  "answered": true/false,             // 客服的回答是否实质解决了用户本轮问题（寒暄视为 true）
  "knowledge_gap": "",                // 若因缺少资料没能回答，写出一个概括后的标准问题，否则为空字符串
  "previous_feedback": "none",        // 用户本轮发言是否表明“上一条客服回答”没帮上忙：helpful / unhelpful / none
  "too_long": true/false,             // 客服回答是否明显啰嗦
  "profile": "以已有档案为基础整体改写，保留仍有效且由用户确认的身份与偏好，上限约2000 token，不推断敏感属性；没有变化沿用原文",
  "memories": [                       // 关于该用户、值得长期记住的事实（最多 3 条，没有则为空数组；每项另含 action=add/update/delete/unchanged，更新删除须引用已有事件 id。eventAt 为用户明确提及的事件时间 RFC3339，未知留空。禁止保存手机号、证件号、令牌、密码）
    {"kind": "fact|preference|issue", "content": "一句话描述", "confidence": 0.0-1.0}
  ]
}
记忆只记录用户本人相关且以后有用的信息（如使用的分组/模型、接入方式、反复遇到的问题、沟通偏好），不要记录密钥、密码、手机号等敏感信息，不要记录一次性的寒暄。`

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
