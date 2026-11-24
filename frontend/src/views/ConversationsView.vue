<template>
  <div class="content-page page-fill">
    <PageHeader title="群与会话" description="机器人加入的群和私聊。点任意一行进入这个会话，设置接待方式、管理成员和群规则、发消息。">
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新</el-button>
    </PageHeader>
    <div class="toolbar">
      <el-input v-model="search" placeholder="搜索名称或会话 ID" :prefix-icon="Search" clearable />
      <el-segmented v-model="type" :options="[{ label: '全部', value: 'all' }, { label: '群聊', value: 'group' }, { label: '私聊', value: 'c2c' }]" />
      <el-select v-model="trigger" aria-label="按回复方式筛选">
        <el-option label="全部回复方式" value="all" /><el-option label="被 @ 时回复" value="mention_only" /><el-option label="AI 判断后回复" value="smart" /><el-option label="已停用" value="disabled" />
      </el-select>
      <span class="spacer" />
      <span class="muted">{{ filtered.length }} 个会话</span>
    </div>
    <section class="table-panel">
      <el-table v-loading="loading" :data="filtered" row-key="id" class="clickable-rows" height="100%" @row-click="open">
        <template #empty><EmptyState title="还没有会话" description="把机器人拉进 QQ 群，或有人私聊它之后，会自动出现在这里。" /></template>
        <el-table-column label="会话" min-width="260">
          <template #default="{ row }">
            <div class="cell-primary">
              <span class="entity-icon small"><el-icon><component :is="row.type === 'group' ? ChatDotRound : User" /></el-icon></span>
              <span><strong>{{ row.name }}</strong><small>{{ row.botName }} · {{ row.type === 'group' ? '群聊' : '私聊' }}</small></span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="回复方式" width="170">
          <template #default="{ row }">
            <div class="stacked-cell">
              <StatusBadge :status="row.triggerMode === 'disabled' ? 'offline' : 'online'" :text="triggerText(row.triggerMode)" />
              <span v-if="row.type === 'group' && row.triggerMode === 'smart' && !row.hasFullMessageEvents" class="danger-text">未收到群全量消息</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="知识库" min-width="180"><template #default="{ row }"><span :class="{ faint: !row.knowledgeBaseNames.length }">{{ row.knowledgeBaseNames.join('、') || '未绑定' }}</span></template></el-table-column>
        <el-table-column label="成员" width="90" align="right"><template #default="{ row }"><span class="num">{{ row.memberCount }}</span></template></el-table-column>
        <el-table-column label="消息" width="100" align="right"><template #default="{ row }"><span class="num">{{ row.messageCount.toLocaleString() }}</span></template></el-table-column>
        <el-table-column prop="lastActiveAt" label="最近活跃" width="130" />
        <el-table-column width="96" align="right">
          <template #default="{ row }">
            <div class="row-tail">
              <el-tooltip content="删除这个会话" placement="top"><button type="button" class="row-delete" :aria-label="'删除 ' + row.name" @click.stop="remove(row)"><el-icon><Delete /></el-icon></button></el-tooltip>
              <el-icon class="row-chevron"><ArrowRight /></el-icon>
            </div>
          </template>
        </el-table-column>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowRight, ChatDotRound, Delete, Refresh, Search, User } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import type { Conversation } from '@/types'

const router = useRouter()
const conversations = ref<Conversation[]>([])
const loading = ref(false)
const search = ref('')
const type = ref('all')
const trigger = ref('all')
const filtered = computed(() => conversations.value.filter(c => (!search.value || `${c.name}${c.id}`.includes(search.value)) && (type.value === 'all' || c.type === type.value) && (trigger.value === 'all' || c.triggerMode === trigger.value)))

function triggerText(value: string) { return ({ mention_only: '被 @ 时回复', smart: 'AI 判断后回复', always: '每条都回复', disabled: '已停用' } as Record<string, string>)[value] || value }
function open(row: Conversation) { router.push({ name: 'conversation', params: { id: row.id } }) }
async function remove(row: Conversation) {
  const kind = row.type === 'group' ? '群' : '私聊'
  try {
    await ElMessageBox.confirm(`删除后，「${row.name}」的接待设置、消息记录、成员、用户记忆和定时任务里的这个目标都会清除，无法恢复。知识库本身不受影响。机器人之后再收到这个${kind}的消息，会作为新会话重新出现，使用默认设置。`, `删除${kind}「${row.name}」？`, { confirmButtonText: '删除会话', cancelButtonText: '取消', type: 'warning', confirmButtonClass: 'el-button--danger' })
  } catch { return }
  try {
    await api.conversations.remove(row.id)
    conversations.value = conversations.value.filter(c => c.id !== row.id)
    ElMessage.success(`已删除「${row.name}」`)
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '删除失败') }
}
async function load() {
  loading.value = true
  try {
    const [items, kbs] = await Promise.all([api.conversations.list(), api.knowledge.list()])
    for (const item of items) item.knowledgeBaseNames = (item.knowledgeBaseIds || []).map(id => kbs.find(kb => kb.id === id)?.name || id)
    conversations.value = items
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '会话加载失败') } finally { loading.value = false }
}
onMounted(load)
useAdminEvents(['conversations', 'messages', 'conversation_members', 'bots', 'knowledge_bases', 'conversation_knowledge_bases'], load)
</script>

<style scoped>
.row-tail { display: inline-flex; align-items: center; justify-content: flex-end; gap: 8px; }
.row-delete { display: grid; place-items: center; width: 28px; height: 28px; padding: 0; border: 0; border-radius: var(--radius-sm); background: none; color: var(--faint); cursor: pointer; opacity: 0; transition: opacity var(--dur-fast) var(--ease), background var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease), transform var(--dur) var(--ease); transform: translateX(4px); }
.el-table__row:hover .row-delete, .row-delete:focus-visible { opacity: 1; transform: none; }
.row-delete:hover { background: var(--danger-light); color: var(--danger); }
@media (hover: none) { .row-delete { opacity: 1; } }
</style>
