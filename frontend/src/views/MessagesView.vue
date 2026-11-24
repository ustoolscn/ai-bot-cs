<template>
  <div class="content-page page-fill">
    <PageHeader title="消息记录" description="每条触发了机器人的消息。点任意一行，查看它从收到到发出回复的完整过程。">
      <el-button :icon="Download" @click="exportRows">导出记录</el-button>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </PageHeader>
    <div class="toolbar">
      <el-input v-model="search" placeholder="搜索问题、回复、发送者或消息 ID" :prefix-icon="Search" clearable />
      <el-segmented v-model="status" :options="[{ label: '全部', value: 'all' }, { label: '成功', value: 'success' }, { label: '转人工', value: 'handoff' }, { label: '无需回复', value: 'skipped' }, { label: '已合并', value: 'merged' }, { label: '失败', value: 'failed' }]" />
      <span class="spacer" />
      <span class="muted" style="font-size: 13px">{{ filtered.length }} 条</span>
    </div>
    <section class="table-panel">
      <el-table v-loading="loading" class="clickable-rows" :data="filtered" height="100%" @row-click="open">
        <template #empty>
          <EmptyState v-if="loadError" mood="thinking" title="消息记录加载失败" :description="loadError"><el-button @click="load">重新加载</el-button></EmptyState>
          <EmptyState v-else title="还没有消息" description="机器人在群里被 @ 或收到私聊后，消息会出现在这里。" />
        </template>
        <el-table-column label="时间" width="168"><template #default="{ row }"><span class="num" style="white-space: nowrap">{{ row.time }}</span></template></el-table-column>
        <el-table-column label="消息" min-width="260"><template #default="{ row }"><div class="message-preview"><strong>{{ row.question }}</strong><span>{{ row.attachments?.length ? `包含 ${row.attachments.length} 个附件 · ` : '' }}{{ row.answer || '未生成回复' }}</span></div></template></el-table-column>
        <el-table-column label="机器人 / 会话" min-width="150"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.botName }}</strong><span>{{ row.conversationName }}</span></div></template></el-table-column>
        <el-table-column label="发送者" width="110"><template #default="{ row }"><div class="sender-cell"><span class="truncate" :title="row.sender">{{ row.sender }}</span><i v-if="row.senderBlockedIds?.length" class="sender-blocked" title="已拉黑" /></div></template></el-table-column>
        <el-table-column label="模型" width="130"><template #default="{ row }"><div class="stacked-cell"><strong class="truncate">{{ row.model || '—' }}</strong><span>{{ row.tokens }} tokens</span></div></template></el-table-column>
        <el-table-column label="知识命中" width="90" align="right"><template #default="{ row }"><span class="num">{{ row.knowledgeHits }}</span></template></el-table-column>
        <el-table-column label="耗时" width="90" align="right"><template #default="{ row }"><span class="num">{{ formatDuration(row.latency) }}</span></template></el-table-column>
        <el-table-column label="结论" width="132"><template #default="{ row }"><StatusBadge v-bind="messageVerdict(row)" /></template></el-table-column>
      </el-table>
    </section>

    <el-drawer v-model="drawer" title="消息处理详情" size="760px" class="message-drawer">
      <div v-if="selected" v-loading="detailLoading" class="md">
        <header class="md-head">
          <div class="md-who">
            <StatusBadge v-bind="messageVerdict(selected)" />
            <strong>{{ selected.sender }}</strong>
            <span class="md-where">{{ selected.conversationName }} · {{ selected.botName }}</span>
            <Transition name="md-swap" mode="out-in">
              <span v-if="senderBlocked" key="blocked" class="md-block-state"><span class="pill bad">已拉黑</span><el-button link type="primary" :loading="unblocking" @click="unblockSender">解除</el-button></span>
              <el-button v-else-if="canBlock" key="block" class="md-block-btn" link type="danger" @click="blockDialog = true">拉黑发送者</el-button>
            </Transition>
          </div>
          <p v-if="selected.status === 'merged'" class="md-merged">同一个人连续发了几条，这条已合并到后面的消息里，由最后一条一起回复。</p>
          <dl class="md-facts">
            <div><dt>时间</dt><dd class="num">{{ selected.time }}</dd></div>
            <div><dt>总用时</dt><dd class="num">{{ selected.latency ? formatDuration(selected.latency) : '—' }}</dd></div>
            <div><dt>模型</dt><dd class="truncate" :title="selected.model">{{ selected.model || '未调用' }}</dd></div>
            <div><dt>记录 ID</dt><dd><el-tooltip :content="selected.traceId" placement="bottom"><button class="md-id" type="button" @click="copy(selected.traceId)">{{ selected.traceId.slice(0, 8) }}<el-icon><CopyDocument /></el-icon></button></el-tooltip></dd></div>
          </dl>
        </header>

        <section class="md-section">
          <h3>对话</h3>
          <div class="md-chat">
            <div class="md-bubble is-user">
              <small>{{ selected.sender }}</small>
              <p>{{ selected.question }}</p>
              <div v-if="selected.attachments?.length" class="message-image-grid"><div v-for="(v,i) in selected.attachments.filter(p=>p.type==='voice')" :key="i"><audio v-if="v.waveUrl" controls preload="none" :src="v.waveUrl"/><p>{{v.transcript}}</p></div><el-image v-for="(image,index) in selected.attachments.filter(p=>p.type==='image')" :key="image.previewUrl || index" :src="image.previewUrl" :preview-src-list="previewUrls(selected.attachments)" fit="cover" loading="lazy" /></div>
            </div>
            <div v-if="selected.status !== 'merged' && (!selected.handoffStatus || botAnswer || selected.answerAttachments?.length)" class="md-bubble is-bot" :class="{ 'is-empty': !selected.answer && !selected.answerAttachments?.length }">
              <small>{{ selected.botName }}</small>
              <p>{{ botAnswer || (selected.answerAttachments?.length ? '图片回复' : selected.status === 'skipped' ? '判断这条消息无需回复。' : '处理尚未完成或失败，未生成回复。') }}</p>
              <div v-if="selected.answerAttachments?.length" class="message-image-grid"><el-image v-for="(image,index) in selected.answerAttachments" :key="image.previewUrl || index" :src="image.previewUrl" :preview-src-list="previewUrls(selected.answerAttachments)" fit="cover" loading="lazy" /></div>
            </div>
            <div v-if="selected.handoffStatus" class="md-bubble is-bot is-human" :class="{ 'is-empty': !selected.humanReplyContent }">
              <small><span class="md-human-tag">人工客服</span>{{ selected.humanRepliedBy || '' }}<template v-if="selected.ticketNumber"> · 工单 #{{ selected.ticketNumber }}</template><template v-if="selected.humanRepliedAt"> · {{ selected.humanRepliedAt }}</template></small>
              <p>{{ selected.humanReplyContent || (selected.handoffStatus === 'human_closed' ? '工单已结单，没有人工回复。' : selected.handoffStatus === 'human_processing' ? '人工客服已接单，还没有回复。' : '已转人工，等待人工客服接单。') }}</p>
              <span v-if="selected.humanReplyContent && selected.handoffStatus === 'human_processing' && selected.humanDeliveryStatus !== 'sent'" class="md-human-state" :class="{ 'is-failed': selected.humanDeliveryStatus === 'failed' || selected.humanDeliveryStatus === 'expired' }"><i v-if="selected.humanDeliveryStatus === 'pending' || selected.humanDeliveryStatus === 'processing'" class="md-sending" />{{ selected.humanDeliveryStatus === 'failed' || selected.humanDeliveryStatus === 'expired' ? '发送失败' + (selected.humanDeliveryError ? '：' + selected.humanDeliveryError : '，可在人工台重新回复') : '正在发到群里' }}</span>
            </div>
          </div>
        </section>

        <section v-if="selected.status !== 'merged'" class="md-section">
          <h3>处理过程 <small v-if="selected.timeline?.totalMs != null">共 {{ formatDuration(selected.timeline.totalMs) }} · 悬停查看每步说明</small></h3>
          <StageBars :stages="selected.timeline?.stages || flowStages" waterfall />
        </section>

        <section v-if="selected.status !== 'merged'" class="md-section">
          <h3>Token 与成本</h3>
          <div class="md-tokens">
            <div><span>输入</span><strong class="num">{{ selected.inputTokens.toLocaleString() }}</strong></div>
            <div><span>输出</span><strong class="num">{{ selected.outputTokens.toLocaleString() }}</strong></div>
            <div><span>缓存读取</span><strong class="num">{{ (selected.cachedInputTokens || 0).toLocaleString() }}</strong></div>
            <div><span>缓存写入</span><strong class="num">{{ (selected.cacheWriteTokens || 0).toLocaleString() }}</strong></div>
            <div><span>缓存命中率</span><strong class="num">{{ cacheRate }}</strong><i class="md-meter"><b :style="{ transform: `scaleX(${cacheRatio})` }" /></i></div>
            <div><span>估算成本</span><strong class="num" v-if="selected.estimatedCost != null">¥{{ formatCost(selected.estimatedCost) }}</strong><el-button v-else link type="primary" @click="$router.push('/models')">设置价格</el-button></div>
          </div>
        </section>

        <MessageAgentPanel :message-id="selected.id" />

        <el-collapse class="md-collapse">
          <el-collapse-item v-if="selected.retrievedChunks?.length" name="chunks">
            <template #title><strong>知识召回</strong><small>{{ selected.retrievedChunks.length }} 个片段</small></template>
            <div class="retrieved-chunk-list"><article v-for="(chunk,index) in selected.retrievedChunks" :key="chunk.id || index"><strong>片段 {{ index + 1 }}<span v-if="chunk.score !== undefined"> · 相似度 {{ (chunk.score * 100).toFixed(1) }}%</span></strong><p>{{ chunk.content }}</p></article></div>
          </el-collapse-item>
          <el-collapse-item name="context">
            <template #title><strong>模型完整上下文</strong><small>{{ selected.contextMessages?.length || 0 }} 条</small></template>
            <div v-if="selected.contextMessages?.length" class="context-message-list">
              <article v-for="(message, index) in contextView" :key="`${index}-${message.role}`" class="context-message-item">
                <span :class="`role-${message.role}`" :title="message.speaker">{{ message.speaker }}</span>
                <div><pre>{{ message.text }}</pre><div v-if="message.parts?.length" class="message-image-grid compact"><el-image v-for="(image,imageIndex) in message.parts" :key="image.previewUrl || imageIndex" :src="image.previewUrl" :preview-src-list="previewUrls(message.parts)" fit="cover" loading="lazy" /></div></div>
              </article>
            </div>
            <p v-else class="muted md-note">这条消息没有保存上下文。比较早的消息没有记录当时发给模型的内容。</p>
          </el-collapse-item>
          <el-collapse-item name="request">
            <template #title><strong>请求信息</strong><small>{{ selected.eventType }}</small></template>
            <dl class="detail-list"><div><dt>事件类型</dt><dd>{{ selected.eventType }}</dd></div><div><dt>知识命中</dt><dd>{{ selected.knowledgeHits }} 条</dd></div><div><dt>QQ 消息 ID</dt><dd class="detail-id"><el-tooltip :content="selected.platformMessageId || '—'"><span>{{ selected.platformMessageId || '—' }}</span></el-tooltip></dd></div><div><dt>投递状态</dt><dd><StatusBadge :status="selected.deliveryStatus" /></dd></div><div v-if="selected.deliveryError" style="grid-column: 1 / -1"><dt>投递失败原因</dt><dd class="delivery-error">{{ selected.deliveryError }}</dd></div></dl>
          </el-collapse-item>
        </el-collapse>
      </div>
    </el-drawer>
    <BlockUserDialog v-if="selected" v-model="blockDialog" :bot-id="selected.botId || ''" :bot-name="selected.botName" :conversation-id="selected.conversationId || ''" :conversation-name="selected.conversationName" :is-group="selected.conversationType !== 'private'" :user-id="selected.senderId || ''" :display-name="selected.sender" :quote="selected.question" @blocked="refreshSelected" />
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, ref } from 'vue'
import { CopyDocument, Download, Refresh, Search } from '@element-plus/icons-vue'
import { connection } from '@/utils/connection'
import StageBars from '@/components/fx/StageBars.vue'
import EmptyState from '@/components/EmptyState.vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { agentApi } from '@/api/agent'
import BlockUserDialog from '@/components/conversation/BlockUserDialog.vue'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import MessageAgentPanel from '@/components/agent/MessageAgentPanel.vue'
import type { MessageAttachment, MessageRecord, Status } from '@/types'
import { formatDuration, messageVerdict, statusText } from '@/utils/format'

