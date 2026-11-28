<template>
  <div ref="pageRef" class="content-page page-fill">
    <section class="holo-hero">
      <HoloBanner3D variant="messages" class="holo-hero-bg" :alert="failedCount > 0" />
      <div class="holo-hero-scrim" aria-hidden="true" />
      <div class="holo-hero-content">
        <div class="holo-hero-title-row">
          <span class="holo-badge" :class="{ 'is-alert': failedCount > 0 }"><i /><span>MESSAGE TELEMETRY STREAM</span></span>
          <h1>消息追踪</h1>
        </div>
        <p class="holo-hero-desc">实时观测每条触发机器人的消息从收到、上下文装配、知识检索、模型生成到 QQ 投递的完整链路。</p>
        <div class="holo-kpi-row">
          <span class="holo-kpi is-cyan">总记录 <strong>{{ messages.length }}</strong></span>
          <span class="holo-kpi is-green">已回复 <strong>{{ repliedCount }}</strong></span>
          <span class="holo-kpi is-warn">转人工 <strong>{{ handoffCount }}</strong></span>
          <span v-if="failedCount" class="holo-kpi is-danger">失败 <strong>{{ failedCount }}</strong></span>
        </div>
      </div>
      <div class="holo-hero-actions">
        <el-button :icon="Download" @click="exportRows">导出记录</el-button>
        <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
      </div>
    </section>
    <div class="toolbar">
      <el-input v-model="search" placeholder="搜索问题、回复、发送者或消息 ID" :prefix-icon="Search" clearable />
      <el-segmented v-model="status" :options="[{ label: '全部', value: 'all' }, { label: '成功', value: 'success' }, { label: '转人工', value: 'handoff' }, { label: '无需回复', value: 'skipped' }, { label: '已合并', value: 'merged' }, { label: '失败', value: 'failed' }]" />
      <span class="spacer" />
      <span class="muted">{{ filtered.length }} 条</span>
    </div>
    <section class="table-panel">
      <el-table v-loading="loading" class="clickable-rows" :data="filtered" row-key="id" height="100%" highlight-current-row @row-click="open">
        <template #empty>
          <EmptyState v-if="loadError" mood="thinking" title="消息记录加载失败" :description="loadError"><el-button @click="load">重新加载</el-button></EmptyState>
          <EmptyState v-else title="还没有消息" description="机器人在群里被 @ 或收到私聊后，消息会出现在这里。" />
        </template>
        <el-table-column label="时间" width="112"><template #default="{ row }"><div class="stacked-cell num"><strong>{{ String(row.time).split(' ')[1] || row.time }}</strong><span>{{ String(row.time).split(' ')[0] }}</span></div></template></el-table-column>
        <el-table-column label="消息" min-width="260"><template #default="{ row }"><div class="message-preview"><strong>{{ mentions(row.question, row.mentionNames) }}</strong><span>{{ row.attachments?.length ? `包含 ${row.attachments.length} 个附件 · ` : '' }}{{ mentions(row.answer, row.mentionNames) || '未生成回复' }}</span></div></template></el-table-column>
        <el-table-column label="机器人 / 会话" min-width="150"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.botName }}</strong><span>{{ row.conversationName }}</span></div></template></el-table-column>
        <el-table-column label="发送者" width="110"><template #default="{ row }"><div class="sender-cell"><span class="truncate" :title="row.sender">{{ row.sender }}</span><i v-if="row.senderBlockedIds?.length" class="sender-blocked" title="已拉黑" /></div></template></el-table-column>
        <el-table-column label="模型" width="130"><template #default="{ row }"><div class="stacked-cell"><strong class="truncate">{{ row.model || '—' }}</strong><span>{{ row.tokens }} tokens</span></div></template></el-table-column>
        <el-table-column label="知识命中" width="84" align="right"><template #default="{ row }"><span class="num">{{ row.knowledgeHits }}</span></template></el-table-column>
        <el-table-column label="耗时" width="80" align="right"><template #default="{ row }"><span class="num">{{ formatDuration(row.latency) }}</span></template></el-table-column>
        <el-table-column label="结论" width="132"><template #default="{ row }"><StatusBadge v-bind="messageVerdict(row)" /></template></el-table-column>
      </el-table>
    </section>

    <MessageDetailDrawer v-model="drawer" :message-id="selectedId" :summary="selectedRow" @changed="load(true)" />
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { formatMentions as mentions } from '@/utils/mentions'
import { Download, Refresh, Search } from '@element-plus/icons-vue'
import EmptyState from '@/components/EmptyState.vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import StatusBadge from '@/components/StatusBadge.vue'
import MessageDetailDrawer from '@/components/message/MessageDetailDrawer.vue'
import HoloBanner3D from '@/components/three/HoloBanner3D.vue'
import { animateStagger } from '@/utils/motion'
import type { MessageRecord } from '@/types'
import { formatDuration, messageVerdict, statusText } from '@/utils/format'

