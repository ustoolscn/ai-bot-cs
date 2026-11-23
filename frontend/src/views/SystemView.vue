<template>
  <div class="content-page">
    <PageHeader title="系统状态"><el-button :icon="Refresh" :loading="loading" @click="load">刷新状态</el-button></PageHeader>
    <div class="system-grid">
      <section class="settings-panel">
        <h2>服务状态</h2>
        <div class="service-list"><div v-for="service in services" :key="service.name"><span class="entity-icon small"><el-icon><component :is="service.icon" /></el-icon></span><span><strong>{{ service.name }}</strong><small>{{ service.detail }}</small></span><StatusBadge :status="connection.lost ? 'warning' : service.status" :text="connection.lost ? '未连接' : undefined" /></div></div>
      </section>
      <section class="settings-panel">
        <div class="panel-title"><h2>任务队列</h2><el-button size="small" :disabled="!overview.queues.failed" @click="retryFailed">重试失败任务</el-button></div>
        <div class="queue-metrics" style="grid-template-columns: repeat(2, 1fr)"><div><span>待处理消息</span><strong>{{ overview.queues.inbox }}</strong></div><div><span>待发送回复</span><strong>{{ overview.queues.outbox }}</strong></div><div><span>待索引文档</span><strong>{{ overview.queues.knowledge }}</strong></div><div><span>失败任务</span><strong :class="{ 'danger-text': overview.queues.failed }">{{ overview.queues.failed }}</strong></div></div>
        
      </section>
      <section class="settings-panel">
        <h2>公网地址</h2>
        <el-form label-position="top" @submit.prevent>
          <el-form-item label="基础地址"><el-input v-model="settings.publicBaseUrl" placeholder="https://bot.example.com" :disabled="!settingsLoaded" class="mono-input" /></el-form-item>
          <dl class="addr-list">
            <div><dt>机器人回调</dt><dd class="mono">{{ base || '—' }}/callbacks/qq/&lt;机器人 ID&gt;</dd></div>
            <div><dt>临时网页</dt><dd class="mono">{{ base || '—' }}/p/&lt;网页 ID&gt;</dd></div>
          </dl>
          <el-button type="primary" :loading="savingBase" :disabled="!settingsLoaded || !baseDirty" @click="saveBase">保存公网地址</el-button>
        </el-form>
      </section>
      <section class="settings-panel">
        <div class="panel-title"><div class="panel-title-text"><h2>模型代理</h2><p class="panel-sub">海外模型直连访问不了时，在这里添加代理，再到“模型”里为它选择。</p></div><el-button size="small" :icon="Plus" @click="openProxy()">添加代理</el-button></div>
        <div v-if="proxies.length" class="proxy-list">
          <div v-for="p in proxies" :key="p.id" class="proxy-row">
            <span class="proxy-name"><strong>{{ p.name }}</strong><small class="mono" :title="p.address">{{ p.address }}</small></span>
            <span class="proxy-result" :title="proxyTests[p.id]?.error || ''"><StatusBadge v-if="proxyTests[p.id]" v-bind="proxyBadge(p.id)" /></span>
            <span class="proxy-actions">
              <el-button size="small" :loading="proxyTests[p.id]?.testing" @click="testProxy(p)">测试连通</el-button>
              <el-button size="small" link type="primary" @click="openProxy(p)">编辑</el-button>
              <el-button size="small" link type="danger" @click="removeProxy(p)">删除</el-button>
            </span>
          </div>
        </div>
        <p v-else class="muted proxy-empty">{{ proxiesLoaded ? '还没有代理。所有模型都直连访问。' : '代理列表读取失败，点右上角“刷新状态”重试。' }}</p>
      </section>
      <section class="settings-panel">
        <h2>全局参数</h2>

        <el-form label-position="top">
          <el-form-item label="新会话默认带上几条消息"><el-input-number v-model="settings.defaultContextLimit" :min="1" :max="100" :disabled="!settingsLoaded" /><small class="form-tip">包含当前问题；只影响之后新出现的会话，已有会话在“群与会话”里单独调整。</small></el-form-item>
          <el-form-item label="AI 请求超时"><el-input-number v-model="settings.aiRequestTimeoutSeconds" :min="10" :max="600" :disabled="!settingsLoaded" /><span class="unit-label">秒</span><small class="form-tip" style="width: 100%">保存后立即生效。</small></el-form-item>
          <el-form-item label="消息记录保留"><el-input-number v-model="settings.messageRetentionDays" :min="1" :max="3650" :disabled="!settingsLoaded" /><span class="unit-label">天</span><small class="form-tip" style="width: 100%">超过天数的消息约 1 分钟内自动清理。</small></el-form-item>
          <div style="display: flex; align-items: center; gap: 12px"><el-button type="primary" :loading="saving" :disabled="!settingsLoaded || saving" @click="saveSettings">保存全局参数</el-button><small class="muted">{{ settingsLoaded ? (settings.updatedAt ? '最近更新：' + formatUpdatedAt(settings.updatedAt) : '') : '参数读取失败，暂时不能修改' }}</small></div>
        </el-form>
      </section>
      <section class="settings-panel">
        <h2>管理员密码</h2>
        <p class="muted" style="font-size: 12px; margin: -6px 0 12px">修改后，其他已登录的地方会被退出。</p>
        <el-form label-position="top">
          <el-form-item label="当前密码"><el-input v-model="password.current" type="password" show-password autocomplete="current-password" /></el-form-item>
          <el-form-item label="新密码"><el-input v-model="password.next" type="password" show-password autocomplete="new-password" placeholder="至少 8 位" /></el-form-item>
          <el-form-item label="再输入一次新密码"><el-input v-model="password.confirm" type="password" show-password autocomplete="new-password" /></el-form-item>
          <el-button :disabled="!password.current || !password.next" @click="changePassword">更新密码</el-button>
        </el-form>
      </section>
    </div>
    <el-dialog v-model="proxyDialog" :title="proxyForm.id ? '编辑代理' : '添加代理'" width="480px">
      <el-form label-position="top" @submit.prevent>
        <el-form-item label="名称"><el-input v-model="proxyForm.name" placeholder="例如：香港线路" maxlength="40" /></el-form-item>
        <el-form-item label="代理地址"><el-input v-model="proxyForm.address" class="mono-input" placeholder="socks5://127.0.0.1:1080" /><small class="form-tip">支持 http://、https://、socks5://，需要包含端口；暂不支持账号密码。</small></el-form-item>
      </el-form>
      <template #footer><el-button @click="proxyDialog = false">取消</el-button><el-button type="primary" :loading="proxySaving" :disabled="!proxyForm.name.trim() || !proxyForm.address.trim()" @click="saveProxy">保存代理</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, markRaw, onMounted, reactive, ref } from 'vue'
