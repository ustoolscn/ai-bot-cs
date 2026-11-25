<template>
  <section class="chat-panel">
    <div ref="scroller" class="chat-scroll chat-feed" @scroll.passive="onScroll">
      <div class="feed-top">
        <el-button v-if="nextBefore" text size="small" :loading="loadingOlder" @click="loadOlder">查看更早的消息</el-button>
        <span v-else-if="items.length" class="muted">没有更早的消息了</span>
      </div>
      <template v-if="loading && !items.length">
        <div v-for="n in 4" :key="n" class="skeleton feed-skeleton" :class="{ right: n % 2 === 0 }" />
      </template>
      <TransitionGroup name="msg" tag="div" class="feed-list">
        <template v-for="(m, i) in items" :key="m.id">
          <div v-if="showTime(i)" :key="m.id + '-t'" class="feed-time">{{ timeLabel(m.at) }}</div>
          <div v-if="isEvent(m)" :key="m.id + '-s'" class="feed-system">{{ $at(m.content, m.mentionNames) || '系统消息' }}</div>
          <div v-else :key="m.id + '-m'" class="feed-row" :class="[m.direction, { target: m.id === target?.id }]">
            <AvatarImage class="feed-avatar" :src="m.senderAvatar" :name="whoOf(m)" :tone="m.direction === 'outbound' ? 'primary' : ''" />
            <div class="feed-body">
              <small class="feed-who"><span>{{ whoOf(m) }}</span><em v-for="t in tagsOf(m)" :key="t" class="feed-tag" :class="{ human: t.startsWith('人工') }">{{ t }}</em></small>
              <div v-if="m.replyToId && byId.get(m.replyToId)" class="feed-quote">{{ whoOf(byId.get(m.replyToId)!) }}：{{ textOf(byId.get(m.replyToId)!) || '[图片]' }}</div>
              <div class="feed-bubble">
                <span v-if="m.content">{{ textOf(m) }}</span>
                <div v-if="images(m).length" class="feed-images">
                  <el-image v-for="(img, k) in images(m)" :key="k" :src="img.previewUrl" :preview-src-list="images(m).map(x => x.previewUrl!)" :initial-index="k" fit="cover" preview-teleported />
                </div>
                <span v-for="(f, k) in files(m)" :key="'f' + k" class="feed-file"><el-icon><Document /></el-icon>{{ f.filename || f.transcript || '附件' }}</span>
              </div>
              <span v-if="m.status === 'failed'" class="feed-state is-failed" :title="m.error"><el-icon><WarningFilled /></el-icon>发送失败{{ m.error ? '：' + friendly(m.error) : '' }}</span>
              <span v-else-if="m.direction === 'outbound' && (m.status === 'pending' || m.status === 'processing')" class="feed-state"><el-icon class="is-loading"><Loading /></el-icon>发送中</span>
            </div>
            <div class="feed-actions">
              <el-tooltip v-if="m.direction === 'inbound' && (m.canReply || m.quotable)" content="回复这条" placement="top" :show-after="300">
                <button type="button" aria-label="回复这条" @click="pick(m)"><el-icon><ChatLineSquare /></el-icon></button>
              </el-tooltip>
              <el-tooltip v-if="m.messageId" content="查看处理详情" placement="top" :show-after="300">
                <button type="button" aria-label="查看处理详情" @click="$router.push({ path: '/messages', query: { id: m.messageId } })"><el-icon><Tickets /></el-icon></button>
              </el-tooltip>
            </div>
          </div>
        </template>
      </TransitionGroup>
      <EmptyState v-if="!loading && !items.length" compact title="还没有聊天记录" :description="conversation.type === 'group' ? '群里有人发言、机器人回复或你在下面发消息后会出现在这里。' : '对方发来消息或你在下面发消息后会出现在这里。'" />
    </div>

    <Transition name="fade">
      <button v-if="unseen" type="button" class="new-chip" @click="toBottom(true)"><el-icon><ArrowDown /></el-icon>{{ unseen }} 条新消息</button>
    </Transition>

    <div v-if="!conversation.enabled" class="composer-gate"><span>这个会话已停用，机器人不会接收和发送消息。在设置里重新启用后才能发消息。</span><el-button @click="emit('open-settings')">打开设置</el-button></div>
    <footer v-else class="composer chat-composer">
      <Transition name="fade">
        <div v-if="target" class="composer-target">
          <el-icon><ChatLineSquare /></el-icon>
          <span>回复 {{ whoOf(target) }}：{{ textOf(target) || '[图片]' }}</span>
          <button type="button" class="target-clear" aria-label="取消回复" @click="target = undefined"><el-icon><Close /></el-icon></button>
        </div>
      </Transition>
      <el-input v-model="text" type="textarea" :autosize="{ minRows: 2, maxRows: 6 }" resize="none" :placeholder="`以 ${conversation.botName} 的身份发到这个${conversation.type === 'group' ? '群' : '私聊'}，Enter 发送，Shift + Enter 换行`" @keydown.enter="onEnter" />
      <div class="composer-actions">
        <el-tooltip content="发送图片" placement="top" :show-after="300">
          <button type="button" class="tool-btn" aria-label="发送图片" :disabled="sending" @click="filePicker?.click()"><el-icon><Picture /></el-icon></button>
        </el-tooltip>
        <input ref="filePicker" type="file" accept="image/*" hidden @change="sendImage">
        <el-tooltip content="Markdown、卡片、按钮、视频和文件" placement="top" :show-after="300">
          <button type="button" class="tool-btn" aria-label="更多发送方式" @click="emit('open-settings', 'send')"><el-icon><MoreFilled /></el-icon></button>
        </el-tooltip>
        <span class="muted">{{ target ? (target.canReply ? '作为被动回复发送' : '引用这条后主动发送') : '主动发送，会占用 QQ 主动消息额度' }}</span>
        <el-button type="primary" :icon="Promotion" :loading="sending" :disabled="!text.trim()" @click="send">发送</el-button>
      </div>
    </footer>
  </section>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { ArrowDown, ChatLineSquare, Close, Document, Loading, MoreFilled, Picture, Promotion, Tickets, WarningFilled } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import { useAdminEvents } from '@/composables/useAdminEvents'
