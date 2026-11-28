<template>
  <div ref="pageRef" class="content-page page-fill">
    <section class="holo-hero">
      <HoloBanner3D variant="ops" class="holo-hero-bg" :alert="failedKickCount > 0" />
      <div class="holo-hero-scrim" aria-hidden="true" />
      <div class="holo-hero-content">
        <div class="holo-hero-title-row">
          <span class="holo-badge" :class="{ 'is-alert': failedKickCount > 0 }"><i /><span>COMMUNITY OPS & GUARD</span></span>
          <h1>群运营与风控</h1>
        </div>
        <p class="holo-hero-desc">统一管理 QQ 群每日定时播报任务与社群黑名单拦截规则，保障群内自动化节奏与环境清朗。</p>
        <div class="holo-kpi-row">
          <span class="holo-kpi is-cyan">定时任务 <strong>{{ tasks.length }}</strong></span>
          <span class="holo-kpi is-green">已启用 <strong>{{ tasks.filter(t => t.enabled).length }}</strong></span>
          <span class="holo-kpi is-warn">黑名单人数 <strong>{{ blacklist.length }}</strong></span>
          <span v-if="failedKickCount" class="holo-kpi is-danger">踢出失败预警 <strong>{{ failedKickCount }}</strong></span>
        </div>
      </div>
      <div class="holo-hero-actions">
        <el-segmented v-model="activeTab" :options="tabOptions" @change="syncTab" />
        <el-button v-if="activeTab === 'schedules'" type="primary" :icon="Plus" @click="editTask()">新建定时任务</el-button>
        <el-button v-else :icon="Refresh" :loading="blLoading" @click="loadBlacklist">刷新风控列表</el-button>
      </div>
    </section>

    <!-- Tab 1: 定时播报任务 -->
    <section v-if="activeTab === 'schedules'" class="table-panel">
      <el-table v-loading="schedLoading" :data="tasks" height="100%" class="clickable-rows" @row-click="editTask">
        <template #empty>
          <EmptyState v-if="schedError" mood="thinking" title="加载失败" :description="schedError"><el-button @click="loadSchedules">重新加载</el-button></EmptyState>
          <EmptyState v-else title="还没有定时任务" description="例如每天早上 8 点在群里发天气，或者发前一天的群统计。"><el-button type="primary" :icon="Plus" @click="editTask()">新建定时任务</el-button></EmptyState>
        </template>
        <el-table-column label="任务" min-width="240"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.name }}</strong><span>{{ row.instruction }}</span></div></template></el-table-column>
        <el-table-column label="每天" width="90"><template #default="{ row }"><span class="num">{{ row.sendTime }}</span></template></el-table-column>
        <el-table-column label="内容 / 格式" width="150"><template #default="{ row }"><div class="stacked-cell"><strong>{{ sources[row.source] || row.source }}</strong><span>{{ formats[row.format] || row.format }}</span></div></template></el-table-column>
        <el-table-column label="目标群" min-width="180"><template #default="{ row }"><span class="truncate">{{ row.allGroups ? '全部群' : row.groupIds.map(groupName).join('、') || '目标群已移除' }}</span></template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }"><StatusBadge :status="row.enabled ? 'online' : 'offline'" :text="row.enabled ? '已启用' : '已暂停'" /></template></el-table-column>
        <el-table-column label="操作" width="220" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click.stop="openTest(row)">测试发送</el-button>
            <el-button link @click.stop="showRuns(row)">执行记录</el-button>
            <el-button link type="danger" @click.stop="removeTask(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </section>

    <!-- Tab 2: 黑名单风控 -->
    <template v-else>
      <div class="toolbar">
        <el-input v-model="search" placeholder="搜索昵称、成员 ID 或原因" :prefix-icon="Search" clearable />
        <el-segmented v-model="statusFilter" :options="statusOptions" />
      </div>
      <section class="table-panel">
        <el-table v-loading="blLoading" :data="filteredBlacklist" row-key="id" height="100%">
          <template #empty>
            <EmptyState mood="happy" title="黑名单是空的" description="群里有人捣乱时，打开“群与会话”里的这个群，在“成员”里点“拉黑”。" />
          </template>
          <el-table-column label="成员" min-width="200">
            <template #default="{ row }"><div class="stacked-cell"><strong>{{ row.displayName || '未知昵称' }}</strong><span class="mono">{{ row.userId }}</span></div></template>
          </el-table-column>
          <el-table-column label="范围" min-width="170">
            <template #default="{ row }">
              <div class="stacked-cell">
                <strong>{{ row.conversationId ? conversationName(row.conversationId) : '机器人所在的所有群' }}</strong>
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
          <el-table-column label="操作" width="140" fixed="right">
            <template #default="{ row }">
              <el-button v-if="row.autoKick && row.actions.some((a: BlockedAction) => a.status === 'failed')" link type="primary" @click="retryKick(row)">重试</el-button>
              <el-button link type="danger" @click="removeBlocked(row)">解除拉黑</el-button>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </template>

    <!-- 定时任务抽屉 & 弹窗 -->
    <el-drawer v-model="editing" :title="form.id ? '编辑定时任务' : '新建定时任务'" size="min(600px, 96vw)" append-to-body>
      <el-form label-position="top" @submit.prevent="saveTask">
        <div class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>基础与调度</strong><span>按北京时间每日定时触发</span></div>
            <div class="form-grid">
              <el-form-item label="任务名称"><el-input v-model="form.name" maxlength="120" placeholder="例如：每日天气预告" /></el-form-item>
              <el-form-item label="每天发送时间（北京时间）"><el-time-picker v-model="form.sendTime" format="HH:mm" value-format="HH:mm" placeholder="选择时间" :clearable="false" style="width: 100%" /></el-form-item>
            </div>
            <el-form-item label="发送到">
              <el-radio-group v-model="form.allGroups"><el-radio-button :value="false">指定群</el-radio-button><el-radio-button :value="true">全部群</el-radio-button></el-radio-group>
              <el-select v-if="!form.allGroups" v-model="form.groupIds" multiple filterable placeholder="选择一个或多个群" style="margin-top: 8px"><el-option v-for="g in groups" :key="g.id" :value="g.id" :label="groupName(g.id)" /></el-select>
              <small v-else class="form-tip">执行时包含所有已启用机器人所接待的已启用群。</small>
            </el-form-item>
          </div>

          <div class="dlg-section">
            <div class="dlg-section-head"><strong>内容生成策略</strong><span>选择信息来源与创作指令</span></div>
            <el-form-item label="内容来源">
              <el-radio-group v-model="form.source"><el-radio-button value="web">联网查询</el-radio-button><el-radio-button value="group">昨日群统计</el-radio-button><el-radio-button value="instruction">按指令创作</el-radio-button></el-radio-group>
            </el-form-item>
            <el-form-item label="内容指令"><el-input v-model="form.instruction" type="textarea" :rows="4" maxlength="4000" placeholder="查询当天杭州天气，发布气温、降雨概率和出行提醒。" /></el-form-item>
          </div>

          <div class="dlg-section">
            <div class="dlg-section-head"><strong>发送格式与开关</strong><span>控制消息表现形式</span></div>
            <el-form-item label="发送格式">
              <el-radio-group v-model="form.format"><el-radio-button value="text">文字</el-radio-button><el-radio-button value="image">网页截图</el-radio-button><el-radio-button value="link">网页链接</el-radio-button></el-radio-group>
            </el-form-item>
            <div class="setting-row" style="padding-bottom: 0">
              <div><strong>启用定时执行</strong><span>关闭后保留任务，但不再每天发送</span></div>
              <el-switch v-model="form.enabled" />
            </div>
          </div>
        </div>
      </el-form>
      <template #footer>
        <el-button @click="editing = false">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="!form.name.trim() || !form.sendTime || !form.instruction.trim() || (!form.allGroups && !form.groupIds.length)" @click="saveTask">保存任务</el-button>
      </template>
    </el-drawer>

    <el-dialog v-model="testing" title="测试发送" width="min(520px, 95vw)" append-to-body>
      <div class="dlg-stack">
        <div class="dlg-tip-box">立即执行「{{ active?.name }}」，结果会真实发送到下面所选的群，不影响当天正常定时触发。</div>
        <div class="dlg-section">
          <div class="dlg-section-head"><strong>接收测试消息的群</strong><span>仅列出可用且在任务范围内的群</span></div>
          <el-select v-model="testGroups" multiple filterable placeholder="选择接收测试消息的群" style="width: 100%"><el-option v-for="g in testOptions" :key="g.id" :value="g.id" :label="groupName(g.id)" /></el-select>
        </div>
      </div>
      <template #footer><el-button @click="testing = false">取消</el-button><el-button type="primary" :disabled="!testGroups.length" :loading="sending" @click="sendTest">发送到 {{ testGroups.length }} 个群</el-button></template>
    </el-dialog>

    <el-drawer v-model="history" :title="(active?.name || '') + ' 的执行记录'" size="min(720px, 96vw)" append-to-body>
      <el-table :data="runs" empty-text="还没有执行记录">
        <el-table-column label="开始时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        <el-table-column label="类型" width="70"><template #default="{ row }">{{ row.isTest ? '测试' : '定时' }}</template></el-table-column>
        <el-table-column label="目标群" min-width="160"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.groupName }}</strong><span>{{ row.botName }}</span></div></template></el-table-column>
        <el-table-column label="结果" min-width="140"><template #default="{ row }"><div class="stacked-cell"><StatusBadge :status="runTone(row.status)" :text="statuses[row.status] || row.status" /><span v-if="row.error" class="danger-text">{{ row.error }}</span></div></template></el-table-column>
      </el-table>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import { request } from '@/api/client'
