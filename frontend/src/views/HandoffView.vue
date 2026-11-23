<template>
  <div class="content-page wide">
    <PageHeader title="人工台" description="用户要求人工、或机器人答不上来时会转到这里。转人工期间，机器人对这位用户保持安静，结单后恢复。">
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
              <el-tag v-if="h.status !== 'open'" size="small" type="success">已结单</el-tag>
              <time>{{ relativeTime(h.createdAt) }}</time>
            </div>
            <p>{{ h.messageContent || lastInbound(h)?.content || '—' }}</p>
            <small>{{ h.conversationName }} · {{ h.reason || '转人工' }}</small>
          </button>
          <EmptyState v-if="!loading && !items.length" :mood="status === 'open' ? 'happy' : 'sleepy'" :title="status === 'open' ? '没有人在等人工' : '这里还没有记录'" :description="status === 'open' ? '用户说“找人工”或机器人答不上来时，会出现在这里。转人工的规则在 智能体 → 学习与转人工。' : undefined" />
        </div>
      </aside>

      <section class="inbox-detail">
        <template v-if="current">
          <header class="inbox-detail-head">
            <span class="avatar">{{ (current.displayName || current.userId).slice(0, 1) }}</span>
            <div>
              <h2>{{ current.displayName || current.userId }}</h2>
              <p>{{ current.conversationName }} · {{ current.conversationType === 'group' ? '群聊' : '私聊' }} · 转入于 {{ formatTime(current.createdAt) }}</p>
            </div>
            <el-button v-if="current.status === 'open'" type="success" plain :icon="CircleCheck" @click="resolve(current)">结单并恢复机器人</el-button>
            <el-tag v-else type="success">已结单 · {{ formatTime(current.resolvedAt) }}</el-tag>
          </header>
          <el-alert v-if="current.reason" class="inbox-reason" type="warning" :closable="false" :title="`转人工原因：${current.reason}`" />
          <div ref="chatBox" class="chat-scroll">
            <div v-for="m in current.recent" :key="m.id" class="chat-row" :class="[m.direction, { human: m.eventType === 'HUMAN_REPLY', selected: m.id === targetId }]">
              <small>{{ m.direction === 'inbound' ? (m.senderName || '用户') : m.eventType === 'HUMAN_REPLY' ? '人工回复' : '机器人' }} · {{ formatTime(m.eventAt) }}</small>
              <div class="chat-bubble">{{ m.content }}</div>
              <el-button v-if="m.direction === 'inbound' && current.status === 'open' && m.id !== targetId" link type="primary" size="small" @click="targetId = m.id">回复这条</el-button>
            </div>
            <p v-if="!current.recent.length" class="muted">没有聊天记录。</p>
          </div>
          <footer v-if="current.status === 'open'" class="composer">
            <div v-if="target" class="composer-target"><el-icon><ChatLineSquare /></el-icon><span>回复：{{ target.content }}</span></div>
            <el-input v-model="replyText" type="textarea" :rows="3" resize="none" :disabled="!target" :placeholder="target ? '输入回复，会以机器人身份发到 QQ。Ctrl/⌘ + Enter 发送' : '这位用户没有可回复的消息'" @keydown.enter.ctrl.exact.prevent="send" @keydown.enter.meta.exact.prevent="send" />
            <div class="composer-actions">
              <el-checkbox v-model="learn">同时把这次回答提交为候选知识</el-checkbox>
              <span class="muted">QQ 回复时限：群聊 5 分钟、私聊 60 分钟</span>
              <el-button type="primary" :icon="Promotion" :loading="sending" :disabled="!replyText.trim() || !target" @click="send">发送</el-button>
            </div>
          </footer>
        </template>
        <div v-else class="inbox-placeholder"><EmptyState mood="wave" title="选择左侧的一位用户" description="可以查看最近的聊天记录，并直接回复。" /></div>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref } from 'vue'
import { ChatLineSquare, CircleCheck, Headset, Promotion, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import { agentApi, formatTime, type Handoff } from '@/api/agent'
import { useShellStore } from '@/stores/shell'

const statusOptions = [{ label: '待处理', value: 'open' }, { label: '已结单', value: 'resolved' }, { label: '全部', value: 'all' }]
const status = ref('open')
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
const target = computed(() => current.value?.recent.find(m => m.id === targetId.value))

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
async function load() {
  loading.value = true
  try {
    items.value = (await agentApi.handoffs.list(status.value)).map(h => ({ ...h, recent: h.recent || [] }))
    const keep = items.value.find(h => h.id === selectedId.value) || items.value[0]
    if (keep) select(keep)
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
    ElMessage.success('已发送')
    await load()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '发送失败') } finally { sending.value = false }
}
async function resolve(h: Handoff) {
  await ElMessageBox.confirm(`结单后机器人会重新回复 ${h.displayName || h.userId} 的消息。`, '结单并恢复机器人', { confirmButtonText: '结单', cancelButtonText: '取消' })
  await agentApi.handoffs.resolve(h.id)
  ElMessage.success('已结单，机器人已恢复')
  shell.refreshCounts(true)
  await load()
}
onMounted(load)
</script>
