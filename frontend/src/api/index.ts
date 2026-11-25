import client, { ApiError, mockFallbackEnabled, request } from './client'
import { connection } from '@/utils/connection'
import { modelProviderPresets } from '@/utils/model-providers'
import { mockBots, mockConversations, mockHumanOperators, mockKnowledgeBases, mockMessages, mockModels, mockOverview, mockProxies, mockCosts, mockChat } from '@/mock/data'
import type { ActiveTask, AlertItem, Bot, Conversation, KnowledgeBase, KnowledgeDocument, MessageAttachment, MessageTimeline, MessageRecord, HandoffStatus, HumanOperator, ModelKind, ModelProfile, ModelProviderId, ModelProviderPreset, ModelProxy, ModelProxyInput, WebSearchMode, ModelTestInput, ProxyTestResult, ModelTestResult, CostRange, CostSummary, Status, SystemOverview, SystemSettings, PipelineOutcome, ChatItem, ChatPage, ChatPreview } from '@/types'
import { createEmptyOverview } from '@/utils/overview'
import { rememberMentionNames } from '@/utils/mentions'

type BackendBot = { id: string; name: string; appId: string; enabled: boolean; status: string; lastEventAt?: string; hasSecret: boolean; callbackUrl?: string }
type BackendModel = { id: string; name: string; kind: ModelKind; baseUrl: string; model: string; dimension?: number; enabled: boolean; isDefault: boolean; hasApiKey: boolean; provider?: ModelProviderId; webSearchMode?: WebSearchMode; reasoningEffort?: 'default' | 'none' | 'minimal' | 'low' | 'medium' | 'high'; extraBody?: Record<string, unknown>; inputPricePerMillion?: number | null; outputPricePerMillion?: number | null; cacheReadPricePerMillion?: number | null; cacheWritePricePerMillion?: number | null; pricePerImage?: number | null; proxyId?: string | null }
type BackendConversation = { avatar?: string; lastMessage?: ChatPreview | null; imageDailyLimit?: number | null; id: string; botId: string; serviceId?: string; channel: string; platformId: string; type: 'group' | 'private'; name: string; enabled: boolean; triggerMode: 'mention_only' | 'always' | 'smart'; contextLimit: number; systemPrompt: string; chatProfileId?: string; botName: string; updatedAt?: string; messageCount?: number | string; memberCount?: number | string; hasFullMessageEvents?: boolean; knowledgeBaseIds?: string[] }
type BackendMessagePart = { waveUrl?:string; transcript?:string; type: string; text?: string; filename?: string; contentType?: string; sizeBytes?: number; width?: number; height?: number; previewUrl?: string; url?: string }
type BackendContextMessage = { name?: string; Name?: string; role?: string; Role?: string; content?: string; Content?: string; parts?: unknown; Parts?: unknown }
type BackendAgentRun = { contextMessages?: BackendContextMessage[]; retrievedChunks?: Array<{ content?: string; score?: number; documentId?: string; id?: string }> }
type BackendMessage = { mentionNames?: Record<string, string>; timeline?: MessageTimeline; id: string; question: string; parts?: unknown; answer?: string; answerParts?: unknown; senderName?: string; senderId?: string; senderBlockedIds?: string[] | null; botId?: string; conversationId?: string; conversationType?: string; eventType?: string; status: string; eventAt?: string; platformMessageId?: string; conversationName: string; botName: string; model?: string; inputTokens?: number; outputTokens?: number; cachedInputTokens?: number; cacheWriteTokens?: number; cacheHitRate?: number | null; estimatedCost?: number | null; modelLatencyMs?: number; contextLatencyMs?: number; retrievalLatencyMs?: number; deliveryLatencyMs?: number; latencyMs?: number; knowledgeHits?: number; deliveryStatus?: string; deliveryError?: string; traceId?: string; agentRuns?: BackendAgentRun[]; handoffStatus?: HandoffStatus | null; ticketNumber?: string | null; humanRepliedAt?: string | null; humanRepliedBy?: string | null; humanReplyContent?: string | null; humanDeliveryStatus?: MessageRecord['humanDeliveryStatus']; humanDeliveryError?: string | null }
type BackendKnowledgeBase = { id: string; name: string; description: string; embeddingProfileId?: string; embeddingModel: string; documentCount: number | string; entryCount?: number | string; chunkCount: number | string; createdAt?: string }
type BackendKnowledgeDocument = { id: string; name: string; status: string; sizeBytes: number; chunkCount?: number | string; imageCount?: number | string; lastError?: string; createdAt?: string; updatedAt?: string }
type BackendKnowledgeDetail = BackendKnowledgeBase & { documents: BackendKnowledgeDocument[] }
type BackendPipeline = { sender?: string; senderId?: string; mentionNames?: Record<string, string>; timeline?: MessageTimeline; id: string; time: string; bot: string; conversation: string; content: string; eventType?: string; eventMs: number; contextMs: number; contextLabel?: string; contextStatus?: Status; retrieval: { status: Status; ms: number; hit: string }; model: { status: Status; ms?: number; name?: string }; delivery: { status: Status; ms?: number }; totalMs: number }
type BackendAlert = AlertItem
type BackendOverview = { bots: number; conversations: number; messages24h: number; totalEvents?: number; triggered?: number; successful?: number; triggeredConversations?: number; averageLatencyMs?: number; slowCalls?: number; modelCalls?: number; sentDeliveries?: number; totalDeliveries?: number; pendingTasks: number; pendingInbox?: number; pendingOutbox?: number; pendingDocuments?: number; processingDocuments?: number; failedTasks?: number; readyDocuments: number; failedDocuments?: number; totalDocuments?: number; pipelines?: BackendPipeline[]; alerts?: BackendAlert[]; knowledgeQueues?: Array<{ id: string; name: string; progress: number; eta: string }>; activeTasks?: ActiveTask[]; queuedTasks?: number; processingTasks?: number; knowledge?: { documentCount: number; entryCount: number; indexedCount: number; indexedRatio: number }; stageAverages?: Array<{ label: string; averageMs: number; samples: number; startOffsetMs?: number }>; version: string }