import { agentApi, formatTime, type BlockedAction, type BlockedUser } from '@/api/agent'
import { useAdminEvents } from '@/composables/useAdminEvents'
import EmptyState from '@/components/EmptyState.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import HoloBanner3D from '@/components/three/HoloBanner3D.vue'
import { animateStagger } from '@/utils/motion'
import type { Conversation } from '@/types'

const pageRef = ref<HTMLElement>()
const route = useRoute()
const router = useRouter()
const tabOptions = [{ label: '定时播报', value: 'schedules' }, { label: '黑名单风控', value: 'blacklist' }]
const activeTab = ref<'schedules' | 'blacklist'>(route.query.tab === 'blacklist' ? 'blacklist' : 'schedules')

watch(() => route.query.tab, (v) => {
  if (v === 'schedules' || v === 'blacklist') activeTab.value = v
})
function syncTab(v: string | number) {
  router.replace({ query: { ...route.query, tab: String(v) } })
  nextTick(() => animateStagger(pageRef.value))
}

// 定时任务逻辑
const blank = () => ({ id: '', name: '', sendTime: '08:00', instruction: '', source: 'web', format: 'text', allGroups: false, groupIds: [] as string[], enabled: true })
type Task = ReturnType<typeof blank>
type Delivery = { createdAt: string; isTest: boolean; groupName: string; botName: string; status: string; error: string }
const tasks = ref<Task[]>([]), groups = ref<Conversation[]>([]), runs = ref<Delivery[]>([])
const active = ref<Task>(), form = ref(blank()), testGroups = ref<string[]>([])
const schedLoading = ref(true), schedError = ref(''), editing = ref(false), saving = ref(false), testing = ref(false), sending = ref(false), history = ref(false)
const sources: Record<string, string> = { web: '联网查询', group: '昨日群统计', instruction: '按指令创作' }
const formats: Record<string, string> = { text: '文字', image: '网页截图', link: '网页链接' }
const runTone = (s: string) => s === 'sent' || s === 'completed' ? 'success' : s === 'failed' || s === 'expired' ? 'failed' : s === 'cancelled' ? 'offline' : 'processing'
const statuses: Record<string, string> = { pending: '等待处理', processing: '执行中', completed: '生成完成', sent: '已发送', expired: '已过期', failed: '失败', cancelled: '已取消' }
const testOptions = computed(() => groups.value.filter(g => g.enabled && (active.value?.allGroups || active.value?.groupIds.includes(g.id))))
const groupName = (id: string) => { const g = groups.value.find(x => x.id === id); return g ? `${g.name} · ${g.botName}` : id }
const explain = (e: unknown) => e instanceof Error ? e.message : '请求失败'

