<template>
  <div class="content-page dash">
    <div class="greeting">
      <h1>{{ greeting }}，{{ auth.username || '管理员' }}</h1>
      <div class="greeting-actions">
        <button v-for="todo in todos" :key="todo.label" class="todo-chip" :class="todo.tone" @click="$router.push(todo.to)"><strong>{{ todo.count }}</strong>{{ todo.label }}</button>
        <button type="button" class="cost-chip" title="按当前价格估算，点开看明细" @click="costOpen = true"><span>今日消费</span><strong>¥{{ money(cost?.today) }}</strong><span class="sep" /><span>累计</span><strong>¥{{ money(cost?.total) }}</strong></button>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
      </div>
    </div>

    <!-- 最需要盯的：现在正在处理哪条。空闲时显示刚处理完的那条。 -->
    <section class="panel now" :class="{ busy }">
      <header class="now-head">
        <span class="now-state"><LiveDot :active="busy" :tone="busy ? 'primary' : 'success'" />{{ busy ? `正在处理 ${overview.active.length} 条` : '空闲，等待新消息' }}</span>
        <span v-if="!busy && latest" class="muted">最近一条</span>
      </header>
      <TransitionGroup v-if="busy" name="live" tag="ul" class="now-list">
        <li v-for="task in overview.active" :key="task.id" class="now-item">
          <span class="who"><b>{{ task.sender || '用户' }}</b> · {{ task.conversation || '私聊' }} · {{ task.bot }}</span>
          <strong class="now-q">{{ mentions(task.question, task.mentionNames) || '（图片或表情）' }}</strong>
          <span class="stage-run"><i />{{ task.stage }}<template v-if="task.elapsedMs"> · 已用 {{ (task.elapsedMs / 1000).toFixed(1) }} 秒</template></span>
        </li>
      </TransitionGroup>
      <div v-else-if="latest" class="now-item is-done" role="button" tabindex="0" @click="openMessage(latest.id)" @keydown.enter.self="openMessage(latest.id)">
        <span class="who"><b>{{ latest.sender || '未知用户' }}</b><template v-if="latest.conversation"> · {{ latest.conversation }}</template> · {{ latest.bot }} · {{ latest.time }}</span>
        <strong class="now-q">{{ mentions(latest.content, latest.mentionNames) || '（没有文字内容）' }}</strong>
        <span class="now-result">
          <span class="pill" :class="outcomeOf(latest).tone" :title="outcomeOf(latest).title">{{ outcomeOf(latest).text }}</span>
          <span class="live-ms" :class="{ slow: rowTotal(latest) > 5000 }">用时 {{ rowTotal(latest) ? (rowTotal(latest) / 1000).toFixed(1) + ' 秒' : '—' }}</span>
          <span class="now-open">查看处理详情<el-icon><ArrowRight /></el-icon></span>
        </span>
      </div>
      <p v-else class="muted now-empty">机器人被 @ 或收到私聊后，正在处理的消息会出现在这里。</p>
    </section>

    <div class="dash-stats">
      <section class="panel dash-card">
        <header>
          <h2>任务状态</h2>
          <span class="head-actions">
            <el-popover v-if="activeAlerts.length" placement="bottom-end" :width="400" trigger="click" popper-class="alert-popover">
              <template #reference><el-button text type="danger">{{ activeAlerts.length }} 条告警</el-button></template>
              <div class="alert-pop">
                <p class="alert-pop-title">{{ activeAlerts.length }} 条未处理的告警</p>
                <ul class="alert-list">
                  <li v-for="alert in activeAlerts" :key="alert.id" :class="'is-' + alert.level">
                    <i class="alert-dot" />
                    <div class="alert-main">
                      <strong>{{ alertTitle(alert) }}</strong>
                      <p>{{ $at(alertReason(alert)) }}</p>
                      <code v-if="alert.detail" class="alert-detail" :title="alert.detail">{{ alert.detail }}</code>
                      <span class="alert-meta">{{ alert.impact }} · {{ alert.bot }} · {{ alert.time }}</span>
                    </div>
                    <el-button size="small" @click="openAlert(alert)">{{ alertAction(alert) }}</el-button>
                  </li>
                </ul>
              </div>
            </el-popover>
            <el-button v-if="overview.queues.failed" text :loading="dismissing" @click="dismissFailed">忽略失败</el-button>
          </span>
        </header>
        <div class="top-body"><QueueFlow :nodes="queueNodes" @select="node => node.to && $router.push(node.to)" /></div>
      </section>

      <section class="panel dash-card">
        <header><h2>处理消息</h2><span class="muted">24 小时</span></header>
        <div class="top-body">
          <div class="stat-nums">
            <div><strong :title="exact(overview.messages24h)"><CountUp v-if="!isBig(overview.messages24h)" :value="overview.messages24h" /><template v-else>{{ wan(overview.messages24h) }}<small>万</small></template></strong><span>条消息</span></div>
            <div><strong :title="exact(successCount)"><CountUp v-if="!isBig(successCount)" :value="successCount" /><template v-else>{{ wan(successCount) }}<small>万</small></template></strong><span>条成功回复</span></div>
            <div><strong>{{ avgTotal ? (avgTotal / 1000).toFixed(1) : '—' }}<small v-if="avgTotal">秒</small></strong><span>平均用时</span></div>
          </div>
          <div class="meter-row">
            <span>成功率</span>
            <span class="meter"><i :style="{ transform: `scaleX(${(overview.successRate ?? 0) / 100})` }" /></span>
            <strong><CountUp :value="overview.successRate" :decimals="1" suffix="%" /></strong>
          </div>
        </div>
      </section>

      <section class="panel dash-card">
        <header><h2>知识库</h2><el-button text type="primary" @click="$router.push('/knowledge')">管理</el-button></header>
        <div class="top-body">
          <div class="stat-nums">
            <div><strong><CountUp :value="overview.knowledge.total" /></strong><span>篇文档</span></div>
            <div><strong><CountUp :value="overview.knowledge.entries ?? stats?.publishedEntries" /></strong><span>条问答</span></div>
            <div><strong :class="{ warn: stats?.pendingCandidates }"><CountUp :value="stats?.pendingCandidates" /></strong><span>条待审核</span></div>
          </div>
          <div class="meter-row">
            <span>已索引</span>
            <span class="meter" :class="{ indexing: overview.knowledge.indexing }"><i :style="{ transform: `scaleX(${overview.knowledge.progress / 100})` }" /></span>
            <strong><CountUp :value="overview.knowledge.progress" suffix="%" /></strong>
          </div>
        </div>
      </section>

    </div>

    <div class="dash-bottom">
      <section class="panel dash-card bottom-card">
        <header><h2>最近处理</h2><el-button text type="primary" @click="$router.push('/messages')">全部消息</el-button></header>
        <div class="live-scroll">
          <TransitionGroup name="live" tag="ul" class="live-list">
            <li v-for="row in recent" :key="row.id" class="live-item" :class="{ selected: traceScope === row.id }" role="button" tabindex="0" :aria-pressed="traceScope === row.id" title="在右侧查看这条消息各环节的用时" @click="pickRow(row.id)" @keydown.enter.self="pickRow(row.id)" @keydown.space.self.prevent="pickRow(row.id)">
              <LiveDot :tone="rowTone(row)" />
              <div class="live-main">
                <span class="who"><b>{{ row.sender || '未知用户' }}</b><template v-if="row.conversation"> · {{ row.conversation }}</template> · {{ row.bot }}</span>
                <strong>{{ mentions(row.content, row.mentionNames) || '（没有文字内容）' }}</strong>
              </div>
              <span class="live-side">
                <span class="pill" :class="outcomeOf(row).tone" :title="outcomeOf(row).title">{{ outcomeOf(row).text }}</span>
                <span class="live-meta"><span>{{ row.time }}</span><span class="live-ms" :class="{ slow: rowTotal(row) > 5000 }">{{ rowTotal(row) ? (rowTotal(row) / 1000).toFixed(1) + 's' : '—' }}</span></span>
              </span>
              <el-tooltip content="查看处理详情" placement="left" :show-after="300">
                <button type="button" class="live-open" aria-label="查看这条消息的处理详情" @click.stop="openMessage(row.id)"><el-icon><ArrowRight /></el-icon></button>
              </el-tooltip>
            </li>
          </TransitionGroup>
          <EmptyState v-if="!recent.length" compact title="还没有消息" description="机器人被 @ 或收到私聊后会出现在这里。" />
        </div>
      </section>

      <section class="panel dash-card bottom-card">
        <header>
          <h2>处理用时</h2>
          <el-select v-model="traceScope" aria-label="选择消息" size="small" style="width: 220px">
            <el-option label="最近消息平均" value="average" />
            <el-option v-for="row in overview.pipelines" :key="row.id" :value="row.id" :label="row.time + ' · ' + (row.sender ? row.sender + '：' : '') + (mentions(row.content, row.mentionNames) || '群消息')" />
          </el-select>
        </header>
        <div class="stage-scroll"><StageBars :stages="shownStages" :waterfall="traceScope !== 'average'" /></div>
        <p class="total-line">{{ traceScope === 'average' ? '平均总用时' : '总用时' }} <strong>{{ shownTotal == null ? '—' : (shownTotal / 1000).toFixed(2) + ' 秒' }}</strong></p>
      </section>
    </div>

    <MessageDetailDrawer v-model="detailOpen" :message-id="detailId" />
    <el-dialog v-model="costOpen" title="消费明细（按当前价格估算）" width="960px" append-to-body>
      <CostPanel ref="costPanel" class="cost-detail" />
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, ref } from 'vue'
import { ArrowRight, Refresh } from '@element-plus/icons-vue'
import { api } from '@/api'
import { agentApi, type LearningStats } from '@/api/agent'
import EmptyState from '@/components/EmptyState.vue'
import CostPanel from '@/components/CostPanel.vue'
import MessageDetailDrawer from '@/components/message/MessageDetailDrawer.vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import CountUp from '@/components/fx/CountUp.vue'
import LiveDot from '@/components/fx/LiveDot.vue'
import QueueFlow from '@/components/fx/QueueFlow.vue'
import StageBars from '@/components/fx/StageBars.vue'
import type { AlertItem, CostSummary, PipelineRow, SystemOverview, TimingStage } from '@/types'
import { formatMentions as mentions } from '@/utils/mentions'
import { useRouter } from 'vue-router'
import { createEmptyOverview } from '@/utils/overview'
import { sortByFlow } from '@/utils/stage-meta'
import { useAuthStore } from '@/stores/auth'
import { useShellStore } from '@/stores/shell'

