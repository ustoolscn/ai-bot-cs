import { ApiError, mockFallbackEnabled, request } from './client'
import { mockBots, mockConversations, mockKnowledgeBases, mockMessages, mockModels, mockOverview } from '@/mock/data'
import type { Bot, Conversation, KnowledgeBase, KnowledgeDocument, MessageAttachment, MessageRecord, ModelProfile, ModelTestInput, ModelTestResult, Status, SystemOverview, SystemSettings } from '@/types'
import { createEmptyOverview } from '@/utils/overview'

type BackendBot = { id: string; name: string; appId: string; enabled: boolean; status: string; lastEventAt?: string; hasSecret: boolean; defaultChatProfileId?: string }
type BackendModel = { id: string; name: string; kind: 'chat' | 'embedding'; baseUrl: string; model: string; dimension?: number; enabled: boolean; isDefault: boolean; hasApiKey: boolean; webSearchMode?: 'disabled' | 'qwen' | 'openai' | 'responses' | 'custom'; reasoningEffort?: 'default' | 'none' | 'minimal' | 'low' | 'medium' | 'high'; extraBody?: Record<string, unknown> }
type BackendConversation = { id: string; channel: string; platformId: string; type: 'group' | 'private'; name: string; enabled: boolean; triggerMode: 'mention_only' | 'always'; contextLimit: number; systemPrompt: string; chatProfileId?: string; botName: string; updatedAt?: string; messageCount?: number | string; memberCount?: number | string; hasFullMessageEvents?: boolean }
type BackendMessagePart = { type: string; text?: string; filename?: string; contentType?: string; sizeBytes?: number; width?: number; height?: number; previewUrl?: string; url?: string }
type BackendContextMessage = { role?: string; Role?: string; content?: string; Content?: string; parts?: unknown; Parts?: unknown }
type BackendAgentRun = { contextMessages?: BackendContextMessage[]; retrievedChunks?: Array<{ content?: string; score?: number; documentId?: string; id?: string }> }
type BackendMessage = { id: string; question: string; parts?: unknown; answer?: string; answerParts?: unknown; senderName?: string; eventType?: string; status: string; eventAt?: string; platformMessageId?: string; conversationName: string; botName: string; model?: string; inputTokens?: number; outputTokens?: number; modelLatencyMs?: number; contextLatencyMs?: number; retrievalLatencyMs?: number; deliveryLatencyMs?: number; latencyMs?: number; knowledgeHits?: number; deliveryStatus?: string; traceId?: string; agentRuns?: BackendAgentRun[] }
type BackendKnowledgeBase = { id: string; name: string; description: string; embeddingProfileId?: string; embeddingModel: string; documentCount: number | string; chunkCount: number | string; createdAt?: string }
type BackendKnowledgeDocument = { id: string; name: string; status: string; sizeBytes: number; chunkCount?: number | string; lastError?: string; createdAt?: string; updatedAt?: string }
type BackendKnowledgeDetail = BackendKnowledgeBase & { documents: BackendKnowledgeDocument[] }
type BackendPipeline = { id: string; time: string; bot: string; conversation: string; content: string; eventType?: string; eventMs: number; contextMs: number; contextLabel?: string; contextStatus?: Status; retrieval: { status: Status; ms: number; hit: string }; model: { status: Status; ms?: number; name?: string }; delivery: { status: Status; ms?: number }; totalMs: number }
type BackendAlert = { id: string; level: 'critical' | 'warning' | 'info'; time: string; bot: string; content: string; impact: string; recovered: boolean }
type BackendOverview = { bots: number; conversations: number; messages24h: number; totalEvents?: number; triggered?: number; successful?: number; triggeredConversations?: number; averageLatencyMs?: number; slowCalls?: number; modelCalls?: number; sentDeliveries?: number; totalDeliveries?: number; pendingTasks: number; pendingInbox?: number; pendingOutbox?: number; pendingDocuments?: number; processingDocuments?: number; failedTasks?: number; readyDocuments: number; failedDocuments?: number; totalDocuments?: number; pipelines?: BackendPipeline[]; alerts?: BackendAlert[]; knowledgeQueues?: Array<{ id: string; name: string; progress: number; eta: string }>; version: string }

