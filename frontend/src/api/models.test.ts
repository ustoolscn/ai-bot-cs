import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./client', () => ({
  ApiError: class ApiError extends Error {},
  mockFallbackEnabled: true,
  request: vi.fn(),
}))

import { api } from './index'
import { request } from './client'

describe('model test contract', () => {
  beforeEach(() => vi.clearAllMocks())

  it('forwards chat input and preserves token/result fields', async () => {
    const result = { kind: 'chat' as const, model: 'qwen-plus', content: '你好', inputTokens: 12, outputTokens: 3, latencyMs: 420 }
    vi.mocked(request).mockResolvedValue(result)

    await expect(api.models.test('chat-id', { input: '你好', systemPrompt: '简洁回答' })).resolves.toEqual(result)
    expect(request).toHaveBeenCalledWith({ method: 'POST', url: '/model-profiles/chat-id/test', data: { input: '你好', systemPrompt: '简洁回答' }, timeout: 610000 })
  })

  it('preserves embedding dimensions and vectorPreview', async () => {
    const result = { kind: 'embedding' as const, model: 'text-embedding-v3', dimensions: 1024, vectorPreview: [0.1, -0.2, 0.3], latencyMs: 180 }
    vi.mocked(request).mockResolvedValue(result)

    await expect(api.models.test('embedding-id', { input: '测试文本' })).resolves.toEqual(result)
  })
})