async function loadSchedules() {
  schedLoading.value = true
  try {
    const [items, convs] = await Promise.all([request<Task[]>({ url: '/schedules' }), api.conversations.list()])
    tasks.value = items; groups.value = convs.filter(g => g.type === 'group'); schedError.value = ''
  } catch (e) { schedError.value = explain(e) }
  finally { schedLoading.value = false }
}
function editTask(row?: Task) { form.value = row ? { ...row, groupIds: [...row.groupIds] } : blank(); editing.value = true }
async function saveTask() {
  if (saving.value) return; saving.value = true
  try { const t = form.value; await request({ url: t.id ? '/schedules/' + t.id : '/schedules', method: t.id ? 'PUT' : 'POST', data: t }); editing.value = false; await loadSchedules(); ElMessage.success('已保存') }
  catch (e) { ElMessage.error(explain(e)) } finally { saving.value = false }
}
async function removeTask(t: Task) {
  try { await ElMessageBox.confirm('删除“' + t.name + '”及其执行记录？删除后不能恢复。', '删除定时任务', { type: 'warning', confirmButtonText: '删除任务', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' }) } catch { return }
  try { await request({ url: '/schedules/' + t.id, method: 'DELETE' }); await loadSchedules() } catch (e) { ElMessage.error(explain(e)) }
}
let requestId = ''
function newRequestId() {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  const bytes = crypto.getRandomValues(new Uint8Array(16))
  bytes[6] = (bytes[6] & 15) | 64; bytes[8] = (bytes[8] & 63) | 128
  const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('')
  return [hex.slice(0, 8), hex.slice(8, 12), hex.slice(12, 16), hex.slice(16, 20), hex.slice(20)].join('-')
}
function openTest(t: Task) { active.value = t; testGroups.value = []; requestId = ''; testing.value = true }
async function sendTest() {
  if (!active.value || sending.value) return; sending.value = true
  try { if (!requestId) requestId = newRequestId(); await request({ url: '/schedules/' + active.value.id + '/test', method: 'POST', data: { groupIds: testGroups.value, requestId } }); testing.value = false; ElMessage.success('测试已提交'); await showRuns(active.value) }
  catch (e) { ElMessage.error(explain(e)) } finally { sending.value = false }
}
async function loadRuns() {
  const id = active.value?.id; if (!history.value || !id) return
  try { const result = await request<Delivery[]>({ url: '/schedules/' + id + '/runs' }); if (active.value?.id === id) runs.value = result }
  catch (e) { ElMessage.error(explain(e)) }
}
async function showRuns(t: Task) { active.value = t; runs.value = []; history.value = true; await loadRuns() }

// 黑名单风控逻辑
const statusText: Record<string, string> = { pending: '正在踢出', kicked: '已踢出', failed: '踢出失败', disabled: '已停用' }
const statusOptions = [{ label: '全部', value: 'all' }, { label: '踢出失败', value: 'failed' }, { label: '已踢出', value: 'kicked' }]
const blacklist = ref<BlockedUser[]>([])
const conversations = ref<Conversation[]>([])
const blLoading = ref(false), search = ref(''), statusFilter = ref('all')

const failedKickCount = computed(() => blacklist.value.filter(u => u.autoKick && u.actions.some(a => a.status === 'failed')).length)
const filteredBlacklist = computed(() => blacklist.value.filter(item => {
  const text = `${item.displayName}${item.userId}${item.reason}`
  if (search.value && !text.includes(search.value)) return false
  if (statusFilter.value !== 'all' && !item.actions.some(a => a.status === statusFilter.value)) return false
  return true
}))
function conversationName(id: string) { return conversations.value.find(c => c.id === id)?.name || '未知群' }

async function loadBlacklist() {
  blLoading.value = true
  try {
    const [list, convs] = await Promise.all([agentApi.blacklist.list(), api.conversations.list().catch(() => [])])
    blacklist.value = list; conversations.value = convs
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '加载失败') }
  finally { blLoading.value = false }
}
async function retryKick(row: BlockedUser) {
  try { await agentApi.blacklist.add({ botId: row.botId, conversationId: row.conversationId || undefined, userId: row.userId, reason: row.reason || undefined, autoKick: true }); ElMessage.success('已重新排队踢出'); await loadBlacklist() }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '重试失败') }
}
async function removeBlocked(row: BlockedUser) {
  try { await ElMessageBox.confirm(`解除对“${row.displayName || row.userId}”的拉黑？已被踢出的人不会自动拉回群。`, '解除拉黑', { confirmButtonText: '解除拉黑', cancelButtonText: '取消', type: 'warning' }) } catch { return }
  try { await agentApi.blacklist.remove(row.id); ElMessage.success('已解除'); await loadBlacklist() }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '解除失败') }
}

onMounted(async () => {
  await Promise.all([loadSchedules(), loadBlacklist()])
  nextTick(() => animateStagger(pageRef.value))
})
useAdminEvents(['scheduled_posts', 'conversations', 'bots'], loadSchedules)
useAdminEvents(['schedule_runs', 'schedule_deliveries', 'agent_jobs', 'outbox_tasks', 'scheduled_posts'], loadRuns)
useAdminEvents(['blacklist_actions', 'blocked_users'], loadBlacklist)
</script>

<style scoped>
.action-list { display: flex; flex-wrap: wrap; gap: 6px; }
.kick-pill { display: inline-flex; align-items: center; gap: 6px; padding: 2px 8px; border-radius: var(--radius-xs); font-size: 12px; font-weight: 500; cursor: default; }
.kick-pill i { width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
.kick-pill em { font-style: normal; font-weight: 400; }
.kick-pill.pending { background: var(--primary-light); color: var(--primary-text); }
.kick-pill.kicked { background: var(--success-light); color: var(--success); }
.kick-pill.failed { background: var(--danger-light); color: var(--danger); cursor: help; }
.kick-pill.disabled { background: var(--surface-3); color: var(--muted); }
</style>