const clone = <T>(value: T): T => structuredClone(value)
const dateText = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '暂无'
const timeText = (value?: string) => value ? new Date(value).toLocaleTimeString('zh-CN', { hour12: false }) : '—'
const asStatus = (value: string, enabled = true): Status => {
  if (!enabled) return 'offline'
  if (['online', 'success', 'failed', 'processing', 'pending', 'warning', 'offline', 'unindexed'].includes(value)) return value as Status
  if (value === 'sent' || value === 'completed' || value === 'ready') return 'success'
  if (value === 'expired') return 'warning'
  return 'pending'
}

async function loadOrMock<T>(loader: () => Promise<T>, fallback: T): Promise<T> {
  try { return await loader() }
  catch (error) {
    if (mockFallbackEnabled && error instanceof ApiError && error.code === 'NETWORK_ERROR') return clone(fallback)
    throw error
  }
}

function mapBot(item: BackendBot): Bot {
  return { id: item.id, name: item.name, appId: item.appId, enabled: item.enabled, status: asStatus(item.status, item.enabled), callbackPath: `/callbacks/qq/${item.id}`, lastEventAt: dateText(item.lastEventAt), modelProfileId: item.defaultChatProfileId || '' }
}
function mapModel(item: BackendModel): ModelProfile {
  return { id: item.id, name: item.name, kind: item.kind, provider: 'OpenAI Compatible', model: item.model, baseUrl: item.baseUrl, maskedKey: item.hasApiKey ? '••••••••••••' : '未配置', status: item.enabled ? 'online' : 'offline', latency: 0, dimension: item.dimension, webSearchMode: item.webSearchMode || 'disabled', reasoningEffort: item.reasoningEffort || 'default', extraBody: item.extraBody || {} }
}
function mapConversation(item: BackendConversation): Conversation {
  return { id: item.id, botId: '', platformId: item.platformId, botName: item.botName, name: item.name || item.platformId, platform: 'QQ', type: item.type === 'private' ? 'c2c' : 'group', triggerMode: item.enabled ? item.triggerMode : 'disabled', messageCount: Number(item.messageCount || 0), memberCount: Number(item.memberCount || 0), hasFullMessageEvents: Boolean(item.hasFullMessageEvents), contextLimit: item.contextLimit, knowledgeBaseNames: [], knowledgeBaseIds: [], chatProfileId: item.chatProfileId || '', systemPrompt: item.systemPrompt, lastActiveAt: dateText(item.updatedAt), enabled: item.enabled }
}
function mapMessage(item: BackendMessage): MessageRecord {
  const inputTokens = Number(item.inputTokens || 0)
  const outputTokens = Number(item.outputTokens || 0)
  const run = item.agentRuns?.[0]
  const contextMessages = (run?.contextMessages || []).map(message => ({ role: message.role || message.Role || 'user', content: message.content || message.Content || '', parts: mapMessageParts(message.parts || message.Parts || []) }))
  const attachments = mapMessageParts(item.parts || [])
  const answerAttachments = mapMessageParts(item.answerParts || [])
  return { id: item.id, time: dateText(item.eventAt), botName: item.botName, conversationName: item.conversationName, sender: item.senderName || 'QQ用户', question: item.question || (attachments.length ? '图片消息' : '空消息'), answer: item.answer || '', eventType: item.eventType || 'QQ_MESSAGE', model: item.model || '未调用', status: asStatus(item.status), latency: Number(item.latencyMs || 0), tokens: inputTokens + outputTokens, inputTokens, outputTokens, knowledgeHits: Number(item.knowledgeHits || 0), deliveryStatus: asStatus(item.deliveryStatus || 'pending'), contextLatency: Number(item.contextLatencyMs || 0), retrievalLatency: Number(item.retrievalLatencyMs || 0), modelLatency: Number(item.modelLatencyMs || 0), deliveryLatency: Number(item.deliveryLatencyMs || 0), traceId: item.traceId || item.id, platformMessageId: item.platformMessageId, attachments, answerAttachments, contextMessages, retrievedChunks: run?.retrievedChunks || [] }
}

