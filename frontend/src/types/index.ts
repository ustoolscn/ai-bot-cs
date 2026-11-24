export interface ApiEnvelope<T> { data: T }
export interface ApiErrorEnvelope { error: { code: string; message: string } }

export type Status = 'online' | 'offline' | 'warning' | 'success' | 'failed' | 'processing' | 'pending' | 'unindexed' | 'skipped' | 'merged'

export interface Bot {
  id: string
  name: string
  appId: string
  status: Status
  enabled: boolean
  callbackPath: string
  callbackUrl: string
  lastEventAt: string
}

export type ModelProviderId = 'compatible' | 'deepseek' | 'zhipu' | 'typesafe'
/** decision = 只做判断、不生成文字的模型（目前仅 TypeSafe Jev） */
export type ModelKind = 'chat' | 'embedding' | 'image' | 'decision'
export type WebSearchMode = 'disabled' | 'qwen' | 'openai' | 'responses' | 'custom' | 'deepseek' | 'zhipu'

export interface ModelPresetModel {
  kind: ModelKind
  model: string
  label: string
  supportsWebSearch?: boolean
  dimension?: number
}

export interface ModelProviderPreset {
  id: Exclude<ModelProviderId, 'compatible'>
  name: string
  baseUrl: string
  webSearchMode: WebSearchMode
  keyUrl?: string
  models: ModelPresetModel[]
}

export interface ModelProfile {
  id: string
  name: string
  kind: ModelKind
  /** compatible = 第三方 OpenAI 兼容接口；其余为官方预置服务商 */
  provider: ModelProviderId
  model: string
  baseUrl: string
  maskedKey: string
  status: Status
  latency: number
  dimension?: number
  webSearchMode: WebSearchMode
  reasoningEffort: 'default' | 'none' | 'minimal' | 'low' | 'medium' | 'high'
  extraBody: Record<string, unknown>
  inputPricePerMillion?: number | null
  outputPricePerMillion?: number | null
  cacheReadPricePerMillion?: number | null
  cacheWritePricePerMillion?: number | null
  /** 生图模型：每张价格（元） */
  pricePerImage?: number | null
  /** null 表示直连 */
  proxyId?: string | null
}

export interface ModelProxy {
  id: string
  name: string
  /** 不含账号密码的地址 */
  address: string
  username: string
  hasPassword: boolean
}

export interface ModelProxyInput {
  id?: string
  name: string
  address: string
  username: string
  /** 留空表示保持原密码 */
  password: string
  clearPassword: boolean
}

export interface ProxyTestResult {
  reachable: boolean
  latencyMs: number
  error: string
}

export interface HumanOperator {
  conversationId: string
  userOpenId: string
  name: string
  enabled: boolean
}

export type HandoffStatus = 'waiting_human' | 'human_processing' | 'human_closed'

export interface ModelTestInput {
  input: string
  systemPrompt?: string
  /** 联网测试：要求本次请求带上联网搜索 */
  webSearch?: boolean
}

export interface ModelSearchSource {
  title?: string
  url: string
  siteName?: string
}

export type ModelTestResult =
  | { kind: 'chat'; model: string; content: string; latencyMs: number; inputTokens: number; outputTokens: number; searched?: boolean; searchQueries?: string[]; sources?: ModelSearchSource[] }
  | { kind: 'embedding'; model: string; dimensions: number; vectorPreview: number[]; latencyMs: number }
  | { kind: 'image'; model: string; imageUrl?: string; latencyMs: number }
  | { kind: 'decision'; model: string; content: string; decision: ModelDecision; latencyMs: number; inputTokens: number; outputTokens: number }

export interface ModelDecision { reply: boolean; socialOnly: boolean; needsWebSearch: boolean }

export interface Conversation {
  serviceId?: string
  id: string
  botId: string
  platformId: string
  botName: string
  name: string
  platform: 'QQ'
  type: 'group' | 'c2c'
  triggerMode: 'mention_only' | 'always' | 'smart' | 'disabled'
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

export interface TimingStage {
  label: string
  status: string
  detail?: string
  ms?: number | null
  elapsedMs?: number | null
  startMs?: number | null
}
export interface MessageTimeline {
  stages: TimingStage[]
  totalMs: number | null
}

export interface MessageRecord {
  timeline?: MessageTimeline
  id: string
  time: string
  botName: string
  botId?: string
  conversationName: string
  conversationId?: string
  conversationType?: 'group' | 'private'
  sender: string
  senderId?: string
  /** 当前对这个发送者生效的黑名单记录（本群 + 机器人全局），空表示没拉黑 */
  senderBlockedIds?: string[]
  question: string
  answer: string
  eventType: string
  model: string
  status: Status
  latency: number
  tokens: number
  inputTokens: number
  outputTokens: number
  cachedInputTokens?: number
  cacheWriteTokens?: number
  cacheHitRate?: number | null
  estimatedCost?: number | null
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
  handoffStatus?: HandoffStatus | null
  ticketNumber?: string
  humanRepliedAt?: string
  humanRepliedBy?: string
  humanReplyContent?: string
  humanDeliveryStatus?: 'pending' | 'processing' | 'sent' | 'failed' | 'expired' | null
  humanDeliveryError?: string
}

export interface ContextMessage {
  name?: string
  role: 'system' | 'developer' | 'user' | 'assistant' | string
  content: string
  parts?: MessageAttachment[]
}

export interface MessageAttachment {
  waveUrl?:string
  transcript?:string
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
  entries?: number
  chunks: number
  progress: number
  status: Status
  model: string
  updatedAt: string
  documentList: KnowledgeDocument[]
}

export interface PipelineRow {
  timeline?: MessageTimeline
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

/** 正在处理中的一条消息：哪个机器人在哪个群回复谁的什么问题，走到哪一步。 */
export interface ActiveTask {
  id: string
  bot: string
  conversation: string
  sender: string
  question: string
  stage: string
  elapsedMs?: number | null
}

export interface SystemOverview {
  active: ActiveTask[]
  messages24h: number | null
  successRate: number | null
  metrics: Array<{ label: string; value: string; sub?: string; trend?: string; direction?: 'up' | 'down'; good?: boolean }>
  pipelines: PipelineRow[]
  alerts: AlertItem[]
  stageAverages: TimingStage[]
  knowledge: { entries: number | null; progress: number; total: number; completed: number; indexing: number; failed: number; pending: number; queues: Array<{ name: string; progress: number; eta: string }> }
  queues: { inbox: number; outbox: number; knowledge: number; failed: number }
}

export interface SystemSettings {
  publicBaseUrl: string
  defaultContextLimit: number
  aiRequestTimeoutSeconds: number
  messageRetentionDays: number
  updatedAt?: string
}

export type CostRange = 'today' | '7d' | '30d' | 'all'
export interface CostSummary {
  /** 北京时间当天，按当前价格估算（元） */
  today: number | null
  /** 累计，按当前价格估算（元） */
  total: number | null
  byKind: Array<{ kind: 'chat' | 'embedding' | 'image'; cost: number; inputTokens: number; outputTokens: number; images: number }>
  byConversation: Array<{ id: string; name: string; cost: number }>
  byBot: Array<{ id: string; name: string; cost: number }>
  unpricedModels: string[]
}
