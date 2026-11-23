<template>
  <div class="pipeline">
    <p v-if="!stages.length" class="muted">暂无处理记录</p>
    <ol v-else class="flow" :class="{ compact }">
      <li v-for="(stage,index) in stages" :key="index" class="stage" :class="'is-'+stage.status">
        <span class="bubble" aria-hidden="true"><el-icon><component :is="iconFor(stage.label)" /></el-icon></span>
        <strong>{{ stage.label }}</strong>
        <span class="stage-status">{{ labels[stage.status] || stage.status }}</span>
        <span>本步 <b>{{ seconds(stage.ms) }}</b></span>
        <span>累计 <b>{{ seconds(stage.elapsedMs) }}</b></span>
        <small v-if="stage.detail">{{ stage.detail }}</small>
      </li>
    </ol>
    <p v-if="totalMs != null" class="total">{{ average ? '平均总用时' : '总用时' }} <strong>{{ seconds(totalMs) }}</strong></p>
  </div>
</template>

<script setup lang="ts">
import { ChatDotRound, Clock, Connection, Cpu, Picture, Promotion, Search, SetUp } from '@element-plus/icons-vue'
function iconFor(label:string) {
  if (label.startsWith('等待')) return Clock
  if (label.startsWith('投递')) return Promotion
  if (label.startsWith('模型')) return Cpu
  if (label.includes('检索')) return Search
  if (label.includes('图片')) return Picture
  if (label.startsWith('工具')) return SetUp
  return label === '接收' ? ChatDotRound : Connection
}
import type { TimingStage } from '@/types'
defineProps<{stages: TimingStage[]; totalMs?: number | null; compact?: boolean; average?: boolean}>()
const labels: Record<string,string> = { success:'完成', failed:'失败', pending:'等待中', processing:'处理中', skipped:'已结束', warning:'需关注' }
function seconds(ms?: number | null) {
  if (ms == null) return '—'
  if (ms > 0 && ms < 10) return '<0.01 秒'
  return (ms / 1000).toFixed(2) + ' 秒'
}
</script>

<style scoped>
.flow { display:grid; grid-template-columns:repeat(auto-fit,minmax(min(145px,100%),1fr)); gap:8px; margin:0; padding:0; list-style:none }
.stage {display:grid; align-content:start; gap:3px; padding:10px 12px; border:1px solid var(--line-strong); border-top:3px solid var(--success); border-radius:12px; background:var(--surface); min-width:0; overflow-wrap:anywhere;}
.stage span,.stage small {color:var(--muted);font-size:12px}
.stage .bubble {width:32px;height:32px;border-radius:10px;display:grid;place-items:center;background:var(--mint-light);color:var(--success);font-size:17px;margin-bottom:3px}
.is-failed .bubble {color:var(--danger);background:var(--danger-light)}
.stage b,.total strong {font-variant-numeric:tabular-nums;color:var(--text)}
.stage .stage-status {color:var(--success);margin-bottom:4px}
.is-failed {border-top-color:var(--danger)} .is-failed .stage-status {color:var(--danger)}
.is-warning,.is-processing {border-top-color:var(--warning)} .is-warning .stage-status,.is-processing .stage-status {color:var(--warning)}
.is-skipped,.is-pending {border-top-color:var(--faint)} .is-skipped .stage-status,.is-pending .stage-status {color:var(--muted)}
.total {text-align:right;margin:10px 0 0;color:var(--muted)} .total strong {font-size:17px}
</style>
