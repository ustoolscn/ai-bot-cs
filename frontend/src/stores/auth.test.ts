import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@/api', () => ({
  api: {
    auth: {
      me: vi.fn(),
      login: vi.fn(),
      logout: vi.fn(),
      changePassword: vi.fn(),
    },
  },
}))

import { api } from '@/api'
import { useAuthStore } from './auth'

describe('auth restore', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    sessionStorage.clear()
    vi.clearAllMocks()
  })

  it('calls /auth/me and restores the HttpOnly-cookie session without sessionStorage', async () => {
    vi.mocked(api.auth.me).mockResolvedValue({ id: 'admin-id', username: 'admin' })
    const store = useAuthStore()

    await store.restore()

    expect(api.auth.me).toHaveBeenCalledOnce()
    expect(store.authenticated).toBe(true)
    expect(store.username).toBe('admin')
    expect(sessionStorage.getItem('ai-bot-hub-demo-session')).toBe('admin')
  })
})
