<template>
  <div class="content-page">
    <PageHeader title="黑名单" description="拉黑的群成员，机器人不回复他，他的发言也不进上下文、记忆和学习。开了自动踢人的，机器人发现他在群里就会踢出去。">
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </PageHeader>

    <el-alert class="tip" type="info" :closable="false" show-icon>
      <template #title>在“群与会话 → 成员”里点“拉黑”添加。QQ 不提供成员的真实 QQ 号，只能从机器人见过的群成员里选。</template>
      自动踢人需要群主给机器人开启“移除群成员”权限，并在 QQ 机器人后台打开对应开关；没有权限时会在这里显示失败，请到群里手动踢。
    </el-alert>

    <div class="toolbar">
      <el-input v-model="search" placeholder="搜索昵称、成员 ID 或原因" :prefix-icon="Search" clearable />
      <el-segmented v-model="statusFilter" :options="statusOptions" />
    </div>

    <section class="table-panel">
      <el-table v-loading="loading" :data="filtered" row-key="id">
        <template #empty>
          <EmptyState mood="happy" title="黑名单是空的" description="群里有人捣乱时，到“群与会话 → 成员”里点“拉黑”。" />
        </template>
        <el-table-column label="成员" min-width="200">
          <template #default="{ row }"><div class="stacked-cell"><strong>{{ row.displayName || '未知昵称' }}</strong><span class="mono">{{ row.userId }}</span></div></template>
        </el-table-column>
        <el-table-column label="范围" min-width="170">
          <template #default="{ row }">
            <div class="stacked-cell">
              <strong>{{ row.conversationId ? conversationName(row.conversationId) : '所有群' }}</strong>
              <span>{{ row.botName }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="原因" min-width="160"><template #default="{ row }"><span :class="row.reason ? '' : 'faint'">{{ row.reason || '—' }}</span></template></el-table-column>
        <el-table-column label="踢人" min-width="220">
          <template #default="{ row }">
            <span v-if="!row.autoKick" class="faint">只屏蔽，不踢人</span>
            <span v-else-if="!row.actions.length" class="faint">等他在群里出现</span>
            <div v-else class="action-list">
              <el-tooltip v-for="a in row.actions" :key="a.id" :disabled="!a.error" :content="a.error" placement="top">
                <span class="kick-pill" :class="a.status">
                  <i />{{ statusText[a.status] }}<em v-if="!row.conversationId || row.actions.length > 1">· {{ conversationName(a.conversationId) }}</em>
                </span>
              </el-tooltip>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="拉黑时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        <el-table-column label="操作" width="130" fixed="right">
          <template #default="{ row }">
            <el-button v-if="row.autoKick && row.actions.some((a: BlockedAction) => a.status === 'failed')" link type="primary" @click="retry(row)">重试</el-button>
            <el-button link type="danger" @click="remove(row)">解除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api } from '@/api'
import { agentApi, formatTime, type BlockedAction, type BlockedUser } from '@/api/agent'
import type { Conversation } from '@/types'

const statusText: Record<string, string> = { pending: '正在踢出', kicked: '已踢出', failed: '踢出失败', disabled: '已停用' }
const statusOptions = [{ label: '全部', value: 'all' }, { label: '踢出失败', value: 'failed' }, { label: '已踢出', value: 'kicked' }]
const items = ref<BlockedUser[]>([])
const conversations = ref<Conversation[]>([])
const loading = ref(false)
const search = ref('')
const statusFilter = ref('all')

const filtered = computed(() => items.value.filter(item => {
  const text = `${item.displayName}${item.userId}${item.reason}`
  if (search.value && !text.includes(search.value)) return false
  if (statusFilter.value !== 'all' && !item.actions.some(a => a.status === statusFilter.value)) return false
  return true
}))

function conversationName(id: string) { return conversations.value.find(c => c.id === id)?.name || '未知群' }

onMounted(load)
async function load() {
  loading.value = true
  try {
    const [list, convs] = await Promise.all([agentApi.blacklist.list(), api.conversations.list().catch(() => [])])
    items.value = list
    conversations.value = convs
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}
async function retry(row: BlockedUser) {
  try {
    await agentApi.blacklist.add({ botId: row.botId, conversationId: row.conversationId || undefined, userId: row.userId, reason: row.reason || undefined, autoKick: true })
    ElMessage.success('已重新排队踢出')
    await load()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '重试失败') }
}
async function remove(row: BlockedUser) {
  try {
    await ElMessageBox.confirm(`解除对“${row.displayName || row.userId}”的拉黑？已被踢出的人不会自动拉回群。`, '解除拉黑', { confirmButtonText: '解除', cancelButtonText: '取消', type: 'warning' })
  } catch { return }
  try { await agentApi.blacklist.remove(row.id); ElMessage.success('已解除'); await load() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '解除失败') }
}
</script>

<style scoped>
.tip { margin-bottom: 16px; }
.mono { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 11px; }
.faint { color: var(--faint); }
.action-list { display: flex; flex-wrap: wrap; gap: 6px; }
.kick-pill { display: inline-flex; align-items: center; gap: 6px; padding: 3px 10px; border-radius: 999px; font-size: 12px; font-weight: 600; cursor: default; }
.kick-pill i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.kick-pill em { font-style: normal; font-weight: 400; opacity: .8; }
.kick-pill.pending { background: var(--sky-light); color: var(--sky); }
.kick-pill.pending i { animation: blink 1.2s ease-in-out infinite; }
.kick-pill.kicked { background: var(--success-light); color: var(--success); }
.kick-pill.failed { background: var(--danger-light); color: var(--danger); cursor: help; }
.kick-pill.disabled { background: var(--info-light); color: var(--muted); }
@keyframes blink { 50% { opacity: .3; } }
</style>
