<template>
  <div class="content-page">
    <PageHeader title="系统状态" description="后台服务是否正常、任务有没有堆积，以及全局参数和管理员密码。"><el-button :icon="Refresh" :loading="loading" @click="load">刷新状态</el-button></PageHeader>
    <div class="system-grid">
      <section class="settings-panel">
        <h2>服务状态</h2>
        <div class="service-list"><div v-for="service in services" :key="service.name"><span class="entity-icon small"><el-icon><component :is="service.icon" /></el-icon></span><span><strong>{{ service.name }}</strong><small>{{ service.detail }}</small></span><StatusBadge :status="connection.lost ? 'warning' : service.status" :text="connection.lost ? '未连接' : undefined" /></div></div>
      </section>
      <section class="settings-panel">
        <div class="panel-title"><h2>任务队列</h2><el-button size="small" :disabled="!overview.queues.failed" @click="retryFailed">重试失败任务</el-button></div>
        <div class="queue-metrics" style="grid-template-columns: repeat(2, 1fr)"><div><span>待处理消息</span><strong><CountUp :value="overview.queues.inbox" /></strong></div><div><span>待发送回复</span><strong><CountUp :value="overview.queues.outbox" /></strong></div><div><span>待索引文档</span><strong><CountUp :value="overview.queues.knowledge" /></strong></div><div><span>失败任务</span><strong :class="{ 'danger-text': overview.queues.failed }"><CountUp :value="overview.queues.failed" /></strong></div></div>
        <p class="muted" style="margin: 0; font-size: 13px">数字持续变大说明处理不过来，可以检查模型响应速度或 QQ 投递是否异常。</p>
      </section>
      <section class="settings-panel">
        <h2>全局参数</h2>
        <el-alert v-if="settingsError" :title="settingsError" type="error" :closable="false" show-icon class="settings-load-error" />
        <el-form label-position="top">
          <el-form-item label="新会话默认带上几条消息"><el-input-number v-model="settings.defaultContextLimit" :min="1" :max="100" :disabled="!settingsLoaded" /><small class="form-tip">包含当前问题；只影响之后新出现的会话，已有会话在“群与会话”里单独调整。</small></el-form-item>
          <el-form-item label="AI 请求超时"><el-input-number v-model="settings.aiRequestTimeoutSeconds" :min="10" :max="600" :disabled="!settingsLoaded" /><span class="unit-label">秒</span><small class="form-tip" style="width: 100%">保存后立即生效。</small></el-form-item>
          <el-form-item label="消息记录保留"><el-input-number v-model="settings.messageRetentionDays" :min="1" :max="3650" :disabled="!settingsLoaded" /><span class="unit-label">天</span><small class="form-tip" style="width: 100%">超过天数的消息约 1 分钟内自动清理。</small></el-form-item>
          <div style="display: flex; align-items: center; gap: 12px"><el-button type="primary" :loading="saving" :disabled="!settingsLoaded || saving" @click="saveSettings">保存参数</el-button><small v-if="settings.updatedAt" class="muted">最近更新：{{ formatUpdatedAt(settings.updatedAt) }}</small></div>
        </el-form>
      </section>
      <section class="settings-panel">
        <h2>管理员密码</h2>
        <p class="muted" style="font-size: 13px">修改后，其他已登录的地方会被退出。</p>
        <el-form label-position="top">
          <el-form-item label="当前密码"><el-input v-model="password.current" type="password" show-password autocomplete="current-password" /></el-form-item>
          <el-form-item label="新密码"><el-input v-model="password.next" type="password" show-password autocomplete="new-password" placeholder="至少 8 位" /></el-form-item>
          <el-form-item label="再输入一次新密码"><el-input v-model="password.confirm" type="password" show-password autocomplete="new-password" /></el-form-item>
          <el-button type="primary" :disabled="!password.current || !password.next" @click="changePassword">更新密码</el-button>
        </el-form>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { markRaw, onMounted, reactive, ref } from 'vue'
import { Connection, Coin, FolderOpened, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import CountUp from '@/components/fx/CountUp.vue'
import type { SystemOverview, SystemSettings } from '@/types'
import { createEmptyOverview } from '@/utils/overview'
import { connection } from '@/utils/connection'
import { applySystemSettings, createSystemSettingsForm } from '@/utils/system-settings'

const overview=ref<SystemOverview>(createEmptyOverview()),settings=reactive<SystemSettings>(createSystemSettingsForm()),loading=ref(false),saving=ref(false),settingsLoaded=ref(false),settingsError=ref('')
const services=[{name:'后端服务',detail:'管理端和 QQ 回调的接口',status:'online',icon:markRaw(Connection)},{name:'数据库',detail:'PostgreSQL + pgvector，保存消息和知识索引',status:'online',icon:markRaw(Coin)},{name:'文件存储',detail:'本地存储（DATA_DIR），支持 .txt、.md',status:'online',icon:markRaw(FolderOpened)}]
let savedSettings = JSON.stringify(settings)
function rememberSettings(value: SystemSettings) {
  applySystemSettings(settings, value)
  savedSettings = JSON.stringify(settings)
}
const password=reactive({current:'',next:'',confirm:''})
onMounted(load)
async function load(){loading.value=true;settingsError.value='';const [overviewResult,settingsResult]=await Promise.allSettled([api.overview(),api.system.getSettings()]);if(overviewResult.status==='fulfilled')overview.value=overviewResult.value;if(settingsResult.status==='fulfilled'){rememberSettings(settingsResult.value);settingsLoaded.value=true}else{settingsLoaded.value=false;settingsError.value=settingsResult.reason instanceof Error?settingsResult.reason.message:'无法读取系统设置'}loading.value=false}
async function saveSettings(){if(!settingsLoaded.value||saving.value)return;saving.value=true;settingsError.value='';try{const saved=await api.system.updateSettings(settings);rememberSettings(saved);ElMessage.success('已保存，无需重启')}catch(error){settingsError.value=error instanceof Error?error.message:'保存系统设置失败'}finally{saving.value=false}}
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
