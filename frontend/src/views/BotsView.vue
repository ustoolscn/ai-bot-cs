<template>
  <div class="content-page">
    <PageHeader title="机器人与客服"><el-button :icon="Refresh" @click="load">刷新状态</el-button><el-button type="primary" :icon="Plus" @click="openCreate">添加机器人</el-button></PageHeader>
    <div class="stat-strip"><div><span>机器人</span><strong>{{ bots.length }}</strong></div><div><span>在线</span><strong>{{ bots.filter(b => b.status === 'online').length }}</strong></div><div><span>最近 24 小时消息</span><strong>{{ todayMessages }}</strong></div><div><span>成功回复率</span><strong>{{ averageSuccessRate }}</strong></div></div>
    <div v-if="bots.length" class="entity-list">
      <div v-for="bot in bots" :key="bot.id" class="entity-row">
        <div class="entity-name"><span class="entity-icon"><el-icon><Connection /></el-icon></span><div><strong>{{ bot.name }} <StatusBadge :status="bot.enabled ? bot.status : 'offline'" :text="bot.enabled ? undefined : '已停用'" /></strong><small>AppID {{ bot.appId }}</small></div></div>
        <div class="entity-facts">
          <div><span>最近收到消息</span><strong>{{ bot.lastEventAt || '—' }}</strong></div>
          <div><span>回调地址</span><strong v-if="isAbsolute(bot.callbackUrl)" class="mono" :title="bot.callbackUrl">{{ bot.callbackUrl }}</strong><el-button v-else link type="warning" @click="$router.push('/system')">先配置公网地址</el-button></div>
        </div>
        <div class="entity-actions">
          <el-tooltip :content="bot.enabled ? '停用后不再回复任何消息' : '启用机器人'"><el-switch v-model="bot.enabled" :aria-label="'启用 ' + bot.name" @change="toggle(bot)" /></el-tooltip>
          <el-button :icon="CopyDocument" :disabled="!isAbsolute(bot.callbackUrl)" @click="copy(bot.callbackUrl)">复制回调</el-button>
          <el-button @click="edit(bot)">接入配置</el-button>
          <el-button :icon="Headset" @click="openOperators(bot)">人工客服</el-button>
          <el-button type="primary" plain @click="$router.push({ name: 'bot-services', params: { id: bot.id } })">AI 客服</el-button>
        </div>
      </div>
    </div>
    <div v-else class="panel"><EmptyState title="还没有接入机器人" description="在 QQ 开放平台创建机器人后，把 AppID 和 AppSecret 填进来，再把回调地址配置到开放平台。"><el-button type="primary" :icon="Plus" @click="openCreate">添加机器人</el-button></EmptyState></div>

    <el-dialog v-model="dialogVisible" :title="form.id ? '配置机器人' : '添加 QQ 机器人'" width="620px">
      <el-form label-position="top"><div class="form-grid"><el-form-item label="机器人名称"><el-input v-model="form.name" placeholder="例如：客服小Q" /></el-form-item><el-form-item label="AppID"><el-input v-model="form.appId" placeholder="QQ 开放平台 AppID" /></el-form-item></div><el-form-item label="AppSecret"><el-input v-model="secret" type="password" show-password :placeholder="form.id ? '留空则不覆盖现有密钥' : '请输入 AppSecret'" /><small class="form-tip">保存后不会再显示。修改时留空表示不变。</small></el-form-item></el-form>
      <template #footer><div class="dialog-foot"><el-button v-if="form.id" type="danger" link :icon="Delete" @click="remove">删除机器人</el-button><span class="spacer" /><el-button @click="dialogVisible = false">取消</el-button><el-button type="primary" :loading="saving" @click="save">保存机器人</el-button></div></template>
    </el-dialog>
    <el-dialog v-model="opVisible" :title="`人工客服 · ${opBot?.name || ''}`" width="560px">
      <ol class="op-steps">
        <li>用自己的 QQ 加这个机器人为好友，再给它发一条私聊，比如“绑定人工客服”。</li>
        <li>在下面找到这个私聊，打开开关。可以绑定多个人。</li>
        <li>有人转人工时，机器人会私信你工单。在手机 QQ 回复 <code>#工单号 回复内容</code>，例如 <code>#123 已为您处理，请重试。</code>，就会以“【人工客服】”开头发回原来的群。</li>
      </ol>
      <div class="op-head"><strong>私聊过机器人的账号</strong><el-button size="small" link :icon="Refresh" :loading="opLoading" @click="loadOperators">刷新</el-button></div>
      <div v-loading="opLoading" class="op-list">
        <div v-for="op in operators" :key="op.conversationId" class="op-row">
          <span><strong>{{ op.name || '未命名用户' }}</strong><small class="mono" :title="op.userOpenId">{{ op.userOpenId }}</small></span>
          <span class="op-state"><StatusBadge :status="op.enabled ? 'success' : 'skipped'" :text="op.enabled ? '人工客服' : '未绑定'" /></span>
          <el-switch v-model="op.enabled" :loading="opSaving === op.conversationId" :aria-label="'设为人工客服 ' + (op.name || op.userOpenId)" @change="toggleOperator(op)" />
        </div>
        <p v-if="!opLoading && !operators.length" class="muted op-empty">还没有人私聊过这个机器人。按上面第 1 步发一条私聊后点“刷新”。</p>
      </div>
      <template #footer><el-button @click="opVisible = false">完成</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, reactive, ref } from 'vue'