import EmptyState from '@/components/EmptyState.vue'
import AvatarImage from '@/components/AvatarImage.vue'
import { formatMentions } from '@/utils/mentions'
import type { ChatItem, Conversation, MessageAttachment } from '@/types'

const props = defineProps<{ conversation: Conversation }>()
const emit = defineEmits<{ 'open-settings': [tab?: string] }>()

const items = ref<ChatItem[]>([])
const nextBefore = ref<string | null>(null)
const loading = ref(false)
const loadingOlder = ref(false)
const sending = ref(false)
const text = ref('')
const target = ref<ChatItem>()
const unseen = ref(0)
const scroller = ref<HTMLElement>()
const filePicker = ref<HTMLInputElement>()
const byId = computed(() => new Map(items.value.map(m => [m.id, m])))

// 机器人发出的所有消息（AI 回复、定时任务、通知、人工在后台发的）都用机器人的身份和气泡；
// 只有入群、退群这类事件才居中显示成系统提示
const isEvent = (m: ChatItem) => m.role === 'system'
function whoOf(m: ChatItem) {
  if (m.direction === 'inbound') return m.senderName || (props.conversation.type === 'group' ? '群成员' : props.conversation.name)
  return props.conversation.botName || '机器人'
}
const kindName: Record<string, string> = { scheduled: '定时任务', report: '日报', notice: '通知', welcome: '入群欢迎' }
function tagsOf(m: ChatItem) {
  if (m.direction !== 'outbound') return []
  if (m.role === 'human') return [m.senderName ? `人工 · ${m.senderName}` : '人工']
  const k = m.kind && kindName[m.kind]
  return k ? [k] : []
}
// 后端返回的是 QQ 里实际发出的文字（人工消息已带【人工客服】前缀）
const textOf = (m: ChatItem) => formatMentions(m.content, m.mentionNames)
const images = (m: ChatItem) => m.attachments.filter((a: MessageAttachment) => a.type === 'image' && a.previewUrl)
const files = (m: ChatItem) => m.attachments.filter((a: MessageAttachment) => !(a.type === 'image' && a.previewUrl))
const friendly = (v: string) => v.replace(/^[a-z_]+:\s*/, '')

// 两条消息间隔超过 5 分钟才插一条时间
function showTime(i: number) {
  if (i === 0) return true
  return new Date(items.value[i].at).getTime() - new Date(items.value[i - 1].at).getTime() > 5 * 60000
}
function timeLabel(value: string) {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  const pad = (n: number) => String(n).padStart(2, '0')
  const hm = `${pad(d.getHours())}:${pad(d.getMinutes())}`
  const today = new Date()
  const days = Math.round((new Date(today.toDateString()).getTime() - new Date(d.toDateString()).getTime()) / 86400000)
  if (days === 0) return hm
  if (days === 1) return '昨天 ' + hm
  return `${d.getMonth() + 1}月${d.getDate()}日 ${hm}`
}

