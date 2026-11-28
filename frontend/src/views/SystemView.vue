<template>
  <div ref="pageRef" class="content-page">
    <PageHeader title="运维与系统" description="清晰划分实时运维监控与系统参数设置（模型网络代理已迁至“模型与代理”）。">
      <el-segmented v-model="activeTab" :options="tabOptions" @change="syncTab" />
      <el-button :icon="Refresh" :loading="loading" @click="load">刷新状态</el-button>
    </PageHeader>

    <!-- Tab 1: 实时运维监控 -->
    <template v-if="activeTab === 'monitor'">
      <section class="infra-stage">
        <SystemInfra3D :queues="overview.queues" :active-count="overview.active.length" :connection-lost="connection.lost" />
        <div class="infra-hud">
          <span class="holo-badge" :class="{ 'is-alert': connection.lost || overview.queues.failed > 0 }">
            <i /><span>{{ connection.lost ? 'LINK DEGRADED' : overview.queues.failed > 0 ? 'QUEUE ALERT' : 'SYSTEM NOMINAL' }}</span>
          </span>
        </div>
      </section>

      <div class="system-grid">
        <section class="panel cyber-panel sys-card">
          <div class="panel-head"><div><h2>核心服务状态</h2><p>机器人收发消息与知识索引依赖的三项底层服务。</p></div></div>
          <div class="panel-body grow">
            <div class="service-list">
              <div v-for="service in services" :key="service.name">
                <span class="entity-icon small"><el-icon><component :is="service.icon" /></el-icon></span>
                <span><strong>{{ service.name }}</strong><small>{{ service.detail }}</small></span>
                <StatusBadge :status="connection.lost ? 'warning' : service.status" :text="connection.lost ? '未连接' : undefined" />
              </div>
            </div>
          </div>
        </section>

        <section class="panel cyber-panel sys-card">
          <div class="panel-head"><div><h2>任务队列实时负载</h2><p>正在排队的消息、回复、文档索引及异常任务。</p></div></div>
          <div class="panel-body grow">
            <div class="queue-metrics">
              <div><span>待处理消息</span><strong><CountUp :value="overview.queues.inbox" /></strong></div>
              <div><span>待发送回复</span><strong><CountUp :value="overview.queues.outbox" /></strong></div>
              <div><span>待索引文档</span><strong><CountUp :value="overview.queues.knowledge" /></strong></div>
              <div><span>失败任务</span><strong :class="{ 'danger-text': overview.queues.failed }"><CountUp :value="overview.queues.failed" /></strong></div>
            </div>
          </div>
          <div class="panel-foot">
            <span class="note">{{ overview.queues.failed ? '失败任务可重入队列，或忽略后不再计入失败' : '所有队列运行平稳' }}</span>
            <el-button :disabled="!overview.queues.failed" @click="dismissFailed">忽略失败任务</el-button>
            <el-button :disabled="!overview.queues.failed" @click="retryFailed">重试失败任务</el-button>
          </div>
        </section>
      </div>
    </template>

    <!-- Tab 2: 系统参数与安全 -->
    <template v-else>
      <div class="system-grid">
        <section class="panel cyber-panel sys-card">
          <div class="panel-head"><div><h2>公网地址与回调</h2><p>QQ 开放平台回调和临时网页均使用此公网地址。</p></div></div>
          <el-form label-position="top" class="panel-body grow" @submit.prevent>
            <el-form-item label="基础地址"><el-input v-model="settings.publicBaseUrl" placeholder="https://bot.example.com" :disabled="!settingsLoaded" class="mono-input" /></el-form-item>
            <dl class="addr-list">
              <div><dt>机器人回调</dt><dd class="mono">{{ base || '—' }}/callbacks/qq/&lt;机器人 ID&gt;</dd></div>
              <div><dt>临时网页</dt><dd class="mono">{{ base || '—' }}/p/&lt;网页 ID&gt;</dd></div>
            </dl>
          </el-form>
          <div class="panel-foot"><el-button type="primary" :loading="savingBase" :disabled="!settingsLoaded || !baseDirty" @click="saveBase">保存公网地址</el-button></div>
        </section>

        <section class="panel cyber-panel sys-card">
          <div class="panel-head"><div><h2>管理员安全密码</h2><p>修改后，其他已登录控制台的会话会立刻下线。</p></div></div>
          <el-form label-position="top" class="panel-body grow">
            <el-form-item label="当前密码"><el-input v-model="password.current" type="password" show-password autocomplete="current-password" /></el-form-item>
            <div class="form-grid">
              <el-form-item label="新密码" class="last-item"><el-input v-model="password.next" type="password" show-password autocomplete="new-password" placeholder="至少 8 位" /></el-form-item>
              <el-form-item label="再输入一次新密码" class="last-item"><el-input v-model="password.confirm" type="password" show-password autocomplete="new-password" /></el-form-item>
            </div>
          </el-form>
          <div class="panel-foot"><el-button type="primary" plain :disabled="!password.current || !password.next" @click="changePassword">更新密码</el-button></div>
        </section>

        <section class="panel cyber-panel sys-card span-two">
          <div class="panel-head"><div><h2>全局运行参数</h2><p>对所有机器人生效，保存后即时生效，无需重启服务。</p></div></div>
          <el-form label-position="top" class="panel-body grow global-params-grid">
            <el-form-item label="新会话默认带上几条消息">
              <el-input-number v-model="settings.defaultContextLimit" :min="1" :max="100" :disabled="!settingsLoaded" />
              <small class="form-tip">包含当前问题；已有会话可在“群与会话”里单独调整。</small>
            </el-form-item>
            <el-form-item label="AI 请求超时（秒）">
              <el-input-number v-model="settings.aiRequestTimeoutSeconds" :min="10" :max="600" :disabled="!settingsLoaded" />
            </el-form-item>
            <el-form-item label="每人每天生图上限（张）">
              <el-input-number v-model="settings.imageDailyLimitDefault" :min="0" :max="999" :disabled="!settingsLoaded" />
              <small class="form-tip">0 表示不限制；仅限制生图模型画的图。</small>
            </el-form-item>
            <el-form-item label="消息记录保留（天）">
              <el-input-number v-model="settings.messageRetentionDays" :min="1" :max="3650" :disabled="!settingsLoaded" />
              <small class="form-tip">超期记录会在后台自动清理。</small>
            </el-form-item>
          </el-form>
          <div class="panel-foot">
            <span class="note">{{ settingsLoaded ? (settings.updatedAt ? '最近更新：' + formatUpdatedAt(settings.updatedAt) : '') : '参数读取失败，暂时不能修改' }}</span>
            <el-button type="primary" :loading="saving" :disabled="!settingsLoaded || saving" @click="saveSettings">保存全局参数</el-button>
          </div>
        </section>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, markRaw, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Connection, Coin, FolderOpened, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import CountUp from '@/components/fx/CountUp.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import SystemInfra3D from '@/components/three/SystemInfra3D.vue'
