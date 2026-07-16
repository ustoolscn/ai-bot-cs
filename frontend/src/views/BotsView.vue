<template>
  <div class="content-page">
    <PageHeader title="机器人" description="管理 QQ 机器人凭证、回调地址和默认模型。"><el-button :icon="Refresh" @click="load">刷新状态</el-button><el-button type="primary" :icon="Plus" @click="openCreate">添加机器人</el-button></PageHeader>
    <div class="stat-strip"><div><span>机器人总数</span><strong>{{ bots.length }}</strong></div><div><span>当前在线</span><strong>{{ bots.filter(b => b.status === 'online').length }}</strong></div><div><span>今日消息</span><strong>{{ todayMessages }}</strong></div><div><span>平均成功率</span><strong>{{ averageSuccessRate }}</strong></div></div>
    <div class="bot-grid">
      <article v-for="bot in bots" :key="bot.id" class="entity-card">
        <div class="entity-card-head"><span class="entity-icon blue"><el-icon><Cpu /></el-icon></span><div><h3>{{ bot.name }}</h3><p>QQ · AppID {{ bot.appId }}</p></div><StatusBadge :status="bot.status" /></div>
        <dl><div><dt>Webhook 回调</dt><dd>{{ bot.callbackPath }}</dd></div><div><dt>默认模型</dt><dd>{{ modelName(bot.modelProfileId) }}</dd></div><div><dt>最近事件</dt><dd>{{ bot.lastEventAt }}</dd></div></dl>
        <div class="entity-card-foot"><el-switch v-model="bot.enabled" inline-prompt active-text="启" inactive-text="停" @change="toggle(bot)" /><span class="spacer" /><el-button @click="copy(bot.callbackPath)">复制回调</el-button><el-button type="primary" plain @click="edit(bot)">配置</el-button></div>
      </article>
    </div>

    <el-dialog v-model="dialogVisible" :title="form.id ? '配置机器人' : '添加 QQ 机器人'" width="620px">
      <el-form label-position="top"><div class="form-grid"><el-form-item label="机器人名称"><el-input v-model="form.name" placeholder="例如：客服小Q" /></el-form-item><el-form-item label="AppID"><el-input v-model="form.appId" placeholder="QQ 开放平台 AppID" /></el-form-item></div><el-form-item label="AppSecret"><el-input v-model="secret" type="password" show-password :placeholder="form.id ? '留空则不覆盖现有密钥' : '请输入 AppSecret'" /><small class="form-tip">密钥仅用于覆盖更新，保存后不会回显。</small></el-form-item><el-form-item label="默认对话模型"><el-select v-model="form.modelProfileId"><el-option v-for="model in chatModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item></el-form>
      <template #footer><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" @click="save">保存机器人</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Cpu, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'; import StatusBadge from '@/components/StatusBadge.vue'
import type { Bot, ModelProfile, SystemOverview } from '@/types'
import { createEmptyOverview } from '@/utils/overview'
const bots = ref<Bot[]>([]); const models = ref<ModelProfile[]>([]); const overview = ref<SystemOverview>(createEmptyOverview()); const dialogVisible = ref(false); const secret = ref(''); const form = reactive<Partial<Bot>>({})
const chatModels = computed(() => models.value.filter(m => m.kind === 'chat'))
const todayMessages = computed(() => overview.value.messages24h === null ? '—' : overview.value.messages24h.toLocaleString())
const averageSuccessRate = computed(() => overview.value.successRate === null ? '—' : `${overview.value.successRate.toFixed(1)}%`)
onMounted(load)
async function load() { [bots.value, models.value, overview.value] = await Promise.all([api.bots.list(), api.models.list(), api.overview()]) }
function modelName(id: string) { return models.value.find(m => m.id === id)?.model || id }
function openCreate() { Object.assign(form, { id: '', name: '', appId: '', modelProfileId: chatModels.value[0]?.id, enabled: true }); secret.value = ''; dialogVisible.value = true }
function edit(bot: Bot) { Object.assign(form, bot); secret.value = ''; dialogVisible.value = true }
async function save() { await api.bots.save(form, secret.value); await load(); dialogVisible.value = false; ElMessage.success('机器人配置已保存') }
function copy(value: string) { navigator.clipboard?.writeText(`${location.origin}${value}`); ElMessage.success('回调地址已复制') }
async function toggle(bot: Bot) { await api.bots.save(bot); ElMessage.success('机器人状态已更新') }
</script>
