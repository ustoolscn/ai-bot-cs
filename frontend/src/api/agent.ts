import { request } from './client'
import * as mock from '@/mock/agent'
import { mockMemories, mockMemoryDirectory, mockProfile } from '@/mock/data'

export interface AgentExample { question: string; answer: string }
export type StatusImageStyle = 'brief' | 'table' | 'heatmap' | 'night' | 'page' | 'off'
export type EmojiPolicy = 'off' | 'minimal' | 'natural' | 'expressive'
export type GroupChimeInLevel = 'off' | 'restrained' | 'moderate' | 'active'
export type RelationshipStage = 'stranger' | 'acquaintance' | 'familiar' | 'close_friend'

export interface CompanionSettings {
  enabled: boolean
  backstory: string
  personalityTraits: string[]
  catchphrases: string[]
  humorLevel: number
  warmthLevel: number
  emojiPolicy: EmojiPolicy
  circadianAware: boolean
  realTimeEmotion: boolean
  groupChimeIn: GroupChimeInLevel
  chimeInCooldownMinutes: number
  quietHoursStart: string
  quietHoursEnd: string
  reflectionIntervalTurns: number
  proactiveEnabled: boolean
  proactiveDailyBudget: number
  typingIndicator: boolean
  dynamicTypingDelay: boolean
}

export function defaultCompanionSettings(): CompanionSettings {
  return {
    enabled: true,
    backstory: '',
    personalityTraits: ['真诚', '幽默', '懂行', '不端着'],
    catchphrases: [],
    humorLevel: 45,
    warmthLevel: 70,
    emojiPolicy: 'natural',
    circadianAware: true,
    realTimeEmotion: true,
    groupChimeIn: 'moderate',
    chimeInCooldownMinutes: 8,
    quietHoursStart: '01:00',
    quietHoursEnd: '08:00',
    reflectionIntervalTurns: 6,
    proactiveEnabled: true,
    proactiveDailyBudget: 1,
    typingIndicator: false,
    dynamicTypingDelay: true,
  }
}

