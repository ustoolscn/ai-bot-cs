<template>
  <div class="content-page page-fill">
    <PageHeader title="用户记忆" description="机器人从对话里记住的用户信息，回复时会带上相关的几条。先选群聊或私聊，再选一位用户。">
      <el-button :icon="Refresh" @click="reload">刷新</el-button>
    </PageHeader>
        <div class="toolbar">
          <el-select v-model="memoryGroup" filterable clearable placeholder="选择群聊或私聊" aria-label="选择群聊或私聊" style="width: 300px" @change="changeMemoryGroup">
            <el-option v-for="group in memoryGroups" :key="group.key" :value="group.key" :label="group.label" />
          </el-select>
          <el-select v-model="memoryUser" filterable clearable :disabled="!memoryGroup" placeholder="选择用户" aria-label="选择用户" style="width: 240px" @change="changeMemoryUser">
            <el-option v-for="member in memoryMembers" :key="member.userId" :value="member.userId" :label="memberLabel(member)" />
          </el-select>
          <el-input v-if="memoryUser" v-model="memoryQuery" placeholder="搜索该用户的记忆" :prefix-icon="Search" clearable @change="reload" />
          <span class="spacer" />
          <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('memories', '用户记忆')">删除选中的 {{ selectedIds.length }} 条</el-button>
        </div>
        <div class="memory-split fill" :class="{ solo: !currentMember }">
        <section v-if="currentMember" class="panel profile-panel" v-loading="profileLoading">
          <header>
            <el-segmented v-model="profileTab" :options="[{ label: '人格画像', value: 'persona' }, { label: '文字档案', value: 'text' }]" />
            <span class="spacer" />
            <el-button v-if="profileSaved || profile.persona" size="small" type="danger" plain @click="clearProfile">清空</el-button>
            <el-button v-if="profileTab === 'text'" size="small" type="primary" :disabled="profileDraft === profileSaved" :loading="profileSaving" @click="saveProfile">保存档案</el-button>
          </header>
          <div class="profile-scroll">
            <Transition name="tab" mode="out-in">
              <PersonaCard v-if="profileTab === 'persona'" key="persona" :persona="profile.persona" />
              <div v-else key="text" class="profile-text">
                <p class="muted">AI 每次对话后整体改写，回复时会带上。约 {{ profileTokens }} / 2000 token<template v-if="profile.updatedAt"> · {{ formatTime(profile.updatedAt) }} 更新</template></p>
                <div class="token-meter" :class="{ over: profileTokens > 2000 }"><i :style="{ transform: `scaleX(${Math.min(1, profileTokens / 2000)})` }" /></div>
                <el-input v-model="profileDraft" type="textarea" :autosize="{ minRows: 8, maxRows: 24 }" placeholder="还没有档案。对话后会自动总结，也可以直接填写。" />
              </div>
            </Transition>
          </div>
        </section>
        <section class="table-panel">
          <el-table row-key="id" height="100%" :key="JSON.stringify([memoryGroup,memoryUser,memoryQuery])" v-loading="loading" :data="memories" @selection-change="(rows: {id:string}[]) => selectRows(rows)">
            <template #empty><EmptyState :mood="memoryUser ? 'sleepy' : 'thinking'" :title="!directory.length ? '还没有用户记忆' : !memoryGroup ? '先选一个群聊或私聊' : !memoryUser ? '再选一位用户' : '没有匹配的记忆'" :description="!directory.length ? '机器人和用户聊过之后，会自动记下关键信息。' : !memoryGroup ? '在上方选择要查看的会话。' : !memoryUser ? '选择后显示这位用户的全部记忆和档案。' : '换个关键词试试。'" /></template>
            <el-table-column type="selection" reserve-selection width="48" />
            <el-table-column label="记忆" min-width="170"><template #default="{ row }"><span v-if="row.pinned" class="pill busy pinned">常驻</span>{{ $at(row.content) }}</template></el-table-column>
            <el-table-column label="类型 / 来源" width="110"><template #default="{ row }"><div class="stacked-cell"><strong>{{ kindText[row.kind] || row.kind }}</strong><span>{{ memorySourceText[row.source] || row.source }}<template v-if="row.hasEmbedding === false"> · 无向量</template></span></div></template></el-table-column>
            <el-table-column label="发生 / 更新" width="112"><template #default="{ row }"><div class="stacked-cell" :title="'更新于 ' + formatTime(row.updatedAt)"><strong>{{ formatTime(row.eventAt || row.updatedAt).slice(5) }}</strong><span>更新 {{ formatTime(row.updatedAt).slice(5, 10) }}</span></div></template></el-table-column>
            <el-table-column label="操作" width="100" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="editMemory(row)">编辑</el-button><el-button link type="danger" @click="removeMemory(row)">删除</el-button></template></el-table-column>
          </el-table>
        </section>
        </div>
    <el-dialog v-model="memoryDialog" title="编辑记忆" width="520px">
      <el-form label-position="top">
        <el-form-item label="内容"><el-input v-model="memoryForm.content" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="类型"><el-select v-model="memoryForm.kind"><el-option v-for="(v, k) in kindText" :key="k" :label="v" :value="k" /></el-select></el-form-item>
        <el-checkbox v-model="memoryForm.pinned">常驻（每次回复都带上）</el-checkbox>
      </el-form>
      <template #footer><el-button @click="memoryDialog = false">取消</el-button><el-button type="primary" @click="saveMemory">保存记忆</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useBulkDelete } from '@/composables/useBulkDelete'