const clone = <T>(value: T): T => structuredClone(value)
// 选填单价：没填就传 null，让后端清空。
const price = (value?: number | null) => typeof value === 'number' && Number.isFinite(value) ? value : null
const dateText = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '暂无'
const timeText = (value?: string) => value ? new Date(value).toLocaleTimeString('zh-CN', { hour12: false }) : '—'
const asStatus = (value: string, enabled = true): Status => {
  if (!enabled) return 'offline'
  if (['online', 'success', 'failed', 'processing', 'pending', 'warning', 'offline', 'unindexed', 'skipped', 'merged'].includes(value)) return value as Status
  if (value === 'sent' || value === 'completed' || value === 'ready') return 'success'
  if (value === 'expired') return 'warning'
  return 'pending'
}

// 下载类接口出错时响应体也是 Blob，要读出来才能拿到后端的错误说明。
async function blobError(error: unknown, fallback: string) {
  const response = (error as { response?: { status?: number; data?: unknown } }).response
  if (response?.data instanceof Blob) {
    try { const body = JSON.parse(await response.data.text()); if (body?.error) return new ApiError(body.error.code, body.error.message, response.status) } catch { /* 不是 JSON */ }
  }
  const down = !response || [502, 503, 504].includes(response.status || 0)
  return new ApiError(down ? 'NETWORK_ERROR' : 'HTTP_ERROR', down ? '暂时无法连接到服务' : `${fallback}（HTTP ${response?.status}）`, response?.status)
}

async function loadOrMock<T>(loader: () => Promise<T>, fallback: T): Promise<T> {
  try { return await loader() }
  catch (error) {
    if (mockFallbackEnabled && error instanceof ApiError && error.code === 'NETWORK_ERROR') { connection.usingMock = true; return clone(fallback) }
    throw error
  }
}

