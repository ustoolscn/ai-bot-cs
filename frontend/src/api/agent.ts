import { request } from './client'
import * as mock from '@/mock/agent'

export interface AgentExample { question: string; answer: string }
export interface LiveDataSettings {
  enabled: boolean
  baseUrl: string
  hasAccessToken: boolean
  userId: string
  refreshSeconds: number
  syncAnnouncements: boolean
  announcementKnowledgeBaseId: string
  injectSnapshot: boolean
  priceNote: string
  statusPageUrl?: string
}
export interface ImageGenerationSettings {
  baseUrl: string
  model: string
  hasApiKey?: boolean
}
export interface AgentSettings {
  personaName: string
  stylePrompt: string
  examples: AgentExample[]
  groupReplyChars: number
  privateReplyChars: number
  splitLongReplies: boolean
  maxReplyParts: number
  sanitizeMarkdown: boolean
  maxOutputTokens: number
  historyTokenBudget: number
  groupThreadMode: boolean
  rewriteQuery: boolean
  summaryEnabled: boolean
  memoryEnabled: boolean
  maxMemories: number
  decisionProfileId: string
  auxChatProfileId: string
  embeddingProfileId: string
  toolsEnabled: boolean
  maxToolSteps: number
  handoffPauseMinutes: number
  evaluationEnabled: boolean
  staffMiningEnabled: boolean
  autoPublishStaff: boolean
  autoPublishMinConfidence: number
  learningKnowledgeBaseId: string
  gapSimilarity: number
  liveData: LiveDataSettings
  imageGeneration?: ImageGenerationSettings
}

export interface Announcement { content: string; publishDate: string; type?: string }
export interface Monitor { category: string; name: string; status: number; uptime: number }
export interface PriceQuote {
  model: string
  group: string
  groupRatio: number
  billing: 'per_token' | 'per_call' | 'tiered_expr' | 'video_seconds' | 'unknown'
  inputUsdPerMillionTokens?: number
  outputUsdPerMillionTokens?: number
  cacheReadUsdPerMillionTokens?: number
  cacheWriteUsdPerMillionTokens?: number
  usdPerCall?: number
  baseBillingExpr?: string
  outputUsdPerSecond?: Record<string, number>
  inputUsdPerSecond?: number
  baseFps?: number
  availableGroups?: string[]
}
export interface ServiceMetrics {
  request_count: number
  success_count: number
  success_rate: number | null
  median_ttft_ms: number | null
}
export interface LiveData {
  serviceStatus?: Partial<Record<'hour' | 'day', { generatedAt: string; overall: ServiceMetrics }>>
  enabled: boolean
  fetchedAt?: string
  lastAttemptAt?: string
  lastError: string
  systemName: string
  notice: string
  announcements: Announcement[] | null
  monitors: Monitor[] | null
  groups: Array<{ group: string; ratio?: number; description?: string }> | null
  modelCount: number
  models: PriceQuote[] | null
  promptPreview: string
}

export interface Skill { id?: string; name: string; description: string; instructions: string; tools: string[]; enabled: boolean; sortOrder: number }
export interface BuiltinTool { name: string; description: string; requiresLiveData: boolean; requiresKnowledgeBase: boolean }
export interface CustomTool {
  id?: string
  name: string
  description: string
  enabled: boolean
  method: 'GET' | 'POST'
  url: string
  headerNames?: string[]
  timeoutSeconds: number
  parameters: Record<string, unknown>
}

