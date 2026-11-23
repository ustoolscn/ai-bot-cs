<template>
  <div class="pipeline">
    <p v-if="!stages.length" class="muted">暂无处理记录</p>
    <ol v-else class="flow" :class="{ compact }">
      <li v-for="(stage, index) in stages" :key="index" class="stage" :class="'is-' + stage.status">
        <div class="stage-head">
          <el-icon><component :is="iconFor(stage.label)" /></el-icon>
          <strong>{{ stage.label }}</strong>
        </div>
        <span class="stage-status"><i />{{ labels[stage.status] || stage.status }}</span>
        <span class="stage-time">本步 <b>{{ seconds(stage.ms) }}</b></span>
        <span class="stage-time">累计 <b>{{ seconds(stage.elapsedMs) }}</b></span>
        <small v-if="stage.detail">{{ stage.detail }}</small>
      </li>
    </ol>
    <p v-if="totalMs != null" class="total">{{ average ? '平均总用时' : '总用时' }} <strong>{{ seconds(totalMs) }}</strong></p>
  </div>
</template>

<script setup lang="ts">
import { ChatDotRound, Clock, Connection, Cpu, Picture, Promotion, Search, SetUp } from '@element-plus/icons-vue'
import type { TimingStage } from '@/types'

defineProps<{ stages: TimingStage[]; totalMs?: number | null; compact?: boolean; average?: boolean }>()
const labels: Record<string, string> = { success: '完成', failed: '失败', pending: '等待中', processing: '处理中', skipped: '已结束', warning: '需关注' }
function iconFor(label: string) {
  if (label.startsWith('等待')) return Clock
  if (label.startsWith('投递')) return Promotion
  if (label.startsWith('模型')) return Cpu
  if (label.includes('检索')) return Search
  if (label.includes('图片')) return Picture
  if (label.startsWith('工具')) return SetUp
  return label === '接收' ? ChatDotRound : Connection
}
function seconds(ms?: number | null) {
  if (ms == null) return '—'
  if (ms > 0 && ms < 10) return '<0.01 秒'
  return (ms / 1000).toFixed(2) + ' 秒'
}
</script>

<style scoped>
.flow { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(132px, 100%), 1fr)); margin: 0; padding: 0; list-style: none; border: 1px solid var(--line); border-radius: var(--radius-sm); overflow: hidden; }
.stage { display: flex; flex-direction: column; gap: 2px; padding: 10px 12px; border-left: 1px solid var(--line); min-width: 0; overflow-wrap: anywhere; }
.stage:first-child { border-left: 0; }
.stage-head { display: flex; align-items: center; gap: 6px; margin-bottom: 2px; color: var(--muted); }
.stage-head strong { color: var(--text); font-size: 13px; font-weight: 600; }
.stage-status { display: inline-flex; align-items: center; gap: 5px; color: var(--success); font-size: 12px; }
.stage-status i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.is-failed .stage-status { color: var(--danger); }
.is-warning .stage-status, .is-processing .stage-status { color: var(--warning); }
.is-skipped .stage-status, .is-pending .stage-status { color: var(--muted); }
.stage-time, .stage small { color: var(--muted); font-size: 12px; }
.stage b, .total strong { font-weight: 600; color: var(--text); font-variant-numeric: tabular-nums; }
.total { margin: 8px 0 0; text-align: right; color: var(--muted); font-size: 13px; }
.total strong { font-size: 15px; }
</style>
