// 开发模式下后端不可用时的示例数据，仅用于本地预览界面。
import type { AgentSettings, BuiltinTool, Candidate, CustomTool, Entry, EvalCase, EvalRun, Feedback, Gap, Handoff, LearningStats, LiveData, Memory, MessageAgentDetail, Skill } from '@/api/agent'

const now = Date.now()
const ago = (minutes: number) => new Date(now - minutes * 60000).toISOString()

export const mockAgentSettings: AgentSettings = {
  personaName: '小Q',
  stylePrompt: '你是平台的在线客服，像一位熟练、友好的真人客服那样在 QQ 里聊天。\n1. 先直接给结论，一般 1～3 句话说完。\n2. 口语化、自然，不用标题和列表。',
  examples: [{ question: '充值后多久到账？', answer: '一般几秒内到账，超过 5 分钟没到把订单号发我帮你查。' }],
  groupReplyChars: 120,
  privateReplyChars: 300,
  splitLongReplies: true,
  maxReplyParts: 3,
  sanitizeMarkdown: true,
  maxOutputTokens: 0,
  historyTokenBudget: 4000,
  groupThreadMode: true,
  rewriteQuery: true,
  summaryEnabled: true,
  memoryEnabled: true,
  maxMemories: 8,
  decisionProfileId: '',
  auxChatProfileId: '',
  embeddingProfileId: '',
  toolsEnabled: true,
  maxToolSteps: 4,
  handoffPauseMinutes: 30,
  evaluationEnabled: true,
  staffMiningEnabled: true,
  autoPublishStaff: false,
  autoPublishMinConfidence: 0.85,
  learningKnowledgeBaseId: '',
  gapSimilarity: 0.82,
  liveData: { enabled: true, baseUrl: 'https://example-api.com', hasAccessToken: false, userId: '', refreshSeconds: 300, syncAnnouncements: true, announcementKnowledgeBaseId: '', injectSnapshot: true, priceNote: '价格以官网为准' },
}

export const mockLiveData: LiveData = {
  enabled: true, fetchedAt: ago(3), lastAttemptAt: ago(3), lastError: '', systemName: '示例 API 站', notice: '',
  announcements: [{ content: '今晚 23:00 维护 10 分钟', publishDate: ago(120) }],
  monitors: [{ category: '分组', name: 'default', status: 1, uptime: 99.9 }, { category: '分组', name: 'vip', status: 1, uptime: 99.7 }, { category: '分组', name: 'claude', status: 2, uptime: 92.1 }],
  groups: [{ group: 'default', ratio: 1 }, { group: 'vip', ratio: 0.8, description: '会员分组' }],
  serviceStatus: {
    hour: { generatedAt: ago(3), overall: { request_count: 1000, success_count: 997, success_rate: 99.7, median_ttft_ms: 900 } },
    day: { generatedAt: ago(3), overall: { request_count: 80000, success_count: 79600, success_rate: 99.5, median_ttft_ms: 1050 } },
  },
  modelCount: 64, models: [], promptPreview: '【公告】今晚 23:00 维护 10 分钟\n【异常分组】claude',
}

export const mockSkills: Skill[] = [
  { id: 'sk1', name: '查价格', description: '用户问某个模型多少钱时', instructions: '先调用价格工具，再用一句话回复。', tools: ['query_price'], enabled: true, sortOrder: 1 },
  { id: 'sk2', name: '充值问题', description: '用户说充值没到账时', instructions: '先问订单号，再引导查看账单。', tools: [], enabled: true, sortOrder: 2 },
]
export const mockTools: { builtin: BuiltinTool[]; custom: CustomTool[] } = {
  builtin: [
    { name: 'search_knowledge', description: '检索会话绑定的知识库', requiresLiveData: false, requiresKnowledgeBase: true },
    { name: 'query_price', description: '查询模型实时价格', requiresLiveData: true, requiresKnowledgeBase: false },
    { name: 'request_handoff', description: '把用户转给人工客服', requiresLiveData: false, requiresKnowledgeBase: false },
  ],
  custom: [{ id: 'tl1', name: 'query_order', description: '按订单号查询订单状态', enabled: true, method: 'GET', url: 'https://example.com/api/orders', headerNames: ['Authorization'], timeoutSeconds: 10, parameters: {} }],
}

