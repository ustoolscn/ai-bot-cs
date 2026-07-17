export interface ApiEnvelope<T> { data: T }
export interface ApiErrorEnvelope { error: { code: string; message: string } }

export type Status = 'online' | 'offline' | 'warning' | 'success' | 'failed' | 'processing' | 'pending' | 'unindexed'

export interface Bot {
  id: string
  name: string
  appId: string
  status: Status
  enabled: boolean
  callbackPath: string
  lastEventAt: string
  modelProfileId: string
}

export interface ModelProfile {
  id: string
  name: string
  kind: 'chat' | 'embedding'
  provider: string
  model: string
  baseUrl: string
  maskedKey: string
  status: Status
  latency: number
  dimension?: number
  webSearchMode: 'disabled' | 'qwen' | 'openai' | 'responses' | 'custom'
  reasoningEffort: 'default' | 'none' | 'minimal' | 'low' | 'medium' | 'high'
  extraBody: Record<string, unknown>
}

export interface ModelTestInput {
  input: string
  systemPrompt?: string
}

export type ModelTestResult =
  | { kind: 'chat'; model: string; content: string; latencyMs: number; inputTokens: number; outputTokens: number }
  | { kind: 'embedding'; model: string; dimensions: number; vectorPreview: number[]; latencyMs: number }

export interface Conversation {
  id: string
  botId: string
  platformId: string
  botName: string
  name: string
  platform: 'QQ'
  type: 'group' | 'c2c'
  triggerMode: 'mention_only' | 'always' | 'disabled'
  messageCount: number
  memberCount: number
  hasFullMessageEvents: boolean
  contextLimit: number
  knowledgeBaseNames: string[]
  knowledgeBaseIds: string[]
  chatProfileId: string
  systemPrompt: string
  lastActiveAt: string
  enabled: boolean
}

export interface MessageRecord {
  id: string
  time: string
  botName: string
  conversationName: string
  sender: string
  question: string
  answer: string
  eventType: string
  model: string
  status: Status
  latency: number
  tokens: number
  inputTokens: number
  outputTokens: number
  knowledgeHits: number
  deliveryStatus: Status
  deliveryError?: string
  contextLatency: number
  retrievalLatency: number
  modelLatency: number
  deliveryLatency: number
  traceId: string
  platformMessageId?: string
  attachments: MessageAttachment[]
  answerAttachments: MessageAttachment[]
  contextMessages?: ContextMessage[]
  retrievedChunks?: Array<{ content?: string; score?: number; documentId?: string; id?: string }>
}

export interface ContextMessage {
  role: 'system' | 'developer' | 'user' | 'assistant' | string
  content: string
  parts?: MessageAttachment[]
}

export interface MessageAttachment {
  type: string
  text?: string
  filename?: string
  contentType?: string
  sizeBytes?: number
  width?: number
  height?: number
  previewUrl?: string
}

export interface KnowledgeDocument {
  id: string
  name: string
  type: 'TXT' | 'MD'
  size: string
  chunks: number
  status: Status
  updatedAt: string
  lastError?: string
}

export interface KnowledgeBase {
  id: string
  name: string
  description: string
  scope: 'global' | 'bot' | 'conversation'
  documents: number
  chunks: number
  progress: number
  status: Status
  model: string
  updatedAt: string
  documentList: KnowledgeDocument[]
}

export interface PipelineRow {
  id: string
  time: string
  bot: string
  conversation?: string
  content?: string
  eventType?: string
  eventMs: number
  contextMs: number
  contextLabel?: string
  contextStatus?: Status
  retrieval: { status: Status; ms: number; hit: string }
  model: { status: Status; ms?: number; name?: string }
  delivery: { status: Status; ms?: number }
  totalMs: number
}

export interface AlertItem {
  id: string
  level: 'critical' | 'warning' | 'info'
  time: string
  bot: string
  content: string
  impact: string
  recovered: boolean
}

export interface SystemOverview {
  messages24h: number | null
  successRate: number | null
  metrics: Array<{ label: string; value: string; sub?: string; trend?: string; direction?: 'up' | 'down'; good?: boolean }>
  pipelines: PipelineRow[]
  alerts: AlertItem[]
  knowledge: { progress: number; total: number; completed: number; indexing: number; failed: number; pending: number; queues: Array<{ name: string; progress: number; eta: string }> }
  queues: { inbox: number; outbox: number; knowledge: number; failed: number }
}

export interface SystemSettings {
  defaultContextLimit: number
  aiRequestTimeoutSeconds: number
  messageRetentionDays: number
  updatedAt?: string
}