export interface LiveDataSettings {
  enabled: boolean
  baseUrl: string
  hasAccessToken: boolean
  userId: string
  injectSnapshot: boolean
  priceNote: string
  statusPageUrl?: string
  statusImageStyle?: StatusImageStyle
}
export interface AgentSettings {
  personaName: string
  stylePrompt: string
  examples: AgentExample[]
  companion?: CompanionSettings
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
  replyQuietSeconds?: number
  replyMaxWaitSeconds?: number
  evaluationEnabled: boolean
  staffMiningEnabled: boolean
  autoPublishStaff: boolean
  autoPublishMinConfidence: number
  learningKnowledgeBaseId: string
  gapSimilarity: number
  liveData: LiveDataSettings
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
  /** 超过 24 小时未同步、上次同步失败或数据不完整 */
  possiblyStale?: boolean
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

/** 一条问答、候选或回复带的素材（按顺序发出） */
export interface Material { id: string; kind: 'image' | 'video'; mime?: string; url: string; filename?: string; sizeBytes?: number; posterUrl?: string | null }
/** 编辑中的素材：已保存的（existing）或新选的文件（file），数组顺序就是发送顺序 */
export interface MaterialDraft { key: string; existing?: Material; file?: File }
/** 素材数量和大小上限，与后端一致 */
export const materialLimits = { max: 9, imageBytes: 5 << 20, videoBytes: 25 << 20, totalBytes: 64 << 20 }
/**
 * 带素材的写请求。drafts 不传：不动原有素材（编辑/审核时保留原组）。
 * 传了：按数组顺序提交最终结果；有新文件时用 multipart（files + materials 顺序），否则 JSON。
 */
function withMaterials<T extends object>(data: T, drafts?: MaterialDraft[]): T | FormData | (T & { materials: string[] }) {
  if (!drafts) return data
  const files = drafts.filter(d => d.file).map(d => d.file!)
  let n = 0
  const order = drafts.map(d => d.existing ? `keep:${d.existing.id}` : `new:${n++}`)
  if (!files.length) return { ...data, materials: order }
  const form = new FormData()
  for (const [key, value] of Object.entries(data)) {
    if (value === undefined || value === null) continue
    if (Array.isArray(value)) value.forEach(v => form.append(key, String(v)))
    else form.set(key, String(value))
  }
  files.forEach(f => form.append('files', f))
  form.set('materials', JSON.stringify(order))
  return form
}
/** 把已保存的素材转成编辑草稿 */
export function draftsOf(materials?: Material[] | null): MaterialDraft[] { return (materials || []).map(m => ({ key: m.id, existing: m })) }
const uploadTimeout = 180000

export interface Candidate { id: string; materials?: Material[]; knowledgeBaseId?: string; knowledgeBaseName?: string; question: string; aliases: string[]; answer: string; source: string; evidence: Array<Record<string, string>>; confidence: number; status: string; reviewNote: string; createdAt: string }
export interface Entry { id: string; materials?: Material[]; knowledgeBaseId: string; knowledgeBaseName: string; question: string; aliases: string[]; answer: string; source: string; status: string; indexStatus: string; indexError: string; version: number; hitCount: number; helpfulCount: number; unhelpfulCount: number; updatedAt: string }
export interface Gap { id: string; question: string; occurrences: number; status: string; firstSeenAt: string; lastSeenAt: string; knowledgeBaseId?: string | null; knowledgeBaseName?: string }
export interface Feedback { id: string; source: string; rating: string; comment: string; correctedAnswer: string; createdAt: string; answer: string; question: string; messageId?: string; conversationName: string; read?: boolean; knowledgeBaseId?: string | null; knowledgeBaseName?: string }
export interface MemoryMember { botId: string; botName: string; conversationId: string; conversationName: string; conversationType: string; userId: string; displayName: string; subjectId?: string; count: number }
export interface Memory {
  id: string
  subjectId?: string
  botId: string
  botName: string
  conversationId?: string
  sourceConversationName?: string
  sourceBotName?: string
  userId: string
  displayName: string
  kind: string
  content: string
  hasEmbedding: boolean
  confidence: number
  importance?: number
  emotionTag?: string
  accessCount?: number
  resolvedAt?: string | null
  pinned: boolean
  source: string
  updatedAt: string
  lastUsedAt?: string
  eventAt?: string
}
export interface IdentityLink {
  id: string
  botId?: string | null
  botName: string
  identityType: 'qq_union_id' | 'member_openid' | 'user_openid' | string
  identityValue: string
  displayName: string
  conversationId?: string | null
  conversationName?: string
}
export interface UserMoodState {
  currentMood?: string
  intensity?: number
  valence?: number
  negativeStreak?: number
  updatedAt?: string
}
export interface SubjectSummary {
  id: string
  displayName: string
  nicknames: string[]
  profileContent: string
  persona?: Persona | null
  familiarity: number
  affinity: number
  trust: number
  relationshipStage: RelationshipStage
  interactionTurns: number
  interactionDays: number
  turnsSinceReflection: number
  lastReflectedAt?: string | null
  lastInteractionAt?: string | null
  moodState?: UserMoodState
  updatedAt?: string
  memoryCount: number
  links: IdentityLink[]
}
export interface GroupMemoryItem {
  id: string
  kind: string
  content: string
  importance: number
  expiresAt?: string | null
  createdAt?: string
  updatedAt?: string
}
export interface ProactiveTrigger {
  id: string
  subjectId?: string | null
  subjectName?: string
  conversationId: string
  conversationName: string
  botName: string
  kind: string
  topic: string
  scheduledAt: string
  status: 'pending' | 'sent' | 'skipped' | 'failed' | 'cancelled' | string
  skipReason?: string
  sentMessageId?: string | null
  createdAt: string
}
export interface TurnPerception {
  replyMode?: string
  socialOnly?: boolean
  needsWebSearch?: boolean
  isChimeIn?: boolean
  userMood?: string
  moodIntensity?: number
  valence?: number
  sarcasm?: boolean
  needEmpathyFirst?: boolean
  memoryFocus?: string
  topicHint?: string
  suggestHandoff?: boolean
}
export interface RecalledMemoryItem {
  id?: string
  kind: string
  content: string
  importance?: number
  emotionTag?: string
  sourceWhere?: string
  resolved?: boolean
  score?: number
}
/** 一个人格维度：0-100 分，confidence 0-1，evidence 是来自对话的一句依据；还看不出来时为 null */
export type PersonaTrait = { score: number; confidence: number; evidence?: string } | null
export interface Persona {
  summary?: string
  observedTurns?: number
  updatedAt?: string
  bigFive?: Partial<Record<'openness' | 'conscientiousness' | 'extraversion' | 'agreeableness' | 'stability', PersonaTrait>>
  communication?: Partial<Record<'verbosity' | 'formality' | 'directness' | 'patience' | 'emotion' | 'humor' | 'decision', PersonaTrait>>
  expertise?: (NonNullable<PersonaTrait> & { domains?: string[] }) | null
  relationship?: { satisfaction?: PersonaTrait; trust?: PersonaTrait; mood?: string; trend?: 'up' | 'flat' | 'down' } | null
  roles?: string[]
  interests?: string[]
  habits?: string[]
  needs?: string[]
  avoid?: string[]
  tips?: string[]
}
export interface UserProfile { conversationId: string; userId: string; content: string; updatedAt?: string; persona?: Persona | null }
export interface LearningStats { pendingCandidates: number; publishedEntries: number; openGaps: number; feedback7d: number; negativeFeedback7d: number; unreadNegativeFeedback?: number; activeMemories: number; openHandoffs: number }
export interface HandoffMessage { id: string; direction: string; senderName: string; content: string; eventType: string; eventAt: string; status?: string; deliveryError?: string }
export type HandoffCloseMethod = 'qq' | 'web' | 'timeout'
export interface Handoff { id: string; claimedByName?: string | null; claimedById?: string | null; claimedVia?: 'qq' | 'web' | null; claimedAt?: string | null; closeMethod?: HandoffCloseMethod | null; conversationName: string; conversationType: string; userId: string; displayName: string; messageId?: string; messageContent: string; reason: string; status: string; pausedUntil?: string; createdAt: string; resolvedAt?: string; recent: HandoffMessage[]; ticketNumber?: string; humanRepliedBy?: string; humanReplyContent?: string; humanRepliedAt?: string; notifications?: Array<{ conversationId: string; status: string; error?: string }> }
export interface Member { userId: string; displayName: string; avatar?: string; joinedAt?: string; unionId?: string; role?: string; isBot?: boolean; active: boolean; isStaff: boolean; isBlocked?: boolean; blockedUserIds?: string[]; lastSeenAt: string; memoryCount: number; messageCount: number; /** 今天在这个会话里用生图模型生成了几张 */ imagesToday?: number }
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
  agentRuns?: Array<{
    rewrittenQuery?: string
    steps?: AgentStep[]
    evaluation?: Record<string, unknown>
    perception?: TurnPerception
    recalledMemories?: RecalledMemoryItem[]
    directorNotes?: string
  }>
  feedback?: Array<{ id: string; source: string; rating: string; comment: string; correctedAnswer: string; createdAt: string }>
  replies?: Array<{ id: string; eventType: string; content: string; status: string; eventAt: string }>
}

