<template>
  <section v-loading="learningLoading" class="panel">
    <div class="panel-head">
      <div>
        <h2>客服语气学习</h2>
        <p>把群里的真人客服标记为“官方客服”后，AI 会模仿他们的说话方式，他们的回答会进入知识库的待审核列表。</p>
      </div>
      <span v-if="jobStatus" class="pill" :class="jobStatus.tone">{{ jobStatus.text }}</span>
    </div>
    <div class="panel-body">
      <template v-if="learning?.staff.length">
        <div class="fact-row" style="margin-bottom: 12px">
          <span v-for="s in learning.staff" :key="s.userId"><strong>{{ s.displayName || s.userId }}</strong> · {{ s.availableMessages }} 条发言</span>
        </div>
        <div v-if="learning.style" class="content-box">{{ learning.style }}</div>
        <p v-else class="muted">{{ jobRunning ? '正在阅读客服的历史发言，通常一两分钟就好。' : learning.job?.status === 'failed' ? `上次学习没成功：${learning.job.error || '原因未知'}。可以再试一次。` : '还没有学到语气。点“开始学习”让 AI 读一遍客服的历史发言。' }}</p>
      </template>
      <p v-else class="muted">在下面的成员列表里打开“官方客服”开关，AI 就会开始学习这些人的语气和回答。</p>
    </div>
    <div class="panel-foot">
      <span class="note">
        <template v-if="learning?.learnedAt">参考了 {{ learning.sampleCount }} 条发言 · {{ formatTime(learning.learnedAt) }}。</template>
        只能学习机器人进群后收到的消息，QQ 不提供补拉历史记录的接口。
      </span>
      <el-button v-if="isGroup" @click="openCandidates">查看本群学到的知识</el-button>
      <el-button v-if="learning?.staff.length" type="primary" :loading="starting || jobRunning" @click="startLearning">{{ learning.learnedAt ? '重新学习' : '开始学习' }}</el-button>
    </div>
  </section>

  <section class="panel">
    <div class="panel-head">
      <div><h2>成员</h2><p>{{ rosterStatus || (isGroup ? '机器人见过的群成员。可以从 QQ 同步完整名单。' : '和机器人私聊过的用户。') }}</p></div>
      <el-button v-if="isGroup" :icon="Refresh" :loading="syncing" @click="sync(true)">同步 QQ 群成员</el-button>
    </div>
    <div class="panel-body" style="padding-bottom: 12px">
      <el-input v-model="filter" placeholder="搜索昵称或成员 ID" :prefix-icon="Search" clearable style="max-width: 280px" />
    </div>
    <el-table v-loading="loading" :data="filtered" max-height="560" empty-text="还没有成员记录">
      <el-table-column label="成员" min-width="220">
        <template #default="{ row }">
          <div class="cell-primary">
            <el-avatar :src="row.avatar || undefined" :size="28">{{ (row.displayName || row.userId).slice(0, 1) }}</el-avatar>
            <span>
              <strong>{{ row.displayName || '未知昵称' }}<span v-if="roleText(row)" class="muted"> · {{ roleText(row) }}</span></strong>
              <small class="mono">{{ row.userId }}</small>
            </span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="入群时间" width="150"><template #default="{ row }">{{ row.joinedAt ? formatTime(row.joinedAt) : '—' }}</template></el-table-column>
      <el-table-column label="消息" width="80" align="right"><template #default="{ row }"><span class="num">{{ row.messageCount }}</span></template></el-table-column>
      <el-table-column label="记忆" width="80" align="right">
        <template #default="{ row }"><el-button v-if="Number(row.memoryCount) > 0" link type="primary" @click="showMemories(row)">{{ row.memoryCount }}</el-button><span v-else class="faint">0</span></template>
      </el-table-column>
      <el-table-column label="官方客服" width="100" align="center">
        <template #default="{ row }"><el-switch v-model="row.isStaff" :disabled="row.isBlocked" :aria-label="'官方客服：' + (row.displayName || row.userId)" @change="toggle(row)" /></template>
      </el-table-column>
      <el-table-column label="黑名单" width="120" fixed="right">
        <template #default="{ row }">
          <span v-if="row.isBlocked" class="pill bad">已拉黑</span>
          <el-button v-if="row.isBlocked" link type="primary" @click="unblock(row)">解除</el-button>
          <el-button v-else link type="danger" @click="openBlock(row)">拉黑</el-button>
        </template>
      </el-table-column>
    </el-table>
  </section>

  <el-dialog v-model="blockDialog" :title="`拉黑 ${blockTarget?.displayName || blockTarget?.userId || ''}`" width="460px" append-to-body>
    <p class="muted" style="margin-bottom: 16px">拉黑后，机器人不再回复他，他的发言也不会进入上下文、记忆和学习。</p>
    <el-form label-position="top">
      <el-form-item label="拉黑范围">
        <el-radio-group v-model="blockForm.scope">
          <el-radio value="group">只在本群</el-radio>
          <el-radio value="bot">{{ botName || '这个机器人' }} 所在的所有群</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="原因（选填）"><el-input v-model="blockForm.reason" type="textarea" :rows="2" maxlength="1000" placeholder="例如：发广告、骚扰其他群友" /></el-form-item>
      <div class="setting-row" style="padding: 0">
        <div><strong>发现时自动踢出群</strong><span>需要群主给机器人开启“移除群成员”权限，没有权限时会在黑名单里显示失败</span></div>
        <el-switch v-model="blockForm.autoKick" />
      </div>
    </el-form>
    <template #footer><el-button @click="blockDialog = false">取消</el-button><el-button type="danger" :loading="blocking" @click="confirmBlock">确认拉黑</el-button></template>
  </el-dialog>

  <el-dialog v-model="memDialog" :title="`${memUser} 的记忆`" width="520px" append-to-body>
    <div class="record-list">
      <div v-for="m in mems" :key="m.id" class="record-item"><span>{{ m.content }}</span><small>{{ formatTime(m.updatedAt) }}</small></div>
    </div>
  </el-dialog>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { agentApi, formatTime, type Member, type Memory, type StaffLearning } from '@/api/agent'

