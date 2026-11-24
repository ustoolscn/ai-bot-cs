<template>
  <div class="content-page">
    <PageHeader :title="isNew ? '添加 AI 客服' : form?.name || 'AI 客服'" :back="{ label: (botName || '机器人') + ' 的 AI 客服', to: { name: 'bot-services', params: { id: botId } } }" />

    <div v-if="form" class="settings-layout">
      <nav class="settings-toc" aria-label="设置目录">
        <a v-for="s in sections" :key="s.id" :class="{ active: activeSection === s.id }" @click="scrollTo(s.id)">{{ s.label }}</a>
      </nav>

      <div class="settings-sections">
        <section id="basic" class="settings-section">
          <header><h2>基本信息</h2><p>名称、职责，以及这个客服用到的模型。</p></header>
          <div class="settings-card">
            <el-form label-position="top">
              <el-form-item label="客服名称"><el-input v-model="form.name" maxlength="20" style="max-width: 320px" /></el-form-item>
              <el-form-item label="职责"><el-input v-model="form.instructions" type="textarea" :autosize="{ minRows: 2, maxRows: 6 }" placeholder="例如：负责网站 A 的接入、价格和分组问题" /></el-form-item>
            </el-form>
            <div class="setting-row"><div><strong>对话模型</strong></div>
              <el-select v-model="form.chatProfileId" clearable :value-on-clear="''" placeholder="系统默认"><el-option v-for="m in chatModels" :key="m.id" :value="m.id" :label="m.name + ' · ' + m.model" :disabled="m.status === 'offline'" /></el-select></div>
            <div class="setting-row"><div><strong>辅助模型</strong><span>改写、摘要、记忆提取</span></div>
              <el-select v-model="form.simpleProfileId" clearable :value-on-clear="''" placeholder="跟随通用设置"><el-option v-for="m in chatModels" :key="m.id" :value="m.id" :label="m.name" :disabled="m.status === 'offline'" /></el-select></div>
            <div class="setting-row"><div><strong>群消息判断模型</strong><span>判断群消息要不要接话</span></div>
              <el-select v-model="form.decisionProfileId" clearable :value-on-clear="''" placeholder="跟随通用设置"><el-option v-for="m in decisionCandidates" :key="m.id" :value="m.id" :label="m.kind === 'decision' ? `${m.name} · 判断模型` : m.name" :disabled="m.status === 'offline'" /></el-select></div>
            <div class="setting-row"><div><strong>生图模型</strong><span>“生成图片”技能用哪个模型，在“模型”页添加</span></div>
              <el-select v-model="form.imageProfileId" clearable :value-on-clear="''" :placeholder="imageModels.length ? '不使用生图' : '还没有生图模型'"><el-option v-for="m in imageModels" :key="m.id" :value="m.id" :label="m.name + ' · ' + m.model" :disabled="m.status === 'offline'" /></el-select></div>
          </div>
        </section>

        <section id="persona" class="settings-section">
          <header><h2>人设与风格</h2><p>说话风格会写进每次回复的提示词；示例对话帮模型学会语气。</p></header>
          <div class="settings-card">
            <el-input v-model="form.settings.stylePrompt" type="textarea" :autosize="{ minRows: 8, maxRows: 18 }" aria-label="说话风格" />
          </div>
          <div class="settings-card">
            <h3>示例对话</h3>
            <div v-for="(ex, index) in form.settings.examples" :key="index" class="example-row">
              <el-input v-model="ex.question" placeholder="用户问" />
              <el-input v-model="ex.answer" type="textarea" :autosize="{ minRows: 1, maxRows: 4 }" placeholder="客服答" />
              <el-button :icon="Delete" aria-label="删除示例" title="删除示例" @click="form.settings.examples.splice(index, 1)" />
            </div>
            <el-button :icon="Plus" @click="form.settings.examples.push({ question: '', answer: '' })">添加示例</el-button>
          </div>
        </section>

        <section id="timing" class="settings-section">
          <header><h2>连续消息合并</h2><p>用户连发几条时，等说完再合在一起回复。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>等对方说完</strong><span>最后一条之后这么久没有新消息，才把几段合起来回复</span></div><span><el-input-number v-model="form.settings.replyQuietSeconds" :min="1" :max="form.settings.replyMaxWaitSeconds" /><span class="unit-label">秒</span></span></div>
            <div class="setting-row"><div><strong>最多等待</strong><span>从第一条算起，到时间就回复</span></div><span><el-input-number v-model="form.settings.replyMaxWaitSeconds" :min="form.settings.replyQuietSeconds" :max="120" /><span class="unit-label">秒</span></span></div>
          </div>
        </section>

        <section id="live" class="settings-section">
          <header><h2>网站实时数据</h2><p>接入这个客服负责的网站，回答价格、分组和服务状态。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>启用</strong></div><el-switch v-model="live.enabled" /></div>
            <template v-if="live.enabled">
              <div class="setting-row"><div><strong>网站地址</strong></div><el-input v-model="live.baseUrl" placeholder="https://" /></div>
              <div class="setting-row stack"><div><strong>状态图样式</strong><span>用户问分组或服务状态时，用网站实时数据画成图片发出，并由 AI 写一句结论</span></div>
                <StatusStylePicker v-model="statusStyle" /></div>
              <div v-if="statusStyle === 'page'" class="setting-row"><div><strong>状态页地址</strong><span>截取这个网页发给用户</span></div><el-input v-model="live.statusPageUrl" placeholder="https://" /></div>
              <div class="setting-row"><div><strong>系统访问令牌</strong><span>只有价格页需要登录时才填</span></div>
                <div class="token-field">
                  <el-input v-model="accessToken" type="password" show-password :placeholder="live.hasAccessToken && !clearToken ? '已保存，留空不修改' : '可不填'" />
                  <el-button v-if="live.hasAccessToken && !clearToken" link type="danger" @click="clearToken = true; accessToken = ''">清除令牌</el-button>
                </div>
              </div>
              <div class="setting-row"><div><strong>用户 ID</strong></div><el-input v-model="live.userId" /></div>
              <div class="setting-row"><div><strong>公告和异常分组放进提示词</strong></div><el-switch v-model="live.injectSnapshot" /></div>
              <div class="setting-row"><div><strong>报价附带说明</strong></div><el-input v-model="live.priceNote" /></div>
            </template>
          </div>
          <div v-if="!isNew" class="settings-card live-card">
            <LiveDataPanel ref="livePanel" :source="liveSource" :blocked-reason="liveBlockedReason" />
          </div>
        </section>

        <section id="knowledge" class="settings-section">
          <header><h2>知识库</h2><p>回答前检索哪些知识库。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>跟随群里绑定的知识库</strong></div><el-switch :model-value="form.settings.knowledgeBaseIds === null" @update:model-value="(v: string | number | boolean) => form!.settings.knowledgeBaseIds = v ? null : []" /></div>
            <div v-if="form.settings.knowledgeBaseIds !== null" class="setting-row stack">
              <el-select v-model="form.settings.knowledgeBaseIds" multiple placeholder="不选则不检索知识库" style="width: 100%"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
            </div>
          </div>
        </section>

        <section id="skills" class="settings-section">
          <header><h2>技能与工具</h2><p>回复时可以调用的工具和技能。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>允许调用工具</strong></div><el-switch v-model="form.settings.toolsEnabled" /></div>
            <template v-if="form.settings.toolsEnabled">
              <div class="setting-row"><div><strong>一次回复最多调用几轮</strong></div><el-input-number v-model="form.settings.maxToolSteps" :min="1" :max="8" /></div>
              <div class="setting-row"><div><strong>使用全部已启用的工具</strong></div><el-switch :model-value="form.settings.toolNames === null" @update:model-value="(v: string | number | boolean) => form!.settings.toolNames = v ? null : toolOptions.map(t => t.name)" /></div>
              <div v-if="form.settings.toolNames !== null" class="setting-row stack">
                <el-checkbox-group v-model="form.settings.toolNames" class="pick-grid">
                  <el-checkbox v-for="t in toolOptions" :key="t.name" :value="t.name"><span class="pick-name">{{ t.label }}</span></el-checkbox>
                </el-checkbox-group>
              </div>
            </template>
            <div class="setting-row"><div><strong>使用全部已启用的技能</strong></div><el-switch :model-value="form.settings.skillIds === null" @update:model-value="(v: string | number | boolean) => form!.settings.skillIds = v ? null : skills.filter(s => s.id).map(s => s.id!)" /></div>
            <div v-if="form.settings.skillIds !== null" class="setting-row stack">
              <el-checkbox-group v-model="form.settings.skillIds" class="pick-grid">
                <el-checkbox v-for="s in skills" :key="s.id" :value="s.id" :disabled="!s.enabled"><span class="pick-name">{{ s.name }}</span></el-checkbox>
              </el-checkbox-group>
            </div>
          </div>
        </section>

        <section id="learning" class="settings-section">
          <header><h2>学习与转人工</h2><p>自动质检、从官方客服学习，以及转人工后机器人安静多久。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>每轮自动质检</strong></div><el-switch v-model="form.settings.evaluationEnabled" /></div>
            <div class="setting-row"><div><strong>从官方客服的回答里学习</strong></div><el-switch v-model="form.settings.staffMiningEnabled" /></div>
            <div class="setting-row"><div><strong>可信度够高时自动发布</strong></div><el-switch v-model="form.settings.autoPublishStaff" /></div>
            <div v-if="form.settings.autoPublishStaff" class="setting-row stack"><div><strong>自动发布门槛</strong></div><el-slider v-model="form.settings.autoPublishMinConfidence" :min="0.5" :max="1" :step="0.05" show-input /></div>
            <div class="setting-row"><div><strong>候选知识发布到</strong></div>
              <el-select v-model="form.settings.learningKnowledgeBaseId" clearable placeholder="选择知识库"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></div>
            <div class="setting-row"><div><strong>转人工后安静</strong></div><span><el-input-number v-model="form.settings.handoffPauseMinutes" :min="1" :max="1440" /><span class="unit-label">分钟</span></span></div>
          </div>
        </section>

        <transition name="save-bar">
          <div v-if="dirty" class="save-bar" role="status">
            <el-icon><WarningFilled /></el-icon>
            <span>有未保存的修改</span>
            <span class="spacer" />
            <el-button v-if="!isNew" @click="discard">放弃修改</el-button>
            <el-button type="primary" :loading="saving" :disabled="!form.name.trim()" @click="save">{{ isNew ? '创建客服' : '保存设置' }}</el-button>
          </div>
        </transition>
      </div>
    </div>
    <section v-else-if="loadError" class="panel"><EmptyState mood="thinking" title="加载失败" :description="loadError"><el-button @click="load()">重新加载</el-button></EmptyState></section>
    <div v-else class="settings-layout"><div class="skeleton" style="height: 240px" /><div class="skeleton" style="height: 520px; border-radius: var(--radius)" /></div>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRouter } from 'vue-router'