function mapBot(item: BackendBot): Bot {
  return { id: item.id, name: item.name, appId: item.appId, enabled: item.enabled, status: asStatus(item.status, item.enabled), callbackPath: `/callbacks/qq/${item.id}`, callbackUrl: item.callbackUrl || `/callbacks/qq/${item.id}`, lastEventAt: dateText(item.lastEventAt) }
}
function mapModel(item: BackendModel): ModelProfile {
  return { id: item.id, name: item.name, kind: item.kind, provider: item.provider || 'compatible', model: item.model, baseUrl: item.baseUrl, maskedKey: item.hasApiKey ? '••••••••••••' : '未配置', status: item.enabled ? 'online' : 'offline', latency: 0, dimension: item.dimension, webSearchMode: item.webSearchMode || 'disabled', reasoningEffort: item.reasoningEffort || 'default', extraBody: item.extraBody || {}, inputPricePerMillion: item.inputPricePerMillion ?? null, outputPricePerMillion: item.outputPricePerMillion ?? null, cacheReadPricePerMillion: item.cacheReadPricePerMillion ?? null, cacheWritePricePerMillion: item.cacheWritePricePerMillion ?? null, pricePerImage: item.pricePerImage ?? null, proxyId: item.proxyId || null }
}
function mapConversation(item: BackendConversation): Conversation {
  return { id: item.id, botId: item.botId, serviceId: item.serviceId || '', platformId: item.platformId, botName: item.botName, name: item.name || item.platformId, platform: 'QQ', type: item.type === 'private' ? 'c2c' : 'group', triggerMode: item.enabled ? (item.triggerMode === 'always' && item.type === 'group' ? 'smart' : item.triggerMode) : 'disabled', messageCount: Number(item.messageCount || 0), memberCount: Number(item.memberCount || 0), hasFullMessageEvents: Boolean(item.hasFullMessageEvents), contextLimit: item.contextLimit, knowledgeBaseNames: [], knowledgeBaseIds: item.knowledgeBaseIds || [], chatProfileId: item.chatProfileId || '', systemPrompt: item.systemPrompt, lastActiveAt: dateText(item.updatedAt), enabled: item.enabled, lastMessage: item.lastMessage ? (rememberMentionNames(item.lastMessage.mentionNames), item.lastMessage) : null, imageDailyLimit: item.imageDailyLimit ?? null, avatar: item.avatar || '' }
}
function mapMessage(item: BackendMessage): MessageRecord {
  rememberMentionNames(item.mentionNames)
  const inputTokens = Number(item.inputTokens || 0)
  const outputTokens = Number(item.outputTokens || 0)
  const run = item.agentRuns?.[0]
  const contextMessages = (run?.contextMessages || []).map(message => ({ name: message.name || message.Name || undefined, role: message.role || message.Role || 'user', content: message.content || message.Content || '', parts: mapMessageParts(message.parts || message.Parts || []) }))
  const attachments = mapMessageParts(item.parts || [])
  const answerAttachments = mapMessageParts(item.answerParts || [])
  return { mentionNames: item.mentionNames, id: item.id, time: dateText(item.eventAt), botName: item.botName, conversationName: item.conversationName, botId: item.botId, conversationId: item.conversationId, conversationType: item.conversationType === 'private' ? 'private' : item.conversationType === 'group' ? 'group' : undefined, sender: item.senderName || 'QQ用户', senderId: item.senderId || undefined, senderBlockedIds: item.senderBlockedIds || [], question: item.question || (attachments.some(p=>p.type==='voice') ? '语音消息' : attachments.length ? '图片消息' : '空消息'), answer: item.answer || '', eventType: item.eventType || 'QQ_MESSAGE', model: item.model || '未调用', status: item.handoffStatus ? 'pending' : asStatus(item.status), latency: Number(item.timeline?.totalMs ?? item.latencyMs ?? 0), tokens: inputTokens + outputTokens, inputTokens, outputTokens, cachedInputTokens: Number(item.cachedInputTokens || 0), cacheWriteTokens: Number(item.cacheWriteTokens || 0), cacheHitRate: item.cacheHitRate ?? null, estimatedCost: item.estimatedCost ?? null, knowledgeHits: Number(item.knowledgeHits || 0), deliveryStatus: item.status === 'merged' ? 'merged' : item.status === 'skipped' && !item.answer ? 'skipped' : asStatus(item.deliveryStatus || 'pending'), deliveryError: sanitizeServiceError(item.deliveryError), contextLatency: Number(item.contextLatencyMs || 0), retrievalLatency: Number(item.retrievalLatencyMs || 0), modelLatency: Number(item.modelLatencyMs || 0), deliveryLatency: Number(item.deliveryLatencyMs || 0), timeline: item.timeline, traceId: item.traceId || item.id, platformMessageId: item.platformMessageId, attachments, answerAttachments, contextMessages, retrievedChunks: run?.retrievedChunks || [], handoffStatus: item.handoffStatus || null, ticketNumber: item.ticketNumber || undefined, humanRepliedAt: item.humanRepliedAt ? dateText(item.humanRepliedAt) : undefined, humanRepliedBy: item.humanRepliedBy || undefined, humanReplyContent: item.humanReplyContent || undefined, humanDeliveryStatus: item.humanDeliveryStatus || null, humanDeliveryError: sanitizeServiceError(item.humanDeliveryError || undefined) }
}

