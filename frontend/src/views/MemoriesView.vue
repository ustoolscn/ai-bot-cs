<template>
  <div class="content-page">
    <PageHeader title="记忆" description="按会话和用户查看、编辑机器人记住的信息。">
      <el-button type="danger" plain :disabled="!selectedIds.length || loading" :loading="deleting" @click="deleteSelected('memories', '用户记忆')">批量删除{{ selectedIds.length ? '（'+selectedIds.length+'）' : '' }}</el-button>
      <el-button :icon="Refresh" @click="reload">刷新</el-button>
    </PageHeader>
        <div class="toolbar">
          <el-select v-model="memoryGroup" filterable clearable placeholder="选择群聊或私聊" aria-label="选择群聊或私聊" @change="changeMemoryGroup">
            <el-option v-for="group in memoryGroups" :key="group.key" :value="group.key" :label="group.label" />
          </el-select>
          <el-select v-model="memoryUser" filterable clearable :disabled="!memoryGroup" placeholder="选择用户" aria-label="选择用户" @change="changeMemoryUser">
            <el-option v-for="member in memoryMembers" :key="member.userId" :value="member.userId" :label="memberLabel(member)" />
          </el-select>
          <el-input v-if="memoryUser" v-model="memoryQuery" placeholder="搜索该用户的记忆" :prefix-icon="Search" clearable @change="reload" />
        </div>
        <section class="table-panel">
          <el-table row-key="id" :key="JSON.stringify([memoryGroup,memoryUser,memoryQuery])" v-loading="loading" :data="memories" @selection-change="(rows: {id:string}[]) => selectRows(rows)" :empty-text="!directory.length ? '暂无用户记忆' : !memoryGroup ? '请选择群聊或私聊' : !memoryUser ? '请选择该会话中的用户' : '暂无匹配的记忆'">
            <el-table-column type="selection" reserve-selection width="48" />
            <el-table-column label="用户" width="170"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.displayName || '—' }}</strong><span>{{ row.userId }}</span></div></template></el-table-column>
            <el-table-column label="记忆" min-width="320"><template #default="{ row }"><el-icon v-if="row.pinned" class="pin"><StarFilled /></el-icon>{{ row.content }}</template></el-table-column>
            <el-table-column label="类型" width="80"><template #default="{ row }">{{ kindText[row.kind] || row.kind }}</template></el-table-column>
            <el-table-column label="记忆向量" width="105">
              <template #default="{ row }">{{ row.hasEmbedding === true ? '已生成' : row.hasEmbedding === false ? '未生成' : '未知' }}</template>
            </el-table-column>
            <el-table-column label="来源" width="80"><template #default="{ row }">{{ memorySourceText[row.source] || row.source }}</template></el-table-column>
            <el-table-column label="更新" width="150"><template #default="{ row }">{{ formatTime(row.updatedAt) }}</template></el-table-column>
            <el-table-column label="操作" width="120" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="editMemory(row)">编辑</el-button><el-button link type="danger" @click="removeMemory(row)">删除</el-button></template></el-table-column>
          </el-table>
        </section>
    <el-dialog v-model="memoryDialog" title="编辑记忆" width="520px">
      <el-form label-position="top">
        <el-form-item label="内容"><el-input v-model="memoryForm.content" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="类型"><el-select v-model="memoryForm.kind"><el-option v-for="(v, k) in kindText" :key="k" :label="v" :value="k" /></el-select></el-form-item>
        <el-checkbox v-model="memoryForm.pinned">常驻（每次回复都带上）</el-checkbox>
      </el-form>
      <template #footer><el-button @click="memoryDialog = false">取消</el-button><el-button type="primary" @click="saveMemory">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useBulkDelete } from '@/composables/useBulkDelete'
import { useAdminEvents } from '@/composables/useAdminEvents'
import { onMounted, reactive, ref, computed, watch } from 'vue'
import { Search, Refresh, StarFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import { agentApi, formatTime, type Memory, type MemoryMember } from '@/api/agent'
const kindText: Record<string, string> = { fact: '事实', preference: '偏好', issue: '问题', profile: '资料' }
const memorySourceText: Record<string, string> = { extracted: '自动', tool: 'AI 记录', admin: '手动' }
const loading = ref(false)
const memories = ref<Memory[]>([])
const memoryQuery = ref('')
const memoryUser = ref('')
const memoryGroup = ref('')
const directory = ref<MemoryMember[]>([])
const groupKey = (m: MemoryMember) => m.botId + '/' + m.conversationId
const memoryGroups = computed(() => {
  const groups = new Map<string, string>()
  for (const member of directory.value) {
    const type = member.conversationType === 'group' ? '群聊' : member.conversationType === 'unassigned' ? '历史' : '私聊'
    groups.set(groupKey(member), member.botName + ' · ' + type + ' · ' + member.conversationName)
  }
  return Array.from(groups, ([key, label]) => ({ key, label }))
})
const memoryMembers = computed(() => directory.value.filter(m => groupKey(m) === memoryGroup.value))
function memberLabel(member: MemoryMember) {
  return (member.displayName ? member.displayName + ' · ' : '') + member.userId + '（' + member.count + '）'
}
function changeMemoryGroup() {
  memoryUser.value = ''
  changeMemoryUser()
}
function changeMemoryUser() {
  memories.value = []
  memoryQuery.value = ''
  reload()
}
let memoryRequest = 0
async function loadMemoryTab() {
  const version = ++memoryRequest
  const members = await agentApi.memories.directory()
  if (version !== memoryRequest) return
  directory.value = members
  if (!memoryMembers.value.length) memoryGroup.value = ''
  const member = memoryMembers.value.find(m => m.userId === memoryUser.value)
  if (!member) {
    memoryUser.value = ''
    memories.value = []
    return
  }
  const rows = await agentApi.memories.list({ botId: member.botId, conversationId: member.conversationId, userId: member.userId, q: memoryQuery.value || undefined })
  if (version === memoryRequest) memories.value = rows
}

const { selectedIds, deleting, deleteSelected } = useBulkDelete(reload)
function selectRows(rows: { id: string }[]) {
  const visible = new Set(memories.value.map(row => row.id))
  selectedIds.value = rows.filter(row => visible.has(row.id)).map(row => row.id)
}
watch([memoryGroup,memoryUser,memoryQuery], () => { selectedIds.value = [] }, { flush: 'sync' })
async function reload() {
  loading.value = true
  try { await loadMemoryTab() }
  catch (error) { ElMessage.error(error instanceof Error ? error.message : '加载记忆失败') }
  finally { loading.value = false }
}
// Memories
const memoryDialog = ref(false)
const memoryForm = reactive({ id: '', content: '', kind: 'fact', pinned: false })
function editMemory(row: Memory) { Object.assign(memoryForm, { id: row.id, content: row.content, kind: row.kind, pinned: row.pinned }); memoryDialog.value = true }
async function saveMemory() { await agentApi.memories.update(memoryForm.id, { content: memoryForm.content, kind: memoryForm.kind, pinned: memoryForm.pinned }); memoryDialog.value = false; await reload() }
async function removeMemory(row: Memory) { await ElMessageBox.confirm('删除这条记忆？', '确认', { type: 'warning' }); await agentApi.memories.remove(row.id); await reload() }

onMounted(reload)
useAdminEvents(['user_memories','conversations','bots'], reload)
</script>
<style scoped>
.pin { color: #e6a23c; margin-right: 4px; vertical-align: -2px; }
</style>