const overview = ref<SystemOverview>(createEmptyOverview())
const stats = ref<LearningStats | null>(null)
const loading = ref(false)
const dismissing = ref(false)
const costPanel = ref<InstanceType<typeof CostPanel>>()

// 403 这类重试也不会好的失败，忽略后不再计入失败数，记录仍保留
async function dismissFailed() {
  const n = overview.value.queues.failed
  try { await ElMessageBox.confirm(`把 ${n} 个失败任务标记为已忽略？忽略后不再计入失败，也不会被“重试失败任务”重新执行，记录仍会保留。`, '忽略失败任务', { confirmButtonText: '忽略', cancelButtonText: '取消', type: 'warning' }) } catch { return }
  dismissing.value = true
  try { const { dismissed } = await api.system.dismissFailed(); ElMessage.success(`已忽略 ${dismissed} 个失败任务`); await load(true) }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '忽略失败') }
  finally { dismissing.value = false }
}
const shell = useShellStore()
const router = useRouter()
// 处理详情就地用抽屉打开，不跳到消息记录页。
const detailOpen = ref(false)
const detailId = ref('')
function openMessage(id: string) { detailId.value = id; detailOpen.value = true }
// 消费只看个大概：今日和累计；明细放在弹窗里。
const cost = ref<CostSummary | null>(null)
const costOpen = ref(false)
// 上万的条数用“万”显示，卡片窄时也放得下；悬停看准确数字。
const isBig = (v: number | null) => (v ?? 0) >= 10000
const wan = (v: number | null) => ((v ?? 0) / 10000).toFixed(1)
const exact = (v: number | null) => v == null ? undefined : v.toLocaleString() + ' 条'
const money = (v?: number | null) => v == null ? '—' : v >= 100 ? v.toFixed(0) : v.toFixed(2)