import { Connection, CopyDocument, Delete, Headset, Plus, Refresh } from '@element-plus/icons-vue'
import EmptyState from '@/components/EmptyState.vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'; import StatusBadge from '@/components/StatusBadge.vue'
import type { Bot, HumanOperator, SystemOverview } from '@/types'
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
async function remove() {
  const name = form.name || '这个机器人'
  try {
    await ElMessageBox.confirm(`删除后，「${name}」不再接收和回复任何消息，它下面的群与会话、消息记录、专属 AI 客服、记忆和黑名单都会一起清除，无法恢复。共享的模型、知识库和定时任务保留。只想暂时不用的话，关掉列表里的开关即可。`, `删除机器人「${name}」？`, { confirmButtonText: '删除机器人', cancelButtonText: '取消', type: 'warning', confirmButtonClass: 'el-button--danger' })
  } catch { return }
  try {
    await api.bots.remove(String(form.id))
    dialogVisible.value = false
    await load()
    ElMessage.success(`已删除「${name}」`)
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '删除失败') }
}
const isAbsolute = (url: string) => /^https?:\/\//.test(url)
function copy(value: string) { navigator.clipboard?.writeText(value); ElMessage.success('回调地址已复制') }
async function toggle(bot: Bot) { try { await api.bots.save(bot); ElMessage.success(bot.enabled ? '机器人已启用' : '机器人已停用') } catch (e) { bot.enabled = !bot.enabled; ElMessage.error(e instanceof Error ? e.message : '操作失败') } }
const opVisible = ref(false), opBot = ref<Bot>(), operators = ref<HumanOperator[]>([]), opLoading = ref(false), opSaving = ref('')
function openOperators(bot: Bot) { opBot.value = bot; operators.value = []; opVisible.value = true; loadOperators() }
async function loadOperators() {
  if (!opBot.value) return
  opLoading.value = true
  try { operators.value = (await api.bots.humanOperators(opBot.value.id)).sort((a, b) => Number(b.enabled) - Number(a.enabled)) }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '读取私聊列表失败') } finally { opLoading.value = false }
}
async function toggleOperator(op: HumanOperator) {
  if (!opBot.value) return
  opSaving.value = op.conversationId
  try { await api.bots.setHumanOperator(opBot.value.id, op.conversationId, op.enabled); ElMessage.success(op.enabled ? `${op.name || '该账号'} 已设为人工客服` : `已取消 ${op.name || '该账号'} 的人工客服`) }
  catch (e) { op.enabled = !op.enabled; ElMessage.error(e instanceof Error ? e.message : '操作失败') } finally { opSaving.value = '' }
}
useAdminEvents(['bots','messages','system_alerts'], load)
</script>

<style scoped>
.dialog-foot { display: flex; align-items: center; gap: 8px; }
.dialog-foot .el-button + .el-button { margin-left: 0; }
.op-steps { display: grid; gap: 6px; margin: 0 0 20px; padding-left: 20px; color: var(--text-2); font-size: 13px; line-height: 1.6; }
.op-steps code { padding: 1px 5px; border-radius: 4px; background: var(--surface-3); font: 12px var(--font-mono); color: var(--text); }
.op-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 8px; }
.op-head strong { font-size: 14px; font-weight: 600; }
.op-list { max-height: 320px; min-height: 96px; overflow-y: auto; border: 1px solid var(--line); border-radius: var(--radius-sm); }
.op-row { display: grid; grid-template-columns: minmax(0, 1fr) 88px auto; align-items: center; gap: 12px; padding: 10px 14px; border-top: 1px solid var(--line); }
.op-row:first-child { border-top: 0; }
.op-row > span:first-child { display: flex; flex-direction: column; min-width: 0; }
.op-row strong { font-weight: 500; }
.op-row small { overflow: hidden; color: var(--muted); font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
.op-empty { margin: 0; padding: 28px 16px; font-size: 13px; text-align: center; }
</style>
