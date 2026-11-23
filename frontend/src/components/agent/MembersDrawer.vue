<template>
  <el-drawer :model-value="modelValue" :title="`成员 · ${conversationName}`" size="640px" @update:model-value="emit('update:modelValue', $event)">
    <section v-loading="learningLoading" class="learn-card">
      <header class="learn-head">
        <div class="learn-title">
          <span class="learn-icon">🎙️</span>
          <div>
            <strong>客服语气学习</strong>
            <small>AI 会模仿本群客服的说话方式，客服的回答会进入知识审核</small>
          </div>
        </div>
        <span v-if="jobStatus" class="pill" :class="jobStatus.tone">{{ jobStatus.text }}</span>
      </header>

      <template v-if="learning?.staff.length">
        <div class="staff-chips">
          <span v-for="s in learning.staff" :key="s.userId" class="chip">
            {{ s.displayName || s.userId }}<em>{{ s.availableMessages }} 条发言</em>
          </span>
        </div>

        <blockquote v-if="learning.style" class="style-quote">{{ learning.style }}</blockquote>
        <p v-else-if="!jobRunning" class="style-empty">还没有学到语气。{{ learning.job?.status === 'failed' ? '上次学习没成功，可以再试一次。' : '点“开始学习”让 AI 读一遍客服的历史发言。' }}</p>
        <p v-else class="style-empty">正在阅读客服的历史发言，通常一两分钟就好…</p>

        <p v-if="learning.job?.status === 'failed' && learning.job.error" class="learn-error">失败原因：{{ learning.job.error }}</p>

        <footer class="learn-foot">
          <span class="muted">
            <template v-if="learning.learnedAt">参考了 {{ learning.sampleCount }} 条发言 · {{ formatTime(learning.learnedAt) }}</template>
            <template v-else>最多参考最近 200 条客服发言</template>
          </span>
          <div class="learn-actions">
            <el-button link type="primary" @click="openCandidates">查看本群学到的知识 →</el-button>
            <el-button size="small" type="primary" :loading="starting || jobRunning" @click="startLearning">{{ learning.learnedAt ? '重新学习' : '开始学习' }}</el-button>
          </div>
        </footer>
      </template>
      <p v-else class="style-empty">在下面的成员列表里把客服打开“官方客服”，AI 就会自动学习他们的语气和回答。</p>

      <p class="learn-note">只能学习机器人进群后、系统收到并保存的消息。QQ 不提供补拉历史记录的接口，进群前的聊天学不到。</p>
    </section>

    <div v-if="isGroup" class="gap"><el-button :loading="syncing" @click="sync(true)">同步 QQ 群成员</el-button> <small>{{ rosterStatus }}</small></div>
    <el-input v-model="filter" class="gap" placeholder="搜索昵称或 ID" clearable />
    <el-table v-loading="loading" :data="filtered" max-height="480">
      <el-table-column label="成员" min-width="200"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.displayName || '未知昵称' }} {{ row.role === 'owner' ? '· 群主' : row.role === 'admin' ? '· 管理员' : '' }} {{ row.isBot ? '· 机器人' : '' }}</strong><span>{{ row.userId }}</span></div></template></el-table-column>
      <el-table-column prop="messageCount" label="消息" width="70" />
      <el-table-column label="记忆" width="70"><template #default="{ row }"><el-button v-if="Number(row.memoryCount) > 0" link type="primary" @click="showMemories(row)">{{ row.memoryCount }}</el-button><span v-else>0</span></template></el-table-column>
      <el-table-column label="官方客服" width="90"><template #default="{ row }"><el-switch v-model="row.isStaff" :disabled="row.isBlocked" @change="toggle(row)" /></template></el-table-column>
      <el-table-column label="黑名单" width="110"><template #default="{ row }">
        <div v-if="row.isBlocked" class="blocked-cell"><el-tag size="small" type="danger" effect="light" round>已拉黑</el-tag><el-button link type="primary" size="small" @click="unblock(row)">解除</el-button></div>
        <el-button v-else link type="danger" @click="openBlock(row)">拉黑</el-button>
      </template></el-table-column>
    </el-table>
    <el-dialog v-model="blockDialog" :title="`拉黑 · ${blockTarget?.displayName || blockTarget?.userId || ''}`" width="460px" append-to-body>
      <el-form label-position="top">
        <el-form-item label="拉黑范围">
          <el-radio-group v-model="blockForm.scope">
            <el-radio value="group">只在本群</el-radio>
            <el-radio value="bot">{{ botName || '这个机器人' }} 所在的所有群</el-radio>
          </el-radio-group>
        </el-form-item>
        <el-form-item label="原因（可选）"><el-input v-model="blockForm.reason" type="textarea" :rows="2" maxlength="1000" placeholder="例如：发广告、骚扰其他群友" /></el-form-item>
        <el-form-item>
          <div class="kick-row"><el-switch v-model="blockForm.autoKick" /><div><strong>发现时自动踢出群</strong><small>需要群主给机器人开启“移除群成员”权限，没权限时会提醒你手动踢</small></div></div>
        </el-form-item>
      </el-form>
      <p class="muted">拉黑后，机器人不再回复他，他的发言也不会进入上下文、记忆和学习。</p>
      <template #footer><el-button @click="blockDialog = false">取消</el-button><el-button type="danger" :loading="blocking" @click="confirmBlock">拉黑</el-button></template>
    </el-dialog>
    <el-dialog v-model="memDialog" :title="`记忆 · ${memUser}`" width="520px" append-to-body>
      <div v-for="m in mems" :key="m.id" class="content-box gap">{{ m.content }}<br /><small class="muted">{{ formatTime(m.updatedAt) }}</small></div>
    </el-dialog>
  </el-drawer>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { agentApi, formatTime, type Member, type Memory, type StaffLearning } from '@/api/agent'