// 告警：说清楚出了什么事、影响什么，按钮直接带到能处理它的地方
const alertTitle = (a: AlertItem) => a.kind === 'document' ? '知识文档索引失败' : a.kind === 'outbox' ? '回复没有发出去' : a.kind === 'inbox' ? '消息处理失败' : a.content.split('：')[0] || '告警'
const alertReason = (a: AlertItem) => { const i = a.content.indexOf('：'); return i >= 0 ? a.content.slice(i + 1) : a.content }
const alertAction = (a: AlertItem) => a.kind === 'document' ? '查看文档' : a.messageId ? '查看消息' : '查看失败消息'
function openAlert(a: AlertItem) {
  if (a.kind === 'document') router.push({ path: '/knowledge', query: a.knowledgeBaseId ? { kb: a.knowledgeBaseId } : {} })
  else if (a.messageId) openMessage(a.messageId)
  else router.push({ path: '/messages', query: { status: 'failed' } })
}
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
  { label: '失败', count: overview.value.queues.failed, tone: 'danger', to: overview.value.queues.failed ? '/messages?status=failed' : undefined },
])
const activeIds = computed(() => new Set(overview.value.active.map(task => task.id)))
const recent = computed(() => overview.value.pipelines.filter(row => !activeIds.value.has(row.id)).slice(0, 20))
const latest = computed(() => recent.value[0])
const successCount = computed(() => overview.value.messages24h == null || overview.value.successRate == null ? null : Math.round(overview.value.messages24h * overview.value.successRate / 100))

