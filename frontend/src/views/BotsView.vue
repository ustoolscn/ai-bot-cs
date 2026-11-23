<template>
  <div class="content-page">
    <PageHeader title="机器人与客服" description="接入的 QQ 机器人。把回调地址填到 QQ 开放平台，机器人才能收到消息；每个机器人下可以配置多个 AI 客服，再到群里选择由谁接待。"><el-button :icon="Refresh" @click="load">刷新状态</el-button><el-button type="primary" :icon="Plus" @click="openCreate">添加机器人</el-button></PageHeader>
    <div class="stat-strip"><div><span>机器人</span><strong>{{ bots.length }}</strong></div><div><span>在线</span><strong>{{ bots.filter(b => b.status === 'online').length }}</strong></div><div><span>最近 24 小时消息</span><strong>{{ todayMessages }}</strong></div><div><span>成功回复率</span><strong>{{ averageSuccessRate }}</strong></div></div>
    <div v-if="bots.length" class="entity-list">
      <div v-for="bot in bots" :key="bot.id" class="entity-row">
        <div class="entity-name"><span class="entity-icon"><el-icon><Connection /></el-icon></span><div><strong>{{ bot.name }} <StatusBadge :status="bot.enabled ? bot.status : 'offline'" :text="bot.enabled ? undefined : '已停用'" /></strong><small>AppID {{ bot.appId }}</small></div></div>
        <div class="entity-facts">
          <div><span>最近收到消息</span><strong>{{ bot.lastEventAt || '—' }}</strong></div>
          <div><span>回调地址</span><strong class="mono" :title="bot.callbackPath">{{ bot.callbackPath }}</strong></div>
        </div>
        <div class="entity-actions">
          <el-tooltip :content="bot.enabled ? '停用后不再回复任何消息' : '启用机器人'"><el-switch v-model="bot.enabled" :aria-label="'启用 ' + bot.name" @change="toggle(bot)" /></el-tooltip>
          <el-button :icon="CopyDocument" @click="copy(bot.callbackPath)">复制回调</el-button>
          <el-button @click="edit(bot)">接入配置</el-button>
          <el-button type="primary" plain @click="$router.push({ name: 'bot-services', params: { id: bot.id } })">AI 客服</el-button>
        </div>
      </div>
    </div>
    <div v-else class="panel"><EmptyState title="还没有接入机器人" description="在 QQ 开放平台创建机器人后，把 AppID 和 AppSecret 填进来，再把回调地址配置到开放平台。"><el-button type="primary" :icon="Plus" @click="openCreate">添加机器人</el-button></EmptyState></div>

    <el-dialog v-model="dialogVisible" :title="form.id ? '配置机器人' : '添加 QQ 机器人'" width="620px">
      <el-form label-position="top"><div class="form-grid"><el-form-item label="机器人名称"><el-input v-model="form.name" placeholder="例如：客服小Q" /></el-form-item><el-form-item label="AppID"><el-input v-model="form.appId" placeholder="QQ 开放平台 AppID" /></el-form-item></div><el-form-item label="AppSecret"><el-input v-model="secret" type="password" show-password :placeholder="form.id ? '留空则不覆盖现有密钥' : '请输入 AppSecret'" /><small class="form-tip">保存后不会再显示。修改时留空表示不变。</small></el-form-item></el-form>
      <template #footer><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存机器人</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, reactive, ref } from 'vue'
import { Connection, CopyDocument, Plus, Refresh } from '@element-plus/icons-vue'
import EmptyState from '@/components/EmptyState.vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'; import StatusBadge from '@/components/StatusBadge.vue'
import type { Bot, SystemOverview } from '@/types'
import { createEmptyOverview } from '@/utils/overview'
const bots = ref<Bot[]>([]); const overview = ref<SystemOverview>(createEmptyOverview()); const dialogVisible = ref(false); const secret = ref(''); const form = reactive<Partial<Bot>>({})
const todayMessages = computed(() => overview.value.messages24h === null ? '—' : overview.value.messages24h.toLocaleString())
const averageSuccessRate = computed(() => overview.value.successRate === null ? '—' : `${overview.value.successRate.toFixed(1)}%`)
onMounted(load)
async function load() { [bots.value, overview.value] = await Promise.all([api.bots.list(), api.overview()]) }
function openCreate() { Object.assign(form, { id: '', name: '', appId: '', enabled: true }); secret.value = ''; dialogVisible.value = true }
function edit(bot: Bot) { Object.assign(form, bot); secret.value = ''; dialogVisible.value = true }
const saving = ref(false)
async function save() { saving.value = true; try { await api.bots.save(form, secret.value); await load(); dialogVisible.value = false; ElMessage.success('机器人配置已保存') } catch (e) { ElMessage.error(e instanceof Error ? e.message : '保存失败') } finally { saving.value = false } }
function copy(value: string) { navigator.clipboard?.writeText(`${location.origin}${value}`); ElMessage.success('回调地址已复制') }
async function toggle(bot: Bot) { try { await api.bots.save(bot); ElMessage.success(bot.enabled ? '机器人已启用' : '机器人已停用') } catch (e) { bot.enabled = !bot.enabled; ElMessage.error(e instanceof Error ? e.message : '操作失败') } }
useAdminEvents(['bots','messages','system_alerts'], load)
</script>