export interface Candidate { id: string; knowledgeBaseId?: string; knowledgeBaseName?: string; question: string; aliases: string[]; answer: string; source: string; evidence: Array<Record<string, string>>; confidence: number; status: string; reviewNote: string; createdAt: string }
export interface Entry { id: string; knowledgeBaseId: string; knowledgeBaseName: string; question: string; aliases: string[]; answer: string; source: string; status: string; indexStatus: string; indexError: string; version: number; hitCount: number; helpfulCount: number; unhelpfulCount: number; updatedAt: string }
export interface Gap { id: string; question: string; occurrences: number; status: string; firstSeenAt: string; lastSeenAt: string }
export interface Feedback { id: string; source: string; rating: string; comment: string; correctedAnswer: string; createdAt: string; answer: string; question: string; messageId?: string; conversationName: string; read?: boolean }
export interface MemoryMember { botId: string; botName: string; conversationId: string; conversationName: string; conversationType: string; userId: string; displayName: string; count: number }
export interface Memory { id: string; botId: string; botName: string; userId: string; displayName: string; kind: string; content: string; hasEmbedding: boolean; confidence: number; pinned: boolean; source: string; updatedAt: string; lastUsedAt?: string; eventAt?: string }
export interface UserProfile { conversationId: string; userId: string; content: string; updatedAt?: string }
export interface LearningStats { pendingCandidates: number; publishedEntries: number; openGaps: number; feedback7d: number; negativeFeedback7d: number; unreadNegativeFeedback?: number; activeMemories: number; openHandoffs: number }
export interface HandoffMessage { id: string; direction: string; senderName: string; content: string; eventType: string; eventAt: string; status?: string; deliveryError?: string }
export interface Handoff { id: string; conversationName: string; conversationType: string; userId: string; displayName: string; messageId?: string; messageContent: string; reason: string; status: string; pausedUntil?: string; createdAt: string; resolvedAt?: string; recent: HandoffMessage[]; ticketNumber?: string; humanRepliedBy?: string; humanReplyContent?: string; humanRepliedAt?: string; notifications?: Array<{ conversationId: string; status: string; error?: string }> }
export interface Member { userId: string; displayName: string; avatar?: string; joinedAt?: string; unionId?: string; role?: string; isBot?: boolean; active: boolean; isStaff: boolean; isBlocked?: boolean; blockedUserIds?: string[]; lastSeenAt: string; memoryCount: number; messageCount: number }
export interface BlockedAction { id: string; conversationId: string; status: 'pending' | 'kicked' | 'failed' | 'disabled'; attempts: number; error: string; lastAttemptAt: string | null; updatedAt: string }
export interface BlockedUser { id: string; botId: string; botName: string; conversationId: string | null; userId: string; displayName: string; reason: string; autoKick: boolean; createdAt: string; actions: BlockedAction[] }
export interface StaffLearningJob { id: string; status: 'pending' | 'processing' | 'completed' | 'failed'; attempts: number; error: string; updatedAt: string }
export interface StaffLearning { staff: Array<{ userId: string; displayName: string; availableMessages: number }>; style: string; sampleCount: number; sourceMessageIds: string[]; learnedAt: string | null; source: string; job: StaffLearningJob | null }
export interface EvalCase { id?: string; question: string; expected: string; source?: string; enabled: boolean }
export interface EvalRun { id: string; name: string; chatProfileName: string; status: string; total: number; completed: number; avgScore?: number; avgLength?: number; error?: string; createdAt: string; finishedAt?: string }
export interface EvalResult { id: string; question: string; expected: string; answer: string; score?: number; verdict: string; toolsUsed: string[]; latencyMs: number; error?: string }
export interface AgentStep { type: string; name?: string; input?: string; output?: string; error?: string; latencyMs: number }
export interface MessageAgentDetail {
  id: string
  question: string
  answer: string
  senderName?: string
  senderIsStaff?: boolean
  eventAt?: string
  agentRuns?: Array<{ rewrittenQuery?: string; steps?: AgentStep[]; evaluation?: Record<string, unknown> }>
  feedback?: Array<{ id: string; source: string; rating: string; comment: string; correctedAnswer: string; createdAt: string }>
  replies?: Array<{ id: string; eventType: string; content: string; status: string; eventAt: string }>
}

