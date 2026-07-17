import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./client', () => ({
  ApiError: class ApiError extends Error {},
  mockFallbackEnabled: false,
  request: vi.fn(),
}))

import { api } from './index'
import { request } from './client'

describe('message trace mapping', () => {
  beforeEach(() => vi.clearAllMocks())

  it('maps actual model usage and the saved context snapshot', async () => {
    vi.mocked(request).mockResolvedValue({
      id: 'message-1', question: '@机器人 最新消息？', parts: [{ type: 'image', filename: 'question.png', previewUrl: '/api/messages/message-1/attachments/0' }], answer: '这是最新结果。', answerParts: [{ type: 'image', previewUrl: 'https://example.com/result.png' }], senderName: '用户', eventType: 'GROUP_AT_MESSAGE_CREATE', eventAt: '2026-07-16T08:00:00Z', platformMessageId: 'ROBOT1.0_long_message_id', conversationName: '测试群', botName: '机器人', model: 'gpt-test', status: 'completed', inputTokens: 120, outputTokens: 30, modelLatencyMs: 800, contextLatencyMs: 20, retrievalLatencyMs: 40, deliveryLatencyMs: 100, latencyMs: 960, knowledgeHits: 2, deliveryStatus: 'failed', deliveryError: 'QQ send status 400: invalid markdown', traceId: 'message-1', agentRuns: [{ contextMessages: [{ role: 'system', content: '系统提示' }, { role: 'user', content: '最新消息？', parts: [{ type: 'image', url: '/api/messages/message-1/attachments/0' }] }], retrievedChunks: [{ content: '资料', score: 0.9 }] }],
    })

    const detail = await api.messages.detail('message-1')
    expect(detail).toMatchObject({ tokens: 150, inputTokens: 120, outputTokens: 30, model: 'gpt-test', deliveryStatus: 'failed', deliveryError: 'QQ send status 400: invalid markdown', traceId: 'message-1' })
    expect(detail.attachments).toEqual([{ type: 'image', filename: 'question.png', previewUrl: '/api/messages/message-1/attachments/0', text: undefined, contentType: undefined, sizeBytes: undefined, width: undefined, height: undefined }])
    expect(detail.answerAttachments[0]?.previewUrl).toBe('https://example.com/result.png')
    expect(detail.contextMessages?.[1]?.parts?.[0]?.previewUrl).toBe('/api/messages/message-1/attachments/0')
  })

  it('tolerates legacy or malformed attachment JSON without blanking the page', async () => {
    vi.mocked(request).mockResolvedValue({
      id: 'legacy-message', question: '旧消息', parts: '{not-json', answerParts: { type: 'image' },
      conversationName: '测试群', botName: '机器人', status: 'completed',
      agentRuns: [{ contextMessages: [{ role: 'user', content: '旧上下文', parts: '{}' }] }],
    })

    const detail = await api.messages.detail('legacy-message')
    expect(detail.attachments).toEqual([])
    expect(detail.answerAttachments).toEqual([])
    expect(detail.contextMessages?.[0]?.parts).toEqual([])
  })
})