function mapMessageParts(value: unknown): MessageAttachment[] {
  let parts = value
  if (typeof parts === 'string') {
    try { parts = JSON.parse(parts) }
    catch { return [] }
  }
  if (!Array.isArray(parts)) return []
  return parts.filter((part): part is BackendMessagePart => Boolean(part) && typeof part === 'object' && (part as BackendMessagePart).type === 'image').map(part => ({ type: part.type, text: part.text, filename: part.filename, contentType: part.contentType, sizeBytes: part.sizeBytes, width: part.width, height: part.height, previewUrl: part.previewUrl || part.url }))
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
  return { id: item.id, name: item.name, type: item.name.toLowerCase().endsWith('.md') ? 'MD' : 'TXT', size: `${Math.max(1, Math.round(item.sizeBytes / 1024))} KB`, chunks: Number(item.chunkCount || 0), status: asStatus(item.status), updatedAt: dateText(item.updatedAt || item.createdAt), lastError: sanitizeServiceError(item.lastError) }
}
function mapKnowledgeBase(item: BackendKnowledgeBase, documents: BackendKnowledgeDocument[] = []): KnowledgeBase {
  const documentCount = Number(item.documentCount ?? documents.length)
  const chunkCount = Number(item.chunkCount ?? documents.reduce((total, document) => total + Number(document.chunkCount || 0), 0))
  const ready = documents.filter((document) => document.status === 'ready').length
  const progress = documents.length ? Math.round(ready / documents.length * 100) : (chunkCount > 0 ? 100 : 0)
  const hasFailure = documents.some((document) => document.status === 'failed')
  return { id: item.id, name: item.name, description: item.description, scope: 'global', documents: documentCount, chunks: chunkCount, progress, status: hasFailure ? 'failed' : progress === 100 ? 'success' : documents.some((document) => document.status === 'processing') ? 'processing' : 'pending', model: item.embeddingModel, updatedAt: dateText(item.createdAt), documentList: documents.map(mapDocument) }
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
    return view
  }, mockOverview),
  bots: {
    list: () => loadOrMock(async () => (await request<BackendBot[]>({ url: '/bots' })).map(mapBot), mockBots),
    save: async (bot: Partial<Bot>, appSecret = '') => {
      const result = await request<{ id: string }>({ method: bot.id ? 'PUT' : 'POST', url: bot.id ? `/bots/${bot.id}` : '/bots', data: { name: bot.name, appId: bot.appId, appSecret, enabled: bot.enabled, modelProfileId: bot.modelProfileId } }, { id: bot.id || `bot-${Date.now()}` })
      return result
    },
    remove: (id: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/bots/${id}` }),
  },
  models: {
    list: () => loadOrMock(async () => (await request<BackendModel[]>({ url: '/model-profiles' })).map(mapModel), mockModels),
    save: (profile: Partial<ModelProfile>, apiKey = '') => request<{ id: string }>({ method: profile.id ? 'PUT' : 'POST', url: profile.id ? `/model-profiles/${profile.id}` : '/model-profiles', data: { name: profile.name, kind: profile.kind, baseUrl: profile.baseUrl, apiKey, model: profile.model, dimension: profile.dimension, enabled: profile.status !== 'offline', isDefault: false, webSearchMode: profile.kind === 'chat' ? profile.webSearchMode || 'disabled' : 'disabled', reasoningEffort: profile.kind === 'chat' ? profile.reasoningEffort || 'default' : 'default', extraBody: profile.kind === 'chat' ? profile.extraBody || {} : {} } }, { id: profile.id || `model-${Date.now()}` }),
    test: (id: string, input: ModelTestInput) => request<ModelTestResult>({ method: 'POST', url: `/model-profiles/${id}/test`, data: input, timeout: 610000 }),
  },
  conversations: {
    list: () => loadOrMock(async () => (await request<BackendConversation[]>({ url: '/conversations' })).map(mapConversation), mockConversations),
    detail: (id: string) => request<BackendConversation & { knowledgeBaseIds: string[] }>({ url: `/conversations/${id}` }),
    save: (id: string, data: Partial<Conversation>) => request<{ id: string }>({ method: 'PUT', url: `/conversations/${id}`, data: { name: data.name, enabled: data.triggerMode !== 'disabled', triggerMode: data.triggerMode === 'disabled' ? 'mention_only' : data.triggerMode, contextLimit: data.contextLimit, systemPrompt: data.systemPrompt || '', chatProfileId: data.chatProfileId || '', knowledgeBaseIds: data.knowledgeBaseIds || [] } }, { id }),
  },
  messages: {
    list: () => loadOrMock(async () => (await request<BackendMessage[]>({ url: '/messages' })).map(mapMessage), mockMessages),
    detail: async (id: string) => mapMessage(await request<BackendMessage>({ url: `/messages/${id}` })),
  },
  knowledge: {
    list: () => loadOrMock(async () => (await request<BackendKnowledgeBase[]>({ url: '/knowledge-bases' })).map((item) => mapKnowledgeBase(item)), mockKnowledgeBases),
    get: async (id: string) => { const detail = await request<BackendKnowledgeDetail>({ url: `/knowledge-bases/${id}` }); return mapKnowledgeBase(detail, detail.documents || []) },
    create: (name: string, description: string, embeddingProfileId: string) => request<{ id: string }>({ method: 'POST', url: '/knowledge-bases', data: { name, description, embeddingProfileId } }, { id: `kb-${Date.now()}` }),
    upload: (id: string, file: File) => { const data = new FormData(); data.append('file', file); return request<{ id: string; status: string }>({ method: 'POST', url: `/knowledge-bases/${id}/documents`, data }, { id: `doc-${Date.now()}`, status: 'pending' }) },
    retryDocument: (id: string, documentId: string) => request<{ queued: boolean }>({ method: 'POST', url: `/knowledge-bases/${id}/documents/${documentId}/retry` }, { queued: true }),
    deleteDocument: (id: string, documentId: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/knowledge-bases/${id}/documents/${documentId}` }, { deleted: true }),
    deleteDocumentIndex: (id: string, documentId: string) => request<{ deleted: boolean }>({ method: 'DELETE', url: `/knowledge-bases/${id}/documents/${documentId}/index` }, { deleted: true }),
    test: (id: string, query: string) => loadOrMock(() => request<Array<{ content: string; score: number }>>({ method: 'POST', url: `/knowledge-bases/${id}/search`, data: { query, limit: 8 } }), [{ content: '命中知识片段：机器人通过统一消息入口接收 QQ Webhook 事件。', score: 0.92 }]),
  },
  system: {
    retryFailed: () => request<{ queued: number }>({ method: 'POST', url: '/system/retry-failed' }, { queued: 0 }),
    getSettings: () => request<SystemSettings>({ method: 'GET', url: '/system/settings' }),
    updateSettings: (settings: SystemSettings) => request<SystemSettings>({ method: 'PUT', url: '/system/settings', data: { defaultContextLimit: settings.defaultContextLimit, aiRequestTimeoutSeconds: settings.aiRequestTimeoutSeconds, messageRetentionDays: settings.messageRetentionDays } }),
  },
}