import { Delete, Plus, WarningFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import LiveDataPanel from '@/components/agent/LiveDataPanel.vue'
import StatusStylePicker from '@/components/agent/StatusStylePicker.vue'
import { api } from '@/api'
import { agentApi, type Skill, type StatusImageStyle } from '@/api/agent'
import { botServices, type BotService } from '@/api/support'
import type { KnowledgeBase, ModelProfile } from '@/types'

const props = defineProps<{ botId: string; serviceId: string }>()
const router = useRouter()
const sections = [
  { id: 'basic', label: '基本信息' },
  { id: 'persona', label: '人设与风格' },
  { id: 'timing', label: '连续消息合并' },
  { id: 'live', label: '网站实时数据' },
  { id: 'knowledge', label: '知识库' },
  { id: 'skills', label: '技能与工具' },
  { id: 'learning', label: '学习与转人工' },
]
const toolLabels: Record<string, string> = {
  publish_html_page: '发布网页链接', render_html_image: 'HTML 排版成图片', search_knowledge: '检索知识库', get_model_price: '查询模型价格',
  get_service_status: '查询服务状态', get_group_status: '查询分组状态', get_announcements: '获取网站公告', remember_user_fact: '记住用户信息',
  handoff_to_human: '转人工', quote_previous_reply: '引用之前的回复', generate_image: '生成图片',
}

const isNew = computed(() => props.serviceId === 'new')
const form = ref<BotService | null>(null)
const snapshot = ref('')
const botName = ref('')
const models = ref<ModelProfile[]>([])
const knowledgeBases = ref<KnowledgeBase[]>([])
const skills = ref<Skill[]>([])
const toolOptions = ref<Array<{ name: string; label: string }>>([])
const loadError = ref('')
const saving = ref(false)
const accessToken = ref('')
const clearToken = ref(false)
const activeSection = ref('basic')
const livePanel = ref<InstanceType<typeof LiveDataPanel> | null>(null)
const chatModels = computed(() => models.value.filter(m => m.kind === 'chat'))
// 群消息判断可以用对话模型，也可以用只做判断的模型（如 TypeSafe Jev），判断模型排在前面
const decisionCandidates = computed(() => [...models.value.filter(m => m.kind === 'decision'), ...chatModels.value])
const imageModels = computed(() => models.value.filter(m => m.kind === 'image'))
const live = computed(() => form.value!.settings.liveData)
// 旧配置没有样式字段：填过状态页地址的沿用截网页，否则默认简报
const statusStyle = computed({
  get: () => live.value.statusImageStyle || (live.value.statusPageUrl ? 'page' : 'brief'),
  set: (value: StatusImageStyle) => { live.value.statusImageStyle = value },
})
const liveSource = computed(() => botServices(props.botId).liveData(props.serviceId))
const dirty = computed(() => !!form.value && (isNew.value || JSON.stringify(form.value) !== snapshot.value || !!accessToken.value.trim() || clearToken.value))
const liveBlockedReason = computed(() => {
  if (dirty.value) return '先保存修改再同步。'
  if (!JSON.parse(snapshot.value || '{}')?.settings?.liveData?.enabled) return '启用并保存后才能同步。'
  return ''
})

function apply(value: BotService) {
  value.settings.examples = value.settings.examples || []
  value.settings.replyQuietSeconds = value.settings.replyQuietSeconds || 6
  value.settings.replyMaxWaitSeconds = value.settings.replyMaxWaitSeconds || 20
  value.settings.liveData.statusPageUrl = value.settings.liveData.statusPageUrl || ''
  form.value = value
  snapshot.value = JSON.stringify(value)
  accessToken.value = ''
  clearToken.value = false
}

async function load() {
  loadError.value = ''
  try {
    const [list, bots, profiles, kbs, skillList, tools, defaults] = await Promise.all([
      botServices(props.botId).list(), api.bots.list(), api.models.list(), api.knowledge.list(), agentApi.skills.list(), agentApi.tools.list(), agentApi.settings.get(),
    ])
    botName.value = bots.find(b => b.id === props.botId)?.name || ''
    models.value = profiles
    knowledgeBases.value = kbs
    skills.value = skillList
    toolOptions.value = [
      ...tools.builtin.map(t => ({ name: t.name, label: toolLabels[t.name] || t.name })),
      ...tools.custom.filter(t => t.enabled).map(t => ({ name: t.name, label: t.name })),
    ]
    if (isNew.value) {
      apply({
        id: '', name: '', instructions: '', chatProfileId: '', imageProfileId: '', simpleProfileId: '', decisionProfileId: '',
        settings: {
          stylePrompt: defaults.stylePrompt, examples: structuredClone(defaults.examples || []), toolsEnabled: defaults.toolsEnabled, maxToolSteps: defaults.maxToolSteps,
          skillIds: null, toolNames: null, knowledgeBaseIds: null, evaluationEnabled: defaults.evaluationEnabled, staffMiningEnabled: defaults.staffMiningEnabled,
          autoPublishStaff: defaults.autoPublishStaff, autoPublishMinConfidence: defaults.autoPublishMinConfidence, learningKnowledgeBaseId: defaults.learningKnowledgeBaseId,
          handoffPauseMinutes: defaults.handoffPauseMinutes, replyQuietSeconds: defaults.replyQuietSeconds || 6, replyMaxWaitSeconds: defaults.replyMaxWaitSeconds || 20, liveData: { ...defaults.liveData, enabled: false, baseUrl: '', hasAccessToken: false, userId: '', statusPageUrl: '' },
        },
      })
    } else {
      const found = list.find(s => s.id === props.serviceId)
      if (!found) { loadError.value = '找不到这个 AI 客服，可能已被删除。'; return }
      apply(found)
    }
    nextTick(observeSections)
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : '加载失败'
  }
}

function discard() { if (snapshot.value) apply(JSON.parse(snapshot.value)) }
async function save() {
  if (!form.value || !form.value.name.trim()) return
  saving.value = true
  try {
    const record = { ...form.value, settings: { ...form.value.settings, examples: form.value.settings.examples.filter(e => e.question.trim() && e.answer.trim()) } }
    const token = accessToken.value.trim() ? accessToken.value.trim() : clearToken.value ? '' : undefined
    const { id } = await botServices(props.botId).save(record, token)
    ElMessage.success(isNew.value ? '客服已创建' : '设置已保存')
    snapshot.value = JSON.stringify(form.value)
    if (isNew.value) { await router.replace({ name: 'bot-service', params: { id: props.botId, serviceId: id } }); return }
    await load()
    livePanel.value?.load()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存失败')
  } finally { saving.value = false }
}

let observer: IntersectionObserver | undefined
function observeSections() {
  observer?.disconnect()
  observer = new IntersectionObserver((entries) => {
    const visible = entries.filter(e => e.isIntersecting).sort((a, b) => a.boundingClientRect.top - b.boundingClientRect.top)
    if (visible[0]) activeSection.value = visible[0].target.id
  }, { rootMargin: '-10% 0px -70% 0px' })
  sections.forEach(s => { const el = document.getElementById(s.id); if (el) observer!.observe(el) })
}
function scrollTo(id: string) { activeSection.value = id; document.getElementById(id)?.scrollIntoView({ behavior: 'smooth', block: 'start' }) }
function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value && !isNew.value) { event.preventDefault(); event.returnValue = '' } }
onBeforeRouteLeave(async () => {
  if (!dirty.value || saving.value) return true
  if (isNew.value && !form.value?.name.trim()) return true
  try {
    await ElMessageBox.confirm('还有未保存的修改，离开后会丢失。', '离开此页？', { confirmButtonText: '不保存，离开', cancelButtonText: '留下', type: 'warning' })
    return true
  } catch { return false }
})
watch(() => [props.botId, props.serviceId], () => { form.value = null; load() })
onMounted(() => { load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { observer?.disconnect(); window.removeEventListener('beforeunload', beforeUnload) })
</script>

<style scoped>
.live-card { padding: 20px 24px; }
.live-card :deep(.settings-panel) { border: 0; box-shadow: none; padding: 0; }
.settings-card h3 { margin-bottom: 12px; font-size: 14px; font-weight: 600; }
.pick-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 4px 16px; }
.pick-name { font-size: 13px; }
.token-field { display: flex; align-items: center; gap: 12px; }
.token-field .el-input { width: 280px; }
</style>
