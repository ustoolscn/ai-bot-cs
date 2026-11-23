<template>
  <div class="content-page dash">
    <div class="greeting">
      <div>
        <h1>{{ greeting }}，{{ auth.username || '管理员' }}</h1>
        <p class="live-line"><LiveDot :active="busy" :tone="busy ? 'primary' : 'success'" />{{ busy ? `正在回复 ${overview.active.length} 条消息` : '空闲，等待新消息' }}</p>
      </div>
      <div class="greeting-actions">
        <button v-for="todo in todos" :key="todo.label" class="todo-chip" :class="todo.tone" @click="$router.push(todo.to)"><strong>{{ todo.count }}</strong>{{ todo.label }}</button>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
      </div>
    </div>

    <div class="dash-top">
      <section class="panel dash-card">
        <header><h2>任务状态</h2><el-button v-if="activeAlerts.length" text type="danger" @click="$router.push('/system')">{{ activeAlerts.length }} 条告警</el-button></header>
        <QueueFlow :nodes="queueNodes" />
      </section>

      <section class="panel dash-card">
        <header><h2>处理消息</h2><span class="muted">24 小时</span></header>
        <div class="big-num"><CountUp :value="overview.messages24h" /><span>条</span></div>
        <div class="meter-row">
          <span>成功回复</span>
          <span class="meter"><i :style="{ transform: `scaleX(${(overview.successRate ?? 0) / 100})` }" /></span>
          <strong><CountUp :value="overview.successRate" :decimals="1" suffix="%" /></strong>
        </div>
        <div class="meter-row">
          <span>平均用时</span>
          <span class="meter"><i class="is-time" :style="{ transform: `scaleX(${Math.min(1, avgTotal / 8000)})` }" /></span>
          <strong>{{ avgTotal ? (avgTotal / 1000).toFixed(2) + ' 秒' : '—' }}</strong>
        </div>
      </section>

      <section class="panel dash-card">
        <header><h2>知识库</h2><el-button text type="primary" @click="$router.push('/knowledge')">管理</el-button></header>
        <div class="kb-nums">
          <div><strong><CountUp :value="overview.knowledge.total" /></strong><span>篇文档</span></div>
          <div><strong><CountUp :value="overview.knowledge.entries ?? stats?.publishedEntries" /></strong><span>条知识问答</span></div>
          <div><strong :class="{ warn: stats?.pendingCandidates }"><CountUp :value="stats?.pendingCandidates" /></strong><span>条待审核</span></div>
        </div>
        <div class="meter-row">
          <span>已索引</span>
          <span class="meter" :class="{ indexing: overview.knowledge.indexing }"><i :style="{ transform: `scaleX(${overview.knowledge.progress / 100})` }" /></span>
          <strong><CountUp :value="overview.knowledge.progress" suffix="%" /></strong>
        </div>
      </section>
    </div>

    <div class="dash-bottom">
      <section class="panel dash-card">
        <header>
          <h2>各环节用时</h2>
          <el-select v-model="traceScope" aria-label="选择消息" size="small" style="width: 220px">
            <el-option label="最近消息平均" value="average" />
            <el-option v-for="row in overview.pipelines" :key="row.id" :value="row.id" :label="row.time + ' · ' + (row.content || '群消息')" />
          </el-select>
        </header>
        <StageBars :stages="shownStages" />
        <p class="total-line">{{ traceScope === 'average' ? '平均总用时' : '总用时' }} <strong>{{ shownTotal == null ? '—' : (shownTotal / 1000).toFixed(2) + ' 秒' }}</strong></p>
      </section>

      <section class="panel dash-card">
        <header><h2>最新动态</h2><el-button text type="primary" @click="$router.push('/messages')">全部消息</el-button></header>
        <TransitionGroup name="live" tag="ul" class="live-list">
          <li v-for="task in overview.active" :key="'a' + task.id" class="live-item is-active">
            <LiveDot active tone="primary" />
            <div class="live-main">
              <span class="who"><b>{{ task.bot }}</b> 正在 <b>{{ task.conversation || '私聊' }}</b> 回复<template v-if="task.sender"> <b>{{ task.sender }}</b></template></span>
              <strong>{{ task.question || '（图片或表情）' }}</strong>
              <span class="stage-run"><i />{{ task.stage }}</span>
            </div>
          </li>
          <li v-for="row in recent" :key="row.id" class="live-item" :class="{ selected: traceScope === row.id }" @click="traceScope = traceScope === row.id ? 'average' : row.id">
            <LiveDot :tone="rowTone(row)" />
            <div class="live-main">
              <span class="who">{{ row.time }} · {{ row.bot }}<template v-if="row.conversation"> · {{ row.conversation }}</template></span>
              <strong>{{ row.content || '（没有文字内容）' }}</strong>
            </div>
            <span class="live-ms" :class="{ slow: rowTotal(row) > 5000 }">{{ rowTotal(row) ? (rowTotal(row) / 1000).toFixed(1) + 's' : '—' }}</span>
          </li>
        </TransitionGroup>
        <EmptyState v-if="!overview.active.length && !recent.length" compact title="还没有消息" description="机器人被 @ 或收到私聊后会出现在这里。" />
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { api } from '@/api'
import { agentApi, type LearningStats } from '@/api/agent'
import EmptyState from '@/components/EmptyState.vue'
import CountUp from '@/components/fx/CountUp.vue'
import LiveDot from '@/components/fx/LiveDot.vue'
import QueueFlow from '@/components/fx/QueueFlow.vue'
import StageBars from '@/components/fx/StageBars.vue'
import type { PipelineRow, SystemOverview, TimingStage } from '@/types'
import { createEmptyOverview } from '@/utils/overview'
import { useAuthStore } from '@/stores/auth'
import { useShellStore } from '@/stores/shell'

