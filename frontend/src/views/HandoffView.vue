<template>
  <div class="content-page wide">
    <PageHeader title="人工台" description="接单后可以连续回复，结单或双方一段时间没说话后机器人才恢复。手机 QQ 里发“#工单号 接单 / 回复内容 / 结单”同样有效。">
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </PageHeader>

    <div class="inbox">
      <aside class="inbox-list">
        <div class="inbox-filter">
          <el-segmented v-model="status" :options="statusOptions" @change="load" />
        </div>
        <div v-loading="loading" class="inbox-items">
          <button v-for="h in items" :key="h.id" class="inbox-item" :class="{ active: h.id === selectedId }" @click="select(h)">
            <div class="inbox-item-top">
              <strong>{{ h.displayName || h.userId }}</strong>
              <span v-if="h.ticketNumber" class="ticket-no">#{{ h.ticketNumber }}</span>
              <span class="pill" :class="stateOf(h).tone">{{ stateOf(h).text }}</span>
              <time>{{ relativeTime(h.createdAt) }}</time>
            </div>
            <p>{{ h.messageContent || lastInbound(h)?.content || '—' }}</p>
            <small>{{ h.conversationName }} · {{ h.reason || '转人工' }}</small>
          </button>
          <EmptyState v-if="!loading && !items.length" :mood="status === 'active' ? 'happy' : 'sleepy'" :title="status === 'active' ? '没有进行中的工单' : '这里还没有记录'" :description="status === 'active' ? '用户说“找人工”或机器人答不上来时，会出现在这里。' : undefined" />
        </div>
      </aside>

      <section class="inbox-detail">
        <template v-if="current">
          <header class="inbox-detail-head">
            <span class="avatar">{{ (current.displayName || current.userId).slice(0, 1) }}</span>
            <div>
              <h2>{{ current.displayName || current.userId }}</h2>
              <p><template v-if="current.ticketNumber">工单 #{{ current.ticketNumber }} · </template><template v-if="current.status === 'processing'"><b class="claimer">{{ current.claimedByName || '人工' }} 已接单</b> · </template>{{ current.conversationName }} · {{ current.conversationType === 'group' ? '群聊' : '私聊' }} · 转入于 {{ formatTime(current.createdAt) }}</p>
            </div>
            <div class="head-actions">
              <span v-if="current.status === 'resolved'" class="pill idle">{{ stateOf(current).detail }}</span>
              <el-button v-if="current.status !== 'resolved'" :icon="CircleCheck" :loading="acting" @click="resolve(current)">结单</el-button>
            </div>
          </header>
          <div class="inbox-reason"><span>转人工原因：{{ current.reason || '用户要求人工' }}<template v-if="idleText"> · {{ idleText }}</template></span><span v-if="notice" class="notice-state" :class="`is-${notice.state}`"><i />{{ notice.text }}</span></div>
          <div ref="chatBox" class="chat-scroll">
            <div v-for="m in current.recent" :key="m.id" class="chat-row" :class="[m.direction, { human: m.eventType === 'HUMAN_REPLY', selected: m.id === targetId }]">
              <small>{{ m.direction === 'inbound' ? (m.senderName || '用户') : m.eventType === 'HUMAN_REPLY' ? '人工客服' + (m.senderName ? ' ' + m.senderName : '') : '机器人' }} · {{ formatTime(m.eventAt) }}</small>
              <div class="chat-bubble">{{ m.content }}</div>
              <span v-if="m.status === 'failed'" class="send-failed" :title="m.deliveryError"><el-icon><WarningFilled /></el-icon>发送失败{{ m.deliveryError ? `：${friendlyError(m.deliveryError)}` : '' }}</span>
              <span v-else-if="m.eventType === 'HUMAN_REPLY' && (m.status === 'pending' || m.status === 'processing')" class="send-pending"><el-icon class="is-loading"><Loading /></el-icon>发送中</span>
              <el-button v-if="m.direction === 'inbound' && mine && m.id !== targetId" link type="primary" size="small" @click="targetId = m.id">回复这条</el-button>
            </div>
            <p v-if="!current.recent.length" class="muted">没有聊天记录。</p>
          </div>
          <div v-if="current.status === 'open'" class="composer-gate"><span>接单后才能回复，用户会收到“人工客服已接入”。</span><el-button type="primary" :icon="Headset" :loading="acting" @click="claim(current)">接单</el-button></div>
          <div v-else-if="current.status === 'processing' && !mine" class="composer-gate"><span>{{ current.claimedByName || '其他客服' }}{{ current.claimedVia === 'qq' ? ' 正在手机 QQ 上处理' : ' 正在处理' }}，只有接单人可以回复和结单。</span></div>
          <footer v-else-if="current.status === 'processing'" class="composer">
            <div v-if="target" class="composer-target"><el-icon><ChatLineSquare /></el-icon><span>回复：{{ target.content }}</span></div>
            <el-input v-model="replyText" type="textarea" :rows="3" resize="none" :disabled="!target" :placeholder="target ? '输入回复，会以机器人身份发到 QQ。Ctrl/⌘ + Enter 发送' : '这位用户没有可回复的消息'" @keydown.enter.ctrl.exact.prevent="send" @keydown.enter.meta.exact.prevent="send" />
            <div class="composer-actions">
              <el-checkbox v-model="learn">同时把这次回答提交为候选知识</el-checkbox>
              <span class="muted">超过 QQ 被动回复时限后会自动改为主动消息发送</span>
              <el-button type="primary" :icon="Promotion" :loading="sending" :disabled="!replyText.trim() || !target" @click="send">发送回复</el-button>
            </div>
          </footer>
        </template>
        <div v-else class="inbox-placeholder"><EmptyState title="选择左侧的一位用户" description="可以查看最近的聊天记录，并直接回复。" /></div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, nextTick, onMounted, ref } from 'vue'
