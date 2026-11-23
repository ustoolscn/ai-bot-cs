<template>
  <div class="flow" :class="{ compact }">
    <div class="flow-track" :style="{ '--stop': `${stopAt}%` }">
      <i v-for="n in 3" :key="n" class="flow-dot" :style="{ animationDelay: `${(n - 1) * 1.1}s` }" />
    </div>
    <div v-for="(stage, index) in stages" :key="stage.label" class="flow-stage" :class="`is-${stage.status}`" :style="{ animationDelay: `${index * 90}ms` }">
      <span class="flow-bubble">
        <el-icon><component :is="icons[index] || Connection" /></el-icon>
        <em v-if="stage.status === 'failed'" class="flow-flag">!</em>
      </span>
      <strong>{{ stage.label }}</strong>
      <small>{{ stage.ms !== undefined && stage.ms > 0 ? formatDuration(Math.round(stage.ms)) : stage.status === 'pending' ? '未执行' : '—' }}</small>
    </div>
  </div>
</template>

<script setup lang="ts">
// 消息处理流水线：五个站点用虚线连起来，小圆点沿线流动，表示消息正在一站站往下走。
import { computed, markRaw } from 'vue'
import { ChatDotRound, Connection, Cpu, Promotion, Search } from '@element-plus/icons-vue'
import type { Status } from '@/types'
import { formatDuration } from '@/utils/format'

export interface FlowStage { label: string; ms?: number; status: Status | string }
const props = defineProps<{ stages: FlowStage[]; compact?: boolean }>()
const icons = [markRaw(ChatDotRound), markRaw(Connection), markRaw(Search), markRaw(Cpu), markRaw(Promotion)]
// 流动的小圆点停在第一个失败/未执行的站点之前
const stopAt = computed(() => {
  const index = props.stages.findIndex(stage => stage.status === 'failed' || stage.status === 'pending')
  if (index < 0) return 100
  return Math.max(0, (index / Math.max(1, props.stages.length - 1)) * 100)
})
</script>

<style scoped>
.flow { position: relative; display: grid; grid-template-columns: repeat(5, 1fr); padding: 6px 0 2px; }
.flow-track { position: absolute; left: 10%; right: 10%; top: 30px; height: 0; border-top: 2px dashed var(--line-strong); }
.compact .flow-track { top: 24px; }
.flow-dot { position: absolute; top: -5px; left: 0; width: 8px; height: 8px; border-radius: 50%; background: var(--primary); box-shadow: 0 0 0 3px var(--primary-soft); opacity: 0; animation: flow-run 3.3s linear infinite; }
.flow-stage { position: relative; display: flex; flex-direction: column; align-items: center; gap: 2px; text-align: center; animation: stage-pop .5s var(--ease-bounce) both; }
.flow-bubble { position: relative; width: 50px; height: 50px; display: grid; place-items: center; border-radius: 18px; background: var(--mint-light); color: var(--success); font-size: 21px; border: 2px solid var(--surface); box-shadow: var(--shadow); margin-bottom: 6px; transition: transform .2s var(--ease-bounce); }
.compact .flow-bubble { width: 40px; height: 40px; border-radius: 14px; font-size: 17px; }
.flow-stage:hover .flow-bubble { transform: translateY(-3px) rotate(-6deg); }
.flow-stage.is-warning .flow-bubble { background: var(--butter-light); color: var(--warning); }
.flow-stage.is-failed .flow-bubble { background: var(--danger-light); color: var(--danger); animation: shake 2.6s ease-in-out infinite; }
.flow-stage.is-pending .flow-bubble { background: var(--surface-3); color: var(--faint); }
.flow-flag { position: absolute; top: -6px; right: -6px; width: 18px; height: 18px; border-radius: 50%; display: grid; place-items: center; background: var(--danger); color: #fff; font: 800 11px/1 var(--font-round); font-style: normal; }
.flow-stage strong { font-size: 13px; font-weight: 600; color: var(--text-2); }
.flow-stage small { font: 600 13px/1.3 var(--font-round); color: var(--muted); font-variant-numeric: tabular-nums; }
.flow-stage.is-warning small { color: var(--warning); }
.flow-stage.is-failed small { color: var(--danger); }
@keyframes flow-run { 0% { left: 0; opacity: 0; } 8% { opacity: 1; } 92% { opacity: 1; } 100% { left: var(--stop); opacity: 0; } }
@keyframes stage-pop { from { opacity: 0; transform: translateY(8px) scale(.85); } to { opacity: 1; transform: none; } }
@keyframes shake { 0%, 86%, 100% { transform: none; } 89% { transform: rotate(-8deg); } 92% { transform: rotate(8deg); } 95% { transform: rotate(-4deg); } }
@media (prefers-reduced-motion: reduce) { .flow-dot { display: none; } .flow-stage, .flow-bubble { animation: none; } }
</style>