import { Connection, Coin, FolderOpened, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { ApiError } from '@/api/client'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import type { ModelProxy, ProxyTestResult, SystemOverview, SystemSettings } from '@/types'
import { createEmptyOverview } from '@/utils/overview'
import { connection } from '@/utils/connection'
import { applySystemSettings, createSystemSettingsForm } from '@/utils/system-settings'

const overview=ref<SystemOverview>(createEmptyOverview()),settings=reactive<SystemSettings>(createSystemSettingsForm()),loading=ref(false),saving=ref(false),settingsLoaded=ref(false),settingsError=ref('')
const services=[{name:'后端服务',detail:'管理端和 QQ 回调的接口',status:'online',icon:markRaw(Connection)},{name:'数据库',detail:'PostgreSQL + pgvector，保存消息和知识索引',status:'online',icon:markRaw(Coin)},{name:'文件存储',detail:'本地存储（DATA_DIR），支持 .txt、.md',status:'online',icon:markRaw(FolderOpened)}]
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
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '保存失败') } finally { savingBase.value = false }
}
function rememberSettings(value: SystemSettings) {
  applySystemSettings(settings, value)
  savedSettings = JSON.stringify(settings)
  savedBase.value = settings.publicBaseUrl
}

const proxies = ref<ModelProxy[]>([]), proxiesLoaded = ref(true), proxyDialog = ref(false), proxySaving = ref(false)
const proxyForm = reactive({ id: '', name: '', address: '' })
const proxyTests = reactive<Record<string, Partial<ProxyTestResult> & { testing?: boolean }>>({})
async function loadProxies() {
  try { proxies.value = await api.system.proxies.list(); proxiesLoaded.value = true }
  catch (error) { proxiesLoaded.value = false; ElMessage.error('代理列表读取失败：' + (error instanceof Error ? error.message : '')) }
}
function openProxy(p?: ModelProxy) { Object.assign(proxyForm, p ? { id: p.id, name: p.name, address: p.address } : { id: '', name: '', address: '' }); proxyDialog.value = true }
async function saveProxy() {
  proxySaving.value = true
  try {
    await api.system.proxies.save({ id: proxyForm.id || undefined, name: proxyForm.name.trim(), address: proxyForm.address.trim() })
    if (proxyForm.id) delete proxyTests[proxyForm.id]
    proxyDialog.value = false
    await loadProxies()
    ElMessage.success('代理已保存')
  } catch (error) { ElMessage.error(error instanceof Error ? error.message : '保存代理失败') } finally { proxySaving.value = false }
}
async function testProxy(p: ModelProxy) {
  proxyTests[p.id] = { testing: true }
  try {
    const result = await api.system.proxies.test(p.id)
    proxyTests[p.id] = result
    if (!result.reachable) ElMessage.error(`${p.name} 连不通：${result.error || '没有返回原因'}`)
  } catch (error) { proxyTests[p.id] = { reachable: false, error: error instanceof Error ? error.message : '测试失败' }; ElMessage.error(proxyTests[p.id].error!) }
}
function proxyBadge(id: string) {
  const t = proxyTests[id]
  if (t.testing) return { status: 'processing', text: '测试中' }
  return t.reachable ? { status: 'success', text: `连通 · ${t.latencyMs}ms` } : { status: 'failed', text: '连不通' }
}
async function removeProxy(p: ModelProxy) {
  try { await ElMessageBox.confirm(`删除后不能恢复。确定删除代理“${p.name}”吗？`, '删除代理', { confirmButtonText: '删除代理', cancelButtonText: '取消', confirmButtonClass: 'el-button--danger' }) } catch { return }
  try { await api.system.proxies.remove(p.id); delete proxyTests[p.id]; await loadProxies(); ElMessage.success('代理已删除') }
  catch (error) { ElMessage.error(error instanceof ApiError && error.code === 'proxy_in_use' ? `还有模型在使用“${p.name}”，先到“模型”里把它们改成直连或其他代理。` : error instanceof Error ? error.message : '删除失败') }
}
const password=reactive({current:'',next:'',confirm:''})
onMounted(load)
async function load(){loading.value=true;settingsError.value='';const [overviewResult,settingsResult]=await Promise.allSettled([api.overview(),api.system.getSettings(),loadProxies()]);if(overviewResult.status==='fulfilled')overview.value=overviewResult.value;if(settingsResult.status==='fulfilled'){rememberSettings(settingsResult.value);settingsLoaded.value=true}else{settingsLoaded.value=false;settingsError.value=settingsResult.reason instanceof Error?settingsResult.reason.message:'无法读取系统设置';ElMessage.error('系统参数读取失败：'+settingsError.value)}loading.value=false}
async function saveSettings(){if(!settingsLoaded.value||saving.value)return;saving.value=true;settingsError.value='';try{const saved=await api.system.updateSettings({...settings,publicBaseUrl:savedBase.value});rememberSettings(saved);ElMessage.success('已保存，无需重启')}catch(error){settingsError.value=error instanceof Error?error.message:'保存系统设置失败';ElMessage.error(settingsError.value)}finally{saving.value=false}}
async function retryFailed(){const result=await api.system.retryFailed();await load();ElMessage.success(`已重新入队 ${result.queued} 个任务`)}
function formatUpdatedAt(value:string){const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString('zh-CN',{hour12:false})}
async function changePassword(){if(password.next.length<8){ElMessage.error('新密码至少 8 位');return}if(password.next!==password.confirm){ElMessage.error('两次输入的新密码不一致');return}await api.auth.changePassword(password.current,password.next);Object.assign(password,{current:'',next:'',confirm:''});ElMessage.success('管理员密码已更新')}
useAdminEvents(['inbox_tasks','outbox_tasks','system_alerts','knowledge_documents','messages'], async () => { overview.value = await api.overview() })
useAdminEvents(['system_settings'], async () => {
  if (saving.value || JSON.stringify(settings) !== savedSettings) return
  const latest = await api.system.getSettings()
  if (!saving.value && JSON.stringify(settings) === savedSettings) rememberSettings(latest)
})
</script>

