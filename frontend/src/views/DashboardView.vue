<template>
  <div class="content-page">
    <div class="greeting">
      <div><h1>{{ greeting }}，{{ auth.username || '管理员' }}</h1><p>{{ heroLine }}</p></div>
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </div>

    <div class="section-label"><h2>需要你处理</h2><span>点开直接处理</span></div>
    <div class="todo-grid">
      <button v-for="todo in todos" :key="todo.label" class="todo-card" :class="todo.count ? todo.tone : 'is-clear'" @click="$router.push(todo.to)">
        <strong>{{ todo.count ?? '—' }}</strong>
        <span>{{ todo.count ? todo.label : todo.clear }}</span>
        <el-icon class="arrow"><ArrowRight /></el-icon>
      </button>
    </div>

    <div class="section-label"><h2>最近 24 小时</h2><span v-if="overviewError" class="danger-text">{{ overviewError }}</span></div>
    <div class="kpi-grid">
      <div v-for="kpi in kpis" :key="kpi.label" class="kpi">
        <span>{{ kpi.label }}</span>
        <strong>{{ kpi.value }}</strong>
        <p v-if="kpi.trend">较昨日 <em :class="kpi.good ? 'trend-good' : 'trend-bad'"><el-icon><component :is="kpi.direction === 'up' ? Top : Bottom" /></el-icon>{{ kpi.trend }}</em></p>
        <p v-else>{{ kpi.hint }}</p>
        <div v-if="kpi.percent !== undefined" class="meter"><i :style="{ width: Math.max(0, Math.min(100, kpi.percent)) + '%' }" /></div>
      </div>
    </div>

    <div class="overview-grid">
      <div class="side-stack">
        <section class="panel">
          <div class="panel-head">
            <div><h2>消息处理过程</h2><p>{{ traceScope === 'average' ? '各步按有计时的样本取平均，总用时按已结束的消息计算。' : '从后台收到消息开始累计，包含排队和分段等待。' }}</p></div>
            <el-select v-model="traceScope" aria-label="选择消息" style="width: 260px; flex: none">
              <el-option label="最近消息平均" value="average" />
              <el-option v-for="row in overview.pipelines" :key="row.id" :value="row.id" :label="row.time + ' · ' + (row.content || row.eventType || '群消息')" />
            </el-select>
          </div>
          <div class="panel-body"><PipelineFlow :stages="shownStages" :total-ms="shownTotal" :average="traceScope === 'average'" /></div>
        </section>

        <section class="panel">
          <div class="panel-head">
            <div><h2>最近处理的消息</h2><p>点一条消息，上面显示它的各步耗时</p></div>
            <el-button text type="primary" @click="$router.push('/messages')">全部消息</el-button>
          </div>
          <ul v-if="overview.pipelines.length" class="recent-list">
            <li v-for="row in overview.pipelines" :key="row.id" class="recent-item" :class="{ active: traceScope === row.id }" @click="traceScope = row.id">
              <div class="recent-main">
                <strong>{{ row.content || '（没有文字内容）' }}</strong>
                <span>{{ row.time }} · {{ row.bot }}<template v-if="row.conversation"> · {{ row.conversation }}</template></span>
              </div>
              <div class="recent-meta">
                <span v-if="row.timeline">{{ row.timeline.stages.find(s => s.status === 'failed')?.label || row.timeline.stages.at(-1)?.label }}</span>
                <el-tooltip v-else :content="stageTip(row)" placement="top">
                  <span class="stage-dots">
                    <i class="stage-dot is-success" />
                    <i class="stage-dot" :class="`is-${row.contextStatus || 'success'}`" />
                    <i class="stage-dot" :class="`is-${row.retrieval.status}`" />
                    <i class="stage-dot" :class="`is-${row.model.status}`" />
                    <i class="stage-dot" :class="`is-${row.delivery.status}`" />
                  </span>
                </el-tooltip>
                <span class="recent-total" :class="{ 'danger-text': row.totalMs > 5000 }">{{ row.timeline ? (row.timeline.totalMs == null ? '—' : formatDuration(row.timeline.totalMs)) : formatDuration(row.totalMs) }}</span>
              </div>
            </li>
          </ul>
          <EmptyState v-else title="还没有处理过的消息" description="机器人在群里被 @ 或收到私聊后，处理记录会出现在这里。" />
        </section>
      </div>

      <div class="side-stack">
        <section class="panel">
          <div class="panel-head"><h2>服务健康</h2><el-button text type="primary" @click="$router.push('/system')">系统状态</el-button></div>
          <div class="panel-body" style="padding-bottom: 8px">
            <div class="fact-row" style="justify-content: space-between; margin-bottom: 6px"><span>知识库索引</span><strong class="num">{{ overview.knowledge.completed }} / {{ overview.knowledge.total }} 篇</strong></div>
            <div class="progress-line" :class="{ 'is-failed': overview.knowledge.failed }"><i :style="{ width: overview.knowledge.progress + '%' }" /></div>
            <p class="muted" style="margin-top: 6px; font-size: 12px">正在索引 {{ overview.knowledge.indexing }} 篇，排队 {{ overview.knowledge.pending }} 篇<span v-if="overview.knowledge.failed" class="danger-text">，{{ overview.knowledge.failed }} 篇失败</span></p>
          </div>
          <div class="health-list">
            <div class="health-row"><span>待处理消息</span><strong>{{ overview.queues.inbox }}</strong></div>
            <div class="health-row"><span>待发送回复</span><strong>{{ overview.queues.outbox }}</strong></div>
            <div class="health-row"><span>失败任务</span><strong :class="{ 'danger-text': overview.queues.failed }">{{ overview.queues.failed }}</strong></div>
          </div>
        </section>

        <section class="panel">
          <div class="panel-head"><h2>告警</h2><span class="muted" style="font-size: 13px">{{ activeAlerts.length ? `${activeAlerts.length} 条未恢复` : '' }}</span></div>
          <ul v-if="overview.alerts.length" class="alert-list">
            <li v-for="alert in overview.alerts.slice(0, 5)" :key="alert.id" :class="alert.recovered ? 'recovered' : alert.level">
              <el-icon><component :is="alert.recovered ? CircleCheck : WarningFilled" /></el-icon>
              <div><strong>{{ alert.content }}</strong><span>{{ alert.time }} · {{ alert.bot }}<template v-if="alert.impact"> · {{ alert.impact }}</template><template v-if="alert.recovered"> · 已恢复</template></span></div>
            </li>
          </ul>
          <EmptyState v-else compact title="一切正常" description="最近没有告警。" />
        </section>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, ref } from 'vue'