import { ChatLineSquare, CircleCheck, Headset, Loading, Promotion, Refresh, WarningFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import { agentApi, formatTime, type Handoff } from '@/api/agent'
import { useShellStore } from '@/stores/shell'
import { useAuthStore } from '@/stores/auth'

const statusOptions = [{ label: '进行中', value: 'active' }, { label: '已结单', value: 'resolved' }, { label: '全部', value: 'all' }]
const status = ref('active')
const acting = ref(false)
const auth = useAuthStore()
const closeLabels: Record<string, string> = { qq: 'QQ 指令结单', web: '网页结单', timeout: '超时自动结单' }
const items = ref<Handoff[]>([])
const loading = ref(false)
const selectedId = ref('')
const targetId = ref('')
const replyText = ref('')
const learn = ref(true)
const sending = ref(false)
const chatBox = ref<HTMLElement | null>(null)
const shell = useShellStore()

const current = computed(() => items.value.find(h => h.id === selectedId.value))
// 网页接单人才能在这里回复；QQ 接单的工单只能在手机 QQ 上处理
const mine = computed(() => current.value?.status === 'processing' && current.value.claimedVia === 'web' && current.value.claimedByName === auth.username)
function stateOf(h: Handoff) {
  if (h.status === 'open') return { tone: 'warn', text: '待接单', detail: '待接单' }
  if (h.status === 'processing') return { tone: 'busy', text: `${h.claimedByName || '人工'} 处理中`, detail: '' }
  const how = h.closeMethod ? closeLabels[h.closeMethod] : '已结单'
  return { tone: 'idle', text: '已结单', detail: `${how} · ${formatTime(h.resolvedAt).slice(5, 16)}` }
}
// 空闲到期时间：用户来信或人工回复都会往后顺延
const idleText = computed(() => {
  const h = current.value
  if (!h || h.status === 'resolved' || !h.pausedUntil) return ''
  const left = Math.round((new Date(h.pausedUntil).getTime() - Date.now()) / 60000)
  if (Number.isNaN(left)) return ''
  return left <= 0 ? '即将超时结单' : `${left} 分钟无消息将自动结单`
})
const target = computed(() => current.value?.recent.find(m => m.id === targetId.value))
// 私信给人工客服 QQ 的通知进度
const notice = computed(() => {
  const list = current.value?.notifications || []
  if (!list.length) return current.value?.status !== 'resolved' ? { state: 'none', text: '没有绑定人工客服 QQ，只能在这里回复' } : null
  const sent = list.filter(n => n.status === 'sent').length
  const failed = list.filter(n => n.status === 'failed' || n.status === 'expired')
  if (sent) return { state: 'sent', text: `已私信 ${sent} 位人工客服，可在手机 QQ 回复 #${current.value?.ticketNumber ?? ''}` }
  if (failed.length === list.length) return { state: 'failed', text: 'QQ 私信发送失败' + (failed[0].error ? '：' + friendlyError(failed[0].error) : '') }
  return { state: 'sending', text: '正在私信人工客服' }
})

// 后端错误形如 “qq_active_send_failed: 提示文字：QQ 原始错误”，只展示错误码之后的中文部分
function friendlyError(value: string) { return value.replace(/^[a-z_]+:\s*/, '') }
function lastInbound(h: Handoff) { return [...h.recent].reverse().find(m => m.direction === 'inbound') }
function relativeTime(value?: string) {
  if (!value) return ''
  const diff = (Date.now() - new Date(value).getTime()) / 60000
  if (Number.isNaN(diff)) return value
  if (diff < 1) return '刚刚'
  if (diff < 60) return `${Math.floor(diff)} 分钟前`
  if (diff < 1440) return `${Math.floor(diff / 60)} 小时前`
  return formatTime(value).slice(5, 10)
}
function select(h: Handoff) {
  selectedId.value = h.id
  targetId.value = h.messageId && h.recent.some(m => m.id === h.messageId) ? h.messageId : lastInbound(h)?.id || ''
  replyText.value = ''
  nextTick(() => { if (chatBox.value) chatBox.value.scrollTop = chatBox.value.scrollHeight })
}
async function load(background: unknown = false) {
  if (background !== true) loading.value = true
  try {
    items.value = (await agentApi.handoffs.list(status.value)).map(h => ({ ...h, recent: h.recent || [] }))
    const keep = items.value.find(h => h.id === selectedId.value) || items.value[0]
    if (keep && (background !== true || keep.id !== selectedId.value)) select(keep)
    else if (keep) { if (!keep.recent.some(message => message.id === targetId.value) && !replyText.value.trim()) targetId.value = lastInbound(keep)?.id || '' }
    else selectedId.value = ''
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '加载失败') } finally { loading.value = false }
}
async function send() {
  const text = replyText.value.trim()
  if (!text || !target.value || sending.value) return
  sending.value = true
  try {
    await agentApi.messages.reply(target.value.id, text, learn.value)
    replyText.value = ''
    ElMessage.success('已提交发送')
    await load()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '发送失败') } finally { sending.value = false }
}
async function claim(h: Handoff) {
  acting.value = true
  try {
    await agentApi.handoffs.claim(h.id)
    ElMessage.success(`已接单 #${h.ticketNumber ?? ''}，用户会收到“人工客服已接入”`)
    await load(true)
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '接单失败'); await load(true) } finally { acting.value = false }
}
async function resolve(h: Handoff) {
  try { await ElMessageBox.confirm(`用户会收到“本次人工服务已结束”，之后由机器人继续回复 ${h.displayName || h.userId}。`, '结单', { confirmButtonText: '结单', cancelButtonText: '取消' }) } catch { return }
  acting.value = true
  try {
    await agentApi.handoffs.resolve(h.id)
    ElMessage.success('已结单，机器人已恢复')
    shell.refreshCounts(true)
    await load()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '结单失败'); await load(true) } finally { acting.value = false }
}
onMounted(load)
useAdminEvents(['handoffs','messages','outbox_tasks'], () => load(true))
</script>

