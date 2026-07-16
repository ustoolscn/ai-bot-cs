import { describe, expect, it, vi } from 'vitest'

vi.mock('./client', () => ({
  ApiError: class ApiError extends Error {},
  mockFallbackEnabled: true,
  request: vi.fn(),
}))

import { mapDocument, sanitizeServiceError } from './index'

describe('knowledge document failure mapping', () => {
  it('maps lastError for direct table display and removes credential-like values', () => {
    const document = mapDocument({
      id: 'doc-1',
      name: 'manual.md',
      status: 'failed',
      sizeBytes: 2048,
      lastError: 'POST /embeddings failed: Bearer secret-token-value api_key=sk-1234567890abcdef',
      createdAt: '2026-07-16T00:00:00Z',
    })

    expect(document.status).toBe('failed')
    expect(document.lastError).toContain('[已隐藏凭证]')
    expect(document.lastError?.match(/\[已隐藏凭证\]/g)).toHaveLength(2)
    expect(document.lastError).not.toContain('secret-token-value')
    expect(document.lastError).not.toContain('sk-1234567890abcdef')
  })

  it('keeps multiline diagnostic text readable', () => {
    expect(sanitizeServiceError('第一行\n第二行')).toBe('第一行\n第二行')
  })

  it('turns an HTML/non-JSON response into a Base URL guidance message', () => {
    expect(sanitizeServiceError("invalid character '<' looking for beginning of value")).toContain('有时需要包含 /v1')
  })
})
