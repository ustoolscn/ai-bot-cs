import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('./client', () => ({
  ApiError: class ApiError extends Error {},
  mockFallbackEnabled: true,
  request: vi.fn(),
}))

import { api } from './index'
import { request } from './client'
import { applySystemSettings, createSystemSettingsForm } from '@/utils/system-settings'

describe('system settings contract', () => {
  beforeEach(() => vi.clearAllMocks())

  it('loads settings from GET /system/settings and maps all database values', async () => {
    const response = { defaultContextLimit: 36, aiRequestTimeoutSeconds: 240, messageRetentionDays: 365, updatedAt: '2026-07-16T03:00:00Z' }
    vi.mocked(request).mockResolvedValue(response)

    const target = createSystemSettingsForm()
    applySystemSettings(target, await api.system.getSettings())

    expect(request).toHaveBeenCalledWith({ method: 'GET', url: '/system/settings' })
    expect(target).toEqual(response)
  })

  it('saves only editable fields through PUT /system/settings', async () => {
    const settings = { defaultContextLimit: 48, aiRequestTimeoutSeconds: 300, messageRetentionDays: 730, updatedAt: 'old-value' }
    vi.mocked(request).mockResolvedValue({ ...settings, updatedAt: '2026-07-16T04:00:00Z' })

    await api.system.updateSettings(settings)

    expect(request).toHaveBeenCalledWith({ method: 'PUT', url: '/system/settings', data: { defaultContextLimit: 48, aiRequestTimeoutSeconds: 300, messageRetentionDays: 730 } })
  })
})
