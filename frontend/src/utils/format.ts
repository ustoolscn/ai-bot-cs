export function formatDuration(ms?: number) {
  if (ms === undefined) return '—'
  return ms >= 1000 ? `${(ms / 1000).toFixed(2)}s` : `${ms}ms`
}

export function statusText(status: string) {
  return ({ online: '在线', offline: '离线', warning: '异常', success: '成功', failed: '失败', processing: '处理中', pending: '待处理', unindexed: '未索引' } as Record<string, string>)[status] || status
}
