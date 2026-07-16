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
      pendingTasks: 3,
      pendingInbox: 2,
      pendingOutbox: 1,
      pendingDocuments: 4,
      processingDocuments: 1,
      failedTasks: 0,
      readyDocuments: 5,
      totalDocuments: 10,
      version: '0.1.0',
    })

    const overview = await api.overview()

    expect(overview.messages24h).toBe(12)
    expect(overview.metrics[0].value).toBe('12')
    expect(overview.metrics[1].value).toBe('—')
    expect(overview.metrics[5].value).toBe('7')
    expect(overview.pipelines).toEqual([])
    expect(overview.alerts).toEqual([])
    expect(overview.knowledge.queues).toEqual([])
    expect(overview.queues).toEqual({ inbox: 2, outbox: 1, knowledge: 5, failed: 0 })
  })
})