const rowTotal = (row: PipelineRow) => row.timeline ? row.timeline.totalMs ?? 0 : row.totalMs
function rowFailed(row: PipelineRow) {
  return row.outcome ? row.outcome === 'failed' : !!row.timeline?.stages?.some(s => s.status === 'failed') || row.delivery.status === 'failed'
}
function rowTone(row: PipelineRow): 'success' | 'warning' | 'danger' | 'muted' {
  if (rowFailed(row)) return 'danger'
  if (row.outcome === 'handoff') return 'warning'
  if (row.outcome === 'skipped' || row.outcome === 'merged') return 'muted'
  if (rowTotal(row) > 5000) return 'warning'
  return 'success'
}
// 处理结果：后端给 outcome 就用它，旧数据只能分出失败和完成
function outcomeOf(row: PipelineRow): { text: string; tone: string; title?: string } {
  switch (row.outcome) {
    case 'handoff': return { text: row.ticketNumber ? `转人工 #${row.ticketNumber}` : '转人工', tone: 'warn' }
    case 'skipped': return { text: '无需回复', tone: 'idle' }
    case 'merged': return { text: '已合并', tone: 'idle', title: '和同一人前后几条消息合在一起回复' }
    case 'processing': return { text: '处理中', tone: 'busy' }
    case 'failed': return { text: '失败', tone: 'bad', title: row.error }
    case 'replied': return { text: '已回复', tone: 'good' }
  }
  return rowFailed(row) ? { text: '失败', tone: 'bad' } : { text: '已完成', tone: 'good' }
}
// 点一行：左边“各环节用时”切到这条；再点一次回到平均
function pickRow(id: string) { traceScope.value = traceScope.value === id ? 'average' : id }
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
  const rows = overview.value.pipelines
  const samples = rows.flatMap(row => row.timeline?.stages || [])
  if (overview.value.stageAverages.length) return sortByFlow(overview.value.stageAverages, samples)
  if (!samples.length) return rows.length ? legacyStages(rows) : []
  const groups = new Map<string, TimingStage[]>()
  for (const stage of samples) groups.set(stage.label, [...(groups.get(stage.label) || []), stage])
  return sortByFlow(Array.from(groups, ([label, group]) => ({
    label,
    ms: average(group.map(s => s.ms)),
    elapsedMs: average(group.map(s => s.elapsedMs)),
    status: group.some(s => s.status === 'failed') ? 'failed' : group.some(s => s.status === 'processing') ? 'processing' : 'success',
  })), samples)
})