import { ArrowRight, Bottom, CircleCheck, Refresh, Top, WarningFilled } from '@element-plus/icons-vue'
import { api } from '@/api'
import { agentApi, type LearningStats } from '@/api/agent'
import EmptyState from '@/components/EmptyState.vue'
import PipelineFlow from '@/components/fx/PipelineFlow.vue'
import type { TimingStage, PipelineRow, Status, SystemOverview } from '@/types'
import { formatDuration } from '@/utils/format'
import { createEmptyOverview } from '@/utils/overview'
import { useAuthStore } from '@/stores/auth'
import { useShellStore } from '@/stores/shell'

const overview = ref<SystemOverview>(createEmptyOverview())
const stats = ref<LearningStats | null>(null)
const loading = ref(false)
const overviewError = ref('')
const shell = useShellStore()
const auth = useAuthStore()

const greeting = computed(() => {
  const hour = new Date().getHours()
  return hour < 6 ? '夜深了' : hour < 12 ? '早上好' : hour < 14 ? '中午好' : hour < 18 ? '下午好' : '晚上好'
})
const heroLine = computed(() => {
  const waiting = stats.value?.openHandoffs || 0
  const pending = stats.value?.pendingCandidates || 0
  if (waiting) return `有 ${waiting} 位用户在等人工回复。`
  if (pending) return `有 ${pending} 条新知识等你审核。`
  return '没有待处理的事项，机器人运行正常。'
})
const activeAlerts = computed(() => overview.value.alerts.filter(alert => !alert.recovered))
const todos = computed(() => [
  { label: '位用户在等人工回复', clear: '没有人在等人工', count: stats.value?.openHandoffs, to: '/handoffs', tone: 'is-hot' },
  { label: '条新知识等你审核', clear: '没有待审核的知识', count: stats.value?.pendingCandidates, to: '/knowledge?tab=candidates', tone: 'is-info' },
  { label: '个问题机器人没答上', clear: '没有新的知识缺口', count: stats.value?.openGaps, to: '/knowledge?tab=gaps', tone: 'is-warn' },
  { label: '个任务处理失败', clear: '没有失败的任务', count: overview.value.queues.failed, to: '/system', tone: 'is-hot' },
])
interface Kpi { label: string; value: string; hint: string; trend?: string; direction?: 'up' | 'down'; good?: boolean; percent?: number }
const kpis = computed((): Kpi[] => {
  const m = overview.value.metrics
  const delivery = Number.parseFloat(m[4].value)
  return [
    { ...m[0], label: '收到的消息', hint: '最近 24 小时' },
    { ...m[1], label: '成功回复率', value: overview.value.successRate === null ? '—' : `${overview.value.successRate.toFixed(1)}%`, hint: `共 ${m[1].value} 条`, percent: overview.value.successRate ?? 0 },
    { ...m[2], label: '平均处理耗时', hint: '从收到消息到发出回复' },
    { ...m[4], label: '投递成功率', hint: '回复成功发到 QQ 的比例', percent: Number.isFinite(delivery) ? delivery : 0 },
  ]
})