export const agentApi = {
  settings: {
    get: () => request<AgentSettings>({ url: '/agent/settings' }, mock.mockAgentSettings),
    save: (settings: AgentSettings, accessToken?: string) => request<AgentSettings>({ method: 'PUT', url: '/agent/settings', data: accessToken === undefined ? settings : { ...settings, liveDataAccessToken: accessToken } }),
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
    approve: (id: string, data: { knowledgeBaseId: string; question: string; aliases: string[]; answer: string; note?: string }, materials?: MaterialDraft[]) => request<{ entryId: string }>({ method: 'POST', url: `/knowledge/candidates/${id}/approve`, data: withMaterials(data, materials), timeout: uploadTimeout }),
    reject: (id: string, note: string) => request<{ rejected: boolean }>({ method: 'POST', url: `/knowledge/candidates/${id}/reject`, data: { note } }),
    entries: (params: { knowledgeBaseId?: string; status?: string; q?: string }) => request<Entry[]>({ url: '/knowledge/entries', params }, mock.mockEntries),
    createEntry: (data: { knowledgeBaseId: string; question: string; aliases: string[]; answer: string }, materials?: MaterialDraft[]) => request<{ id: string }>({ method: 'POST', url: '/knowledge/entries', data: withMaterials(data, materials?.length ? materials : undefined), timeout: uploadTimeout }),
    updateEntry: (id: string, data: { question: string; aliases: string[]; answer: string; status: string }, materials?: MaterialDraft[]) => request<{ updated: boolean }>({ method: 'PUT', url: `/knowledge/entries/${id}`, data: withMaterials(data, materials), timeout: uploadTimeout }),
    deleteEntry: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/knowledge/entries/${id}` }),
    gaps: (status = 'open', knowledgeBaseId = '') => request<Gap[]>({ url: '/knowledge/gaps', params: { status, ...(knowledgeBaseId ? { knowledgeBaseId } : {}) } }, mock.mockGaps),
    answerGap: (id: string, data: { knowledgeBaseId: string; question: string; answer: string; aliases: string[] }, materials?: MaterialDraft[]) => request<{ entryId: string }>({ method: 'POST', url: `/knowledge/gaps/${id}/answer`, data: withMaterials(data, materials?.length ? materials : undefined), timeout: uploadTimeout }),
    ignoreGap: (id: string) => request<{ ignored: boolean }>({ method: 'POST', url: `/knowledge/gaps/${id}/ignore` }),
    feedback: (rating = '', knowledgeBaseId = '') => request<Feedback[]>({ url: '/feedback', params: { rating, ...(knowledgeBaseId ? { knowledgeBaseId } : {}) } }, mock.mockFeedback),
    markFeedbackRead: (target: { ids: string[] } | { all: true }) => request<{ updated: number }>({ method: 'POST', url: '/feedback/read', data: target }, { updated: 0 }),
  },
  memories: {
    directory: () => request<MemoryMember[]>({ url: '/memories/directory' }, mockMemoryDirectory),
    list: (params: { subjectId?: string; userId?: string; botId?: string; conversationId?: string; q?: string }) => request<Memory[]>({ url: '/memories', params }, mockMemories),
    update: (id: string, data: { content: string; kind: string; pinned: boolean; importance?: number; emotionTag?: string; resolved?: boolean }) => request<{ updated: boolean }>({ method: 'PUT', url: `/memories/${id}`, data }),
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/memories/${id}` }),
    profile: (conversationId: string, userId: string) => {
      const url = `/conversations/${conversationId}/user-profiles/${encodeURIComponent(userId)}`
      return {
        get: () => request<UserProfile>({ url }, mockProfile),
        save: (content: string) => request<UserProfile>({ method: 'PUT', url, data: { content } }),
        clear: () => request<UserProfile>({ method: 'DELETE', url }),
      }
    },
  },
  subjects: {
    list: (params: { q?: string; stage?: string; botId?: string } = {}) => request<SubjectSummary[]>({ url: '/subjects', params }, mock.mockSubjects),
    get: (id: string) => request<SubjectSummary>({ url: `/subjects/${id}` }, mock.mockSubjects[0]),
    update: (id: string, data: { displayName?: string; profileContent?: string; nicknames?: string[] }) => request<{ updated: boolean }>({ method: 'PUT', url: `/subjects/${id}`, data }),
    remove: (id: string, mode?: 'clear_profile') => request<{ deleted?: boolean; cleared?: boolean }>({ method: 'DELETE', url: `/subjects/${id}`, params: mode ? { mode } : undefined }),
    merge: (primaryId: string, secondaryId: string) => request<{ merged: boolean; subjectId: string }>({ method: 'POST', url: '/subjects/merge', data: { primaryId, secondaryId } }),
    unlink: (id: string, linkId: string) => request<{ unlinked: boolean; newSubjectId: string }>({ method: 'POST', url: `/subjects/${id}/unlink`, data: { linkId } }),
    reflect: (id: string) => request<{ reflected: boolean }>({ method: 'POST', url: `/subjects/${id}/reflect` }),
  },
  groupMemories: {
    list: (conversationId: string) => request<{ groupVibe: string; lastDigestAt?: string; items: GroupMemoryItem[] }>({ url: `/conversations/${conversationId}/group-memories` }, mock.mockGroupMemories),
    digest: (conversationId: string) => request<{ digested: boolean }>({ method: 'POST', url: `/conversations/${conversationId}/group-memories/digest` }),
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/group-memories/${id}` }),
  },
  proactive: {
    list: (status = 'all') => request<ProactiveTrigger[]>({ url: '/proactive-triggers', params: { status } }, mock.mockProactiveTriggers),
    cancel: (id: string) => request<{ cancelled: boolean }>({ method: 'DELETE', url: `/proactive-triggers/${id}` }),
  },
  handoffs: {
    list: (status = 'active') => request<Handoff[]>({ url: '/handoffs', params: { status } }, status === 'active' || status === 'all' ? mock.mockHandoffs : []),
    claim: (id: string) => request<{ status: string }>({ method: 'POST', url: `/handoffs/${id}/claim` }),
    resolve: (id: string) => request<{ status: string; resolved: boolean }>({ method: 'POST', url: `/handoffs/${id}/resolve` }),
  },
  messages: {
    detail: (id: string) => request<MessageAgentDetail>({ url: `/messages/${id}` }, { ...mock.mockMessageAgentDetail, id }),
    reply: (id: string, text: string, learn: boolean, materials?: MaterialDraft[]) => request<{ id: string; messageIds?: string[] }>({ method: 'POST', url: `/messages/${id}/reply`, data: withMaterials({ text, learn }, materials?.length ? materials : undefined), timeout: uploadTimeout }),
    feedback: (id: string, data: { rating: string; comment?: string; correctedAnswer?: string; publish?: boolean; knowledgeBaseId?: string; question?: string }, materials?: MaterialDraft[]) => request<{ saved: boolean; entryId?: string; candidateId?: string }>({ method: 'POST', url: `/messages/${id}/feedback`, data: withMaterials(data, materials?.length ? materials : undefined), timeout: uploadTimeout }),
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
