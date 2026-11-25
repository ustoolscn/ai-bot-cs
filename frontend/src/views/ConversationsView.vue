<template>
  <div class="content-page wide page-fill">
    <PageHeader title="群与会话" description="左边选一个群或私聊，右边直接以机器人身份聊天；接待方式、成员、群管理在右上角的“设置”里。">
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </PageHeader>

    <div class="inbox chat-workspace">
      <aside class="inbox-list">
        <div class="inbox-filter list-tools">
          <el-input v-model="search" placeholder="搜索群名或会话 ID" :prefix-icon="Search" clearable />
          <el-segmented v-model="type" :options="[{ label: '全部', value: 'all' }, { label: '群聊', value: 'group' }, { label: '私聊', value: 'c2c' }]" />
        </div>
        <div v-loading="loading && !conversations.length" class="inbox-items">
          <button v-for="c in filtered" :key="c.id" type="button" class="inbox-item conv-item" :class="{ active: c.id === props.id, off: c.triggerMode === 'disabled' }" @click="open(c)">
            <AvatarImage :src="c.avatar" :name="c.name" :size="36" :tone="c.type" />
            <span class="conv-text">
              <span class="inbox-item-top"><em class="conv-type" :class="c.type">{{ c.type === 'group' ? '群聊' : '私聊' }}</em><strong>{{ c.name }}</strong><time>{{ shortTime(c.lastMessage?.at) }}</time></span>
              <p>{{ preview(c) }}</p>
              <small>{{ c.botName }}<template v-if="c.triggerMode === 'disabled'"> · 已停用</template><template v-else-if="c.type === 'group' && c.triggerMode === 'smart' && !c.hasFullMessageEvents"> · <b class="danger-text">未收到群全量消息</b></template></small>
            </span>
          </button>
          <EmptyState v-if="!loading && !filtered.length" compact :title="conversations.length ? '没有匹配的会话' : '还没有会话'" :description="conversations.length ? '换个关键词或筛选试试。' : '把机器人拉进 QQ 群，或有人私聊它之后，会自动出现在这里。'" />
        </div>
      </aside>

      <section class="inbox-detail">
        <template v-if="current">
          <header class="inbox-detail-head">
            <AvatarImage :src="current.avatar" :name="current.name" :size="32" :tone="current.type" />
            <div>
              <h2>{{ current.name }}</h2>
              <p>{{ summary }}</p>
            </div>
            <div class="head-actions">
              <el-button :icon="Setting" @click="openSettings()">设置</el-button>
              <el-dropdown trigger="click" @command="onCommand">
                <el-button :icon="MoreFilled" aria-label="更多操作" />
                <template #dropdown>
                  <el-dropdown-menu>
                    <el-dropdown-item command="messages" :icon="ChatLineSquare">消息记录</el-dropdown-item>
                    <el-dropdown-item v-if="current.type === 'group'" command="sync" :icon="Refresh">同步群名</el-dropdown-item>
                    <el-dropdown-item command="delete" :icon="Delete" divided class="danger-item">删除会话</el-dropdown-item>
                  </el-dropdown-menu>
                </template>
              </el-dropdown>
            </div>
          </header>
          <ChatPanel :conversation="current" @open-settings="openSettings" />
        </template>
        <div v-else class="inbox-placeholder"><EmptyState :title="props.id && !loading ? '找不到这个会话' : '选择左侧的一个群或私聊'" :description="props.id && !loading ? '它可能已被删除，或者机器人已经退出了这个群。' : '可以查看聊天记录，并直接以机器人身份发消息。'" /></div>
      </section>
    </div>

    <el-drawer v-model="drawer" :size="drawerSize" :title="current ? current.name + ' · 设置' : '设置'" class="conv-drawer" append-to-body destroy-on-close @closed="clearTab">
      <template v-if="current">
        <nav class="drawer-tabs" aria-label="设置分区">
          <button v-for="s in sections" :key="s.id" type="button" :class="{ active: section === s.id }" @click="section = s.id">
            <strong>{{ s.label }}</strong><small>{{ s.hint }}</small>
          </button>
        </nav>
        <Transition name="tab" mode="out-in">
          <div :key="section" class="drawer-body">
            <ConversationSettings v-if="section === 'settings'" :conversation="current" @saved="load" />
            <MembersPanel v-else-if="section === 'members'" :conversation-id="current.id" :conversation-name="current.name" :is-group="current.type === 'group'" :bot-id="current.botId" :bot-name="current.botName" @synced="load" />
            <GroupRulesPanel v-else-if="section === 'rules'" :conversation-id="current.id" />
            <QQGroupPanel v-else-if="section === 'qq'" :conversation-id="current.id" />
            <ComposePanel v-else-if="section === 'send'" :row="current" />
          </div>
        </Transition>
      </template>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChatLineSquare, Delete, MoreFilled, Refresh, Search, Setting } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { useAdminEvents } from '@/composables/useAdminEvents'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import AvatarImage from '@/components/AvatarImage.vue'
