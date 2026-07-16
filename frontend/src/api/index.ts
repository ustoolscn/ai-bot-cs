import { ApiError, mockFallbackEnabled, request } from './client'
import { mockBots, mockConversations, mockKnowledgeBases, mockMessages, mockModels, mockOverview } from '@/mock/data'
import type { Bot, Conversation, KnowledgeBase, KnowledgeDocument, MessageRecord, ModelProfile, ModelTestInput, ModelTestResult, Status, SystemOverview, SystemSettings } from '@/types'
import { createEmptyOverview } from '@/utils/overview'

type BackendBot = { id: string; name: string; appId: string; enabled: boolean; status: string; lastEventAt?: string; hasSecret: boolean; defaultChatProfileId?: string }
type BackendModel = { id: string; name: string; kind: 'chat' | 'embedding'; baseUrl: string; model: string; dimension?: number; enabled: boolean; isDefault: boolean; hasApiKey: boolean }
type BackendConversation = { id: string; channel: string; platformId: string; type: 'group' | 'private'; name: string; enabled: boolean; triggerMode: 'mention_only' | 'always'; contextLimit: number; systemPrompt: string; chatProfileId?: string; botName: string; updatedAt?: string }
type BackendMessage = { id: string; direction: 'inbound' | 'outbound'; content: string; senderName?: string; eventType?: string; status: string; eventAt?: string; platformMessageId?: string; replyToMessageId?: string; conversationName: string; botName: string }
type BackendKnowledgeBase = { id: string; name: string; description: string; embeddingProfileId?: string; embeddingModel: string; documentCount: number | string; chunkCount: number | string; createdAt?: string }
type BackendKnowledgeDocument = { id: string; name: string; status: string; sizeBytes: number; lastError?: string; createdAt?: string }
type BackendKnowledgeDetail = BackendKnowledgeBase & { documents: BackendKnowledgeDocument[] }
type BackendOverview = { bots: number; conversations: number; messages24h: number; pendingTasks: number; pendingInbox?: number; pendingOutbox?: number; pendingDocuments?: number; processingDocuments?: number; failedTasks?: number; readyDocuments: number; totalDocuments?: number; version: string }

