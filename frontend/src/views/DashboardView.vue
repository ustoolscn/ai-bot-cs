<template>
  <div class="content-page">
    <section class="hero">
      <span class="hero-blob b1" /><span class="hero-blob b2" /><span class="hero-blob b3" />
      <Mascot mood="wave" :size="92" float />
      <div>
        <h1>{{ greeting }}，{{ auth.username || '管理员' }}</h1>
        <p>{{ heroLine }}</p>
      </div>
      <div class="hero-actions"><el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button></div>
    </section>

    <div class="section-label"><h2>需要你处理</h2><span>最近 24 小时</span></div>
    <div class="todo-grid">
      <button v-for="(todo, index) in todos" :key="todo.label" class="todo-card" :class="todo.count ? todo.tone : 'is-clear'" @click="$router.push(todo.to)">
        <span class="todo-icon"><el-icon><component :is="todo.count ? todo.icon : Select" /></el-icon></span>
        <span><strong><CountUp :value="todo.count ?? '—'" :delay="index * 80" /></strong><span>{{ todo.count ? todo.label : todo.clear }}</span></span>
        <el-icon class="arrow"><ArrowRight /></el-icon>
      </button>
    </div>

    <div class="section-label"><h2>核心指标</h2><span v-if="overviewError" class="danger-text">{{ overviewError }}</span></div>
    <div class="kpi-grid">
      <div v-for="(kpi, index) in kpis" :key="kpi.label" class="kpi">
        <ProgressRing v-if="kpi.percent !== undefined" :percent="kpi.percent" :size="64" :stroke="7" :tone="kpi.tone" :delay="index * 100"><el-icon class="kpi-ring-icon"><component :is="kpi.icon" /></el-icon></ProgressRing>
        <span v-else class="kpi-icon"><el-icon><component :is="kpi.icon" /></el-icon></span>
        <div>
          <span>{{ kpi.label }}</span>
          <strong><CountUp :value="kpi.value" :delay="index * 100" /></strong>
          <p v-if="kpi.trend">较昨日 <em :class="kpi.good ? 'trend-good' : 'trend-bad'"><el-icon><component :is="kpi.direction === 'up' ? Top : Bottom" /></el-icon>{{ kpi.trend }}</em></p>
          <p v-else>{{ kpi.hint }}</p>
        </div>
      </div>
    </div>

    <div class="overview-grid">
      <div class="side-stack">
        <section class="panel flow-panel">
          <div class="panel-head"><div><h2>消息是怎么被处理的</h2><p>最近 {{ overview.pipelines.length }} 条消息在每一步的平均耗时</p></div></div>
          <div class="panel-body"><PipelineFlow :stages="flowStages" /></div>
        </section>

        <section class="panel">
          <div class="panel-head">
            <div><h2>最近处理的消息</h2><p>小圆点依次是：接收、上下文、检索、模型、投递</p></div>
            <el-button text type="primary" @click="$router.push('/messages')">全部消息 <el-icon class="el-icon--right"><ArrowRight /></el-icon></el-button>
          </div>
          <ul v-if="overview.pipelines.length" class="recent-list">
            <li v-for="(row, index) in overview.pipelines" :key="row.id" class="recent-item" :style="{ animationDelay: `${index * 40}ms` }" @click="$router.push('/messages')">
              <span class="recent-avatar" :style="{ background: avatarColor(row.bot) }">{{ row.bot.slice(0, 1) }}</span>
              <div class="recent-main">
                <strong>{{ row.content || '（没有文字内容）' }}</strong>
                <span>{{ row.time }} · {{ row.bot }}<template v-if="row.conversation"> · {{ row.conversation }}</template></span>
              </div>
              <div class="recent-meta">
                <el-tooltip :content="stageTip(row)" placement="top">
                  <span class="stage-dots">
                    <i class="stage-dot is-success" />
                    <i class="stage-dot" :class="`is-${row.contextStatus || 'success'}`" />
                    <i class="stage-dot" :class="`is-${row.retrieval.status}`" />
                    <i class="stage-dot" :class="`is-${row.model.status}`" />
                    <i class="stage-dot" :class="`is-${row.delivery.status}`" />
                  </span>
                </el-tooltip>
                <span class="recent-total" :class="{ 'danger-text': row.totalMs > 5000 }">{{ formatDuration(row.totalMs) }}</span>
              </div>
            </li>
          </ul>
          <EmptyState v-else title="还没有处理过的消息" description="机器人在群里被 @ 或收到私聊后，处理记录会出现在这里。" />
        </section>
      </div>

      <div class="side-stack">
        <section class="panel">
          <div class="panel-head"><h2>知识库索引</h2><el-button text type="primary" @click="$router.push('/knowledge')">去看看</el-button></div>
          <div class="health-hero">
            <WaveProgress :percent="overview.knowledge.progress" :size="112" label="已完成" />
            <div>
              <strong><CountUp :value="overview.knowledge.completed" /> / {{ overview.knowledge.total }} 篇文档</strong>
              <span>正在索引 {{ overview.knowledge.indexing }} 篇，排队 {{ overview.knowledge.pending }} 篇</span>
              <span v-if="overview.knowledge.failed" class="danger-text">{{ overview.knowledge.failed }} 篇失败，需要处理</span>
            </div>
          </div>
          <div class="health-list">
            <div class="health-row"><span>待处理消息 / 待发送回复</span><strong>{{ overview.queues.inbox }} / {{ overview.queues.outbox }}</strong></div>
            <div class="health-row"><span>失败任务</span><strong :class="{ 'danger-text': overview.queues.failed }">{{ overview.queues.failed }}</strong></div>
          </div>
        </section>

        <section class="panel">
          <div class="panel-head"><h2>告警</h2><span class="muted">{{ activeAlerts.length ? `${activeAlerts.length} 条未恢复` : '' }}</span></div>
          <ul v-if="overview.alerts.length" class="alert-list">
            <li v-for="alert in overview.alerts.slice(0, 5)" :key="alert.id" :class="alert.recovered ? 'recovered' : alert.level">
              <el-icon><component :is="alert.recovered ? CircleCheck : WarningFilled" /></el-icon>
              <div><strong>{{ alert.content }}</strong><span>{{ alert.time }} · {{ alert.bot }}<template v-if="alert.impact"> · {{ alert.impact }}</template><template v-if="alert.recovered"> · 已恢复</template></span></div>
            </li>
          </ul>
          <EmptyState v-else mood="happy" compact title="一切正常" description="最近没有告警。" />
        </section>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, markRaw, onMounted, ref } from 'vue'
