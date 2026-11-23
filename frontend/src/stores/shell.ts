import { defineStore } from 'pinia'
import { agentApi } from '@/api/agent'

// 侧栏待办数字：待处理的人工工单、待审核的候选知识。
export const useShellStore = defineStore('shell', {
  state: () => ({ openHandoffs: 0, pendingCandidates: 0, lastLoaded: 0 }),
  actions: {
    async refreshCounts(force = false) {
      if (!force && Date.now() - this.lastLoaded < 15000) return
      this.lastLoaded = Date.now()
      try {
        const stats = await agentApi.learning.stats()
        this.openHandoffs = stats.openHandoffs || 0
        this.pendingCandidates = stats.pendingCandidates || 0
      } catch { /* 数字只是提示，失败时保持原值 */ }
    },
  },
})
