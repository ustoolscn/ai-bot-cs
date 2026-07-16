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

  it('saves built-in web search mode and custom request parameters', async () => {
    vi.mocked(request).mockResolvedValue({ id: 'chat-id' })
    await api.models.save({ id: 'chat-id', kind: 'chat', name: '联网模型', baseUrl: 'https://example.com/v1', model: 'qwen-plus', status: 'online', webSearchMode: 'qwen', extraBody: { search_options: { forced_search: true } } })
    expect(request).toHaveBeenCalledWith(expect.objectContaining({
      method: 'PUT',
      url: '/model-profiles/chat-id',
      data: expect.objectContaining({ webSearchMode: 'qwen', extraBody: { search_options: { forced_search: true } } }),
    }), { id: 'chat-id' })
  })

  it('preserves Responses API web search mode from the backend', async () => {
    vi.mocked(request).mockResolvedValue([{ id: 'chat-id', name: 'Responses 模型', kind: 'chat', baseUrl: 'https://example.com/v1', model: 'gpt-test', enabled: true, isDefault: false, hasApiKey: true, webSearchMode: 'responses', extraBody: {} }])
    await expect(api.models.list()).resolves.toEqual([expect.objectContaining({ id: 'chat-id', webSearchMode: 'responses' })])
  })
})