const props = defineProps<{ conversationId: string; conversationName: string; isGroup: boolean; botId: string; botName: string }>()
const emit = defineEmits<{ synced: [] }>()
const router = useRouter()
const members = ref<Member[]>([])
const loading = ref(false)
const syncing = ref(false)
const rosterStatus = ref('')
const filter = ref('')
const learning = ref<StaffLearning | null>(null)
const learningLoading = ref(false)
const starting = ref(false)
const memDialog = ref(false)
const memUser = ref('')
const mems = ref<Memory[]>([])
let pollTimer: ReturnType<typeof setTimeout> | undefined
let alive = true

const filtered = computed(() => members.value.filter(m => !filter.value || `${m.displayName}${m.userId}`.includes(filter.value)))
const jobRunning = computed(() => learning.value?.job?.status === 'pending' || learning.value?.job?.status === 'processing')
const jobStatus = computed(() => {
  if (!learning.value?.staff.length) return null
  const status = learning.value.job?.status
  if (status === 'pending' || status === 'processing') return { text: '学习中', tone: 'busy' }
  if (status === 'failed') return { text: '学习失败', tone: 'bad' }
  if (learning.value.style) return { text: '已学会', tone: 'good' }
  return { text: '未学习', tone: 'idle' }
})
function roleText(row: Member) { return [row.role === 'owner' ? '群主' : row.role === 'admin' ? '管理员' : '', row.isBot ? '机器人' : ''].filter(Boolean).join(' · ') }

onMounted(async () => {
  loading.value = true
  void loadLearning(true)
  try { members.value = await agentApi.members.list(props.conversationId) }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '成员加载失败') }
  finally { loading.value = false }
  if (props.isGroup) void sync(false)
})
onBeforeUnmount(() => { alive = false; stopPolling() })