const overview = ref<SystemOverview>(createEmptyOverview())
const stats = ref<LearningStats | null>(null)
const loading = ref(false)
const shell = useShellStore()
const auth = useAuthStore()

const greeting = computed(() => {
  const hour = new Date().getHours()
  return hour < 6 ? '夜深了' : hour < 12 ? '早上好' : hour < 14 ? '中午好' : hour < 18 ? '下午好' : '晚上好'
})
const busy = computed(() => overview.value.active.length > 0)
const activeAlerts = computed(() => overview.value.alerts.filter(alert => !alert.recovered))
const todos = computed(() => [
  { label: '人等人工', count: stats.value?.openHandoffs || 0, to: '/handoffs', tone: 'is-hot' },
  { label: '条待审核', count: stats.value?.pendingCandidates || 0, to: '/knowledge?tab=candidates', tone: 'is-info' },
  { label: '个没答上', count: stats.value?.openGaps || 0, to: '/knowledge?tab=gaps', tone: 'is-warn' },
].filter(todo => todo.count))
const queueNodes = computed(() => [
  { label: '排队', count: overview.value.queues.inbox },
  { label: '回复中', count: overview.value.active.length },
  { label: '待发送', count: overview.value.queues.outbox },
  { label: '失败', count: overview.value.queues.failed, tone: 'danger' },
])
const activeIds = computed(() => new Set(overview.value.active.map(task => task.id)))
const recent = computed(() => overview.value.pipelines.filter(row => !activeIds.value.has(row.id)).slice(0, 8))

const rowTotal = (row: PipelineRow) => row.timeline ? row.timeline.totalMs ?? 0 : row.totalMs
function rowTone(row: PipelineRow) {
  const stages = row.timeline?.stages
  if (stages?.some(s => s.status === 'failed') || row.delivery.status === 'failed') return 'danger'
  if (rowTotal(row) > 5000) return 'warning'
  return 'success'
}
function average(values: Array<number | null | undefined>) {
  const measured = values.filter((value): value is number => typeof value === 'number' && value > 0)
  return measured.length ? measured.reduce((sum, value) => sum + value, 0) / measured.length : null
}
const avgTotal = computed(() => average(overview.value.pipelines.map(rowTotal)) ?? 0)

// 旧数据没有 timeline 时，用固定五个环节兜底
function legacyStages(rows: PipelineRow[]): TimingStage[] {
  return [
    { label: '接收', ms: average(rows.map(r => r.eventMs)), status: 'success' },
    { label: '上下文', ms: average(rows.map(r => r.contextMs)), status: 'success' },
    { label: '知识检索', ms: average(rows.map(r => r.retrieval.ms)), status: 'success' },
    { label: '模型生成', ms: average(rows.map(r => r.model.ms)), status: 'success' },
    { label: '投递', ms: average(rows.map(r => r.delivery.ms)), status: 'success' },
  ]
}
const traceScope = ref('average')
const selected = computed(() => overview.value.pipelines.find(row => row.id === traceScope.value))
const shownTotal = computed(() => traceScope.value === 'average' ? avgTotal.value || null : selected.value ? rowTotal(selected.value) : null)
const shownStages = computed((): TimingStage[] => {
  if (selected.value) return selected.value.timeline?.stages || legacyStages([selected.value])
  if (overview.value.stageAverages.length) return overview.value.stageAverages
  const rows = overview.value.pipelines
  const samples = rows.flatMap(row => row.timeline?.stages || [])
  if (!samples.length) return rows.length ? legacyStages(rows) : []
  const groups = new Map<string, TimingStage[]>()
  for (const stage of samples) groups.set(stage.label, [...(groups.get(stage.label) || []), stage])
  return Array.from(groups, ([label, group]) => ({
    label,
    ms: average(group.map(s => s.ms)),
    elapsedMs: average(group.map(s => s.elapsedMs)),
    status: group.some(s => s.status === 'failed') ? 'failed' : group.some(s => s.status === 'processing') ? 'processing' : 'success',
  })).sort((a, b) => (a.elapsedMs ?? Infinity) - (b.elapsedMs ?? Infinity))
})

