<template>
  <div class="content-page">
    <PageHeader title="通用设置" description="管理所有客服共享的回复、记忆和工具设置。未分配 AI 客服的会话使用默认人设。">
      <el-button :icon="Refresh" @click="reload">重新加载</el-button>
    </PageHeader>
    <el-alert v-if="loadError" class="page-error-alert" type="error" :closable="false" show-icon :title="`设置加载失败：${loadError}`" />

    <div v-if="settings" class="settings-layout">
      <nav class="settings-toc" aria-label="设置目录">
        <a v-for="s in sections" :key="s.id" :class="{ active: activeSection === s.id }" @click="scrollTo(s.id)">{{ s.label }}</a>
      </nav>

      <div class="settings-sections">
        <section id="persona" class="settings-section">
          <header><h2>人设与风格</h2><p>未分配 AI 客服时使用这里的名称和说话风格。示例对话由所有客服共享。</p></header>
          <div class="settings-card">
            <el-form label-position="top">
              <el-form-item label="客服名称"><el-input v-model="settings.personaName" maxlength="20" style="max-width: 320px" /></el-form-item>
              <el-form-item label="说话风格">
                <el-input v-model="settings.stylePrompt" type="textarea" :autosize="{ minRows: 8, maxRows: 18 }" />
                <small class="form-tip">写给 AI 的说话规则。每个群在“群与会话”里填的补充提示词会接在后面。<el-button link type="primary" @click="resetStyle">恢复默认模板</el-button></small>
              </el-form-item>
            </el-form>
          </div>
          <div class="settings-card">
            <h3>示例对话 <small class="muted">从官方客服的真实回复里挑几条，语气和长度会更像你们的客服</small></h3>
            <div v-for="(ex, index) in settings.examples" :key="index" class="example-row">
              <el-input v-model="ex.question" placeholder="用户问" />
              <el-input v-model="ex.answer" type="textarea" :autosize="{ minRows: 1, maxRows: 4 }" placeholder="客服答" />
              <el-button :icon="Delete" circle aria-label="删除示例" @click="settings.examples.splice(index, 1)" />
            </div>
            <el-button :icon="Plus" @click="settings.examples.push({ question: '', answer: '' })">添加示例</el-button>
          </div>
        </section>

        <section id="length" class="settings-section">
          <header><h2>回复长度与格式</h2><p>控制每次回复写多长、要不要分几条发。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>群聊目标字数</strong><span>群里回复尽量控制在这个长度以内</span></div><el-input-number v-model="settings.groupReplyChars" :min="30" :max="2000" :step="10" /></div>
            <div class="setting-row"><div><strong>私聊目标字数</strong><span>私聊可以稍微详细一些</span></div><el-input-number v-model="settings.privateReplyChars" :min="30" :max="4000" :step="10" /></div>
            <div class="setting-row"><div><strong>清理格式和套话</strong><span>去掉标题、加粗等 Markdown 装饰和“希望对您有帮助”之类的话（代码块保留）</span></div><el-switch v-model="settings.sanitizeMarkdown" /></div>
            <div class="setting-row"><div><strong>长回复分条发送</strong><span>超过目标字数时按段落拆成几条，像真人分段打字</span></div><el-switch v-model="settings.splitLongReplies" /></div>
            <div v-if="settings.splitLongReplies" class="setting-row"><div><strong>最多拆成几条</strong></div><el-input-number v-model="settings.maxReplyParts" :min="1" :max="4" /></div>
            <div class="setting-row"><div><strong>单次输出 token 上限</strong><span>0 表示不限制。推理模型请保持 0，否则思考过程占满额度会导致空回复</span></div><el-input-number v-model="settings.maxOutputTokens" :min="0" :max="8000" :step="100" /></div>
          </div>
        </section>

        <section id="memory" class="settings-section">
          <header><h2>上下文与记忆</h2><p>回复时带上多少历史消息，以及要不要长期记住用户的信息。</p></header>
          <div class="settings-card">
            <h3>上下文</h3>
            <div class="setting-row"><div><strong>历史消息 token 预算</strong><span>从新到旧装入历史消息，直到用完预算。每个群的“上下文消息数”仍是条数上限</span></div><el-input-number v-model="settings.historyTokenBudget" :min="200" :max="32000" :step="100" /></div>
            <div class="setting-row"><div><strong>群聊只看提问人的对话</strong><span>只带上当前提问用户和官方客服的发言，不被其他人的闲聊干扰</span></div><el-switch v-model="settings.groupThreadMode" /></div>
            <div class="setting-row"><div><strong>检索前改写问题</strong><span>结合上下文把“那这个呢”改写成完整问题，知识库更容易搜到</span></div><el-switch v-model="settings.rewriteQuery" /></div>
            <div class="setting-row"><div><strong>超长历史自动摘要</strong><span>超出预算的旧消息压缩成摘要，而不是直接丢掉</span></div><el-switch v-model="settings.summaryEnabled" /></div>
          </div>
          <div class="settings-card">
            <h3>长期记忆</h3>
            <div class="setting-row"><div><strong>记住用户的关键信息</strong><span>每轮对话后自动记下分组、接入方式、常遇到的问题等。可以在“记忆”里查看和修改</span></div><el-switch v-model="settings.memoryEnabled" /></div>
            <div class="setting-row"><div><strong>每次回复最多带入几条记忆</strong></div><el-input-number v-model="settings.maxMemories" :min="1" :max="30" :disabled="!settings.memoryEnabled" /></div>
            <el-alert type="info" :closable="false" show-icon title="QQ 里同一个人在群聊和私聊的 ID 不同，记忆会按“机器人 + 用户 ID”分开保存。" />
          </div>
        </section>

        <section id="aux" class="settings-section">
          <header><h2>辅助模型</h2><p>改写问题、摘要、质检、记忆提取这些后台工作用哪个模型。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>简单辅助模型</strong><span>建议选便宜、快的模型。不选则使用接待客服的对话模型</span></div>
              <el-select v-model="settings.auxChatProfileId" clearable placeholder="跟随接待客服的对话模型"><el-option v-for="m in chatModels" :key="m.id" :label="`${m.name} · ${m.model}`" :value="m.id" /></el-select></div>
            <div class="setting-row"><div><strong>群消息判断模型</strong><span>逐条判断群消息是否需要回复，留空使用简单辅助模型</span></div>
              <el-select v-model="settings.decisionProfileId" clearable placeholder="使用简单辅助模型"><el-option v-for="m in chatModels" :key="m.id" :label="m.name" :value="m.id" :disabled="m.status === 'offline'" /></el-select></div>
            <div class="setting-row"><div><strong>Embedding 模型</strong><span>用于记忆召回和“没答上的问题”归类</span></div>
              <el-select v-model="settings.embeddingProfileId" clearable placeholder="使用默认 Embedding 配置"><el-option v-for="m in embeddingModels" :key="m.id" :label="`${m.name} · ${m.model}`" :value="m.id" /></el-select></div>
          </div>
        </section>

        <section id="learning" class="settings-section">
          <header><h2>学习与转人工</h2><p>机器人怎么从日常对话里积累知识，以及转人工后的行为。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>每轮自动质检</strong><span>找出没答上的问题、用户不满意的回复</span></div><el-switch v-model="settings.evaluationEnabled" /></div>
            <div class="setting-row"><div><strong>从官方客服的回答里学习</strong><span>官方客服在群里的回答会变成候选知识，等你审核</span></div><el-switch v-model="settings.staffMiningEnabled" /></div>
            <div class="setting-row"><div><strong>可信度够高时自动发布</strong><span>只对官方客服来源生效。默认关闭，全部人工审核</span></div><el-switch v-model="settings.autoPublishStaff" /></div>
            <div v-if="settings.autoPublishStaff" class="setting-row stack"><div><strong>自动发布的可信度门槛</strong></div><el-slider v-model="settings.autoPublishMinConfidence" :min="0.5" :max="1" :step="0.05" show-input /></div>
            <div class="setting-row"><div><strong>候选知识默认发布到</strong></div>
              <el-select v-model="settings.learningKnowledgeBaseId" clearable placeholder="选择知识库"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></div>
            <div class="setting-row"><div><strong>转人工后机器人安静多久</strong><span>期间机器人不回复这位用户；在人工台结单后会提前恢复</span></div><span><el-input-number v-model="settings.handoffPauseMinutes" :min="1" :max="1440" /><span class="unit-label">分钟</span></span></div>
          </div>
        </section>

        <section id="live" class="settings-section">
          <header><h2>网站实时数据</h2><p>接入 new-api 站点，让机器人能回答价格、分组状态和公告这类会变的问题。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>启用实时数据</strong><span>开启后机器人可以查询站点的最新价格和公告</span></div><el-switch v-model="settings.liveData.enabled" /></div>
            <template v-if="settings.liveData.enabled">
              <div class="setting-row"><div><strong>网站地址</strong></div><el-input v-model="settings.liveData.baseUrl" placeholder="https://cooper-api.com" /></div>
              <div class="setting-row"><div><strong>系统访问令牌（可选）</strong><span>new-api 后台“个人设置 → 系统访问令牌”。只有价格页需要登录时才填，加密保存</span></div>
                <div>
                  <el-input v-model="accessToken" type="password" show-password :placeholder="settings.liveData.hasAccessToken && !clearToken ? '已保存，留空不修改' : '公开接口不用填'" />
                  <el-button v-if="settings.liveData.hasAccessToken && !clearToken" link type="danger" @click="clearToken = true; accessToken = ''">清除已保存的令牌</el-button>
                  <small v-if="clearToken" class="form-tip">保存后会清除已保存的令牌。</small>
                </div>
              </div>
              <div class="setting-row"><div><strong>用户 ID</strong><span>配合令牌使用（New-Api-User）</span></div><el-input v-model="settings.liveData.userId" /></div>
              <div class="setting-row"><div><strong>刷新间隔</strong></div><span><el-input-number v-model="settings.liveData.refreshSeconds" :min="30" :max="3600" :step="30" /><span class="unit-label">秒</span></span></div>
              <div class="setting-row"><div><strong>把公告和异常分组放进提示词</strong><span>每次回复都会参考最新公告和异常分组</span></div><el-switch v-model="settings.liveData.injectSnapshot" /></div>
              <div class="setting-row"><div><strong>公告同步到知识库</strong><span>公告变化时自动更新，下线时自动归档</span></div><el-switch v-model="settings.liveData.syncAnnouncements" /></div>
              <div v-if="settings.liveData.syncAnnouncements" class="setting-row"><div><strong>公告同步到</strong></div>
                <el-select v-model="settings.liveData.announcementKnowledgeBaseId" clearable placeholder="选择知识库"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></div>
              <div class="setting-row"><div><strong>报价附带说明</strong><span>回答价格时附在后面的一句话</span></div><el-input v-model="settings.liveData.priceNote" /></div>
            </template>
          </div>
          <div class="settings-card live-card">
            <LiveDataPanel ref="livePanel" :blocked-reason="liveBlockedReason" />
          </div>
        </section>

        <section id="skills" class="settings-section">
          <header><h2>技能与工具</h2><p>技能告诉 AI 遇到某类问题怎么处理；工具让 AI 能查询数据或执行操作。技能和工具的修改会立即保存。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>允许 AI 调用工具</strong><span>关闭后 AI 只根据知识库和上下文回答</span></div><el-switch v-model="settings.toolsEnabled" /></div>
            <div v-if="settings.toolsEnabled" class="setting-row"><div><strong>一次回复最多连续调用几轮</strong></div><el-input-number v-model="settings.maxToolSteps" :min="1" :max="8" /></div>
          </div>
          <SkillsPanel />
          <ToolsPanel />
        </section>

        <transition name="save-bar">
          <div v-if="dirty" class="save-bar" role="status">
            <el-icon><WarningFilled /></el-icon>
            <span>有未保存的修改</span>
            <span class="spacer" />
            <el-button class="ghost" @click="discard">放弃修改</el-button>
            <el-button type="primary" :loading="saving" @click="save">保存</el-button>
          </div>
        </transition>
      </div>
    </div>
    <div v-else-if="!loadError" v-loading="true" style="height: 300px" />
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { Delete, Plus, Refresh, WarningFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import LiveDataPanel from '@/components/agent/LiveDataPanel.vue'
import SkillsPanel from '@/components/agent/SkillsPanel.vue'
import ToolsPanel from '@/components/agent/ToolsPanel.vue'
import { api } from '@/api'
import { agentApi, type AgentSettings } from '@/api/agent'
import type { KnowledgeBase, ModelProfile } from '@/types'

const DEFAULT_STYLE = `你是平台的在线客服，像一位熟练、友好的真人客服那样在 QQ 里聊天。
说话规则：
1. 先直接给结论，一般 1～3 句话说完；只回答用户问到的那一点。
2. 口语化、自然，不用标题、列表、加粗等格式；需要给配置或代码时才使用代码块。
3. 不说“作为 AI”“希望对您有帮助”“如有其他问题请随时联系”这类套话，不重复用户的问题。
4. 信息不够时，先用一句话问清最关键的一项，不要把所有可能性都列出来。
5. 内容确实较多时，先给最关键的一步，再问一句“要我把详细步骤发你吗？”。
6. 不确定的事情不要编，直说不确定，并建议转人工或查看官网。
7. 价格、分组状态、公告这类实时信息，必须以工具查询结果为准。`

const sections = [
  { id: 'persona', label: '人设与风格' },
  { id: 'length', label: '回复长度与格式' },
  { id: 'memory', label: '上下文与记忆' },
  { id: 'aux', label: '辅助模型' },
  { id: 'learning', label: '学习与转人工' },
  { id: 'live', label: '网站实时数据' },
  { id: 'skills', label: '技能与工具' },
]

const settings = ref<AgentSettings | null>(null)
const snapshot = ref('')
const models = ref<ModelProfile[]>([])
const knowledgeBases = ref<KnowledgeBase[]>([])
const saving = ref(false)
const loadError = ref('')
const accessToken = ref('')
const clearToken = ref(false)
const activeSection = ref('persona')
const livePanel = ref<InstanceType<typeof LiveDataPanel> | null>(null)
const chatModels = computed(() => models.value.filter(m => m.kind === 'chat'))
const embeddingModels = computed(() => models.value.filter(m => m.kind === 'embedding'))
const savedLiveEnabled = computed(() => snapshot.value ? (JSON.parse(snapshot.value) as AgentSettings).liveData.enabled : false)

const dirty = computed(() => !!settings.value && (JSON.stringify(settings.value) !== snapshot.value || !!accessToken.value.trim() || clearToken.value))
const liveBlockedReason = computed(() => {
  if (dirty.value) return '设置有未保存的修改，请先点底部的“保存”，再同步。'
  if (!savedLiveEnabled.value) return '请先打开“启用实时数据”、填写网站地址并保存，再同步。'
  return ''
})

function applySettings(value: AgentSettings) {
  value.examples = value.examples || []
  settings.value = value
  snapshot.value = JSON.stringify(value)
  accessToken.value = ''
  clearToken.value = false
}

async function load(preserveEdits = false) {
  loadError.value = ''
  try {
    const [s, m, kbs] = await Promise.all([agentApi.settings.get(), api.models.list(), api.knowledge.list()])
    if (!preserveEdits || !dirty.value) applySettings(s)
    models.value = m
    knowledgeBases.value = kbs
    nextTick(observeSections)
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : '加载失败'
  }
}
async function reload() {
  if (dirty.value) await ElMessageBox.confirm('重新加载会丢掉还没保存的修改。', '重新加载', { confirmButtonText: '丢掉修改并重新加载', cancelButtonText: '取消', type: 'warning' })
  await load()
}
function resetStyle() { if (settings.value) settings.value.stylePrompt = DEFAULT_STYLE }
function discard() { if (snapshot.value) applySettings(JSON.parse(snapshot.value)) }
async function save() {
  if (!settings.value) return
  saving.value = true
  try {
    const s = { ...settings.value, examples: settings.value.examples.filter(e => e.question.trim() && e.answer.trim()) }
    const token = accessToken.value.trim() ? accessToken.value.trim() : clearToken.value ? '' : undefined
    applySettings(await agentApi.settings.save(s, token))
    ElMessage.success('设置已保存')
    livePanel.value?.load()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存失败')
  } finally {
    saving.value = false
  }
}

// 目录高亮：滚动时跟随当前所在的分区
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

function beforeUnload(event: BeforeUnloadEvent) { if (dirty.value) { event.preventDefault(); event.returnValue = '' } }
onBeforeRouteLeave(async () => {
  if (!dirty.value) return true
  try {
    await ElMessageBox.confirm('智能体设置还有未保存的修改，离开后会丢失。', '离开此页？', { confirmButtonText: '不保存，离开', cancelButtonText: '留下', type: 'warning' })
    return true
  } catch { return false }
})
onMounted(() => { load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { observer?.disconnect(); window.removeEventListener('beforeunload', beforeUnload) })
useAdminEvents(['system_settings','knowledge_bases','model_profiles'], async () => { if (!dirty.value) await load(true) })
</script>

<style scoped>
.settings-card h3 small { font-weight: 400; font-size: 13px; margin-left: 6px; }
.live-card { padding: 20px 24px; }
.live-card :deep(.settings-panel) { border: 0; box-shadow: none; padding: 0; }
.settings-section :deep(.table-panel.block) { margin-top: 12px; margin-bottom: 0; }
</style>