async function load(background: unknown = false) {
  if (background !== true) loading.value = true
  const [overviewResult, statsResult, costResult] = await Promise.allSettled([api.overview(), agentApi.learning.stats(), api.system.costs('today')])
  if (overviewResult.status === 'fulfilled') overview.value = overviewResult.value
  if (statsResult.status === 'fulfilled') stats.value = statsResult.value
  if (costResult.status === 'fulfilled') cost.value = costResult.value
  costPanel.value?.refresh(background !== true)
  shell.refreshCounts(true)
  loading.value = false
}
onMounted(load)
useAdminEvents(['messages','system_alerts','inbox_tasks','outbox_tasks','model_calls','handoffs','knowledge_candidates','agent_runs','bots','message_stages','group_reviews','knowledge_documents'], () => load(true))
</script>

<style scoped>
.greeting-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; justify-content: flex-end; }
.todo-chip { display: inline-flex; align-items: center; gap: 6px; height: 32px; padding: 0 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface); color: var(--text-2); font-size: 13px; cursor: pointer; transition: background var(--dur-fast) var(--ease), border-color var(--dur-fast) var(--ease), transform var(--dur-fast) var(--ease); animation: pop-in var(--dur-slow) var(--ease) backwards; }
.todo-chip:hover { border-color: var(--line-strong); background: var(--surface-2); }
.todo-chip:active { transform: scale(.97); }
.todo-chip strong { font-size: 15px; font-variant-numeric: tabular-nums; }
.todo-chip.is-hot strong { color: var(--danger); }
.todo-chip.is-info strong { color: var(--primary-text); }
.todo-chip.is-warn strong { color: var(--warning); }

.dash-stats { display: grid; grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr) minmax(0, 1fr); gap: 16px; margin-top: 16px; }
.dash-bottom { display: grid; grid-template-columns: minmax(0, 1.25fr) minmax(0, 1fr); gap: 16px; margin-top: 16px; height: 420px; }

/* 正在处理：页面顶部，问题本身用较大字号；忙时整块浅蓝，空闲时白底 */
.now { display: flex; flex-direction: column; gap: 10px; padding: 16px 20px; transition: background .4s var(--ease), border-color .4s var(--ease); }
.now.busy { background: var(--primary-light); border-color: var(--primary-soft); }
.now-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; height: 24px; font-size: 13px; }
.now-state { display: inline-flex; align-items: center; gap: 8px; color: var(--text-2); font-weight: 600; }
.now.busy .now-state { color: var(--primary-text); }
.now-list { display: flex; flex-direction: column; gap: 12px; margin: 0; padding: 0; list-style: none; }
.now-list > li + li { padding-top: 12px; border-top: 1px solid var(--primary-soft); }
.now-item { display: flex; flex-direction: column; gap: 4px; min-width: 0; }
.now-item.is-done { margin: -6px -10px; padding: 6px 10px; border-radius: var(--radius-sm); cursor: pointer; transition: background var(--dur-fast) var(--ease); }
.now-item.is-done:hover { background: var(--surface-2); }
.now-item.is-done:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
.now-q { overflow: hidden; font-size: 16px; font-weight: 600; line-height: 1.5; text-overflow: ellipsis; white-space: nowrap; }
.now-result { display: flex; align-items: center; gap: 12px; font-size: 13px; }
.now-result .live-ms { font-size: 13px; }
.now-open { display: inline-flex; align-items: center; gap: 2px; margin-left: auto; color: var(--primary-text); font-size: 13px; }
.now-open .el-icon { transition: transform var(--dur-fast) var(--ease); }
.now-item.is-done:hover .now-open .el-icon { transform: translateX(2px); }
.now-empty { margin: 0; font-size: 13px; }
.now .stage-run { margin-top: 2px; font-size: 13px; }
.dash-card { display: flex; flex-direction: column; gap: 16px; padding: 16px 20px 20px; min-width: 0; }
.dash-card > header { display: flex; align-items: center; justify-content: space-between; gap: 12px; height: 32px; flex: none; }
.dash-card h2 { font-size: 15px; font-weight: 600; white-space: nowrap; }
.head-actions { display: flex; align-items: center; gap: 12px; }
.head-actions .el-button { height: 28px; padding: 0 6px; }
.head-actions .el-button + .el-button { margin-left: 0; }
.dash-card header .muted { font-size: 12px; }
.top-body { display: flex; flex: 1; flex-direction: column; justify-content: space-between; gap: 12px; }