import type { SystemOverview, SystemSettings } from '@/types'
import { createEmptyOverview } from '@/utils/overview'
import { connection } from '@/utils/connection'
import { applySystemSettings, createSystemSettingsForm } from '@/utils/system-settings'
import { animateStagger } from '@/utils/motion'

const pageRef = ref<HTMLElement>()
const route = useRoute()
const router = useRouter()
const tabOptions = [
  { label: '实时运维监控', value: 'monitor' },
  { label: '系统参数与安全', value: 'settings' },
]
const activeTab = ref<'monitor' | 'settings'>(route.query.tab === 'settings' ? 'settings' : 'monitor')

watch(() => route.query.tab, (val) => {
  if (val === 'monitor' || val === 'settings') activeTab.value = val
})
function syncTab(val: string | number) {
  router.replace({ query: { ...route.query, tab: String(val) } })
  nextTick(() => animateStagger(pageRef.value))
}

const overview = ref<SystemOverview>(createEmptyOverview())
const settings = reactive<SystemSettings>(createSystemSettingsForm())
const loading = ref(false), saving = ref(false), settingsLoaded = ref(false)
const services = [
  { name: '后端网关服务', detail: '管理端 API 与 QQ 开放平台回调网关', status: 'online', icon: markRaw(Connection) },
  { name: 'PostgreSQL + pgvector', detail: '存储消息流水、跨群记忆与向量知识索引', status: 'online', icon: markRaw(Coin) },
  { name: '本地文件矩阵', detail: 'DATA_DIR 持久化文档、切片缓存与多模态图像', status: 'online', icon: markRaw(FolderOpened) },
]
let savedSettings = JSON.stringify(settings)
const savedBase = ref('')
const base = computed(() => settings.publicBaseUrl.trim().replace(/\/+$/, ''))
const baseDirty = computed(() => settings.publicBaseUrl.trim() !== savedBase.value)
const savingBase = ref(false)

