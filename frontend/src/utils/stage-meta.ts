import { ChatDotRound, ChatLineRound, Checked, Clock, Connection, Cpu, EditPen, Finished, Picture, Promotion, Scissor, Search, SetUp } from '@element-plus/icons-vue'
import type { TimingStage } from '@/types'

// 环节在流程里的先后位置。模型轮次、工具调用和未列出的环节同属“生成”段，段内按实际开始时间排。
const ORDER: Array<[(label: string) => boolean, number, unknown]> = [
  [l => l === '接收', 0, ChatDotRound],
  [l => l.startsWith('等待处理'), 1, Clock],
  [l => l.startsWith('接单'), 2, Checked],
  [l => l.includes('群规则'), 2.5, Checked],
  [l => l.includes('判断'), 3, ChatLineRound],
  [l => l.includes('上下文') || l.includes('记忆'), 4, Connection],
  [l => l.includes('图片'), 4.5, Picture],
  [l => l.includes('改写'), 5, EditPen],
  [l => l.includes('检索'), 6, Search],
  [l => l.includes('模型'), 7, Cpu],
  [l => l.startsWith('工具'), 7, SetUp],
  [l => l.includes('截图'), 7, Picture],
  [l => l.includes('整理') || l.includes('分段'), 8, Scissor],
  [l => l.startsWith('等待投递'), 9, Clock],
  [l => l.includes('投递'), 10, Promotion],
  [l => l.includes('结束'), 11, Finished],
]

function match(label: string) {
  return ORDER.find(([test]) => test(label))
}
export function stageIcon(label: string) {
  return match(label)?.[2] ?? SetUp
}
export function stageRank(label: string) {
  return match(label)?.[1] ?? 7
}

// 按流程顺序排平均值：先看环节类别，同类再看它在最近消息里的平均开始时间。
export function sortByFlow(stages: TimingStage[], samples: TimingStage[] = []) {
  const offsets = new Map<string, number[]>()
  for (const s of samples) {
    if (s.elapsedMs == null) continue
    const start = s.elapsedMs - (s.ms ?? 0)
    offsets.set(s.label, [...(offsets.get(s.label) || []), start])
  }
  const startOf = new Map(stages.filter(s => s.startMs != null).map(s => [s.label, s.startMs as number]))
  const offset = (label: string) => {
    if (startOf.has(label)) return startOf.get(label) as number
    const list = offsets.get(label)
    return list?.length ? list.reduce((a, b) => a + b, 0) / list.length : Infinity
  }
  // 后端给了每个环节的平均开始时间时，直接按实际先后排。
  if (stages.length && stages.every(s => s.startMs != null)) return [...stages].sort((a, b) => (a.startMs as number) - (b.startMs as number))
  return [...stages].sort((a, b) => stageRank(a.label) - stageRank(b.label) || offset(a.label) - offset(b.label) || a.label.localeCompare(b.label, 'zh-CN', { numeric: true }))
}

export function stageSeconds(ms?: number | null) {
  if (ms == null) return '…'
  if (ms > 0 && ms < 10) return '<10 ms'
  return ms < 1000 ? Math.round(ms) + ' ms' : (ms / 1000).toFixed(2) + ' 秒'
}