import ChatPanel from '@/components/conversation/ChatPanel.vue'
import ConversationSettings from '@/components/conversation/ConversationSettings.vue'
import MembersPanel from '@/components/conversation/MembersPanel.vue'
import GroupRulesPanel from '@/components/conversation/GroupRulesPanel.vue'
import QQGroupPanel from '@/components/conversation/QQGroupPanel.vue'
import ComposePanel from '@/components/conversation/ComposePanel.vue'
import { formatMentions } from '@/utils/mentions'
import type { Conversation } from '@/types'

const props = defineProps<{ id?: string }>()
const route = useRoute()
const router = useRouter()
const conversations = ref<Conversation[]>([])
const loading = ref(false)
const search = ref('')
const type = ref('all')

const atOf = (c: Conversation) => c.lastMessage?.at ? new Date(c.lastMessage.at).getTime() : 0
const filtered = computed(() => conversations.value
  .filter(c => (!search.value || `${c.name}${c.id}${c.platformId}`.includes(search.value)) && (type.value === 'all' || c.type === type.value))
  .sort((a, b) => atOf(b) - atOf(a)))
const current = computed(() => conversations.value.find(c => c.id === props.id))

const triggerText: Record<string, string> = { mention_only: '被 @ 时回复', smart: 'AI 判断后回复', always: '每条都回复', disabled: '已停用' }
const summary = computed(() => {
  const c = current.value
  if (!c) return ''
  return [c.botName, c.type === 'group' ? `群聊 · ${c.memberCount} 人` : '私聊', triggerText[c.triggerMode], c.knowledgeBaseNames.length ? '知识库：' + c.knowledgeBaseNames.join('、') : ''].filter(Boolean).join(' · ')
})
function preview(c: Conversation) {
  const m = c.lastMessage
  if (!m) return '还没有消息'
  const who = m.direction === 'outbound' ? m.senderName || '机器人' : m.senderName || '群成员'
  return `${who}：${formatMentions(m.content, m.mentionNames) || '[图片]'}`
}
function shortTime(value?: string) {
  if (!value) return ''
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return ''
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  if (d.toDateString() === now.toDateString()) return `${pad(d.getHours())}:${pad(d.getMinutes())}`
  const days = Math.round((new Date(now.toDateString()).getTime() - new Date(d.toDateString()).getTime()) / 86400000)
  if (days === 1) return '昨天'
  return d.getFullYear() === now.getFullYear() ? `${d.getMonth() + 1}/${d.getDate()}` : `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`
}

function open(c: Conversation) { if (c.id !== props.id) router.push({ name: 'conversation', params: { id: c.id } }) }

// 设置抽屉：原来详情页的各个分区。?tab= 打开对应分区（别处的链接还能用）
const drawer = ref(false)
const section = ref('settings')
const drawerSize = 'min(920px, 94vw)'
const sections = computed(() => {
  const group = current.value?.type === 'group'
  return [
    { id: 'settings', label: '接待设置', hint: '客服、回复方式、知识库、生图限额' },
    { id: 'members', label: group ? '成员与客服学习' : '用户', hint: group ? '官方客服、拉黑、记忆' : '记忆、拉黑' },
    ...(group ? [
      { id: 'rules', label: '群规则', hint: '自动审批、发言审核' },
      { id: 'qq', label: '入群与禁言', hint: '群资料、入群申请、禁言' },
    ] : []),
    { id: 'send', label: '高级发送', hint: 'Markdown、卡片、按钮、素材' },
  ]
})
function openSettings(tab?: string) {
  section.value = tab && sections.value.some(s => s.id === tab) ? tab : 'settings'
  drawer.value = true
}
function clearTab() { if (route.query.tab) router.replace({ query: {} }) }
watch(() => [route.query.tab, current.value?.id], () => { const tab = route.query.tab; if (tab && current.value) openSettings(String(tab)) }, { immediate: true })