async function saveBase() {
  savingBase.value = true
  try {
    const saved = await api.system.updateSettings({ ...JSON.parse(savedSettings), publicBaseUrl: settings.publicBaseUrl.trim() })
    rememberSettings(saved)
    ElMessage.success('公网地址已保存')
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '保存失败') }
  finally { savingBase.value = false }
}
function rememberSettings(value: SystemSettings) {
  applySystemSettings(settings, value)
  savedSettings = JSON.stringify(settings)
  savedBase.value = settings.publicBaseUrl
}

const password = reactive({ current: '', next: '', confirm: '' })
onMounted(async () => {
  await load()
  nextTick(() => animateStagger(pageRef.value))
})

async function load() {
  loading.value = true
  const [overviewResult, settingsResult] = await Promise.allSettled([api.overview(), api.system.getSettings()])
  if (overviewResult.status === 'fulfilled') overview.value = overviewResult.value
  if (settingsResult.status === 'fulfilled') { rememberSettings(settingsResult.value); settingsLoaded.value = true }
  else {
    settingsLoaded.value = false
    ElMessage.error('系统参数读取失败：' + (settingsResult.reason instanceof Error ? settingsResult.reason.message : ''))
  }
  loading.value = false
}

async function saveSettings() {
  if (!settingsLoaded.value || saving.value) return
  saving.value = true
  try {
    const saved = await api.system.updateSettings({ ...settings, publicBaseUrl: savedBase.value })
    rememberSettings(saved)
    ElMessage.success('全局参数已保存，无需重启')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存系统设置失败')
  } finally { saving.value = false }
}

async function dismissFailed() {
  try { await ElMessageBox.confirm(`把 ${overview.value.queues.failed} 个失败任务标记为已忽略？忽略后不再计入失败、不会被重试，记录仍保留。`, '忽略失败任务', { confirmButtonText: '忽略', cancelButtonText: '取消', type: 'warning' }) } catch { return }
  const result = await api.system.dismissFailed()
  await load()
  ElMessage.success(`已忽略 ${result.dismissed} 个失败任务`)
}
async function retryFailed() {
  const result = await api.system.retryFailed()
  await load()
  ElMessage.success(`已重新入队 ${result.queued} 个任务`)
}
function formatUpdatedAt(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { hour12: false })
}
async function changePassword() {
  if (password.next.length < 8) { ElMessage.error('新密码至少 8 位'); return }
  if (password.next !== password.confirm) { ElMessage.error('两次输入的新密码不一致'); return }
  await api.auth.changePassword(password.current, password.next)
  Object.assign(password, { current: '', next: '', confirm: '' })
  ElMessage.success('管理员密码已更新')
}

useAdminEvents(['inbox_tasks', 'outbox_tasks', 'system_alerts', 'knowledge_documents', 'messages'], async () => { overview.value = await api.overview() })
useAdminEvents(['system_settings'], async () => {
  if (saving.value || JSON.stringify(settings) !== savedSettings) return
  const latest = await api.system.getSettings()
  if (!saving.value && JSON.stringify(settings) === savedSettings) rememberSettings(latest)
})
</script>

<style scoped>
.infra-stage { position: relative; margin-bottom: 20px; }
.infra-hud { position: absolute; left: 14px; top: 12px; z-index: 4; pointer-events: none; }
.system-grid { align-items: stretch; }
.sys-card { display: flex; flex-direction: column; min-width: 0; }
.sys-card .grow { flex: 1; }
.span-two { grid-column: 1 / -1; }
.global-params-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px 24px; }
.sys-card .queue-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); margin-bottom: 0; }
.last-item { margin-bottom: 0; }
.panel-foot .el-button + .el-button { margin-left: 0; }
.addr-list { display: grid; gap: 6px; margin: 0; font-size: 13px; }
.addr-list div { display: grid; grid-template-columns: 84px minmax(0, 1fr); gap: 8px; }
.addr-list dt { color: var(--muted); }
.addr-list dd { margin: 0; overflow-wrap: anywhere; color: var(--text-2); }
@media (max-width: 900px) { .global-params-grid { grid-template-columns: 1fr; } }
</style>