<style scoped>
.addr-list { display: grid; gap: 6px; margin: 0 0 14px; font-size: 13px; }
.addr-list div { display: grid; grid-template-columns: 84px minmax(0, 1fr); gap: 8px; }
.addr-list dt { color: var(--muted); }
.addr-list dd { margin: 0; overflow-wrap: anywhere; color: var(--text-2); }
.panel-title-text { flex: 1 1 200px; min-width: 0; }
.panel-sub { margin: 4px 0 0; color: var(--muted); font-size: 13px; }
.proxy-list { display: flex; flex-direction: column; }
.proxy-row { display: grid; grid-template-columns: minmax(0, 1fr) 112px auto; align-items: center; gap: 12px; padding: 10px 0; border-top: 1px solid var(--line); }
.proxy-row:first-child { border-top: 0; padding-top: 0; }
.proxy-name { display: flex; flex-direction: column; min-width: 0; }
.proxy-name strong { font-weight: 500; }
.proxy-name small { overflow: hidden; color: var(--muted); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.proxy-result { min-width: 0; }
.proxy-actions { display: flex; align-items: center; gap: 4px; }
.proxy-actions .el-button + .el-button { margin-left: 4px; }
.proxy-empty { margin: 0; font-size: 13px; }
@media (max-width: 640px) { .proxy-row { grid-template-columns: minmax(0, 1fr) auto; } .proxy-actions { grid-column: 1 / -1; } }
</style>