async function onCommand(cmd: string) {
  const c = current.value
  if (!c) return
  if (cmd === 'messages') router.push('/messages')
  else if (cmd === 'sync') {
    try { const { name } = await api.conversations.syncName(c.id); c.name = name; ElMessage.success(`已同步群名：${name}`) }
    catch (e) { ElMessage.error(e instanceof Error ? e.message : '群名同步失败') }
  } else if (cmd === 'delete') remove(c)
}
async function remove(row: Conversation) {
  const kind = row.type === 'group' ? '群' : '私聊'
  try {
    await ElMessageBox.confirm(`删除后，「${row.name}」的接待设置、消息记录、成员、用户记忆和定时任务里的这个目标都会清除，无法恢复。知识库本身不受影响。机器人之后再收到这个${kind}的消息，会作为新会话重新出现，使用默认设置。`, `删除${kind}「${row.name}」？`, { confirmButtonText: '删除会话', cancelButtonText: '取消', type: 'warning', confirmButtonClass: 'el-button--danger' })
  } catch { return }
  try {
    await api.conversations.remove(row.id)
    conversations.value = conversations.value.filter(c => c.id !== row.id)
    ElMessage.success(`已删除「${row.name}」`)
    router.replace({ name: 'conversations' })
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '删除失败') }
}

async function load() {
  loading.value = true
  try {
    const [items, kbs] = await Promise.all([api.conversations.list(), api.knowledge.list()])
    for (const item of items) item.knowledgeBaseNames = (item.knowledgeBaseIds || []).map(id => kbs.find(kb => kb.id === id)?.name || id)
    conversations.value = items
    // 没选会话时默认打开最近活跃的那个，和 QQ 一样进来就能聊
    if (!props.id && filtered.value.length && window.innerWidth > 900) router.replace({ name: 'conversation', params: { id: filtered.value[0].id } })
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '会话加载失败') } finally { loading.value = false }
}
onMounted(load)
useAdminEvents(['conversations', 'messages', 'conversation_members', 'bots', 'knowledge_bases', 'conversation_knowledge_bases'], load)
</script>

<style scoped>
.chat-workspace { grid-template-columns: 300px minmax(0, 1fr); min-height: 520px; }
.list-tools { flex-direction: column; align-items: stretch; gap: 8px; height: auto; padding: 12px; }
.conv-item { display: grid; grid-template-columns: 36px minmax(0, 1fr); align-items: center; gap: 10px; padding: 10px 14px; }
.conv-item.off { opacity: .62; }
.conv-text { display: flex; flex-direction: column; gap: 1px; min-width: 0; }
.conv-type { flex: none; padding: 0 5px; border-radius: var(--radius-xs); font-size: 11px; font-style: normal; font-weight: 500; line-height: 17px; }
.conv-type.group { background: var(--primary-light); color: var(--primary-text); }
.conv-type.c2c { background: var(--success-light); color: var(--success); }
.inbox-detail-head .head-actions { display: flex; gap: 8px; }
.inbox-detail-head .head-actions .el-button + .el-button { margin-left: 0; }
.danger-text { color: var(--danger); font-weight: 400; }
:deep(.danger-item) { color: var(--danger); }

.drawer-tabs { display: flex; gap: 4px; margin: -4px 0 16px; padding: 4px; border-radius: var(--radius-md); background: var(--surface-3); overflow-x: auto; }
.drawer-tabs button { display: flex; flex: 1 0 auto; flex-direction: column; align-items: flex-start; gap: 1px; min-width: 120px; padding: 6px 12px; border: 0; border-radius: var(--radius-sm); background: none; color: var(--text-2); text-align: left; cursor: pointer; transition: background var(--dur) var(--ease), color var(--dur) var(--ease), box-shadow var(--dur) var(--ease); }
.drawer-tabs button:hover { color: var(--text); }
.drawer-tabs button.active { background: var(--surface); color: var(--text); box-shadow: 0 1px 2px rgba(17, 24, 39, .08); }
.drawer-tabs strong { font-size: 13px; font-weight: 600; }
.drawer-tabs small { color: var(--muted); font-size: 12px; white-space: nowrap; }
.tab-enter-active, .tab-leave-active { transition: opacity var(--dur) var(--ease), transform var(--dur) var(--ease); }
.tab-enter-from { opacity: 0; transform: translateY(6px); }
.tab-leave-to { opacity: 0; }

@media (max-width: 900px) {
  .chat-workspace { grid-template-columns: 1fr; }
  .chat-workspace .inbox-list { max-height: 260px; }
  .chat-workspace .inbox-detail { min-height: 560px; }
}
</style>