const props = defineProps<{ modelValue: boolean; isGroup?:boolean; conversationId: string; conversationName: string; botId?: string; botName?: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean]; synced: [] }>()
const router = useRouter()
const members = ref<Member[]>([])
const syncing=ref(false)
const rosterStatus=ref('')
let version=0
async function sync(force:boolean){
  const current=version
  const id=props.conversationId
  syncing.value=true
  try{
    const result=await agentApi.members.sync(id,force)
    if(current!==version)return
    const list=await agentApi.members.list(id)
    if(current!==version)return
    members.value=list
    rosterStatus.value='QQ 名单同步于 '+formatTime(result.syncedAt)
    emit('synced')
  }catch(e){if(current===version)rosterStatus.value=(e instanceof Error?e.message:'同步失败')+'。当前显示本地已知成员。'}
  finally{if(current===version)syncing.value=false}
}
const loading = ref(false)
const filter = ref('')
const memDialog = ref(false)
const memUser = ref('')
const mems = ref<Memory[]>([])
const learning = ref<StaffLearning | null>(null)
const learningLoading = ref(false)
const starting = ref(false)
let pollTimer: ReturnType<typeof setTimeout> | undefined

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

watch(() => [props.modelValue, props.conversationId], async () => {
  const current=++version
  syncing.value=false; rosterStatus.value=''; members.value=[]
  stopPolling()
  if (!props.modelValue || !props.conversationId) return
  loading.value = true
  learning.value = null
  loadLearning(true)
  try { const list=await agentApi.members.list(props.conversationId);if(current===version)members.value=list } catch (e) { if(current===version)ElMessage.error(e instanceof Error ? e.message : '加载失败') } finally { if(current===version) loading.value = false }
 if(current===version && props.isGroup)void sync(false)
}, { immediate: true })
onBeforeUnmount(stopPolling)

