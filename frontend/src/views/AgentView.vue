<template>
  <div class="content-page">
    <PageHeader title="通用设置" description="所有 AI 客服共用的回复规则。单个客服的人设、模型和工具在“机器人与客服”里设置。">
      <el-button :icon="Refresh" @click="reload">重新加载</el-button>
    </PageHeader>

    <div v-if="settings" class="settings-layout">
      <nav class="settings-toc" aria-label="设置目录">
        <a v-for="s in sections" :key="s.id" :class="{ active: activeSection === s.id }" @click="scrollTo(s.id)">{{ s.label }}</a>
      </nav>

      <div class="settings-sections">
        

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
            <p class="form-tip" style="margin-top: 12px">QQ 里同一个人在群聊和私聊的 ID 不同，记忆会按“机器人 + 用户 ID”分开保存。</p>
          </div>
        </section>

        <section id="aux" class="settings-section">
          <header><h2>辅助模型</h2><p>改写问题、摘要、质检、记忆提取这些后台工作用哪个模型。</p></header>
          <div class="settings-card">
            <div class="setting-row"><div><strong>简单辅助模型</strong><span>建议选便宜、快的模型。不选则使用接待客服的对话模型</span></div>
              <el-select v-model="settings.auxChatProfileId" clearable placeholder="跟随接待客服的对话模型"><el-option v-for="m in chatModels" :key="m.id" :label="`${m.name} · ${m.model}`" :value="m.id" /></el-select></div>
            <div class="setting-row"><div><strong>群消息判断模型</strong><span>逐条判断群消息是否需要回复，留空使用简单辅助模型。带图片的消息始终由辅助模型判断</span></div>
              <el-select v-model="settings.decisionProfileId" clearable placeholder="使用简单辅助模型"><el-option v-for="m in decisionCandidates" :key="m.id" :label="m.kind === 'decision' ? `${m.name} · 判断模型` : m.name" :value="m.id" :disabled="m.status === 'offline'" /></el-select></div>
            <div class="setting-row"><div><strong>Embedding 模型</strong><span>用于记忆召回和“没答上的问题”归类</span></div>
              <el-select v-model="settings.embeddingProfileId" clearable placeholder="使用默认 Embedding 配置"><el-option v-for="m in embeddingModels" :key="m.id" :label="`${m.name} · ${m.model}`" :value="m.id" /></el-select></div>
          </div>
        </section>

        

        

        <section id="skills" class="settings-section">
          <header><h2>技能与工具库</h2><p>在这里维护，每个 AI 客服里选择使用哪些。</p></header>
          <SkillsPanel />
          <ToolsPanel />
        </section>

        <transition name="save-bar">
          <div v-if="dirty" class="save-bar" role="status">
            <el-icon><WarningFilled /></el-icon>
            <span>有未保存的修改</span>
            <span class="spacer" />
            <el-button @click="discard">放弃修改</el-button>
            <el-button type="primary" :loading="saving" @click="save">保存设置</el-button>
          </div>
        </transition>
      </div>
    </div>
    <section v-else-if="loadError" class="panel"><EmptyState mood="thinking" title="设置加载失败" :description="loadError"><el-button @click="load()">重新加载</el-button></EmptyState></section>
    <div v-else v-loading="true" style="height: 300px" />
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { Refresh, WarningFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import SkillsPanel from '@/components/agent/SkillsPanel.vue'
import ToolsPanel from '@/components/agent/ToolsPanel.vue'
import { api } from '@/api'
import { agentApi, type AgentSettings } from '@/api/agent'
import type { KnowledgeBase, ModelProfile } from '@/types'

const sections = [
  { id: 'length', label: '回复长度与格式' },
  { id: 'memory', label: '上下文与记忆' },
  { id: 'aux', label: '辅助模型' },
  { id: 'skills', label: '技能与工具库' },
]

const settings = ref<AgentSettings | null>(null)
const snapshot = ref('')
const models = ref<ModelProfile[]>([])
const knowledgeBases = ref<KnowledgeBase[]>([])
const saving = ref(false)
const loadError = ref('')
const activeSection = ref('length')
const chatModels = computed(() => models.value.filter(m => m.kind === 'chat'))
// 群消息判断可以用对话模型，也可以用只做判断的模型（如 TypeSafe Jev），判断模型排在前面
const decisionCandidates = computed(() => [...models.value.filter(m => m.kind === 'decision'), ...chatModels.value])
const embeddingModels = computed(() => models.value.filter(m => m.kind === 'embedding'))

const dirty = computed(() => !!settings.value && (JSON.stringify(settings.value) !== snapshot.value))

function applySettings(value: AgentSettings) {
  value.examples = value.examples || []
  settings.value = value
  snapshot.value = JSON.stringify(value)
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
function discard() { if (snapshot.value) applySettings(JSON.parse(snapshot.value)) }
async function save() {
  if (!settings.value) return
  saving.value = true
  try {
    const s = { ...settings.value, examples: settings.value.examples.filter(e => e.question.trim() && e.answer.trim()) }
    applySettings(await agentApi.settings.save(s))
    ElMessage.success('设置已保存')
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
    await ElMessageBox.confirm('通用设置还有未保存的修改，离开后会丢失。', '离开此页？', { confirmButtonText: '不保存，离开', cancelButtonText: '留下', type: 'warning' })
    return true
  } catch { return false }
})
onMounted(() => { load(); window.addEventListener('beforeunload', beforeUnload) })
onBeforeUnmount(() => { observer?.disconnect(); window.removeEventListener('beforeunload', beforeUnload) })
useAdminEvents(['system_settings','knowledge_bases','model_profiles'], async () => { if (!dirty.value) await load(true) })
</script>

<style scoped>
.live-card { padding: 20px 24px; }
.live-card :deep(.settings-panel) { border: 0; box-shadow: none; padding: 0; }
.settings-section :deep(.table-panel.block) { margin-top: 12px; margin-bottom: 0; }
</style>