export const agentApi = {
  settings: {
    get: () => request<AgentSettings>({ url: '/agent/settings' }, mock.mockAgentSettings),
    save: (settings: AgentSettings, accessToken?: string, imageGenerationApiKey?: string) => request<AgentSettings>({ method: 'PUT', url: '/agent/settings', data: { ...settings, ...(accessToken === undefined ? {} : { liveDataAccessToken: accessToken }), ...(imageGenerationApiKey === undefined ? {} : { imageGenerationApiKey }) } }),
  },
  liveData: {
    get: () => request<LiveData>({ url: '/agent/live-data' }, mock.mockLiveData),
    refresh: () => request<LiveData>({ method: 'POST', url: '/agent/live-data/refresh', timeout: 45000 }),
  },
  skills: {
    list: () => request<Skill[]>({ url: '/skills' }, mock.mockSkills),
    save: (skill: Skill) => request<{ saved: boolean }>({ method: skill.id ? 'PUT' : 'POST', url: skill.id ? `/skills/${skill.id}` : '/skills', data: skill }),
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/skills/${id}` }),
  },
  tools: {
    list: () => request<{ builtin: BuiltinTool[]; custom: CustomTool[] }>({ url: '/tools' }, mock.mockTools),
    save: (tool: CustomTool, headers: Record<string, string> | null) => request<{ saved: boolean }>({ method: tool.id ? 'PUT' : 'POST', url: tool.id ? `/tools/${tool.id}` : '/tools', data: { ...tool, headers: headers || {}, keepHeaders: headers === null } }),
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/tools/${id}` }),
    test: (id: string, args: Record<string, unknown>) => request<{ result: string }>({ method: 'POST', url: `/tools/${id}/test`, data: { arguments: args }, timeout: 40000 }),
  },
  learning: {
    stats: () => request<LearningStats>({ url: '/learning/stats' }, mock.mockLearningStats),
    candidates: (status = 'pending', conversationId?: string) => request<Candidate[]>({ url: '/knowledge/candidates', params: { status, conversationId: conversationId || undefined } }, mock.mockCandidates),
    approve: (id: string, data: { knowledgeBaseId: string; question: string; aliases: string[]; answer: string; note?: string }) => request<{ entryId: string }>({ method: 'POST', url: `/knowledge/candidates/${id}/approve`, data }),
    reject: (id: string, note: string) => request<{ rejected: boolean }>({ method: 'POST', url: `/knowledge/candidates/${id}/reject`, data: { note } }),
    entries: (params: { knowledgeBaseId?: string; status?: string; q?: string }) => request<Entry[]>({ url: '/knowledge/entries', params }, mock.mockEntries),
    createEntry: (data: { knowledgeBaseId: string; question: string; aliases: string[]; answer: string }) => request<{ id: string }>({ method: 'POST', url: '/knowledge/entries', data }),
    updateEntry: (id: string, data: { question: string; aliases: string[]; answer: string; status: string }) => request<{ updated: boolean }>({ method: 'PUT', url: `/knowledge/entries/${id}`, data }),
    deleteEntry: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/knowledge/entries/${id}` }),
    gaps: (status = 'open') => request<Gap[]>({ url: '/knowledge/gaps', params: { status } }, mock.mockGaps),
    answerGap: (id: string, data: { knowledgeBaseId: string; question: string; answer: string; aliases: string[] }) => request<{ entryId: string }>({ method: 'POST', url: `/knowledge/gaps/${id}/answer`, data }),
    ignoreGap: (id: string) => request<{ ignored: boolean }>({ method: 'POST', url: `/knowledge/gaps/${id}/ignore` }),
    feedback: (rating = '') => request<Feedback[]>({ url: '/feedback', params: { rating } }, mock.mockFeedback),
    markFeedbackRead: (target: { ids: string[] } | { all: true }) => request<{ updated: number }>({ method: 'POST', url: '/feedback/read', data: target }, { updated: 0 }),
  },
  memories: {
    directory: () => request<MemoryMember[]>({ url: '/memories/directory' }),
    list: (params: { userId?: string; botId?: string; conversationId?: string; q?: string }) => request<Memory[]>({ url: '/memories', params }),
    update: (id: string, data: { content: string; kind: string; pinned: boolean }) => request<{ updated: boolean }>({ method: 'PUT', url: `/memories/${id}`, data }),
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/memories/${id}` }),
    profile: (conversationId: string, userId: string) => {
      const url = `/conversations/${conversationId}/user-profiles/${encodeURIComponent(userId)}`
      return {
        get: () => request<UserProfile>({ url }),
        save: (content: string) => request<UserProfile>({ method: 'PUT', url, data: { content } }),
        clear: () => request<UserProfile>({ method: 'DELETE', url }),
      }
    },
  },
  handoffs: {
    list: (status = 'open') => request<Handoff[]>({ url: '/handoffs', params: { status } }, status === 'open' || status === 'all' ? mock.mockHandoffs : []),
    resolve: (id: string) => request<{ resolved: boolean }>({ method: 'POST', url: `/handoffs/${id}/resolve` }),
  },
  messages: {
    detail: (id: string) => request<MessageAgentDetail>({ url: `/messages/${id}` }, { ...mock.mockMessageAgentDetail, id }),
    reply: (id: string, text: string, learn: boolean) => request<{ id: string }>({ method: 'POST', url: `/messages/${id}/reply`, data: { text, learn } }),
    feedback: (id: string, data: { rating: string; comment?: string; correctedAnswer?: string; publish?: boolean; knowledgeBaseId?: string; question?: string }) => request<{ saved: boolean; entryId?: string; candidateId?: string }>({ method: 'POST', url: `/messages/${id}/feedback`, data }),
  },
  members: {
    sync:(id:string,refresh:boolean)=>request<{syncedAt:string}>({url:`/conversations/${id}/members/sync`,method:'POST',params:{refresh},timeout:600000}),
    list: (conversationId: string) => request<Member[]>({ url: `/conversations/${conversationId}/members` }),
    setStaff: (conversationId: string, userId: string, isStaff: boolean) => request<{ updated: boolean }>({ method: 'PUT', url: `/conversations/${conversationId}/members/${encodeURIComponent(userId)}`, data: { isStaff } }),
    staffLearning: (conversationId: string) => request<StaffLearning>({ url: `/conversations/${conversationId}/staff-learning` }),
    startStaffLearning: (conversationId: string) => request<{ jobId: string; source: string; maxMessages: number }>({ method: 'POST', url: `/conversations/${conversationId}/staff-learning`, data: {} }),
    summaries: (conversationId: string) => request<Array<{ scope: string; summary: string; updatedAt: string }>>({ url: `/conversations/${conversationId}/summaries` }),
  },
  blacklist: {
    list: (params: { botId?: string; conversationId?: string } = {}) => request<BlockedUser[]>({ url: '/blocked-users', params }),
    add: (data: { botId: string; conversationId?: string; userId: string; reason?: string; autoKick?: boolean }) => request<{ id: string; autoKick: boolean; identityType: string }>({ method: 'POST', url: '/blocked-users', data }),
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/blocked-users/${id}` }),
  },
  eval: {
    cases: () => request<EvalCase[]>({ url: '/eval/cases' }, mock.mockEvalCases),
    saveCase: (c: EvalCase) => request<{ saved: boolean }>({ method: c.id ? 'PUT' : 'POST', url: c.id ? `/eval/cases/${c.id}` : '/eval/cases', data: c }),
    removeCase: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/eval/cases/${id}` }),
    fromEntries: () => request<{ created: number }>({ method: 'POST', url: '/eval/cases/from-entries' }),
    runs: () => request<EvalRun[]>({ url: '/eval/runs' }, mock.mockEvalRuns),
    createRun: (data: { name?: string; chatProfileId?: string; knowledgeBaseIds: string[] }) => request<{ id: string }>({ method: 'POST', url: '/eval/runs', data }),
    results: (id: string) => request<EvalResult[]>({ url: `/eval/runs/${id}/results` }),
  },
  alerts: {
    list: () => request<Array<{ id: string; kind: string; title: string; acknowledged: boolean; createdAt: string }>>({ url: '/system/alerts' }),
    ack: () => request<{ acknowledged: boolean }>({ method: 'POST', url: '/system/alerts/ack' }),
  },
}

export function formatTime(value?: string) {
  if (!value) return '—'
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export const sourceText: Record<string, string> = { manual: '手动', staff: '官方客服', feedback: '管理员纠错', gap: '缺口补答', announcement: '网站公告', mined: '挖掘' }
export const ratingText: Record<string, string> = { good: '好', bad: '差', too_long: '太长', stiff: '生硬', wrong: '错误', helpful: '有帮助', unhelpful: '没帮上' }
export const feedbackSourceText: Record<string, string> = { admin: '管理员', user_implicit: '用户隐式', evaluator: '质检', staff: '客服' }