const messages = ref<MessageRecord[]>([])
const search = ref('')
const status = ref('all')
const drawer = ref(false)
const detailLoading = ref(false)
const selected = ref<MessageRecord>()
const loading = ref(false)
const loadError = ref('')
const filtered = computed(() => messages.value.filter(m => (!search.value || `${m.question}${m.answer}${m.sender}${m.traceId}`.includes(search.value)) && (status.value === 'all' || (status.value === 'handoff' ? Boolean(m.handoffStatus) : !m.handoffStatus && m.status === status.value))))
const flowStages = computed(() => {
  const m = selected.value
  if (!m) return []
  const labels = ['接收', '上下文', '检索', '模型', '投递']
  const ms = [undefined, m.contextLatency, m.retrievalLatency, m.modelLatency, m.deliveryLatency]
  let elapsed = 0
  return traceSteps.value.map((step, index) => {
    elapsed += ms[index] || 0
    return { label: labels[index], ms: ms[index] ?? 0, elapsedMs: elapsed, status: step.status, detail: step.description }
  })
})
const traceSteps = computed(() => {
  if (!selected.value) return []
  const message = selected.value
  const pending = message.status === 'pending' || message.status === 'processing'
  const failed = message.status === 'failed'
  // 被合并的消息不单独回复，由同一批里最后一条一起处理
  const merged = message.status === 'merged'
  const stageStatus = (completed: boolean): Status => merged ? 'skipped' : failed ? 'failed' : completed ? 'success' : pending ? 'pending' : 'warning'
  return [
    { title: 'QQ 事件接收', description: message.eventType, duration: '—', status: 'success' as Status },
    { title: '上下文构建', description: `实际发送给模型 ${message.contextMessages?.length || 0} 条消息`, duration: duration(message.contextLatency), status: stageStatus(Boolean(message.contextMessages?.length) || message.contextLatency > 0) },
    { title: '知识检索', description: `召回 ${message.knowledgeHits} 条知识片段`, duration: duration(message.retrievalLatency), status: stageStatus(message.retrievalLatency > 0 || message.knowledgeHits > 0) },
    { title: '模型生成', description: message.model || '尚未调用', duration: duration(message.modelLatency), status: stageStatus(Boolean(message.answer) || Boolean(message.answerAttachments?.length) || message.modelLatency > 0) },
    { title: 'QQ 消息投递', description: merged ? '和后面的消息一起回复' : message.deliveryStatus === 'success' ? '发送成功' : message.deliveryStatus === 'failed' ? '发送失败' : '等待投递', duration: duration(message.deliveryLatency), status: message.deliveryStatus },
  ]
})

