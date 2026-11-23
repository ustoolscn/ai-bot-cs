<template>
  <div class="content-page">
    <PageHeader :title="title" :description="description">
      <el-button v-if="mode === 'services'" @click="$router.push('/bots')">返回机器人</el-button>
      <el-button :disabled="busy || !!error" type="primary" :icon="Plus" @click="edit()">添加{{ label }}</el-button>
    </PageHeader>
    <el-alert v-if="error" type="error" :closable="false" show-icon><template #title>{{ error }} <el-button link @click="load">重试</el-button></template></el-alert>
    <section v-else v-loading="busy" class="table-panel">
      <el-table :data="rows" row-key="id" :empty-text="'还没有' + label + '，点击右上角添加。'">
        <el-table-column prop="name" :label="label + '名称'" min-width="140" />
        <el-table-column v-if="mode === 'personalities'" prop="prompt" label="说话风格" min-width="240" />
        <el-table-column v-if="mode === 'agents'" prop="instructions" label="客服职责" min-width="180" />
        <el-table-column v-if="mode !== 'personalities'" :label="mode === 'services' ? '智能体' : '性格'" min-width="150">
          <template #default="{ row }">{{ mode === 'services' ? agents.find(a => a.id === row.agentId)?.name : presets.find(p => p.id === row.personalityId)?.name }}</template>
        </el-table-column>
        <el-table-column v-if="mode === 'services'" label="对话模型" min-width="170">
          <template #default="{ row }">{{ row.chatProfileId ? models.find(m => m.id === row.chatProfileId)?.name || '模型不可用' : '系统默认模型' }}</template>
        </el-table-column>
        <el-table-column width="90"><template #default="{ row }"><el-button link type="primary" @click="edit(row)">配置</el-button></template></el-table-column>
      </el-table>
    </section>
    <el-dialog v-model="open" :title="(form.id ? '配置' : '添加') + label" width="min(520px, 94vw)">
      <el-form label-position="top" @submit.prevent="save">
        <el-form-item :label="label + '名称'"><el-input v-model="form.name" /></el-form-item>
        <el-form-item v-if="mode === 'personalities'" label="说话风格"><el-input v-model="form.prompt" type="textarea" :rows="5" placeholder="描述用词、语气和回答习惯" /></el-form-item>
        <template v-if="mode === 'agents'">
          <el-form-item label="客服职责"><el-input v-model="form.instructions" type="textarea" :rows="3" placeholder="例如：指导新用户完成首次接入" /></el-form-item>
          <el-form-item label="选用性格"><el-select v-model="form.personalityId"><el-option v-for="p in presets" :key="p.id" :value="p.id" :label="p.name" /></el-select>
            <small class="form-tip">{{ presets.find(p => p.id === form.personalityId)?.prompt }}</small>
          </el-form-item>
        </template>
        <el-form-item v-if="mode === 'services'" label="使用的智能体"><el-select v-model="form.agentId" placeholder="选择智能体"><el-option v-for="a in agents" :key="a.id" :label="a.name" :value="a.id" /></el-select>
          <router-link v-if="!agents.length" to="/agent">先创建智能体</router-link>
        </el-form-item>
        <el-form-item v-if="mode === 'services'" label="对话模型">
          <el-select v-model="form.chatProfileId" placeholder="系统默认模型" clearable :value-on-clear="''">
            <el-option v-for="m in models" :key="m.id" :value="m.id" :label="m.name + ' · ' + m.model" :disabled="m.status === 'offline'" />
          </el-select>
        </el-form-item>
        <template v-if="mode === 'services'">
          <el-form-item label="简单辅助模型"><el-select v-model="form.simpleProfileId" clearable :value-on-clear="''" placeholder="跟随通用辅助设置"><el-option v-for="m in models" :key="m.id" :value="m.id" :label="m.name" :disabled="m.status === 'offline'" /></el-select><small class="form-tip">用于改写、摘要等后台任务。</small></el-form-item>
          <el-form-item label="群消息判断模型"><el-select v-model="form.decisionProfileId" clearable :value-on-clear="''" placeholder="跟随通用判断设置"><el-option v-for="m in models" :key="m.id" :value="m.id" :label="m.name" :disabled="m.status === 'offline'" /></el-select><small class="form-tip">逐条判断是否需要接话，决定接话后才调用对话模型。</small></el-form-item>
        </template>
      </el-form>
      <template #footer><el-button @click="open = false">取消</el-button><el-button type="primary" :disabled="!valid" :loading="saving" @click="save">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import type { ModelProfile } from '@/types'
import { api } from '@/api'
import { botServices, personalities, supportAgents, type BotService, type Personality, type SupportAgent } from '@/api/support'

const props = defineProps<{ mode: 'personalities' | 'agents' | 'services'; botId?: string }>()
const presets = ref<Personality[]>([])
const agents = ref<SupportAgent[]>([])
const services = ref<BotService[]>([])
const models = ref<ModelProfile[]>([])
const botName = ref('')
const busy = ref(true)
const saving = ref(false)
const error = ref('')
const open = ref(false)
const blank = () => ({ id: '', name: '', instructions: '', personalityId: '', prompt: '', agentId: '', chatProfileId: '', simpleProfileId: '', decisionProfileId: '' })
const form = reactive(blank())
const label = computed(() => ({ personalities: '性格', agents: '智能体', services: 'AI 客服' })[props.mode])
const title = computed(() => props.mode === 'services' && botName.value ? botName.value + ' · AI 客服' : label.value)
const description = computed(() => ({
  personalities: '在这里编辑说话风格。使用该性格的智能体会在下一次回复时采用更新后的内容。',
  agents: '为智能体填写客服职责，选择一种性格。同一个智能体可以分配给多个 AI 客服。',
  services: '为这个 QQ 机器人添加客服，为每位客服选择智能体和对话模型，然后在「群与会话」选择接待客服。',
})[props.mode])
const rows = computed(() => props.mode === 'services' ? services.value : props.mode === 'agents' ? agents.value : presets.value)
const valid = computed(() => form.name.trim() && (props.mode === 'services' ? form.agentId : props.mode === 'agents' ? form.personalityId : form.prompt.trim()))
let version = 0
async function load() {
  const current = ++version
  busy.value = true
  open.value = false
  error.value = ''
  try {
    const [p, a, s, bots, profiles] = await Promise.all([
      props.mode !== 'services' ? personalities.list() : [],
      props.mode !== 'personalities' ? supportAgents.list() : [],
      props.botId ? botServices(props.botId).list() : [],
      props.botId ? api.bots.list() : [],
      props.botId ? api.models.list() : [],
    ])
    if (current !== version) return
    presets.value = p
    agents.value = a
    services.value = s
    models.value = profiles.filter(m => m.kind === 'chat')
    botName.value = bots.find(b => b.id === props.botId)?.name || ''
  } catch (e) {
    if (current === version) error.value = e instanceof Error ? e.message : '加载失败'
  } finally {
    if (current === version) busy.value = false
  }
}
watch(() => [props.mode, props.botId], load, { immediate: true })
function edit(row?: Personality | SupportAgent | BotService) {
  Object.assign(form, blank(), row)
  open.value = true
}
async function save() {
  if (!valid.value || saving.value) return
  saving.value = true
  try {
    if (props.mode === 'services' && props.botId) await botServices(props.botId).save(form)
    else if (props.mode === 'agents') await supportAgents.save(form)
    else await personalities.save(form)
    ElMessage.success('已保存')
    await load()
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally { saving.value = false }
}
</script>