async function load(background: unknown = false) {
  if (background !== true) loading.value = true
  const [overviewResult, statsResult] = await Promise.allSettled([api.overview(), agentApi.learning.stats()])
  if (overviewResult.status === 'fulfilled') overview.value = overviewResult.value
  if (statsResult.status === 'fulfilled') stats.value = statsResult.value
  shell.refreshCounts(true)
  loading.value = false
}
onMounted(load)
useAdminEvents(['messages','system_alerts','inbox_tasks','outbox_tasks','model_calls','handoffs','knowledge_candidates','agent_runs','bots','message_stages','group_reviews','knowledge_documents'], () => load(true))
</script>

<style scoped>
.greeting-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.live-line { display: flex; align-items: center; gap: 8px; }
.todo-chip { display: inline-flex; align-items: baseline; gap: 4px; height: 32px; padding: 0 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface); color: var(--text-2); font-size: 13px; cursor: pointer; transition: background .12s var(--ease); line-height: 30px; }
.todo-chip:hover { background: var(--surface-2); }
.todo-chip strong { font-size: 15px; font-variant-numeric: tabular-nums; }
.todo-chip.is-hot strong { color: var(--danger); }
.todo-chip.is-info strong { color: var(--primary-text); }
.todo-chip.is-warn strong { color: var(--warning); }

.dash-top { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr) minmax(0, 1fr); gap: 16px; }
.dash-bottom { display: grid; grid-template-columns: minmax(0, 1fr) minmax(0, 1fr); gap: 16px; margin-top: 16px; align-items: start; }
.dash-card { display: flex; flex-direction: column; gap: 14px; padding: 16px 20px; }
.dash-card > header { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 28px; }
.dash-card h2 { font-size: 15px; font-weight: 600; }
.dash-card header .muted { font-size: 12px; }

.big-num { display: flex; align-items: baseline; gap: 4px; }
.big-num .count-up { font-size: 28px; font-weight: 600; line-height: 1.1; }
.big-num span:last-child { color: var(--muted); font-size: 13px; }
.meter-row { display: grid; grid-template-columns: 56px minmax(0, 1fr) 64px; align-items: center; gap: 10px; font-size: 13px; color: var(--muted); }
.meter-row strong { text-align: right; color: var(--text); font-weight: 600; font-variant-numeric: tabular-nums; }
.meter { position: relative; height: 6px; border-radius: 3px; background: var(--surface-3); overflow: hidden; }
.meter i { position: absolute; inset: 0; border-radius: 3px; background: var(--success); transform-origin: left; transition: transform .8s var(--ease); }
.meter i.is-time { background: var(--primary); }
.meter.indexing i { background: linear-gradient(90deg, var(--primary) 0 40%, #7fb0ee 50%, var(--primary) 60% 100%); background-size: 250% 100%; animation: flow 1.4s linear infinite; }
.kb-nums { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; }
.kb-nums div { display: flex; flex-direction: column; }
.kb-nums strong { font-size: 22px; font-weight: 600; line-height: 1.2; }
.kb-nums strong.warn { color: var(--warning); }
.kb-nums div > span { color: var(--muted); font-size: 12px; }

.total-line { margin: 0; text-align: right; color: var(--muted); font-size: 13px; }
.total-line strong { color: var(--text); font-size: 15px; font-variant-numeric: tabular-nums; }

.live-list { position: relative; margin: 0 -20px -16px; padding: 0; list-style: none; }
.live-item { display: grid; grid-template-columns: 10px minmax(0, 1fr) auto; align-items: center; gap: 12px; padding: 10px 20px; border-top: 1px solid var(--line); cursor: pointer; transition: background .15s var(--ease); }
.live-item:hover { background: var(--surface-2); }
.live-item.selected { background: var(--primary-light); }
.live-item.is-active { grid-template-columns: 10px minmax(0, 1fr); background: var(--primary-light); cursor: default; }
.live-main { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.live-main strong { overflow: hidden; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.who { overflow: hidden; color: var(--muted); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.who b { color: var(--text-2); font-weight: 600; }
.live-ms { color: var(--text-2); font-size: 12px; font-variant-numeric: tabular-nums; }
.live-ms.slow { color: var(--danger); }
.stage-run { position: relative; display: inline-flex; align-items: center; gap: 8px; margin-top: 4px; color: var(--primary-text); font-size: 12px; }
.stage-run i { width: 64px; height: 4px; border-radius: 2px; background: linear-gradient(90deg, transparent 0 30%, var(--primary) 50%, transparent 70% 100%); background-size: 250% 100%; animation: flow 1.1s linear infinite; }

.live-enter-active, .live-leave-active, .live-move { transition: transform .35s var(--ease), opacity .35s var(--ease), background 1.2s var(--ease); }
.live-enter-from { opacity: 0; transform: translateY(-10px); background: var(--primary-soft); }
.live-leave-to { opacity: 0; }
.live-leave-active { position: absolute; left: 0; right: 0; }

@keyframes flow { from { background-position: 100% 0; } to { background-position: -150% 0; } }
@media (max-width: 1100px) { .dash-top { grid-template-columns: 1fr 1fr; } .dash-top > :first-child { grid-column: 1 / -1; } .dash-bottom { grid-template-columns: 1fr; } }
@media (max-width: 640px) { .dash-top { grid-template-columns: 1fr; } .greeting { flex-direction: column; align-items: flex-start; } }
</style>