.meter-row { display: grid; grid-template-columns: 56px minmax(0, 1fr) 64px; align-items: center; gap: 10px; font-size: 13px; color: var(--muted); }
.meter-row strong { text-align: right; color: var(--text); font-weight: 600; font-variant-numeric: tabular-nums; }
.meter { position: relative; height: 6px; border-radius: 3px; background: var(--surface-3); overflow: hidden; }
.meter i { position: absolute; inset: 0; border-radius: 3px; background: var(--success); transform-origin: left; transition: transform .8s var(--ease); }
.meter i.is-time { background: var(--primary); }
.meter.indexing i { background: linear-gradient(90deg, var(--primary) 0 40%, #7fb8f2 50%, var(--primary) 60% 100%); background-size: 250% 100%; animation: flow 1.4s linear infinite; }
.stat-nums { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; }
.stat-nums div { display: flex; flex-direction: column; min-width: 0; }
.stat-nums strong { overflow: hidden; font-size: 24px; text-overflow: ellipsis; white-space: nowrap; font-weight: 600; line-height: 1.2; font-variant-numeric: tabular-nums; }
.stat-nums strong small { margin-left: 2px; color: var(--muted); font-size: 13px; font-weight: 400; }
.stat-nums strong.warn { color: var(--warning); }
.stat-nums div > span { color: var(--muted); font-size: 12px; white-space: nowrap; }

/* 消费只看个大概：放在页头一行小字，明细在弹窗里 */
.cost-chip { display: inline-flex; align-items: center; gap: 6px; height: 32px; padding: 0 12px; border: 0; border-radius: var(--radius-sm); background: none; color: var(--muted); font-size: 13px; cursor: pointer; transition: background var(--dur-fast) var(--ease); }
.cost-chip:hover { background: var(--surface-3); }
.cost-chip:active { transform: scale(.97); }
.cost-chip strong { color: var(--text-2); font-weight: 600; font-variant-numeric: tabular-nums; }
.cost-chip .sep { width: 1px; height: 12px; margin: 0 4px; background: var(--line-strong); }
.cost-detail { margin: 0; padding: 0; border: 0; }
.cost-detail :deep(h2) { display: none; }

.bottom-card { min-height: 0; }
.bottom-card :deep(.stage-bars) { align-content: start; gap: 10px; }
/* 环节多时在卡片里滚动，不截断 */
.stage-scroll { flex: 1; min-height: 0; margin: 0 -20px; padding: 0 20px; overflow-y: auto; overscroll-behavior: contain; }
/* 网格里的卡片不吃全局的 .panel + .panel 上边距，否则同一行会错开 16px */
.dash-stats > .panel, .dash-bottom > .panel { margin-top: 0; }
.total-line { margin: 0; padding-top: 12px; border-top: 1px solid var(--line); text-align: right; color: var(--muted); font-size: 13px; }
.total-line strong { margin-left: 4px; color: var(--text); font-size: 15px; font-variant-numeric: tabular-nums; }

.live-scroll { position: relative; flex: 1; min-height: 0; margin: 0 -20px -20px; overflow-y: auto; border-top: 1px solid var(--line); }
.live-list { position: relative; margin: 0; padding: 0; list-style: none; }
.live-item { position: relative; display: grid; grid-template-columns: 10px minmax(0, 1fr) auto 28px; align-items: center; gap: 12px; min-height: 56px; padding: 8px 12px 8px 20px; border-bottom: 1px solid var(--line); cursor: pointer; transition: background var(--dur-fast) var(--ease); }
.live-item:hover { background: var(--surface-2); }
.live-item::before { content: ''; position: absolute; inset: 0 auto 0 0; width: 3px; background: var(--primary); transform: scaleY(0); transition: transform var(--dur) var(--ease); }
.live-item.selected { background: var(--primary-light); }
.live-item.selected::before { transform: scaleY(1); }
.live-open { display: grid; place-items: center; width: 28px; height: 28px; padding: 0; border: 0; border-radius: var(--radius-sm); background: none; color: var(--faint); cursor: pointer; transition: background var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease), transform var(--dur-fast) var(--ease); }
.live-open:hover { background: var(--surface-3); color: var(--primary-text); transform: translateX(2px); }
.live-open:focus-visible { outline: 2px solid var(--primary); outline-offset: 1px; }
.live-meta { display: inline-flex; gap: 8px; color: var(--faint, var(--muted)); font-size: 12px; font-variant-numeric: tabular-nums; }
.live-main { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.live-main strong { overflow: hidden; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.who { overflow: hidden; color: var(--muted); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.who b { color: var(--text-2); font-weight: 600; }
.live-side { display: flex; flex-direction: column; align-items: flex-end; gap: 2px; }
.live-time { color: var(--faint, var(--muted)); font-size: 12px; font-variant-numeric: tabular-nums; }
.live-ms { color: var(--text-2); font-size: 12px; font-variant-numeric: tabular-nums; }
.live-ms.slow, .live-ms.failed { color: var(--danger); }
.live-item:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
.stage-run { position: relative; display: inline-flex; align-items: center; gap: 8px; margin-top: 4px; color: var(--primary-text); font-size: 12px; }
.stage-run i { width: 64px; height: 4px; border-radius: 2px; background: linear-gradient(90deg, transparent 0 30%, var(--primary) 50%, transparent 70% 100%); background-size: 250% 100%; animation: flow 1.1s linear infinite; }

.live-enter-active, .live-leave-active, .live-move { transition: transform .35s var(--ease), opacity .35s var(--ease), background 1.2s var(--ease); }
.live-enter-from { opacity: 0; transform: translateY(-10px); background: var(--primary-soft); }
.live-leave-to { opacity: 0; }
.live-leave-active { position: absolute; left: 0; right: 0; }

.alert-pop-title { margin: 0 0 8px; color: var(--muted); font-size: 12px; }
.alert-list { display: flex; flex-direction: column; gap: 4px; max-height: min(60vh, 420px); margin: 0 -12px; padding: 0; overflow-y: auto; list-style: none; }
.alert-list li { display: grid; grid-template-columns: 8px minmax(0, 1fr) auto; align-items: start; gap: 10px; padding: 10px 12px; border-radius: var(--radius-sm); transition: background var(--dur-fast) var(--ease); }
.alert-list li:hover { background: var(--surface-2); }
.alert-dot { width: 8px; height: 8px; margin-top: 6px; border-radius: 50%; background: var(--warning); }
.is-critical .alert-dot { background: var(--danger); }
.is-info .alert-dot { background: var(--primary); }
.alert-main { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.alert-main strong { font-size: 14px; font-weight: 600; }
.alert-main p { margin: 0; color: var(--text-2); font-size: 13px; line-height: 1.5; overflow-wrap: anywhere; }
.alert-meta { color: var(--muted); font-size: 12px; }
.alert-detail { display: -webkit-box; overflow: hidden; color: var(--muted); font-size: 12px; line-height: 1.5; overflow-wrap: anywhere; -webkit-box-orient: vertical; -webkit-line-clamp: 2; }

@media (max-width: 1100px) { .dash-stats { grid-template-columns: 1fr 1fr; } .dash-stats > :first-child { grid-column: 1 / -1; } .dash-bottom { grid-template-columns: 1fr; height: auto; } .live-scroll { max-height: 360px; } }
@media (max-width: 640px) { .dash-stats { grid-template-columns: 1fr; } .now-result { flex-wrap: wrap; } .greeting { flex-direction: column; align-items: flex-start; } .greeting-actions { justify-content: flex-start; } }
</style>
