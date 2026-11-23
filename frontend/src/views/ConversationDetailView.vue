<template>
  <div class="content-page">
    <PageHeader :title="conversation?.name || '会话'" :description="summary" :back="{ label: '群与会话', to: { name: 'conversations' } }">
      <el-button v-if="conversation?.type === 'group'" :icon="Refresh" :loading="syncingName" @click="syncName">同步群名</el-button>
      <el-button :icon="ChatLineSquare" @click="$router.push('/messages')">消息记录</el-button>
    </PageHeader>

    <div v-if="conversation" class="detail-layout">
      <nav class="section-nav" aria-label="会话分区">
        <button v-for="s in sections" :key="s.id" type="button" :class="{ active: section === s.id }" @click="go(s.id)">
          <strong>{{ s.label }}</strong><small>{{ s.hint }}</small>
        </button>
      </nav>
      <div class="detail-body">
        <ConversationSettings v-if="section === 'settings'" :conversation="conversation" @saved="load" />
        <MembersPanel v-else-if="section === 'members'" :conversation-id="conversation.id" :conversation-name="conversation.name" :is-group="conversation.type === 'group'" :bot-id="conversation.botId" :bot-name="conversation.botName" @synced="load" />
        <GroupRulesPanel v-else-if="section === 'rules'" :conversation-id="conversation.id" />
        <QQGroupPanel v-else-if="section === 'qq'" :conversation-id="conversation.id" />
        <ComposePanel v-else-if="section === 'send'" :row="conversation" />
      </div>
    </div>
    <div v-else-if="loading" v-loading="true" style="height: 320px" />
    <section v-else class="panel"><EmptyState title="找不到这个会话" description="它可能已被删除，或者机器人已经退出了这个群。"><el-button @click="$router.push('/conversations')">返回群与会话</el-button></EmptyState></section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ChatLineSquare, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import { useAdminEvents } from '@/composables/useAdminEvents'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import ConversationSettings from '@/components/conversation/ConversationSettings.vue'
import MembersPanel from '@/components/conversation/MembersPanel.vue'
import GroupRulesPanel from '@/components/conversation/GroupRulesPanel.vue'
import QQGroupPanel from '@/components/conversation/QQGroupPanel.vue'
import ComposePanel from '@/components/conversation/ComposePanel.vue'
import type { Conversation } from '@/types'

const props = defineProps<{ id: string }>()
const route = useRoute()
const router = useRouter()
const conversation = ref<Conversation>()
const loading = ref(true)
const syncingName = ref(false)

const sections = computed(() => {
  const group = conversation.value?.type === 'group'
  return [
    { id: 'settings', label: '接待设置', hint: '客服、回复方式、知识库' },
    { id: 'members', label: group ? '成员与客服学习' : '用户', hint: group ? '官方客服、拉黑、记忆' : '记忆、拉黑' },
    ...(group ? [
      { id: 'rules', label: '群规则', hint: '自动审批、发言审核' },
      { id: 'qq', label: '入群与禁言', hint: '群资料、入群申请、禁言' },
    ] : []),
    { id: 'send', label: '发消息', hint: '主动发送、素材' },
  ]
})
const section = computed(() => {
  const wanted = String(route.query.tab || 'settings')
  return sections.value.some(s => s.id === wanted) ? wanted : 'settings'
})
const triggerText: Record<string, string> = { mention_only: '被 @ 时回复', smart: 'AI 判断后回复', always: '每条都回复', disabled: '已停用' }
const summary = computed(() => {
  const c = conversation.value
  if (!c) return ''
  return [c.botName, c.type === 'group' ? `群聊 · ${c.memberCount} 人` : '私聊', c.enabled === false ? '已停用' : triggerText[c.triggerMode], c.lastActiveAt && `最近活跃 ${c.lastActiveAt}`].filter(Boolean).join(' · ')
})

function go(id: string) { router.replace({ query: id === 'settings' ? {} : { tab: id } }) }
async function load() {
  try {
    const list = await api.conversations.list()
    conversation.value = list.find(c => c.id === props.id)
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '会话加载失败')
  } finally { loading.value = false }
}
async function syncName() {
  if (!conversation.value || syncingName.value) return
  syncingName.value = true
  try {
    const { name } = await api.conversations.syncName(conversation.value.id)
    conversation.value.name = name
    ElMessage.success(`已同步群名：${name}`)
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '群名同步失败') } finally { syncingName.value = false }
}
onMounted(load)
useAdminEvents(['conversations', 'bots'], load)
</script>
