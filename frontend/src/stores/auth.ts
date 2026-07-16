import { defineStore } from 'pinia'
import { api } from '@/api'
import { ApiError, mockFallbackEnabled } from '@/api/client'

const DEMO_KEY = 'ai-bot-hub-demo-session'

export const useAuthStore = defineStore('auth', {
  state: () => ({ username: '', loading: false }),
  getters: { authenticated: (state) => Boolean(state.username) },
  actions: {
    async restore() {
      const stored = sessionStorage.getItem(DEMO_KEY)
      try {
        const user = await api.auth.me()
        this.username = user.username
        sessionStorage.setItem(DEMO_KEY, this.username)
      } catch (error) {
        if (stored && mockFallbackEnabled && error instanceof ApiError && error.code === 'NETWORK_ERROR') {
          this.username = stored
          return
        }
        this.username = ''
        sessionStorage.removeItem(DEMO_KEY)
      }
    },
    async login(username: string, password: string) {
      this.loading = true
      try {
        const result = await api.auth.login(username, password)
        this.username = result.username
      } catch (error) {
        if (mockFallbackEnabled && error instanceof ApiError && error.code === 'NETWORK_ERROR' && username === 'admin' && password === 'admin123456') this.username = 'admin'
        else throw error
      } finally { this.loading = false }
      sessionStorage.setItem(DEMO_KEY, this.username)
    },
    async logout() {
      try { await api.auth.logout() } catch { /* local session still clears */ }
      this.username = ''
      sessionStorage.removeItem(DEMO_KEY)
    },
  },
})
