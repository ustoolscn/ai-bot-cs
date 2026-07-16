import type { AlertItem, Bot, Conversation, KnowledgeBase, MessageRecord, ModelProfile, SystemOverview } from '@/types'

export const mockBots: Bot[] = [
  { id: 'bot-qq-service', name: '客服小Q', appId: '102*******31', status: 'online', enabled: true, callbackPath: '/callbacks/qq/bot-qq-service', lastEventAt: '刚刚', modelProfileId: 'chat-qwen-plus' },
  { id: 'bot-knowledge', name: '知识助手', appId: '102*******86', status: 'online', enabled: true, callbackPath: '/callbacks/qq/bot-knowledge', lastEventAt: '2 分钟前', modelProfileId: 'chat-qwen-max' },
  { id: 'bot-campaign', name: '活动小助手', appId: '102*******12', status: 'warning', enabled: true, callbackPath: '/callbacks/qq/bot-campaign', lastEventAt: '18 分钟前', modelProfileId: 'chat-qwen-plus' },
]

export const mockModels: ModelProfile[] = [
  { id: 'chat-qwen-plus', name: '通用对话模型', kind: 'chat', provider: 'OpenAI Compatible', model: 'qwen-plus', baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', maskedKey: 'sk-••••••••7K2A', status: 'online', latency: 720, webSearchMode: 'qwen', reasoningEffort: 'default', extraBody: {} },
  { id: 'chat-qwen-max', name: '高质量对话模型', kind: 'chat', provider: 'OpenAI Compatible', model: 'qwen-max', baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', maskedKey: 'sk-••••••••9M4P', status: 'online', latency: 930, webSearchMode: 'disabled', reasoningEffort: 'default', extraBody: {} },
  { id: 'embedding-v3', name: '知识向量模型', kind: 'embedding', provider: 'OpenAI Compatible', model: 'text-embedding-v3', baseUrl: 'https://dashscope.aliyuncs.com/compatible-mode/v1', maskedKey: 'sk-••••••••2H8N', status: 'online', latency: 180, dimension: 1024, webSearchMode: 'disabled', reasoningEffort: 'default', extraBody: {} },
]

export const mockConversations: Conversation[] = [
  { id: 'group-1001', botId: 'bot-qq-service', platformId: 'qq-group-1001', botName: '客服小Q', name: '产品运营交流群', platform: 'QQ', type: 'group', triggerMode: 'mention_only', messageCount: 12842, memberCount: 486, hasFullMessageEvents: true, contextLimit: 20, knowledgeBaseNames: ['产品手册库', '常见问题库'], knowledgeBaseIds: ['kb-products', 'kb-faq'], chatProfileId: 'chat-qwen-plus', systemPrompt: '你是群内的 AI 助手，请根据知识库准确、简洁地回答问题。', lastActiveAt: '刚刚', enabled: true },
  { id: 'group-1002', botId: 'bot-knowledge', platformId: 'qq-group-1002', botName: '知识助手', name: '售后服务群', platform: 'QQ', type: 'group', triggerMode: 'mention_only', messageCount: 8631, memberCount: 238, hasFullMessageEvents: false, contextLimit: 20, knowledgeBaseNames: ['售后知识库'], knowledgeBaseIds: ['kb-after-sale'], chatProfileId: 'chat-qwen-max', systemPrompt: '你是售后服务助手，请优先依据售后知识库回答。', lastActiveAt: '1 分钟前', enabled: true },
  { id: 'group-1003', botId: 'bot-campaign', platformId: 'qq-group-1003', botName: '活动小助手', name: '夏季活动执行群', platform: 'QQ', type: 'group', triggerMode: 'mention_only', messageCount: 3210, memberCount: 92, hasFullMessageEvents: true, contextLimit: 12, knowledgeBaseNames: ['活动文案库'], knowledgeBaseIds: ['kb-campaign'], chatProfileId: 'chat-qwen-plus', systemPrompt: '你是活动执行助手，请使用简洁、明确的活动话术。', lastActiveAt: '12 分钟前', enabled: true },
  { id: 'c2c-88', botId: 'bot-qq-service', platformId: 'qq-user-88', botName: '客服小Q', name: '用户 8G7X', platform: 'QQ', type: 'c2c', triggerMode: 'always', messageCount: 46, memberCount: 1, hasFullMessageEvents: true, contextLimit: 20, knowledgeBaseNames: ['常见问题库'], knowledgeBaseIds: ['kb-faq'], chatProfileId: 'chat-qwen-plus', systemPrompt: '你是私聊智能助手，请准确回答用户问题。', lastActiveAt: '18 分钟前', enabled: true },
]

export const mockMessages: MessageRecord[] = [
  { id: 'msg-001', time: '2025-05-20 15:24:31', botName: '客服小Q', conversationName: '产品运营交流群', sender: '林晓', question: '@客服小Q 怎么申请七天无理由退款？', answer: '您可以在订单详情中发起退款申请，商品需保持完好。具体步骤如下…', eventType: 'GROUP_AT_MESSAGE_CREATE', model: 'qwen-plus', status: 'success', latency: 1750, tokens: 842, inputTokens: 720, outputTokens: 122, knowledgeHits: 5, deliveryStatus: 'success', contextLatency: 210, retrievalLatency: 320, modelLatency: 820, deliveryLatency: 280, traceId: 'tr_01JVC8P7Q2A' },
  { id: 'msg-002', time: '2025-05-20 15:24:28', botName: '客服小Q', conversationName: '售后服务群', sender: '周周', question: '@客服小Q 保修期是多久？', answer: '根据售后政策，主机提供一年有限保修服务。', eventType: 'GROUP_AT_MESSAGE_CREATE', model: 'qwen-plus', status: 'success', latency: 1390, tokens: 516, inputTokens: 450, outputTokens: 66, knowledgeHits: 3, deliveryStatus: 'success', contextLatency: 180, retrievalLatency: 210, modelLatency: 690, deliveryLatency: 210, traceId: 'tr_01JVC8NX8KQ' },
  { id: 'msg-003', time: '2025-05-20 15:24:24', botName: '知识助手', conversationName: '产品运营交流群', sender: '小川', question: '@知识助手 企业版支持哪些权限？', answer: '企业版支持角色权限、审计日志、知识隔离等功能。', eventType: 'GROUP_AT_MESSAGE_CREATE', model: 'qwen-max', status: 'success', latency: 1980, tokens: 1032, inputTokens: 910, outputTokens: 122, knowledgeHits: 8, deliveryStatus: 'success', contextLatency: 190, retrievalLatency: 450, modelLatency: 930, deliveryLatency: 300, traceId: 'tr_01JVC8K16MY' },
  { id: 'msg-004', time: '2025-05-20 15:24:21', botName: '客服小Q', conversationName: '售后服务群', sender: 'Miya', question: '@客服小Q 帮我查询订单', answer: '', eventType: 'GROUP_AT_MESSAGE_CREATE', model: '未调用', status: 'failed', latency: 5510, tokens: 0, inputTokens: 0, outputTokens: 0, knowledgeHits: 1, deliveryStatus: 'failed', contextLatency: 205, retrievalLatency: 5200, modelLatency: 0, deliveryLatency: 0, traceId: 'tr_01JVC8G0A19' },
]

export const mockKnowledgeBases: KnowledgeBase[] = [
  { id: 'kb-products', name: '产品手册库', description: '产品能力、使用说明与版本差异', scope: 'global', documents: 24, chunks: 1680, progress: 92, status: 'processing', model: 'text-embedding-v3', updatedAt: '5 分钟前', documentList: [
    { id: 'doc-1', name: 'AI Bot Hub 产品手册.md', type: 'MD', size: '2.4 MB', chunks: 428, status: 'success', updatedAt: '今天 14:32' },
    { id: 'doc-2', name: '企业版功能说明.txt', type: 'TXT', size: '620 KB', chunks: 116, status: 'success', updatedAt: '今天 11:06' },
  ] },
  { id: 'kb-after-sale', name: '售后知识库', description: '退款、换货、保修与服务流程', scope: 'bot', documents: 15, chunks: 940, progress: 67, status: 'processing', model: 'text-embedding-v3', updatedAt: '12 分钟前', documentList: [] },
  { id: 'kb-campaign', name: '活动文案库', description: '营销活动规则和标准话术', scope: 'conversation', documents: 8, chunks: 326, progress: 45, status: 'processing', model: 'text-embedding-v3', updatedAt: '18 分钟前', documentList: [] },
  { id: 'kb-faq', name: '常见问题库', description: '高频用户问题与标准答案', scope: 'global', documents: 32, chunks: 2184, progress: 100, status: 'success', model: 'text-embedding-v3', updatedAt: '1 小时前', documentList: [] },
]

const alerts: AlertItem[] = [
  { id: 'a1', level: 'critical', time: '05-20 15:24:21', bot: '客服小Q', content: '知识检索超时（耗时 5.20s，超过阈值 5.00s）', impact: '处理失败', recovered: false },
  { id: 'a2', level: 'warning', time: '05-20 15:23:10', bot: '知识助手', content: '索引失败：文档解析错误（文件：产品更新日志_20250519.pdf）', impact: '部分问答受影响', recovered: false },
  { id: 'a3', level: 'warning', time: '05-20 15:21:05', bot: '活动小助手', content: '模型调用延迟升高（当前 P95：4.8s，阈值 4.0s）', impact: '响应时间增加', recovered: true },
  { id: 'a4', level: 'info', time: '05-20 15:18:42', bot: '客服小Q', content: '投递失败率升高（当前 3.1%，阈值 2.0%）', impact: '消息触达可能受影响', recovered: true },
]

export const mockOverview: SystemOverview = {
  messages24h: 23842,
  successRate: null,
  metrics: [
    { label: '总事件数', value: '23,842', trend: '18.6%', direction: 'up', good: false },
    { label: '成功处理', value: '22,681', sub: '(95.1%)', trend: '12.4%', direction: 'up', good: true },
    { label: '平均处理耗时', value: '1.28s', trend: '7.3%', direction: 'down', good: true },
    { label: '超时率（>5s）', value: '0.73%', trend: '0.28pp', direction: 'down', good: true },
    { label: '投递成功率', value: '98.6%', trend: '0.9pp', direction: 'up', good: true },
    { label: '触发会话数', value: '6,842', trend: '15.2%', direction: 'up', good: false },
  ],
  pipelines: [
    { id: 'p1', time: '15:24:31', bot: '客服小Q', eventMs: 120, contextMs: 210, retrieval: { status: 'success', ms: 320, hit: '5/5' }, model: { status: 'success', ms: 820, name: 'qwen-plus' }, delivery: { status: 'success', ms: 280 }, totalMs: 1750 },
    { id: 'p2', time: '15:24:28', bot: '客服小Q', eventMs: 98, contextMs: 180, retrieval: { status: 'success', ms: 210, hit: '3/3' }, model: { status: 'success', ms: 690, name: 'qwen-plus' }, delivery: { status: 'success', ms: 210 }, totalMs: 1390 },
    { id: 'p3', time: '15:24:24', bot: '知识助手', eventMs: 110, contextMs: 190, retrieval: { status: 'success', ms: 450, hit: '6/8' }, model: { status: 'success', ms: 930, name: 'qwen-max' }, delivery: { status: 'success', ms: 300 }, totalMs: 1980 },
    { id: 'p4', time: '15:24:21', bot: '客服小Q', eventMs: 103, contextMs: 205, retrieval: { status: 'warning', ms: 5200, hit: '1/10' }, model: { status: 'pending' }, delivery: { status: 'failed' }, totalMs: 5510 },
    { id: 'p5', time: '15:24:18', bot: '活动小助手', eventMs: 95, contextMs: 160, retrieval: { status: 'success', ms: 180, hit: '2/2' }, model: { status: 'success', ms: 610, name: 'qwen-plus' }, delivery: { status: 'success', ms: 200 }, totalMs: 1250 },
    { id: 'p6', time: '15:24:15', bot: '客服小Q', eventMs: 101, contextMs: 220, retrieval: { status: 'success', ms: 260, hit: '4/4' }, model: { status: 'success', ms: 720, name: 'qwen-plus' }, delivery: { status: 'success', ms: 230 }, totalMs: 1530 },
    { id: 'p7', time: '15:24:12', bot: '知识助手', eventMs: 112, contextMs: 210, retrieval: { status: 'warning', ms: 300, hit: '2/5' }, model: { status: 'success', ms: 800, name: 'qwen-max' }, delivery: { status: 'success', ms: 260 }, totalMs: 1680 },
    { id: 'p8', time: '15:24:09', bot: '客服小Q', eventMs: 97, contextMs: 170, retrieval: { status: 'success', ms: 190, hit: '3/3' }, model: { status: 'success', ms: 660, name: 'qwen-plus' }, delivery: { status: 'success', ms: 220 }, totalMs: 1340 },
  ],
  alerts,
  knowledge: { progress: 78.6, total: 24, completed: 15, indexing: 6, failed: 1, pending: 2, queues: [
    { name: '产品手册库', progress: 92, eta: '5 分钟后' }, { name: '售后知识库', progress: 67, eta: '12 分钟后' }, { name: '活动文案库', progress: 45, eta: '18 分钟后' }, { name: '常见问题库', progress: 38, eta: '21 分钟后' }, { name: '行业资料库', progress: 22, eta: '32 分钟后' }, { name: '历史对话库', progress: 15, eta: '47 分钟后' },
  ] },
  queues: { inbox: 3, outbox: 1, knowledge: 6, failed: 2 },
}