onMounted(load)
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
async function open(row: MessageRecord) {
  selected.value = row
  drawer.value = true
  detailLoading.value = true
  try { selected.value = await api.messages.detail(row.id) }
  catch (error) { if (!connection.lost) ElMessage.error(error instanceof Error ? error.message : '读取消息详情失败') }
  finally { detailLoading.value = false }
}
// 在消息里直接拉黑发送者：先看他说了什么，再决定要不要拉黑。
const blockDialog = ref(false)
const unblocking = ref(false)
const senderBlocked = computed(() => Boolean(selected.value?.senderBlockedIds?.length))
const canBlock = computed(() => {
  const m = selected.value
  return Boolean(m?.senderId && m.botId && m.conversationId)
})
async function refreshSelected() {
  const id = selected.value?.id
  if (!id) return
  try {
    const detail = await api.messages.detail(id)
    if (selected.value?.id === id) selected.value = detail
    await load(true)
  } catch { /* 列表会随管理事件刷新 */ }
}
async function unblockSender() {
  const m = selected.value
  const ids = m?.senderBlockedIds || []
  if (!m || !ids.length) return
  try {
    await ElMessageBox.confirm(`解除对“${m.sender}”的拉黑？${ids.length > 1 ? '他同时在本群和机器人范围的黑名单里，会一起解除。' : ''}已被踢出的人不会自动拉回群。`, '解除拉黑', { confirmButtonText: '解除拉黑', cancelButtonText: '取消', type: 'warning' })
  } catch { return }
  unblocking.value = true
  try { await Promise.all(ids.map(id => agentApi.blacklist.remove(id))); ElMessage.success('已解除'); await refreshSelected() }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '解除失败') }
  finally { unblocking.value = false }
}
function duration(value: number) { return value > 0 ? formatDuration(value) : '—' }
function previewUrls(parts?: MessageAttachment[]) { return (parts || []).flatMap(part => part.previewUrl ? [part.previewUrl] : []) }
function roleText(role: string) { return ({ system: '系统提示', developer: '开发者', user: '用户', assistant: '助手', tool: '工具结果' } as Record<string, string>)[role] || role }
// 群聊历史把发言人拼在内容前面（“昵称: 内容”），这里拆出来当标签；当前提问那条用消息的发送者。
const speakerPrefix = /^(【官方客服】)?([^\n:：]{1,40})[:：] /
const contextView = computed(() => {
  const m = selected.value
  const list = m?.contextMessages || []
  const lastUser = list.map(x => x.role).lastIndexOf('user')
  return list.map((message, index) => {
    if (message.role === 'assistant') return { ...message, speaker: message.name || m?.botName || '助手', text: message.content }
    if (message.role !== 'user') return { ...message, speaker: roleText(message.role), text: message.content }
    if (message.name) return { ...message, speaker: message.name, text: message.content.replace(speakerPrefix, '') }
    const hit = message.content.match(speakerPrefix)
    if (hit) return { ...message, speaker: (hit[1] ? '客服 ' : '') + hit[2], text: message.content.slice(hit[0].length) }
    return { ...message, speaker: index === lastUser ? m?.sender || '用户' : '用户', text: message.content }
  })
})
// 回复字段把机器人和人工的回复拼在一起，人工那段单独显示。
const botAnswer = computed(() => {
  const m = selected.value
  if (!m) return ''
  return m.humanReplyContent ? m.answer.replace(m.humanReplyContent, '').trim() : m.answer
})
const cacheRatio = computed(() => {
  const m = selected.value
  if (m?.cacheHitRate != null) return Math.min(1, Math.max(0, m.cacheHitRate))
  return m && m.inputTokens > 0 ? Math.min(1, (m.cachedInputTokens || 0) / m.inputTokens) : 0
})
const cacheRate = computed(() => selected.value?.cacheHitRate != null || selected.value?.inputTokens ? (cacheRatio.value * 100).toFixed(1) + '%' : '—')
function formatCost(value: number) { return value >= 0.01 ? value.toFixed(4) : value.toPrecision(2) }
async function copy(text: string) { try { await navigator.clipboard.writeText(text); ElMessage.success('已复制记录 ID') } catch { ElMessage.error('复制失败') } }
function exportRows() { const csv = ['时间,机器人,会话,问题,结论,Token', ...filtered.value.map(m => `"${m.time}","${m.botName}","${m.conversationName}","${m.question.replaceAll('"','""')}","${messageVerdict(m).text || statusText(m.status)}","${m.tokens}"`)].join('\n'); const a=document.createElement('a'); a.href=URL.createObjectURL(new Blob(['\ufeff'+csv],{type:'text/csv'})); a.download='messages.csv'; a.click(); URL.revokeObjectURL(a.href); ElMessage.success('消息记录已导出') }
useAdminEvents(['messages','message_retractions','agent_runs','model_calls','tool_calls','outbox_tasks','inbox_tasks','message_stages','group_reviews','blocked_users'], async () => {
  await load(true)
  const id = selected.value?.id
  if (drawer.value && id) {
    const detail = await api.messages.detail(id)
    if (drawer.value && selected.value?.id === id) selected.value = detail
  }
})
</script>