export const mockLearningStats: LearningStats = { pendingCandidates: 3, publishedEntries: 128, openGaps: 5, feedback7d: 42, negativeFeedback7d: 6, unreadNegativeFeedback: 1, activeMemories: 310, openHandoffs: 2 }
export const mockCandidates: Candidate[] = [
  { id: 'c1', knowledgeBaseName: '常见问题库', question: '为什么调用返回 429？', aliases: ['提示请求过多', '限流了'], answer: '429 是触发了分组限流，换个分组或稍后重试就行。', source: 'staff', evidence: [], confidence: 0.92, status: 'pending', reviewNote: '', createdAt: ago(25) },
  { id: 'c2', knowledgeBaseName: '常见问题库', question: '可以开发票吗？', aliases: [], answer: '可以，在控制台“账单 → 发票”里申请，3 个工作日内开出。', source: 'feedback', evidence: [], confidence: 0.8, status: 'pending', reviewNote: '', createdAt: ago(90) },
  { id: 'c3', knowledgeBaseName: '售后知识库', question: '令牌怎么设置额度？', aliases: ['key 限额'], answer: '在令牌列表点编辑，填写额度上限后保存。', source: 'mined', evidence: [], confidence: 0.71, status: 'pending', reviewNote: '', createdAt: ago(300) },
]
export const mockEntries: Entry[] = [
  { id: 'e1', knowledgeBaseId: 'kb1', knowledgeBaseName: '常见问题库', question: '充值多久到账？', aliases: [], answer: '一般几秒内到账。', source: 'staff', status: 'published', indexStatus: 'indexed', indexError: '', version: 2, hitCount: 56, helpfulCount: 12, unhelpfulCount: 1, updatedAt: ago(600) },
]
export const mockGaps: Gap[] = [
  { id: 'g1', question: '支持支付宝自动续费吗？', occurrences: 7, status: 'open', firstSeenAt: ago(4000), lastSeenAt: ago(30) },
  { id: 'g2', question: 'gpt-image 怎么计费？', occurrences: 4, status: 'open', firstSeenAt: ago(2000), lastSeenAt: ago(80) },
]
export const mockFeedback: Feedback[] = [
  { id: 'f1', source: 'admin', rating: 'too_long', comment: '太啰嗦', correctedAnswer: '', createdAt: ago(200), answer: '您好，关于这个问题……', question: '怎么改密码', conversationName: '产品运营交流群', read: false },
  { id: 'f2', source: 'user_implicit', rating: 'wrong', comment: '', correctedAnswer: '在 设置 → 账号安全 中修改。', createdAt: ago(900), answer: '请联系管理员。', question: '密码忘了怎么办', conversationName: '售后服务群', read: true },
]
export const mockMemories: Memory[] = [
  { id: 'm1', botId: 'bot-qq-service', botName: '客服小Q', userId: 'u1', displayName: '林晓', kind: 'profile', content: '使用 vip 分组，主要调用 Claude', hasEmbedding: true, confidence: 0.9, pinned: false, source: 'auto', updatedAt: ago(100) },
]
export const mockHandoffs: Handoff[] = [
  { id: 'h1', conversationName: '售后服务群', conversationType: 'group', userId: 'u2', displayName: '周周', messageId: 'msg-2', messageContent: '我充值了 200 还没到账，急用', reason: '用户要求人工', status: 'open', createdAt: ago(6), recent: [
    { id: 'r1', direction: 'inbound', senderName: '周周', content: '我充值了 200 还没到账', eventType: 'MESSAGE', eventAt: ago(9) },
    { id: 'r2', direction: 'outbound', senderName: '客服小Q', content: '一般几秒内到账，方便把订单号发我吗？', eventType: 'REPLY', eventAt: ago(8) },
    { id: 'msg-2', direction: 'inbound', senderName: '周周', content: '订单号 20261123-8812，急用，找人工', eventType: 'MESSAGE', eventAt: ago(6) },
  ] },
  { id: 'h2', conversationName: '用户 8G7X', conversationType: 'c2c', userId: 'u3', displayName: 'Miya', messageId: 'msg-3', messageContent: '帮我查询订单', reason: 'AI 无法解决', status: 'open', createdAt: ago(40), recent: [
    { id: 'msg-3', direction: 'inbound', senderName: 'Miya', content: '帮我查询订单', eventType: 'MESSAGE', eventAt: ago(41) },
  ] },
]
export const mockEvalCases: EvalCase[] = [
  { id: 'ec1', question: '充值多久到账？', expected: '几秒内到账；超时提供订单号', enabled: true },
  { id: 'ec2', question: '429 是什么意思？', expected: '限流；换分组或稍后重试', enabled: true },
]
export const mockEvalRuns: EvalRun[] = [
  { id: 'run1', name: '调整风格后', chatProfileName: 'qwen-plus', status: 'completed', total: 2, completed: 2, avgScore: 8.2, avgLength: 46, createdAt: ago(60), finishedAt: ago(58) },
  { id: 'run0', name: '基线', chatProfileName: 'qwen-plus', status: 'completed', total: 2, completed: 2, avgScore: 6.4, avgLength: 92, createdAt: ago(1500), finishedAt: ago(1498) },
]
export const mockMessageAgentDetail: MessageAgentDetail = {
  id: 'm1', question: '@客服小Q 怎么申请七天无理由退款？', answer: '您可以在订单详情中发起退款申请。', senderName: '林晓', eventAt: ago(3),
  agentRuns: [{
    rewrittenQuery: '七天无理由退款的申请流程和条件',
    steps: [
      { type: 'rewrite', name: '问题改写', input: '怎么申请七天无理由退款？', output: '七天无理由退款的申请流程和条件', latencyMs: 640 },
      { type: 'model', name: '第 1 次模型调用', output: '调用 search_knowledge', latencyMs: 820 },
      { type: 'tool', name: 'search_knowledge', input: '{"query":"七天无理由退款"}', output: '命中 3 条：退款政策、售后时效、商品完好要求', latencyMs: 210 },
      { type: 'model', name: '第 2 次模型调用', output: '您可以在订单详情中发起退款申请…', latencyMs: 1460 },
    ],
    evaluation: { answered: true, too_long: false },
  }],
  feedback: [{ id: 'f1', source: 'admin', rating: 'good', comment: '', correctedAnswer: '', createdAt: ago(1) }],
  replies: [
    { id: 'r1', eventType: 'AI_RESPONSE', content: '您可以在订单详情中发起退款申请，商品需保持完好。', status: 'sent', eventAt: ago(3) },
    { id: 'r2', eventType: 'AI_RESPONSE', content: '具体步骤：打开订单 → 申请售后 → 选择七天无理由。', status: 'sent', eventAt: ago(3) },
  ],
}