// 把最近消息的各环节聚合成流水线：平均耗时 + 最差状态（失败只标黄，提示“有问题”，不让整条线停掉）
const flowStages = computed(() => {
  const rows = overview.value.pipelines
  const rank: Record<string, number> = { success: 0, warning: 1 }
  const worst = (list: string[]): Status => list.map(item => item === 'success' ? 'success' : 'warning').reduce((acc, item) => (rank[item] > rank[acc] ? item : acc), 'success') as Status
  const avg = (list: Array<number | undefined>) => { const values = list.filter((v): v is number => typeof v === 'number' && v > 0); return values.length ? values.reduce((a, b) => a + b, 0) / values.length : undefined }
  if (!rows.length) return ['接收', '上下文', '知识检索', '模型生成', '投递'].map(label => ({ label, status: 'pending' }))
  return [
    { label: '接收', ms: avg(rows.map(r => r.eventMs)), status: 'success' },
    { label: '上下文', ms: avg(rows.map(r => r.contextMs)), status: worst(rows.map(r => r.contextStatus || 'success')) },
    { label: '知识检索', ms: avg(rows.map(r => r.retrieval.ms)), status: worst(rows.map(r => r.retrieval.status)) },
    { label: '模型生成', ms: avg(rows.map(r => r.model.ms)), status: worst(rows.map(r => r.model.status)) },
    { label: '投递', ms: avg(rows.map(r => r.delivery.ms)), status: worst(rows.map(r => r.delivery.status)) },
  ]
})

const traceScope = ref('average')
const selectedTrace = computed(() => overview.value.pipelines.find(row => row.id === traceScope.value)?.timeline)
function average(values: Array<number | null | undefined>) {
  const measured = values.filter((value): value is number => typeof value === 'number')
  return measured.length ? measured.reduce((sum, value) => sum + value, 0) / measured.length : null
}
const shownTotal = computed(() => traceScope.value === 'average'
  ? average(overview.value.pipelines.map(row => row.timeline?.totalMs))
  : selectedTrace.value?.totalMs)
const shownStages = computed(() => {
  if (traceScope.value !== 'average') return selectedTrace.value?.stages || []
  const samples = overview.value.pipelines.flatMap(row => row.timeline?.stages || [])
  if (!samples.length) return overview.value.pipelines.length ? flowStages.value : []
  const groups = new Map<string, TimingStage[]>()
  for (const stage of samples) {
    const group = groups.get(stage.label) || []
    group.push(stage)
    groups.set(stage.label, group)
  }
  return Array.from(groups, ([label, group]) => ({
    label, ms: average(group.map(s => s.ms)), elapsedMs: average(group.map(s => s.elapsedMs)),
    detail: group.length+' 次记录', status: group.some(s => s.status === 'failed') ? 'failed' : group.some(s => s.status === 'processing') ? 'processing' : group[0].status
  })).sort((a,b) => (a.elapsedMs ?? Infinity) - (b.elapsedMs ?? Infinity))
})
function stageTip(row: PipelineRow) {
  const label: Record<string, string> = { success: '正常', warning: '偏慢', failed: '失败', pending: '未执行' }
  return `上下文 ${label[row.contextStatus || 'success']} · 检索 ${label[row.retrieval.status] || row.retrieval.status}（命中 ${row.retrieval.hit}） · 模型 ${label[row.model.status] || row.model.status}${row.model.name ? `（${row.model.name}）` : ''} · 投递 ${label[row.delivery.status] || row.delivery.status}`
}

async function load(background: unknown = false) {
  if (background !== true) loading.value = true
  overviewError.value = ''
  const [overviewResult, statsResult] = await Promise.allSettled([api.overview(), agentApi.learning.stats()])
  if (overviewResult.status === 'fulfilled') overview.value = overviewResult.value
  else { overview.value = createEmptyOverview(); overviewError.value = overviewResult.reason instanceof Error ? overviewResult.reason.message : '概览数据读取失败' }
  if (statsResult.status === 'fulfilled') stats.value = statsResult.value
  shell.refreshCounts(true)
  loading.value = false
}
onMounted(load)
useAdminEvents(['messages','system_alerts','inbox_tasks','outbox_tasks','model_calls','handoffs','knowledge_candidates','agent_runs','bots','message_stages','group_reviews'], () => load(true))
</script>