function stopPolling() { if (pollTimer) clearTimeout(pollTimer); pollTimer = undefined }
async function loadLearning(showSpinner = false) {
  const id = props.conversationId
  if (showSpinner) learningLoading.value = true
  try {
    const data = await agentApi.members.staffLearning(id)
    if (id !== props.conversationId) return
    learning.value = data
  } catch {
    // 学习状态加载失败不影响成员列表
  } finally {
    learningLoading.value = false
  }
  stopPolling()
  if (jobRunning.value && props.modelValue) pollTimer = setTimeout(() => loadLearning(), 3000)
}
async function startLearning() {
  starting.value = true
  try { await agentApi.members.startStaffLearning(props.conversationId); ElMessage.success('已开始学习'); await loadLearning() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '启动失败') } finally { starting.value = false }
}
async function toggle(row: Member) {
  try {
    await agentApi.members.setStaff(props.conversationId, row.userId, row.isStaff)
    ElMessage.success(row.isStaff ? '已标记为官方客服，开始学习他的语气' : '已取消')
    await loadLearning()
  } catch (e) { row.isStaff = !row.isStaff; ElMessage.error(e instanceof Error ? e.message : '保存失败') }
}
function openCandidates() {
  emit('update:modelValue', false)
  router.push({ path: '/learning', query: { conversationId: props.conversationId, conversationName: props.conversationName } })
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
    loadLearning()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '拉黑失败') } finally { blocking.value = false }
}
async function unblock(row: Member) {
  const ids = row.blockedUserIds || []
  if (!ids.length) return
  try {
    await ElMessageBox.confirm(`解除对“${row.displayName || row.userId}”的拉黑？${ids.length > 1 ? '他同时在本群和机器人范围的黑名单里，会一起解除。' : ''}已被踢出的人不会自动拉回群。`, '解除拉黑', { confirmButtonText: '解除', cancelButtonText: '取消', type: 'warning' })
  } catch { return }
  try { await Promise.all(ids.map(id => agentApi.blacklist.remove(id))); ElMessage.success('已解除'); await reloadMembers() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '解除失败') }
}
async function showMemories(row: Member) { memUser.value = row.displayName || row.userId; mems.value = await agentApi.memories.list({ userId: row.userId }); memDialog.value = true }
useAdminEvents(['conversation_members','blocked_users'], async () => { if (props.modelValue && props.conversationId) await reloadMembers() })
</script>

<style scoped>
.gap { margin: 12px 0; }
.muted { color: var(--muted); font-size: 12px; }
.learn-card { padding: 16px 18px; border: 1px solid var(--line); border-radius: var(--radius); background: linear-gradient(180deg, var(--primary-light), var(--surface) 70%); box-shadow: var(--shadow); }
.learn-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 12px; }
.learn-title { display: flex; gap: 10px; align-items: flex-start; }
.learn-title strong { display: block; font-family: var(--font-round); font-size: 15px; color: var(--text); }
.learn-title small { color: var(--muted); font-size: 12px; }
.learn-icon { display: grid; place-items: center; width: 34px; height: 34px; flex: none; border-radius: 12px; background: var(--surface); box-shadow: var(--shadow); font-size: 17px; }
.pill { flex: none; padding: 3px 10px; border-radius: 999px; font-size: 12px; font-weight: 600; }
.pill.good { background: var(--success-light); color: var(--success); }
.pill.busy { background: var(--sky-light); color: var(--sky); }
.pill.bad { background: var(--danger-light); color: var(--danger); }
.pill.idle { background: var(--info-light); color: var(--muted); }
.staff-chips { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 14px; }
.chip { display: inline-flex; align-items: center; gap: 6px; padding: 4px 10px; border-radius: 999px; background: var(--surface); border: 1px solid var(--line); font-size: 13px; color: var(--text); }
.chip em { font-style: normal; color: var(--faint); font-size: 12px; }
.style-quote { margin: 12px 0 0; padding: 12px 14px; border-left: 3px solid var(--primary); border-radius: var(--radius-sm); background: var(--surface); color: var(--text); font-size: 13px; line-height: 1.7; white-space: pre-wrap; }
.style-empty { margin: 12px 0 0; color: var(--muted); font-size: 13px; }
.learn-error { margin: 8px 0 0; color: var(--danger); font-size: 12px; }
.learn-foot { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 12px; flex-wrap: wrap; }
.learn-actions { display: flex; align-items: center; gap: 12px; }
.blocked-cell { display: flex; align-items: center; gap: 4px; }
.kick-row { display: flex; gap: 10px; align-items: flex-start; }
.kick-row strong { display: block; font-size: 13px; color: var(--text); line-height: 1.4; }
.kick-row small { display: block; color: var(--muted); font-size: 12px; line-height: 1.5; }
.learn-note { margin: 12px 0 0; padding-top: 10px; border-top: 1px dashed var(--line-strong); color: var(--faint); font-size: 12px; line-height: 1.6; }
</style>