function mapMessageParts(value: unknown): MessageAttachment[] {
  let parts = value
  if (typeof parts === 'string') {
    try { parts = JSON.parse(parts) }
    catch { return [] }
  }
  if (!Array.isArray(parts)) return []
  return parts.filter((part): part is BackendMessagePart => Boolean(part) && typeof part === 'object' && ['image','voice'].includes((part as BackendMessagePart).type)).map(part => ({ type: part.type, waveUrl:part.waveUrl,transcript:part.transcript, text: part.text, filename: part.filename, contentType: part.contentType, sizeBytes: part.sizeBytes, width: part.width, height: part.height, previewUrl: part.previewUrl || part.url }))
}
export function sanitizeServiceError(value?: string): string | undefined {
  if (!value) return undefined
  if (value.includes("invalid character '<' looking for beginning of value")) {
    return '模型接口曾返回 HTML/非 JSON，可能是 Base URL 路径、网关路由或请求参数不兼容；OpenAI 兼容地址有时需要包含 /v1。请先在向量测试中核对实际 endpoint。'
  }
  return value
    .replace(/\bsk-[A-Za-z0-9_-]{8,}\b/g, '[已隐藏密钥]')
    .replace(/\bBearer\s+[A-Za-z0-9._~-]+/gi, 'Bearer [已隐藏凭证]')
    .replace(/((?:[?&]|\b)(?:api[_-]?key|token|access[_-]?token)=)[^&\s]+/gi, '$1[已隐藏凭证]')
}
export function mapDocument(item: BackendKnowledgeDocument): KnowledgeDocument {
  return { id: item.id, name: item.name, type: (item.name.split('.').pop() || 'TXT').toUpperCase().replace('JPEG', 'JPG'), size: `${Math.max(1, Math.round(item.sizeBytes / 1024))} KB`, chunks: Number(item.chunkCount || 0), images: Number(item.imageCount || 0), status: asStatus(item.status), updatedAt: dateText(item.updatedAt || item.createdAt), lastError: sanitizeServiceError(item.lastError) }
}
function mapKnowledgeBase(item: BackendKnowledgeBase, documents: BackendKnowledgeDocument[] = []): KnowledgeBase {
  const documentCount = Number(item.documentCount ?? documents.length)
  const chunkCount = Number(item.chunkCount ?? documents.reduce((total, document) => total + Number(document.chunkCount || 0), 0))
  const ready = documents.filter((document) => document.status === 'ready').length
  const progress = documents.length ? Math.round(ready / documents.length * 100) : (chunkCount > 0 ? 100 : 0)
  const hasFailure = documents.some((document) => document.status === 'failed')
  return { id: item.id, name: item.name, description: item.description, scope: 'global', documents: documentCount, entries: Number(item.entryCount || 0), chunks: chunkCount, progress, status: hasFailure ? 'failed' : progress === 100 ? 'success' : documents.some((document) => document.status === 'processing') ? 'processing' : 'pending', model: item.embeddingModel, embeddingProfileId: item.embeddingProfileId || '', updatedAt: dateText(item.createdAt), documentList: documents.map(mapDocument) }
}