function nearBottom() { const el = scroller.value; return !el || el.scrollHeight - el.scrollTop - el.clientHeight < 80 }
function toBottom(smooth = false) {
  unseen.value = 0
  nextTick(() => { const el = scroller.value; if (el) el.scrollTo({ top: el.scrollHeight, behavior: smooth ? 'smooth' : 'auto' }) })
}
function onScroll() {
  const el = scroller.value
  if (!el) return
  if (nearBottom()) unseen.value = 0
  if (el.scrollTop < 40 && nextBefore.value && !loadingOlder.value) loadOlder()
}

let version = 0
async function loadFirst() {
  const v = ++version
  loading.value = true
  items.value = []
  nextBefore.value = null
  target.value = undefined
  unseen.value = 0
  try {
    const page = await api.conversations.chat(props.conversation.id)
    if (v !== version) return
    items.value = page.items
    nextBefore.value = page.nextBefore
    toBottom()
  } catch (e) { if (v === version) ElMessage.error(e instanceof Error ? e.message : '聊天记录加载失败') }
  finally { if (v === version) loading.value = false }
}
// 实时事件：重新取最新一页，按 id 合并（发送状态会变），保留已翻出的旧消息
async function refreshLatest() {
  if (loading.value) return
  const v = version
  const stick = nearBottom()
  const page = await api.conversations.chat(props.conversation.id).catch(() => null)
  if (!page || v !== version) return
  const known = new Set(items.value.map(m => m.id))
  const latest = new Map(page.items.map(m => [m.id, m]))
  const fresh = page.items.filter(m => !known.has(m.id))
  items.value = [...items.value.map(m => latest.get(m.id) || m), ...fresh]
  if (!fresh.length) return
  if (stick || fresh.every(m => m.direction === 'outbound' && m.role === 'human')) toBottom(true)
  else unseen.value += fresh.length
}
async function loadOlder() {
  if (!nextBefore.value || loadingOlder.value) return
  const v = version
  loadingOlder.value = true
  const el = scroller.value
  const before = el ? el.scrollHeight - el.scrollTop : 0
  try {
    const page = await api.conversations.chat(props.conversation.id, nextBefore.value)
    if (v !== version) return
    const known = new Set(items.value.map(m => m.id))
    items.value = [...page.items.filter(m => !known.has(m.id)), ...items.value]
    nextBefore.value = page.nextBefore
    await nextTick()
    if (el) el.scrollTop = el.scrollHeight - before
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '加载失败') }
  finally { loadingOlder.value = false }
}

function pick(m: ChatItem) {
  target.value = m
  nextTick(() => (document.querySelector('.chat-composer textarea') as HTMLTextAreaElement | null)?.focus())
}
function onEnter(e: Event | KeyboardEvent) {
  const k = e as KeyboardEvent
  if (k.shiftKey || k.isComposing) return
  k.preventDefault()
  send()
}
async function send() {
  const body = text.value.trim()
  if (!body || sending.value) return
  sending.value = true
  const t = target.value
  try {
    await api.conversations.sendText(props.conversation.id, body, t ? { reply: t.canReply ? t.id : '', reference: !t.canReply && t.quotable ? t.id : '' } : undefined)
    text.value = ''
    target.value = undefined
    await refreshLatest()
    toBottom(true)
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '发送失败') }
  finally { sending.value = false }
}
async function sendImage(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || sending.value) return
  sending.value = true
  try {
    await api.conversations.sendImage(props.conversation.id, file)
    await refreshLatest()
    toBottom(true)
  } catch (err) { ElMessage.error(err instanceof Error ? err.message : '图片发送失败') }
  finally { sending.value = false }
}

watch(() => props.conversation.id, () => { text.value = ''; loadFirst() }, { immediate: true })
useAdminEvents(['messages', 'outbox_tasks', 'handoffs'], refreshLatest)
</script>