const clone = <T>(value: T): T => structuredClone(value)
const dateText = (value?: string) => value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '暂无'
const asStatus = (value: string, enabled = true): Status => {
  if (!enabled) return 'offline'
  if (['online', 'success', 'failed', 'processing', 'pending', 'warning', 'offline'].includes(value)) return value as Status
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
  return { id: item.id, name: item.name, kind: item.kind, provider: 'OpenAI Compatible', model: item.model, baseUrl: item.baseUrl, maskedKey: item.hasApiKey ? '••••••••••••' : '未配置', status: item.enabled ? 'online' : 'offline', latency: 0, dimension: item.dimension }
}
function mapConversation(item: BackendConversation): Conversation {
  return { id: item.id, botId: '', name: item.name || item.platformId, platform: 'QQ', type: item.type === 'private' ? 'c2c' : 'group', triggerMode: item.enabled ? item.triggerMode : 'disabled', messageCount: 0, memberCount: 0, contextLimit: item.contextLimit, knowledgeBaseNames: [], knowledgeBaseIds: [], chatProfileId: item.chatProfileId || '', systemPrompt: item.systemPrompt, lastActiveAt: dateText(item.updatedAt), enabled: item.enabled }
}
function mapMessage(item: BackendMessage, reply?: BackendMessage): MessageRecord {
  const outbound = item.direction === 'outbound'
  const status = reply ? asStatus(reply.status) : asStatus(item.status)
  return { id: item.id, time: dateText(item.eventAt), botName: item.botName, conversationName: item.conversationName, sender: item.senderName || (outbound ? '机器人' : 'QQ用户'), question: outbound ? '' : item.content, answer: reply?.content || (outbound ? item.content : ''), eventType: item.eventType || (outbound ? 'BOT_REPLY' : 'QQ_MESSAGE'), model: '查看详情', status, latency: 0, tokens: 0, knowledgeHits: 0, deliveryStatus: reply ? asStatus(reply.status) : outbound ? asStatus(item.status) : 'pending', traceId: item.platformMessageId || item.id }
}
function mapMessages(items: BackendMessage[]): MessageRecord[] {
  const replies = new Map(items.filter((item) => item.direction === 'outbound' && item.replyToMessageId).map((item) => [item.replyToMessageId as string, item]))
  const inbound = items.filter((item) => item.direction === 'inbound')
  if (inbound.length) return inbound.map((item) => mapMessage(item, item.platformMessageId ? replies.get(item.platformMessageId) : undefined))
  return items.map((item) => mapMessage(item))
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
  return { id: item.id, name: item.name, type: item.name.toLowerCase().endsWith('.md') ? 'MD' : 'TXT', size: `${Math.max(1, Math.round(item.sizeBytes / 1024))} KB`, chunks: 0, status: asStatus(item.status), updatedAt: dateText(item.createdAt), lastError: sanitizeServiceError(item.lastError) }
}
function mapKnowledgeBase(item: BackendKnowledgeBase, documents: BackendKnowledgeDocument[] = []): KnowledgeBase {
  const documentCount = Number(item.documentCount || documents.length)
  const chunkCount = Number(item.chunkCount || 0)
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
    view.messages24h = raw.messages24h
    view.metrics[0].value = raw.messages24h.toLocaleString()
    view.metrics[5].value = raw.conversations.toLocaleString()
    view.knowledge.completed = raw.readyDocuments
    view.knowledge.total = raw.totalDocuments ?? raw.readyDocuments
    view.knowledge.indexing = raw.processingDocuments ?? 0
    view.knowledge.pending = raw.pendingDocuments ?? 0
    view.knowledge.failed = raw.failedTasks ?? 0
    view.knowledge.progress = view.knowledge.total ? Math.round(raw.readyDocuments / view.knowledge.total * 100) : 0
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
    save: (profile: Partial<ModelProfile>, apiKey = '') => request<{ id: string }>({ method: profile.id ? 'PUT' : 'POST', url: profile.id ? `/model-profiles/${profile.id}` : '/model-profiles', data: { name: profile.name, kind: profile.kind, baseUrl: profile.baseUrl, apiKey, model: profile.model, dimension: profile.dimension, enabled: profile.status !== 'offline', isDefault: false } }, { id: profile.id || `model-${Date.now()}` }),
    test: (id: string, input: ModelTestInput) => request<ModelTestResult>({ method: 'POST', url: `/model-profiles/${id}/test`, data: input, timeout: 610000 }),
  },
  conversations: {
    list: () => loadOrMock(async () => (await request<BackendConversation[]>({ url: '/conversations' })).map(mapConversation), mockConversations),
    detail: (id: string) => request<BackendConversation & { knowledgeBaseIds: string[] }>({ url: `/conversations/${id}` }),
    save: (id: string, data: Partial<Conversation>) => request<{ id: string }>({ method: 'PUT', url: `/conversations/${id}`, data: { name: data.name, enabled: data.triggerMode !== 'disabled' && data.enabled, triggerMode: data.triggerMode === 'disabled' ? 'mention_only' : data.triggerMode, contextLimit: data.contextLimit, systemPrompt: data.systemPrompt || '', chatProfileId: data.chatProfileId || '', knowledgeBaseIds: data.knowledgeBaseIds || [] } }, { id }),
  },
  messages: {
    list: () => loadOrMock(async () => mapMessages(await request<BackendMessage[]>({ url: '/messages' })), mockMessages),
    detail: (id: string) => request<Record<string, unknown>>({ url: `/messages/${id}` }),
  },
  knowledge: {
    list: () => loadOrMock(async () => (await request<BackendKnowledgeBase[]>({ url: '/knowledge-bases' })).map((item) => mapKnowledgeBase(item)), mockKnowledgeBases),
    get: async (id: string) => { const detail = await request<BackendKnowledgeDetail>({ url: `/knowledge-bases/${id}` }); return mapKnowledgeBase(detail, detail.documents || []) },
    create: (name: string, description: string, embeddingProfileId: string) => request<{ id: string }>({ method: 'POST', url: '/knowledge-bases', data: { name, description, embeddingProfileId } }, { id: `kb-${Date.now()}` }),
    upload: (id: string, file: File) => { const data = new FormData(); data.append('file', file); return request<{ id: string; status: string }>({ method: 'POST', url: `/knowledge-bases/${id}/documents`, data }, { id: `doc-${Date.now()}`, status: 'pending' }) },
    retryDocument: (id: string, documentId: string) => request<{ queued: boolean }>({ method: 'POST', url: `/knowledge-bases/${id}/documents/${documentId}/retry` }, { queued: true }),
    test: (id: string, query: string) => loadOrMock(() => request<Array<{ content: string; score: number }>>({ method: 'POST', url: `/knowledge-bases/${id}/search`, data: { query, limit: 8 } }), [{ content: '命中知识片段：机器人通过统一消息入口接收 QQ Webhook 事件。', score: 0.92 }]),
  },
  system: {
    retryFailed: () => request<{ queued: number }>({ method: 'POST', url: '/system/retry-failed' }, { queued: 0 }),
    getSettings: () => request<SystemSettings>({ method: 'GET', url: '/system/settings' }),
    updateSettings: (settings: SystemSettings) => request<SystemSettings>({ method: 'PUT', url: '/system/settings', data: { defaultContextLimit: settings.defaultContextLimit, aiRequestTimeoutSeconds: settings.aiRequestTimeoutSeconds, messageRetentionDays: settings.messageRetentionDays } }),
  },
}