<style scoped>
.send-failed, .send-pending { display: inline-flex; align-items: center; gap: 4px; margin-top: 4px; font-size: 12px; max-width: 100%; }
.send-failed { color: var(--danger); }
.ticket-no { color: var(--muted); font: 12px var(--font-mono); }
.inbox-reason { display: flex; flex-wrap: wrap; justify-content: space-between; gap: 4px 16px; }
.notice-state { display: inline-flex; align-items: center; gap: 6px; color: var(--text-2); }
.notice-state i { width: 6px; height: 6px; border-radius: 50%; background: var(--faint); }
.notice-state.is-sent i { background: var(--success); }
.notice-state.is-failed { color: var(--danger); }
.notice-state.is-failed i { background: var(--danger); }
.notice-state.is-sending i { background: var(--warning); animation: notice-breathe 1.4s ease-in-out infinite; }
@keyframes notice-breathe { 50% { opacity: .3; } }
@media (prefers-reduced-motion: reduce) { .notice-state.is-sending i { animation: none; } }
.send-pending { color: var(--muted); }
.claimer { color: var(--primary-text); font-weight: 500; }
.composer-actions { flex-wrap: wrap; }
.composer-actions .muted { flex: 1 1 160px; min-width: 0; }
.head-actions { flex-shrink: 0; display: flex; align-items: center; gap: 8px; margin-left: auto; }
.composer-gate { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 14px 16px; border-top: 1px solid var(--line); color: var(--text-2); font-size: 13px; }
</style>