import { ArrowRight, Bottom, ChatLineSquare, CircleCheck, Headset, Promotion, QuestionFilled, Reading, Refresh, Select, Timer, Top, WarningFilled } from '@element-plus/icons-vue'
import { api } from '@/api'
import { agentApi, type LearningStats } from '@/api/agent'
import EmptyState from '@/components/EmptyState.vue'
import CountUp from '@/components/fx/CountUp.vue'
import Mascot from '@/components/fx/Mascot.vue'
import PipelineFlow from '@/components/fx/PipelineFlow.vue'
import ProgressRing from '@/components/fx/ProgressRing.vue'
import WaveProgress from '@/components/fx/WaveProgress.vue'
import type { PipelineRow, Status, SystemOverview } from '@/types'
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
  if (waiting) return `有 ${waiting} 位用户在等人工回复，先去看看他们吧。`
  if (pending) return `小Q 学到了 ${pending} 条新知识，等你审核一下。`
  return '小Q 一切正常，正在认真回复大家的问题。'
})
const activeAlerts = computed(() => overview.value.alerts.filter(alert => !alert.recovered))
const todos = computed(() => [
  { label: '位用户在等人工回复', clear: '没有人在等人工', count: stats.value?.openHandoffs, to: '/handoffs', icon: markRaw(Headset), tone: 'is-hot' },
  { label: '条新知识等你审核', clear: '没有待审核的知识', count: stats.value?.pendingCandidates, to: '/learning', icon: markRaw(Reading), tone: 'is-info' },
  { label: '个问题机器人没答上', clear: '没有新的知识缺口', count: stats.value?.openGaps, to: '/learning?tab=gaps', icon: markRaw(QuestionFilled), tone: 'is-warn' },
  { label: '个任务处理失败', clear: '没有失败的任务', count: overview.value.queues.failed, to: '/system', icon: markRaw(WarningFilled), tone: 'is-hot' },
])
interface Kpi { label: string; value: string; hint: string; icon: unknown; trend?: string; direction?: 'up' | 'down'; good?: boolean; percent?: number; tone?: 'mint' | 'sky' }
const kpis = computed((): Kpi[] => {
  const m = overview.value.metrics
  const delivery = Number.parseFloat(m[4].value)
  return [
    { ...m[0], label: '收到的消息', hint: '最近 24 小时', icon: markRaw(ChatLineSquare) },
    { ...m[1], label: '成功回复', value: overview.value.successRate === null ? '—' : `${overview.value.successRate.toFixed(1)}%`, hint: `共 ${m[1].value} 条`, percent: overview.value.successRate ?? 0, tone: 'mint', icon: markRaw(Select) },
    { ...m[2], label: '平均处理耗时', hint: '从收到消息到发出回复', icon: markRaw(Timer) },
    { ...m[4], label: '投递成功率', hint: '回复成功发到 QQ 的比例', percent: Number.isFinite(delivery) ? delivery : 0, tone: 'sky', icon: markRaw(Promotion) },
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

const avatarColors = ['linear-gradient(135deg,#ffa477,#f2735a)', 'linear-gradient(135deg,#a99cf5,#7f6ee6)', 'linear-gradient(135deg,#7fdcbc,#3fb48d)', 'linear-gradient(135deg,#8fcdfb,#4a9fe4)', 'linear-gradient(135deg,#ffd774,#f0a92c)']
function avatarColor(name: string) { let hash = 0; for (const ch of name) hash = (hash * 31 + ch.charCodeAt(0)) >>> 0; return avatarColors[hash % avatarColors.length] }
function stageTip(row: PipelineRow) {
  const label: Record<string, string> = { success: '正常', warning: '偏慢', failed: '失败', pending: '未执行' }
  return `上下文 ${label[row.contextStatus || 'success']} · 检索 ${label[row.retrieval.status] || row.retrieval.status}（命中 ${row.retrieval.hit}） · 模型 ${label[row.model.status] || row.model.status}${row.model.name ? `（${row.model.name}）` : ''} · 投递 ${label[row.delivery.status] || row.delivery.status}`
}

async function load() {
  loading.value = true
  overviewError.value = ''
  const [overviewResult, statsResult] = await Promise.allSettled([api.overview(), agentApi.learning.stats()])
  if (overviewResult.status === 'fulfilled') overview.value = overviewResult.value
  else { overview.value = createEmptyOverview(); overviewError.value = overviewResult.reason instanceof Error ? overviewResult.reason.message : '概览数据读取失败' }
  if (statsResult.status === 'fulfilled') stats.value = statsResult.value
  shell.refreshCounts(true)
  loading.value = false
}
onMounted(load)
</script>

<style scoped>
.kpi-icon { width: 56px; height: 56px; flex: none; display: grid; place-items: center; border-radius: 20px; font-size: 23px; background: var(--primary-light); color: var(--primary); }
.kpi:nth-child(3) .kpi-icon { background: var(--butter-light); color: #c98410; }
.kpi-ring-icon { font-size: 20px; color: var(--text-2); }
.recent-item { grid-template-columns: 34px minmax(0, 1fr) auto; animation: rise .4s var(--ease-bounce) both; }
.alert-list .recovered .el-icon { color: var(--success); }
.overview-grid { grid-template-columns: minmax(0, 1fr) 360px; }
@media (max-width: 1180px) { .overview-grid { grid-template-columns: 1fr; } }
</style>
