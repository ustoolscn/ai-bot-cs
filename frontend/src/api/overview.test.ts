import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./client', () => ({
  ApiError: class ApiError extends Error {
    constructor(public code: string, message: string) { super(message) }
  },
  mockFallbackEnabled: true,
  request: vi.fn(),
}))

import { api } from './index'
import { request } from './client'
import { createEmptyOverview } from '@/utils/overview'

describe('system overview mapping', () => {
  beforeEach(() => vi.clearAllMocks())

  it('creates an honest empty overview without mock pipeline or alert data', () => {
    const overview = createEmptyOverview()
    expect(overview.metrics.map((metric) => metric.value)).toEqual(['—', '—', '—', '—', '—', '—'])
    expect(overview.pipelines).toEqual([])
    expect(overview.alerts).toEqual([])
    expect(overview.knowledge.queues).toEqual([])
  })

  it('maps a successful backend response without cloning mock operational data', async () => {
    vi.mocked(request).mockResolvedValue({
      bots: 2,
      conversations: 7,
      messages24h: 12,
      totalEvents: 18,
      triggered: 10,
      successful: 9,
      triggeredConversations: 4,
      averageLatencyMs: 1250,
      slowCalls: 1,
      modelCalls: 10,
      sentDeliveries: 8,
      totalDeliveries: 10,
      pendingTasks: 3,
      pendingInbox: 2,
      pendingOutbox: 1,
      pendingDocuments: 4,
      processingDocuments: 1,
      failedTasks: 0,
      readyDocuments: 5,
      failedDocuments: 1,
      totalDocuments: 10,
      pipelines: [{ id: 'p1', time: '2026-07-16T08:00:00Z', bot: '机器人', conversation: '测试群', content: '测试消息', eventMs: 5, contextMs: 20, retrieval: { status: 'success', ms: 30, hit: '2' }, model: { status: 'success', ms: 800, name: 'qwen' }, delivery: { status: 'success', ms: 100 }, totalMs: 955 }],
      alerts: [{ id: 'a1', level: 'warning', time: '2026-07-16T08:00:00Z', bot: '知识索引', content: '索引失败', impact: '知识不完整', recovered: false }],
      knowledgeQueues: [{ id: 'k1', name: '产品库', progress: 50, eta: '正在索引' }],
      version: '0.2.0',
    })

    const overview = await api.overview()

    expect(overview.messages24h).toBe(12)
    expect(overview.metrics.map(metric => metric.value)).toEqual(['18', '9', '1.25s', '10.00%', '80.0%', '4'])
    expect(overview.pipelines[0]).toMatchObject({ conversation: '测试群', content: '测试消息', totalMs: 955 })
    expect(overview.alerts[0]).toMatchObject({ content: '索引失败', recovered: false })
    expect(overview.knowledge.queues).toEqual([{ id: 'k1', name: '产品库', progress: 50, eta: '正在索引' }])
    expect(overview.queues).toEqual({ inbox: 2, outbox: 1, knowledge: 5, failed: 0 })
  })
})
