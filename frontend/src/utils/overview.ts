import type { SystemOverview } from '@/types'

export function createEmptyOverview(): SystemOverview {
  return {
    messages24h: null,
    successRate: null,
    metrics: [
      { label: '总事件数', value: '—' },
      { label: '成功处理', value: '—' },
      { label: '平均处理耗时', value: '—' },
      { label: '超时率（>5s）', value: '—' },
      { label: '投递成功率', value: '—' },
      { label: '触发会话数', value: '—' },
    ],
    pipelines: [],
    alerts: [],
    knowledge: { progress: 0, total: 0, completed: 0, indexing: 0, failed: 0, pending: 0, queues: [] },
    queues: { inbox: 0, outbox: 0, knowledge: 0, failed: 0 },
  }
}
