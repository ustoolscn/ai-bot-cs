<template>
  <ol v-if="rows.length" class="stage-bars" :class="{ waterfall }">
    <el-tooltip v-for="(row, index) in rows" :key="index + '·' + row.label" placement="top" :show-after="200" :disabled="!row.tip">
      <template #content><div class="stage-tip">{{ row.tip }}</div></template>
      <li :class="['is-' + row.tone, { running: row.status === 'processing' || row.status === 'pending' && row.ms == null }]">
        <el-icon class="icon"><component :is="stageIcon(row.label)" /></el-icon>
        <span class="label" :title="row.label">{{ row.label }}</span>
        <span class="track"><i :style="row.style" /></span>
        <span class="ms">{{ stageSeconds(row.ms) }}</span>
      </li>
    </el-tooltip>
  </ol>
  <p v-else class="muted empty">暂无计时数据</p>
</template>

<script setup lang="ts">
// 各环节用时条形图。
// 普通模式：条长 = 用时占最慢环节的比例。
// waterfall：条的起点 = 该环节开始时刻，长度 = 用时，能看出先后顺序和时间花在哪。
// 最慢的环节标黄，失败标红，进行中的条在流动。
import { computed } from 'vue'
import type { TimingStage } from '@/types'
import { stageIcon, stageSeconds } from '@/utils/stage-meta'

const props = defineProps<{ stages: TimingStage[]; waterfall?: boolean }>()
const statusText: Record<string, string> = { success: '完成', failed: '失败', pending: '等待中', processing: '处理中', skipped: '已结束', warning: '需关注' }

const rows = computed(() => {
  const list = props.stages.filter(s => s.ms != null || s.status === 'processing' || s.status === 'pending')
  const max = Math.max(1, ...list.map(s => s.ms ?? 0))
  const total = Math.max(1, ...list.map(s => s.elapsedMs ?? 0))
  const slowest = list.reduce<TimingStage | null>((a, s) => (s.ms ?? 0) > (a?.ms ?? 0) ? s : a, null)
  return list.map(s => {
    const ms = s.ms ?? 0
    let style: Record<string, string>
    if (props.waterfall) {
      const end = s.elapsedMs ?? total
      const start = Math.max(0, end - ms)
      style = { left: `${(start / total) * 100}%`, width: `max(3px, ${(ms / total) * 100}%)` }
    } else {
      style = { transform: `scaleX(${Math.max(0.02, ms / max)})` }
    }
    const tip = [
      statusText[s.status] || s.status,
      s.ms != null ? `本步 ${stageSeconds(s.ms)}` : '',
      s.elapsedMs != null ? `累计 ${stageSeconds(s.elapsedMs)}` : '',
      s.detail || '',
    ].filter(Boolean).join(' · ')
    return { ...s, style, tip, tone: s.status === 'failed' ? 'failed' : s.status === 'skipped' ? 'skipped' : s === slowest && ms >= 1000 ? 'slow' : 'ok' }
  })
})
</script>

<style scoped>
.stage-bars { display: grid; gap: 6px; margin: 0; padding: 0; list-style: none; }
.stage-bars li { display: grid; grid-template-columns: 16px 112px minmax(0, 1fr) 64px; align-items: center; gap: 10px; min-height: 22px; font-size: 13px; }
.icon { color: var(--muted); font-size: 15px; }
.label { overflow: hidden; color: var(--text-2); text-overflow: ellipsis; white-space: nowrap; }
.track { position: relative; height: 8px; border-radius: 4px; background: var(--surface-3); overflow: hidden; }
.track i { position: absolute; inset: 0; border-radius: 4px; background: var(--primary); transform-origin: left; transition: transform .6s var(--ease), left .6s var(--ease), width .6s var(--ease), background .3s var(--ease); }
.waterfall .track i { right: auto; }
.is-slow .track i { background: var(--warning); }
.is-slow .icon { color: var(--warning); }
.is-failed .track i { background: var(--danger); }
.is-failed .icon, .is-failed .ms { color: var(--danger); }
.is-skipped .track i { background: var(--muted); opacity: .5; }
.running .track i { background: linear-gradient(90deg, var(--primary) 0 40%, var(--primary-soft) 50%, var(--primary) 60% 100%); background-size: 250% 100%; animation: flow 1.2s linear infinite; }
.running .icon { color: var(--primary-text); }
.ms { text-align: right; color: var(--text); font-variant-numeric: tabular-nums; white-space: nowrap; }
.is-slow .ms { color: var(--warning); font-weight: 600; }
.empty { font-size: 13px; }
.stage-tip { max-width: 320px; line-height: 1.6; }
@keyframes flow { from { background-position: 100% 0; } to { background-position: -150% 0; } }
@media (prefers-reduced-motion: reduce) { .running .track i { animation: none; } .track i { transition: none; } }
</style>