async function sync(force: boolean) {
  syncing.value = true
  try {
    const result = await agentApi.members.sync(props.conversationId, force)
    if (!alive) return
    members.value = await agentApi.members.list(props.conversationId)
    rosterStatus.value = 'QQ 名单同步于 ' + formatTime(result.syncedAt)
    emit('synced')
  } catch (e) {
    if (alive) rosterStatus.value = (e instanceof Error ? e.message : '同步失败') + '。当前显示本地已知成员。'
  } finally { syncing.value = false }
}
function stopPolling() { if (pollTimer) clearTimeout(pollTimer); pollTimer = undefined }
async function loadLearning(showSpinner = false) {
  if (showSpinner) learningLoading.value = true
  try { learning.value = await agentApi.members.staffLearning(props.conversationId) }
  catch { /* 学习状态加载失败不影响成员列表 */ }
  finally { learningLoading.value = false }
  stopPolling()
  if (jobRunning.value && alive) pollTimer = setTimeout(() => loadLearning(), 3000)
}
async function startLearning() {
  starting.value = true
  try { await agentApi.members.startStaffLearning(props.conversationId); ElMessage.success('已开始学习'); await loadLearning() }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '启动失败') }
  finally { starting.value = false }
}
async function toggle(row: Member) {
  try {
    await agentApi.members.setStaff(props.conversationId, row.userId, row.isStaff)
    ElMessage.success(row.isStaff ? '已标记为官方客服，开始学习他的语气' : '已取消官方客服')
    await loadLearning()
  } catch (e) { row.isStaff = !row.isStaff; ElMessage.error(e instanceof Error ? e.message : '保存失败') }
}
function openCandidates() {
  router.push({ path: '/knowledge', query: { tab: 'candidates', conversationId: props.conversationId, conversationName: props.conversationName } })
}

const blockDialog = ref(false)
const blocking = ref(false)
const blockTarget = ref<Member | null>(null)
const blockForm = reactive({ scope: 'group' as 'group' | 'bot', reason: '', autoKick: true })
async function reloadMembers() { members.value = await agentApi.members.list(props.conversationId) }
function openBlock(row: Member) {
  if (!props.botId) { ElMessage.error('找不到这个群所属的机器人，请刷新后再试'); return }
  blockTarget.value = row
  Object.assign(blockForm, { scope: 'group', reason: '', autoKick: true })
  blockDialog.value = true
}
async function confirmBlock() {
  if (!blockTarget.value || !props.botId) return
  blocking.value = true
  try {
    const res = await agentApi.blacklist.add({ botId: props.botId, conversationId: blockForm.scope === 'group' ? props.conversationId : undefined, userId: blockTarget.value.userId, reason: blockForm.reason.trim() || undefined, autoKick: blockForm.autoKick })
    ElMessage.success(res.autoKick ? '已拉黑，正在尝试踢出群' : '已拉黑')
    blockDialog.value = false
    await reloadMembers()
    void loadLearning()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '拉黑失败') } finally { blocking.value = false }
}
async function unblock(row: Member) {
  const ids = row.blockedUserIds || []
  if (!ids.length) return
  try {
    await ElMessageBox.confirm(`解除对“${row.displayName || row.userId}”的拉黑？${ids.length > 1 ? '他同时在本群和机器人范围的黑名单里，会一起解除。' : ''}已被踢出的人不会自动拉回群。`, '解除拉黑', { confirmButtonText: '解除拉黑', cancelButtonText: '取消', type: 'warning' })
  } catch { return }
  try { await Promise.all(ids.map(id => agentApi.blacklist.remove(id))); ElMessage.success('已解除'); await reloadMembers() }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '解除失败') }
}
async function showMemories(row: Member) {
  memUser.value = row.displayName || row.userId
  mems.value = await agentApi.memories.list({ conversationId: props.conversationId, userId: row.userId })
  memDialog.value = true
}
useAdminEvents(['conversation_members', 'blocked_users'], reloadMembers)
</script>
