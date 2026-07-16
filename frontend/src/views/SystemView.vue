<template>
  <div class="content-page">
    <PageHeader title="系统设置" description="查看服务运行状态，管理安全、存储与任务配置。"><el-button :icon="Refresh" :loading="loading" @click="load">刷新状态</el-button></PageHeader>
    <div class="system-grid">
      <section class="settings-panel"><h2>运行状态</h2><div class="service-list"><div v-for="service in services" :key="service.name"><span class="entity-icon small"><el-icon><component :is="service.icon" /></el-icon></span><span><strong>{{ service.name }}</strong><small>{{ service.detail }}</small></span><StatusBadge :status="service.status" /></div></div></section>
      <section class="settings-panel"><h2>任务队列</h2><div class="queue-metrics"><div><span>入口待处理</span><strong>{{ overview.queues.inbox }}</strong></div><div><span>出口待发送</span><strong>{{ overview.queues.outbox }}</strong></div><div><span>知识索引</span><strong>{{ overview.queues.knowledge }}</strong></div><div><span>失败任务</span><strong class="danger-text">{{ overview.queues.failed }}</strong></div></div><el-button class="full-button" @click="retryFailed">重试失败任务</el-button></section>
      <section class="settings-panel"><h2>消息与上下文</h2><el-alert v-if="settingsError" :title="settingsError" type="error" :closable="false" show-icon class="settings-load-error" /><el-form label-position="top"><el-form-item label="默认上下文消息数"><el-input-number v-model="settings.defaultContextLimit" :min="4" :max="100" :disabled="!settingsLoaded" /></el-form-item><el-form-item label="AI 请求超时"><el-input-number v-model="settings.aiRequestTimeoutSeconds" :min="10" :max="600" :disabled="!settingsLoaded" /><span class="unit-label">秒</span></el-form-item><el-form-item label="消息记录保留"><el-input-number v-model="settings.messageRetentionDays" :min="1" :max="3650" :disabled="!settingsLoaded" /><span class="unit-label">天</span></el-form-item><div class="settings-notice"><p>设置保存在 PostgreSQL 中。</p><p>AI 请求超时保存后立即生效；默认上下文仅用于新会话；消息保留策略约 1 分钟内执行。</p><small v-if="settings.updatedAt">最近更新：{{ formatUpdatedAt(settings.updatedAt) }}</small></div><el-button type="primary" :loading="saving" :disabled="!settingsLoaded || saving" @click="saveSettings">保存设置</el-button></el-form></section>
      <section class="settings-panel"><h2>本地文件存储</h2><dl class="detail-list"><div><dt>存储类型</dt><dd>Local FileStorage</dd></div><div><dt>数据目录</dt><dd>由 DATA_DIR 配置</dd></div><div><dt>已使用空间</dt><dd>未统计</dd></div><div><dt>允许类型</dt><dd>.txt、.md</dd></div></dl><el-alert title="已预留 MinIO FileStorage 接口，可在后续部署中切换。" type="info" :closable="false" show-icon /></section>
      <section class="settings-panel security-panel"><h2>管理员安全</h2><p>修改当前管理员密码。保存后其他会话将失效。</p><el-form label-position="top"><div class="form-grid three"><el-form-item label="当前密码"><el-input v-model="password.current" type="password" show-password /></el-form-item><el-form-item label="新密码"><el-input v-model="password.next" type="password" show-password /></el-form-item><el-form-item label="确认新密码"><el-input v-model="password.confirm" type="password" show-password /></el-form-item></div><el-button type="primary" @click="changePassword">更新密码</el-button></el-form></section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { markRaw, onMounted, reactive, ref } from 'vue'
import { Connection, Coin, FolderOpened, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import type { SystemOverview, SystemSettings } from '@/types'
import { createEmptyOverview } from '@/utils/overview'
import { applySystemSettings, createSystemSettingsForm } from '@/utils/system-settings'

const overview=ref<SystemOverview>(createEmptyOverview()),settings=reactive<SystemSettings>(createSystemSettingsForm()),loading=ref(false),saving=ref(false),settingsLoaded=ref(false),settingsError=ref('')
const services=[{name:'HTTP API',detail:'当前服务已响应',status:'online',icon:markRaw(Connection)},{name:'PostgreSQL + pgvector',detail:'连接池由后端自动管理',status:'online',icon:markRaw(Coin)},{name:'本地文件存储',detail:'路径由 DATA_DIR 配置',status:'online',icon:markRaw(FolderOpened)}]
const password=reactive({current:'',next:'',confirm:''})
onMounted(load)
async function load(){loading.value=true;settingsError.value='';const [overviewResult,settingsResult]=await Promise.allSettled([api.overview(),api.system.getSettings()]);if(overviewResult.status==='fulfilled')overview.value=overviewResult.value;if(settingsResult.status==='fulfilled'){applySystemSettings(settings,settingsResult.value);settingsLoaded.value=true}else{settingsLoaded.value=false;settingsError.value=settingsResult.reason instanceof Error?settingsResult.reason.message:'无法读取系统设置'}loading.value=false}
async function saveSettings(){if(!settingsLoaded.value||saving.value)return;saving.value=true;settingsError.value='';try{const saved=await api.system.updateSettings(settings);applySystemSettings(settings,saved);ElMessage.success('已保存到数据库，无需重启')}catch(error){settingsError.value=error instanceof Error?error.message:'保存系统设置失败'}finally{saving.value=false}}
async function retryFailed(){const result=await api.system.retryFailed();await load();ElMessage.success(`已重新入队 ${result.queued} 个任务`)}
function formatUpdatedAt(value:string){const date=new Date(value);return Number.isNaN(date.getTime())?value:date.toLocaleString('zh-CN',{hour12:false})}
async function changePassword(){if(password.next.length<8){ElMessage.error('新密码至少 8 位');return}if(password.next!==password.confirm){ElMessage.error('两次输入的新密码不一致');return}await api.auth.changePassword(password.current,password.next);Object.assign(password,{current:'',next:'',confirm:''});ElMessage.success('管理员密码已更新')}
</script>