const pageRef = ref<HTMLElement>()
const messages = ref<MessageRecord[]>([])
const repliedCount = computed(() => messages.value.filter(m => !m.handoffStatus && m.status === 'success').length)
const handoffCount = computed(() => messages.value.filter(m => Boolean(m.handoffStatus)).length)
const failedCount = computed(() => messages.value.filter(m => !m.handoffStatus && m.status === 'failed').length)
const search = ref('')
const status = ref('all')
const drawer = ref(false)
const selectedId = ref('')
const selectedRow = computed(() => messages.value.find(m => m.id === selectedId.value))
const loading = ref(false)
const loadError = ref('')
const filtered = computed(() => messages.value.filter(m => (!search.value || `${mentions(m.question, m.mentionNames)}${m.answer}${m.sender}${m.traceId}`.includes(search.value)) && (status.value === 'all' || (status.value === 'handoff' ? Boolean(m.handoffStatus) : !m.handoffStatus && m.status === status.value))))

// 从概览/告警跳来：?id= 直接打开那条消息的详情，?status= 预设筛选
const route = useRoute()
const router = useRouter()
const validStatus = ['all', 'success', 'handoff', 'skipped', 'merged', 'failed']
function applyQuery() {
  const q = String(route.query.status || '')
  if (validStatus.includes(q)) status.value = q
  const id = String(route.query.id || '')
  if (id && !(drawer.value && selectedId.value === id)) openById(id)
}
function openById(id: string) { selectedId.value = id; drawer.value = true }
function open(row: MessageRecord) { openById(row.id) }
watch(() => [route.query.id, route.query.status], applyQuery)
watch(drawer, visible => { if (!visible && route.query.id) router.replace({ query: { ...route.query, id: undefined } }) })
onMounted(async () => { await load(); applyQuery(); nextTick(() => animateStagger(pageRef.value)) })
async function load(background: unknown = false) {
  if (background !== true) loading.value = true
  loadError.value = ''
  try {
    const result = await api.messages.list()
    messages.value = Array.isArray(result) ? result : []
  } catch (error) {
    messages.value = []
    loadError.value = error instanceof Error ? error.message : '请稍后重试。'
  } finally { loading.value = false }
}
function exportRows() { const csv = ['时间,机器人,会话,问题,结论,Token', ...filtered.value.map(m => `"${m.time}","${m.botName}","${m.conversationName}","${m.question.replaceAll('"','""')}","${messageVerdict(m).text || statusText(m.status)}","${m.tokens}"`)].join('\n'); const a=document.createElement('a'); a.href=URL.createObjectURL(new Blob(['\ufeff'+csv],{type:'text/csv'})); a.download='messages.csv'; a.click(); URL.revokeObjectURL(a.href); ElMessage.success('消息记录已导出') }
useAdminEvents(['messages','message_retractions','agent_runs','model_calls','tool_calls','outbox_tasks','inbox_tasks','message_stages','group_reviews','blocked_users'], () => load(true))
</script>

<style scoped>
.sender-cell { display: flex; align-items: center; gap: 6px; min-width: 0; }
.sender-blocked { flex-shrink: 0; width: 6px; height: 6px; border-radius: 50%; background: var(--danger); }
</style>