import { useAdminEvents } from '@/composables/useAdminEvents'
import { onMounted, reactive, ref, computed, watch } from 'vue'
import { Search, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import PersonaCard from '@/components/memory/PersonaCard.vue'
import { agentApi, formatTime, type Memory, type MemoryMember, type UserProfile } from '@/api/agent'
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

// 用户档案：AI 每次对话后整体改写，上限约 2000 token
const currentMember = computed(() => memoryMembers.value.find(m => m.userId === memoryUser.value))
const profile = ref<Partial<UserProfile>>({})
const profileDraft = ref('')
const profileTab = ref<'persona' | 'text'>('persona')
const profileSaved = ref('')
const profileLoading = ref(false)
const profileSaving = ref(false)
// 粗略估算：中文约 1 字 1 token，其他约 4 字符 1 token
const profileTokens = computed(() => { const text = profileDraft.value; const cjk = (text.match(/[\u3400-\u9fff]/g) || []).length; return Math.round(cjk + (text.length - cjk) / 4) })
function applyProfile(value: Partial<UserProfile>) { profile.value = value; profileDraft.value = value.content || ''; profileSaved.value = value.content || '' }
async function loadProfile() {
  const member = currentMember.value
  if (!member) { applyProfile({}); return }
  profileLoading.value = true
  try { applyProfile(await agentApi.memories.profile(member.conversationId, member.userId).get()) }
  catch { applyProfile({}) }
  finally { profileLoading.value = false }
}
async function saveProfile() {
  const member = currentMember.value
  if (!member) return
  profileSaving.value = true
  try { const saved = await agentApi.memories.profile(member.conversationId, member.userId).save(profileDraft.value); applyProfile({ persona: profile.value.persona, ...saved }); ElMessage.success('档案已保存') }
  catch (error) { ElMessage.error(error instanceof Error ? error.message : '保存失败') }
  finally { profileSaving.value = false }
}
async function clearProfile() {
  const member = currentMember.value
  if (!member) return
  try { await ElMessageBox.confirm('清空这位用户的人格画像和文字档案？AI 之后会从新的对话里重新总结。', '清空档案', { type: 'warning', confirmButtonText: '清空', cancelButtonText: '取消' }) } catch { return }
  applyProfile(await agentApi.memories.profile(member.conversationId, member.userId).clear())
}
watch(currentMember, (now, before) => { if (now?.userId !== before?.userId || now?.conversationId !== before?.conversationId) loadProfile() })

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
async function removeMemory(row: Memory) { try { await ElMessageBox.confirm('删除这条记忆？删除后不能恢复。', '删除记忆', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }) } catch { return }; await agentApi.memories.remove(row.id); await reload() }

onMounted(reload)
useAdminEvents(['user_memories','conversations','bots'], reload)
useAdminEvents(['user_profiles'], () => { if (profileDraft.value === profileSaved.value) loadProfile() })
</script>

<style scoped>
.memory-split { display: grid; grid-template-columns: minmax(400px, 1fr) minmax(0, 1fr); gap: 16px; min-height: 0; }
.memory-split.solo { grid-template-columns: minmax(0, 1fr); }
.memory-split > .table-panel { display: flex; flex-direction: column; min-height: 0; margin-top: 0; }
.memory-split > .table-panel > .el-table { flex: 1; min-height: 0; }
.profile-panel { display: flex; flex-direction: column; gap: 12px; min-height: 0; margin: 0; padding: 16px 20px; }
.profile-scroll { flex: 1; min-height: 0; margin: 0 -20px -16px; padding: 4px 20px 16px; overflow-y: auto; overscroll-behavior: contain; }
.profile-text { display: flex; flex-direction: column; gap: 8px; }
.tab-enter-active, .tab-leave-active { transition: opacity var(--dur) var(--ease), transform var(--dur) var(--ease); }
.tab-enter-from { opacity: 0; transform: translateY(6px); }
.tab-leave-to { opacity: 0; }
@media (max-width: 1100px) { .memory-split { grid-template-columns: 1fr; } .memory-split > .table-panel { min-height: 480px; } .profile-scroll { max-height: 640px; } }
.pinned { margin-right: 6px; }
.profile-panel header { display: flex; align-items: center; gap: 10px; min-height: 28px; }
.profile-panel header .el-button + .el-button { margin-left: 0; }
.profile-panel h2 { font-size: 15px; font-weight: 600; }
.profile-panel .muted { font-size: 12px; font-variant-numeric: tabular-nums; }
.token-meter { position: relative; height: 4px; border-radius: 2px; background: var(--surface-3); overflow: hidden; }
.token-meter i { position: absolute; inset: 0; background: var(--primary); transform-origin: left; transition: transform .4s var(--ease), background .2s var(--ease); }
.token-meter.over i { background: var(--danger); }
</style>