<style scoped>
.chat-panel { position: relative; display: flex; flex: 1; flex-direction: column; min-height: 0; }
.chat-feed { gap: 0; padding: 12px 20px 20px; }
.feed-top { display: flex; justify-content: center; min-height: 28px; margin-bottom: 4px; font-size: 12px; }
.feed-list { display: flex; flex-direction: column; gap: 14px; }
.feed-skeleton { width: 46%; height: 52px; margin-bottom: 14px; border-radius: var(--radius-md); }
.feed-skeleton.right { align-self: flex-end; }
.feed-time { align-self: center; margin-top: 6px; color: var(--muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.feed-system { align-self: center; max-width: 80%; padding: 3px 10px; border-radius: var(--radius-xs); background: var(--surface-3); color: var(--muted); font-size: 12px; text-align: center; }

.feed-row { display: grid; grid-template-columns: 32px minmax(0, auto) auto; align-items: start; gap: 10px; max-width: min(78%, 680px); }
.feed-row.outbound { grid-template-columns: auto minmax(0, auto) 32px; align-self: flex-end; }
.feed-row.outbound .feed-avatar { grid-column: 3; grid-row: 1; }
.feed-row.outbound .feed-body { grid-column: 2; grid-row: 1; align-items: flex-end; }
.feed-row.outbound .feed-actions { grid-column: 1; grid-row: 1; }
.feed-body { display: flex; flex-direction: column; align-items: flex-start; gap: 4px; min-width: 0; }
.feed-who { display: inline-flex; align-items: center; gap: 6px; max-width: 100%; color: var(--muted); font-size: 12px; white-space: nowrap; }
.feed-who > span { overflow: hidden; min-width: 0; text-overflow: ellipsis; }
.feed-tag { flex: none; padding: 0 6px; border-radius: var(--radius-xs); background: var(--surface-3); color: var(--text-2); font-size: 11px; font-style: normal; line-height: 18px; }
.feed-tag.human { background: var(--warning-light); color: var(--warning); }
.feed-bubble { display: flex; flex-direction: column; gap: 6px; max-width: 100%; padding: 8px 12px; border: 1px solid var(--line); border-radius: 4px var(--radius-md) var(--radius-md); background: var(--surface); font-size: 14px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; transition: border-color var(--dur) var(--ease), box-shadow var(--dur) var(--ease); }
.feed-row.outbound .feed-bubble { border-radius: var(--radius-md) 4px var(--radius-md) var(--radius-md); background: var(--primary-light); border-color: var(--primary-soft); }
.feed-row.target .feed-bubble { border-color: var(--primary); box-shadow: 0 0 0 1px var(--primary); }
.feed-quote { overflow: hidden; max-width: 100%; padding: 2px 8px; border-left: 2px solid var(--line-strong); color: var(--muted); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.feed-images { display: flex; flex-wrap: wrap; gap: 6px; }
.feed-images .el-image { width: 160px; max-height: 200px; border-radius: var(--radius-sm); cursor: zoom-in; }
.feed-file { display: inline-flex; align-items: center; gap: 6px; color: var(--text-2); font-size: 13px; }
.feed-state { display: inline-flex; align-items: center; gap: 4px; color: var(--muted); font-size: 12px; }
.feed-state.is-failed { color: var(--danger); }
.feed-actions { display: flex; gap: 2px; align-self: center; opacity: 0; transition: opacity var(--dur-fast) var(--ease); }
.feed-row:hover .feed-actions, .feed-actions:focus-within { opacity: 1; }
.feed-actions button { display: grid; place-items: center; width: 28px; height: 28px; padding: 0; border: 0; border-radius: var(--radius-sm); background: none; color: var(--muted); cursor: pointer; transition: background var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease); }
.feed-actions button:hover { background: var(--surface-3); color: var(--primary-text); }
@media (hover: none) { .feed-actions { opacity: 1; } }

.msg-enter-active { transition: opacity var(--dur-slow) var(--ease), transform var(--dur-slow) var(--ease); }
.msg-enter-from { opacity: 0; transform: translateY(8px); }

.new-chip { position: absolute; left: 50%; bottom: 168px; z-index: 2; display: inline-flex; align-items: center; gap: 6px; height: 30px; padding: 0 12px; border: 1px solid var(--line-strong); border-radius: 15px; background: var(--surface); color: var(--primary-text); font-size: 12px; box-shadow: var(--shadow-pop, 0 4px 16px rgba(0,0,0,.08)); cursor: pointer; transform: translateX(-50%); }
.fade-enter-active, .fade-leave-active { transition: opacity var(--dur) var(--ease), transform var(--dur) var(--ease); }
.fade-enter-from, .fade-leave-to { opacity: 0; transform: translate(-50%, 6px); }
.composer-target.fade-enter-from, .composer-target.fade-leave-to { transform: translateY(4px); }

.chat-composer .composer-target { display: flex; }
.target-clear { display: grid; place-items: center; width: 20px; height: 20px; margin-left: auto; padding: 0; border: 0; border-radius: 4px; background: none; color: var(--muted); cursor: pointer; }
.target-clear:hover { background: var(--surface-3); color: var(--text); }
.tool-btn { display: grid; place-items: center; width: 32px; height: 32px; padding: 0; border: 0; border-radius: var(--radius-sm); background: none; color: var(--text-2); font-size: 16px; cursor: pointer; transition: background var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease), transform var(--dur-fast) var(--ease); }
.tool-btn:hover { background: var(--surface-3); color: var(--primary-text); }
.tool-btn:active { transform: scale(.94); }
.tool-btn:disabled { opacity: .5; cursor: not-allowed; }
.chat-composer .composer-actions { gap: 4px; }
.chat-composer .composer-actions .muted { margin-left: 8px; }
</style>
