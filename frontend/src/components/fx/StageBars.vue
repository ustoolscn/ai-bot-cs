<template>
  <ol v-if="rows.length" class="stage-bars">
    <li v-for="row in rows" :key="row.label" :class="['is-' + row.tone, { running: row.status === 'processing' }]">
      <span class="label" :title="row.label">{{ row.label }}</span>
      <span class="track"><i :style="{ transform: `scaleX(${row.ratio})` }" /></span>
      <span class="ms">{{ seconds(row.ms) }}</span>
    </li>
  </ol>
  <p v-else class="muted empty">暂无计时数据</p>
</template>

<script setup lang="ts">
// 各环节用时：条形长度 = 用时占最慢环节的比例；最慢的环节标黄，失败标红，进行中的条在流动。
import { computed } from 'vue'
import type { TimingStage } from '@/types'

const props = defineProps<{ stages: TimingStage[] }>()
const rows = computed(() => {
  const list = props.stages.filter(s => s.ms != null || s.status === 'processing')
  const max = Math.max(1, ...list.map(s => s.ms ?? 0))
  const slowest = list.reduce<TimingStage | null>((a, s) => (s.ms ?? 0) > (a?.ms ?? 0) ? s : a, null)
  return list.map(s => ({
    ...s,
    ratio: Math.max(0.02, (s.ms ?? 0) / max),
    tone: s.status === 'failed' ? 'failed' : s === slowest && (s.ms ?? 0) >= 1000 ? 'slow' : 'ok',
  }))
})
function seconds(ms?: number | null) {
  if (ms == null) return '…'
  return ms < 1000 ? Math.round(ms) + ' ms' : (ms / 1000).toFixed(2) + ' 秒'
}
</script>

<style scoped>
.stage-bars { display: grid; gap: 8px; margin: 0; padding: 0; list-style: none; }
.stage-bars li { display: grid; grid-template-columns: 112px minmax(0, 1fr) 68px; align-items: center; gap: 12px; font-size: 13px; }
.label { overflow: hidden; color: var(--text-2); text-overflow: ellipsis; white-space: nowrap; }
.track { position: relative; height: 8px; border-radius: 4px; background: var(--surface-3); overflow: hidden; }
.track i { position: absolute; inset: 0; border-radius: 4px; background: var(--primary); transform-origin: left; transition: transform .6s var(--ease), background .3s var(--ease); }
.is-slow .track i { background: #d18a12; }
.is-failed .track i { background: var(--danger); }
.running .track i { background: linear-gradient(90deg, var(--primary) 0 40%, #7fb0ee 50%, var(--primary) 60% 100%); background-size: 250% 100%; animation: flow 1.2s linear infinite; }
.ms { text-align: right; color: var(--text); font-variant-numeric: tabular-nums; }
.is-slow .ms { color: var(--warning); font-weight: 600; }
.is-failed .ms { color: var(--danger); }
.empty { font-size: 13px; }
@keyframes flow { from { background-position: 100% 0; } to { background-position: -150% 0; } }
</style>
