<template>
  <div class="content-page">
    <PageHeader title="人工台" description="用户要求人工或 AI 无法解决时会转到这里。转人工期间机器人对该用户保持静默。">
      <el-select v-model="status" style="width:120px" @change="load"><el-option label="待处理" value="open" /><el-option label="已结单" value="resolved" /><el-option label="全部" value="all" /></el-select>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </PageHeader>
    <el-empty v-if="!loading && !items.length" description="暂无转人工工单" />
    <div v-loading="loading" class="handoff-list">
      <section v-for="h in items" :key="h.id" class="settings-panel handoff">
        <header>
          <div><strong>{{ h.displayName || h.userId }}</strong><span class="muted"> · {{ h.conversationName }} · {{ formatTime(h.createdAt) }}</span></div>
          <el-tag :type="h.status === 'open' ? 'danger' : 'success'" size="small">{{ h.status === 'open' ? '待处理' : '已结单' }}</el-tag>
        </header>
        <p class="reason">转人工原因：{{ h.reason || '—' }}</p>
        <div class="chat">
          <div v-for="m in h.recent" :key="m.id" class="bubble" :class="m.direction">
            <small>{{ m.direction === 'inbound' ? (m.senderName || '用户') : m.eventType === 'HUMAN_REPLY' ? '人工' : '机器人' }} · {{ formatTime(m.eventAt) }}</small>
            <div>{{ m.content }}</div>
            <el-button v-if="m.direction === 'inbound' && h.status === 'open'" link type="primary" size="small" @click="startReply(h, m.id)">回复这条</el-button>
          </div>
        </div>
        <div v-if="replying[h.id]" class="reply-box">
          <el-input v-model="replyText[h.id]" type="textarea" :rows="3" placeholder="输入回复内容，将以机器人身份发送到 QQ" />
          <div class="reply-actions">
            <el-checkbox v-model="learn[h.id]">把这次回答提交为候选知识</el-checkbox>
            <span class="muted">QQ 被动回复时限：群聊 5 分钟、私聊 60 分钟</span>
            <el-button type="primary" :loading="sending[h.id]" @click="send(h)">发送</el-button>
          </div>
        </div>
        <footer v-if="h.status === 'open'"><el-button @click="resolve(h)">结单并恢复机器人</el-button></footer>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import { agentApi, formatTime, type Handoff } from '@/api/agent'

const status = ref('open')
const items = ref<Handoff[]>([])
const loading = ref(false)
const replying = reactive<Record<string, string>>({})
const replyText = reactive<Record<string, string>>({})
const learn = reactive<Record<string, boolean>>({})
const sending = reactive<Record<string, boolean>>({})

async function load() {
  loading.value = true
  try { items.value = (await agentApi.handoffs.list(status.value)).map(h => ({ ...h, recent: h.recent || [] })) } catch (e) { ElMessage.error(e instanceof Error ? e.message : '加载失败') } finally { loading.value = false }
}
function startReply(h: Handoff, messageID: string) { replying[h.id] = messageID; if (learn[h.id] === undefined) learn[h.id] = true }
async function send(h: Handoff) {
  const text = (replyText[h.id] || '').trim()
  if (!text) return
  sending[h.id] = true
  try {
    await agentApi.messages.reply(replying[h.id], text, !!learn[h.id])
    replyText[h.id] = ''
    delete replying[h.id]
    ElMessage.success('已发送')
    await load()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '发送失败') } finally { sending[h.id] = false }
}
async function resolve(h: Handoff) { await agentApi.handoffs.resolve(h.id); ElMessage.success('已结单'); await load() }
onMounted(load)
</script>

<style scoped>
.handoff-list { display: grid; gap: 16px; }
.handoff header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px; }
.reason { color: #4a5568; font-size: 13px; }
.chat { display: flex; flex-direction: column; gap: 8px; max-height: 360px; overflow: auto; padding: 10px; background: #f8fafc; border-radius: 6px; }
.bubble { max-width: 78%; padding: 8px 12px; border-radius: 8px; background: #fff; border: 1px solid var(--line); font-size: 13px; white-space: pre-wrap; }
.bubble.outbound { align-self: flex-end; background: #eef5ff; }
.bubble small { display: block; color: var(--muted); font-size: 11px; margin-bottom: 4px; }
.reply-box { margin-top: 12px; }
.reply-actions { display: flex; align-items: center; gap: 12px; margin-top: 8px; flex-wrap: wrap; }
.reply-actions .el-button { margin-left: auto; }
footer { margin-top: 12px; display: flex; justify-content: flex-end; }
.muted { color: var(--muted); font-size: 12px; }
</style>