<style scoped>
.md-merged { margin: 0; color: var(--muted); font-size: 13px; }
.md { display: flex; flex-direction: column; gap: 24px; }
.md-head { display: flex; flex-direction: column; gap: 12px; }
.md-who { display: flex; align-items: baseline; gap: 10px; min-width: 0; }
.md-who strong { font-size: 16px; font-weight: 600; }
.md-who .md-where { flex: 1; min-width: 0; overflow: hidden; color: var(--muted); font-size: 13px; text-overflow: ellipsis; white-space: nowrap; }
.md-block-state { display: inline-flex; flex-shrink: 0; align-items: center; gap: 8px; }
.md-block-btn { flex-shrink: 0; height: auto; padding: 0; font-size: 13px; }
.md-swap-enter-active, .md-swap-leave-active { transition: opacity .16s var(--ease), transform .16s var(--ease); }
.md-swap-enter-from, .md-swap-leave-to { opacity: 0; transform: translateY(-2px); }
.sender-cell { display: flex; align-items: center; gap: 6px; min-width: 0; }
.sender-blocked { flex-shrink: 0; width: 6px; height: 6px; border-radius: 50%; background: var(--danger); }
.md-facts { display: grid; grid-template-columns: 1.4fr .8fr 1.2fr .8fr; margin: 0; border: 1px solid var(--line); border-radius: var(--radius-sm); }
.md-facts > div { display: flex; flex-direction: column; gap: 2px; min-width: 0; padding: 8px 12px; border-left: 1px solid var(--line); }
.md-facts > div:first-child { border-left: 0; }
.md-facts dt { color: var(--muted); font-size: 12px; }
.md-facts dd { margin: 0; font-size: 13px; }
.md-id { display: inline-flex; align-items: center; gap: 4px; padding: 0; border: 0; background: none; color: var(--text); font: 12px var(--font-mono); cursor: pointer; }
.md-id:hover { color: var(--primary-text); }

