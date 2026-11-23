export function formatDuration(ms?: number) {
  if (ms === undefined) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${ms}ms`
}

export function statusText(status: string) {
  return ({ online: '在线', offline: '离线', warning: '异常', success: '成功', failed: '失败', processing: '处理中', pending: '待处理', unindexed: '未索引', skipped: '无需回复' } as Record<string, string>)[status] || status
}

/** 消息结论：转人工的消息按工单进度显示，其余按原状态。 */
export function messageVerdict(m: { status: string; handoffStatus?: string | null; humanDeliveryStatus?: string | null }): { status: string; text?: string } {
  if (m.handoffStatus === 'human_replied') return { status: 'success', text: '人工已回复' }
  if (m.handoffStatus === 'human_closed') return { status: 'skipped', text: '人工已结单' }
  if (m.handoffStatus === 'waiting_human') {
    if (m.humanDeliveryStatus === 'failed' || m.humanDeliveryStatus === 'expired') return { status: 'failed', text: '人工回复发送失败' }
    if (m.humanDeliveryStatus === 'pending' || m.humanDeliveryStatus === 'processing') return { status: 'processing', text: '人工回复发送中' }
    return { status: 'warning', text: '待人工回复' }
  }
  return { status: m.status }
}