export const api = {
  auth: {
    login: (username: string, password: string) => request<{ id: string; username: string }>({ method: 'POST', url: '/auth/login', data: { username, password } }),
    logout: () => request<void>({ method: 'POST', url: '/auth/logout' }, undefined),
    me: () => request<{ id: string; username: string }>({ url: '/auth/me' }),
    changePassword: (currentPassword: string, newPassword: string) => request<{ changed: boolean }>({ method: 'PUT', url: '/auth/password', data: { currentPassword, newPassword } }),
  },
  overview: () => loadOrMock(async () => {
    const raw = await request<BackendOverview>({ url: '/system/overview' })
    raw.pipelines?.forEach(row => rememberMentionNames(row.mentionNames))
    const view = createEmptyOverview()
    const totalEvents = raw.totalEvents ?? raw.messages24h
    const triggered = raw.triggered ?? 0
    const successful = raw.successful ?? 0
    const successRate = triggered ? successful / triggered * 100 : 0
    const timeoutRate = raw.modelCalls ? (raw.slowCalls || 0) / raw.modelCalls * 100 : 0
    const deliveryRate = raw.totalDeliveries ? (raw.sentDeliveries || 0) / raw.totalDeliveries * 100 : 0
    view.messages24h = raw.messages24h
    view.successRate = triggered ? successRate : null
    view.metrics[0].value = totalEvents.toLocaleString()
    view.metrics[1].value = successful.toLocaleString()
    view.metrics[1].sub = triggered ? `(${successRate.toFixed(1)}%)` : '(暂无触发)'
    view.metrics[2].value = raw.averageLatencyMs ? `${(raw.averageLatencyMs / 1000).toFixed(2)}s` : '—'
    view.metrics[3].value = raw.modelCalls ? `${timeoutRate.toFixed(2)}%` : '—'
    view.metrics[4].value = raw.totalDeliveries ? `${deliveryRate.toFixed(1)}%` : '—'
    view.metrics[5].value = (raw.triggeredConversations ?? raw.conversations).toLocaleString()
    view.pipelines = (raw.pipelines || []).map(row => ({ ...row, time: timeText(row.time) }))
    view.alerts = (raw.alerts || []).map(alert => ({ ...alert, time: dateText(alert.time) }))
    view.knowledge.completed = raw.readyDocuments
    view.knowledge.total = raw.totalDocuments ?? raw.readyDocuments
    view.knowledge.indexing = raw.processingDocuments ?? 0
    view.knowledge.pending = raw.pendingDocuments ?? 0
    view.knowledge.failed = raw.failedDocuments ?? 0
    view.knowledge.progress = view.knowledge.total ? Math.round(raw.readyDocuments / view.knowledge.total * 100) : 0
    view.knowledge.queues = raw.knowledgeQueues || []
    view.queues.inbox = raw.pendingInbox ?? raw.pendingTasks
    view.queues.outbox = raw.pendingOutbox ?? 0
    view.queues.knowledge = (raw.pendingDocuments ?? 0) + (raw.processingDocuments ?? 0)
    view.queues.failed = raw.failedTasks ?? 0
    if (raw.queuedTasks != null) view.queues.inbox = raw.queuedTasks
    if (raw.knowledge) {
      view.knowledge.total = raw.knowledge.documentCount
      view.knowledge.entries = raw.knowledge.entryCount
      view.knowledge.progress = Math.round(raw.knowledge.indexedRatio * 100)
    }
    view.stageAverages = (raw.stageAverages || []).map(s => ({ label: s.label, ms: s.averageMs, startMs: s.startOffsetMs, status: 'success', detail: s.samples + ' 次' }))
    view.active = raw.activeTasks ?? view.pipelines
      .filter(row => row.timeline && row.timeline.totalMs == null && row.timeline.stages.some(s => s.status === 'processing' || s.status === 'pending'))
      .map(row => {
        const stages = row.timeline!.stages
        const current = stages.find(s => s.status === 'processing') ?? stages.find(s => s.status === 'pending')
        return { id: row.id, bot: row.bot, conversation: row.conversation || '', sender: '', question: row.content || '', stage: current?.label || '处理中', elapsedMs: stages.at(-1)?.elapsedMs }
      })
    return view
  }, mockOverview),
  bots: {
    list: () => loadOrMock(async () => (await request<BackendBot[]>({ url: '/bots' })).map(mapBot), mockBots),
    save: async (bot: Partial<Bot>, appSecret = '') => {
      const result = await request<{ id: string }>({ method: bot.id ? 'PUT' : 'POST', url: bot.id ? `/bots/${bot.id}` : '/bots', data: { name: bot.name, appId: bot.appId, appSecret, enabled: bot.enabled } }, { id: bot.id || `bot-${Date.now()}` })
      return result
    },
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/bots/${id}` }),
    humanOperators: (id: string) => request<HumanOperator[]>({ url: `/bots/${id}/human-operators` }, mockHumanOperators),
    setHumanOperator: (id: string, conversationId: string, enabled: boolean) => request<{ enabled: boolean }>({ method: 'PUT', url: `/bots/${id}/human-operators/${conversationId}`, data: { enabled } }),
  },
  models: {
    list: () => loadOrMock(async () => (await request<BackendModel[]>({ url: '/model-profiles' })).map(mapModel), mockModels),
    save: (profile: Partial<ModelProfile>, apiKey = '') => request<{ id: string }>({ method: profile.id ? 'PUT' : 'POST', url: profile.id ? `/model-profiles/${profile.id}` : '/model-profiles', data: { name: profile.name, kind: profile.kind, provider: profile.provider || 'compatible', baseUrl: profile.baseUrl, apiKey, model: profile.model, dimension: profile.dimension, enabled: profile.status !== 'offline', isDefault: false, webSearchMode: profile.kind === 'chat' ? profile.webSearchMode || 'disabled' : 'disabled', reasoningEffort: profile.kind === 'chat' ? profile.reasoningEffort || 'default' : 'default', extraBody: profile.kind === 'chat' ? profile.extraBody || {} : {}, inputPricePerMillion: price(profile.inputPricePerMillion), outputPricePerMillion: price(profile.outputPricePerMillion), cacheReadPricePerMillion: price(profile.cacheReadPricePerMillion), cacheWritePricePerMillion: price(profile.cacheWritePricePerMillion), pricePerImage: profile.kind === 'image' ? price(profile.pricePerImage) : null, proxyId: profile.proxyId || null } }, { id: profile.id || `model-${Date.now()}` }),
    discover: (input: { provider: ModelProviderId; baseUrl?: string; apiKey?: string; profileId?: string; proxyId?: string }) => request<string[]>({ method: 'POST', url: '/model-profiles/discover', data: input, timeout: 30000 }),
    presets: () => request<ModelProviderPreset[]>({ url: '/model-provider-presets' }).then((list) => (Array.isArray(list) && list.length ? list : modelProviderPresets)).catch(() => modelProviderPresets),
    test: (id: string, input: ModelTestInput) => request<ModelTestResult>({ method: 'POST', url: `/model-profiles/${id}/test`, data: input, timeout: 610000 }),
  },
  conversations: {
    syncName: (id: string) => request<{ name: string }>({ method: 'POST', url: `/conversations/${id}/name/sync`, timeout: 45000 }),
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/conversations/${id}` }),
    list: () => loadOrMock(async () => (await request<BackendConversation[]>({ url: '/conversations' })).map(mapConversation), mockConversations),
    detail: (id: string) => request<BackendConversation & { knowledgeBaseIds: string[] }>({ url: `/conversations/${id}` }),
    chat: async (id: string, before?: string | null, limit = 50): Promise<ChatPage> => {
      const page = await request<{ items?: Array<Omit<ChatItem, 'attachments'> & { attachments?: unknown }>; nextBefore?: string | null }>({ url: `/conversations/${id}/chat`, params: { before: before || undefined, limit } }, before ? { items: [], nextBefore: null } : mockChat(id))
      const items = (page.items || []).map(item => { rememberMentionNames(item.mentionNames); return { ...item, content: item.content || '', attachments: mapMessageParts(item.attachments) } })
      return { items, nextBefore: page.nextBefore ?? null }
    },
    sendText: (id: string, text: string, target?: { reply?: string; reference?: string }) => request<unknown>({ method: 'POST', url: `/conversations/${id}/delivery/send`, data: { kind: 'text', text, title: '', description: '', url: '', picture: '', keyboard: '', reference: target?.reference || '', event: '', reply: target?.reply || '', asset: '', seconds: 10, privateReply: false, wakeup: false } }),
    sendImage: (id: string, file: File) => { const data = new FormData(); data.set('kind', '1'); data.set('direct', 'true'); data.set('file', file); return request<unknown>({ method: 'POST', url: `/conversations/${id}/delivery/upload`, data, timeout: 600000 }) },
    save: (id: string, data: Partial<Conversation>) => request<{ id: string }>({ method: 'PUT', url: `/conversations/${id}`, data: { name: data.name, enabled: data.triggerMode !== 'disabled', triggerMode: data.triggerMode === 'disabled' ? 'mention_only' : data.triggerMode, contextLimit: data.contextLimit, serviceId: data.serviceId, systemPrompt: data.systemPrompt || '', chatProfileId: data.chatProfileId || '', knowledgeBaseIds: data.knowledgeBaseIds || [], imageDailyLimit: data.imageDailyLimit === undefined ? undefined : data.imageDailyLimit } }, { id }),
  },
  messages: {
    list: () => loadOrMock(async () => (await request<BackendMessage[]>({ url: '/messages' })).map(mapMessage), mockMessages),
    detail: (id: string) => loadOrMock(async () => mapMessage(await request<BackendMessage>({ url: `/messages/${id}` })), mockMessages.find(m => m.id === id) || { ...mockMessages[0], id }),
  },
  knowledge: {
    list: () => loadOrMock(async () => (await request<BackendKnowledgeBase[]>({ url: '/knowledge-bases' })).map((item) => mapKnowledgeBase(item)), mockKnowledgeBases),
    get: async (id: string) => { const detail = await request<BackendKnowledgeDetail>({ url: `/knowledge-bases/${id}` }); return mapKnowledgeBase(detail, detail.documents || []) },
    create: (name: string, description: string, embeddingProfileId: string) => request<{ id: string }>({ method: 'POST', url: '/knowledge-bases', data: { name, description, embeddingProfileId } }, { id: `kb-${Date.now()}` }),
    upload: (id: string, file: File) => { const data = new FormData(); data.append('file', file); return request<{ id: string; status: string }>({ method: 'POST', url: `/knowledge-bases/${id}/documents`, data }, { id: `doc-${Date.now()}`, status: 'pending' }) },
    retryDocument: (id: string, documentId: string) => request<{ queued: boolean }>({ method: 'POST', url: `/knowledge-bases/${id}/documents/${documentId}/retry` }, { queued: true }),
    deleteDocument: (id: string, documentId: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/knowledge-bases/${id}/documents/${documentId}` }, { deleted: true }),
    deleteDocumentIndex: (id: string, documentId: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/knowledge-bases/${id}/documents/${documentId}/index` }, { deleted: true }),
    changeModel: (id: string, embeddingProfileId: string) => request<{ changed: boolean }>({ method: 'PUT', url: `/knowledge-bases/${id}/embedding-profile`, data: { embeddingProfileId } }),
    // 导出整个知识库（设置、原始文档、问答、待审核、没答上的问题、评测用例）为一个 ZIP。
    exportArchive: async (id: string) => {
      try {
        const response = await client.get<Blob>(`/knowledge-bases/${id}/export`, { responseType: 'blob', timeout: 30 * 60 * 1000 })
        const name = /filename\*?=(?:UTF-8'')?"?([^";]+)"?/i.exec(String(response.headers['content-disposition'] || ''))?.[1]
        return { blob: response.data, filename: name ? decodeURIComponent(name) : `knowledge-${id}.zip` }
      } catch (error) { throw await blobError(error, '导出失败') }
    },
    // 导入为一个新知识库；导入完成后文档在后台重新索引。
    importArchive: async (file: File, embeddingProfileId: string, name: string, onProgress?: (ratio: number) => void) => {
      const data = new FormData()
      data.append('file', file)
      data.append('embeddingProfileId', embeddingProfileId)
      if (name.trim()) data.append('name', name.trim())
      return request<{ id: string; name: string; documentCount: number; entryCount: number }>({ method: 'POST', url: '/knowledge-bases/import', data, timeout: 60 * 60 * 1000, onUploadProgress: e => onProgress?.(e.total ? e.loaded / e.total : 0) })
    },
    chunkImageUrl: (id: string, chunkId: string) => `/api/knowledge-bases/${id}/chunks/${chunkId}/image`,
    test: (id: string, query: string) => loadOrMock(() => request<Array<{ id?: string; content: string; score: number; hasImage?: boolean }>>({ method: 'POST', url: `/knowledge-bases/${id}/search`, data: { query, limit: 8 } }), [{ content: '命中知识片段：机器人通过统一消息入口接收 QQ Webhook 事件。', score: 0.92 }]),
  },
  system: {
    retryFailed: () => request<{ queued: number }>({ method: 'POST', url: '/system/retry-failed' }, { queued: 0 }),
    dismissFailed: (id?: string) => request<{ dismissed: number }>({ method: 'POST', url: id ? `/system/dismiss-failed/${id}` : '/system/dismiss-failed' }, { dismissed: 0 }),
    costs: (range: CostRange = 'all') => loadOrMock(() => request<CostSummary>({ url: '/system/costs', params: { range } }), mockCosts),
    getSettings: () => request<SystemSettings>({ method: 'GET', url: '/system/settings' }),
    proxies: {
      list: () => request<ModelProxy[]>({ url: '/system/proxies' }, mockProxies),
      save: (proxy: ModelProxyInput) => request<{ id: string }>({ method: proxy.id ? 'PUT' : 'POST', url: proxy.id ? `/system/proxies/${proxy.id}` : '/system/proxies', data: { name: proxy.name, address: proxy.address, username: proxy.username, password: proxy.password || undefined, clearPassword: proxy.clearPassword || undefined } }),
      remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/system/proxies/${id}` }),
      test: (id: string) => request<ProxyTestResult>({ method: 'POST', url: `/system/proxies/${id}/test`, timeout: 15000 }),
    },
    updateSettings: (settings: SystemSettings) => request<SystemSettings>({ method: 'PUT', url: '/system/settings', data: { publicBaseUrl: settings.publicBaseUrl, defaultContextLimit: settings.defaultContextLimit, aiRequestTimeoutSeconds: settings.aiRequestTimeoutSeconds, messageRetentionDays: settings.messageRetentionDays, imageDailyLimitDefault: settings.imageDailyLimitDefault } }),
  },
}