.md-section h3 { margin: 0 0 10px; font-size: 14px; font-weight: 600; }
.md-section h3 small { margin-left: 6px; color: var(--muted); font-size: 12px; font-weight: 400; }

.md-chat { display: flex; flex-direction: column; gap: 8px; }
.md-bubble { max-width: 88%; padding: 10px 14px; border-radius: var(--radius); background: var(--surface-2); border: 1px solid var(--line); }
.md-bubble small { display: block; margin-bottom: 2px; color: var(--muted); font-size: 12px; }
.md-bubble p { margin: 0; font-size: 14px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }
.md-bubble.is-bot { align-self: flex-end; background: var(--primary-light); border-color: var(--primary-soft); }
.md-bubble.is-empty { background: var(--surface-2); border-style: dashed; border-color: var(--line-strong); }
.md-bubble.is-empty p { color: var(--muted); }
.md-bubble.is-human { background: var(--warning-light); border-color: color-mix(in srgb, var(--warning) 28%, transparent); }
.md-bubble.is-human.is-empty { border-style: dashed; }
.md-human-tag { display: inline-block; margin-right: 6px; padding: 0 6px; border-radius: 6px; background: var(--surface); color: var(--warning); font-size: 12px; font-weight: 600; line-height: 18px; }
.md-human-state { display: inline-flex; align-items: center; gap: 6px; margin-top: 6px; color: var(--muted); font-size: 12px; }
.md-human-state.is-failed { color: var(--danger); }
.md-sending { width: 6px; height: 6px; border-radius: 50%; background: var(--warning); animation: md-breathe 1.4s ease-in-out infinite; }
@keyframes md-breathe { 50% { opacity: .3; } }

.md-tokens { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); border: 1px solid var(--line); border-radius: var(--radius-sm); }
.md-tokens > div { display: flex; flex-direction: column; gap: 2px; padding: 10px 14px; border-left: 1px solid var(--line); border-top: 1px solid var(--line); min-width: 0; }
.md-tokens > div:nth-child(3n+1) { border-left: 0; }
.md-tokens > div:nth-child(-n+3) { border-top: 0; }
.md-tokens span { color: var(--muted); font-size: 12px; }
.md-tokens strong { font-size: 16px; font-weight: 600; }
.md-tokens .el-button { align-self: flex-start; height: 24px; padding: 0; font-size: 13px; }
.md-meter { display: block; height: 4px; margin-top: 4px; border-radius: 2px; background: var(--surface-3); overflow: hidden; }
.md-meter b { display: block; height: 100%; background: var(--success); transform-origin: left; transition: transform .6s var(--ease); }

.md-collapse { border-top: 1px solid var(--line); border-bottom: 0; }
.md-collapse :deep(.el-collapse-item__header) { gap: 8px; height: 44px; font-size: 14px; }
.md-collapse :deep(.el-collapse-item__header small) { margin-left: 8px; color: var(--muted); font-size: 12px; font-weight: 400; }
.md-collapse :deep(.el-collapse-item__header strong) { font-weight: 600; }
.md-note { margin: 0; font-size: 13px; }
.context-message-item { grid-template-columns: 88px minmax(0, 1fr); }
.context-message-item > span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 640px) { .md-facts, .md-tokens { grid-template-columns: 1fr 1fr; } .md-facts > div:nth-child(3) { border-left: 0; } }
@media (prefers-reduced-motion: reduce) { .md-meter b, .md-swap-enter-active, .md-swap-leave-active { transition: none; } .md-sending { animation: none; } }
</style>
