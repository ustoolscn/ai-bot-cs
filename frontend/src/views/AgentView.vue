<template>
  <div class="content-page">
    <PageHeader title="智能体" description="配置客服的说话风格、记忆、技能工具、网站实时数据和效果评测。">
      <el-button :icon="Refresh" @click="load">刷新</el-button>
      <el-button v-if="settingsTab" type="primary" :loading="saving" :disabled="!settings" @click="save">保存设置</el-button>
    </PageHeader>
    <el-alert v-if="loadError" class="page-error-alert" type="error" :closable="false" show-icon :title="loadError" />
    <el-tabs v-model="tab" class="agent-tabs">
      <el-tab-pane label="回复风格" name="style">
        <div v-if="settings" class="agent-grid">
          <section class="settings-panel">
            <h2>人设与风格</h2>
            <el-form label-position="top">
              <el-form-item label="客服名称"><el-input v-model="settings.personaName" maxlength="20" /></el-form-item>
              <el-form-item label="风格提示词">
                <el-input v-model="settings.stylePrompt" type="textarea" :rows="12" />
                <small class="form-tip">决定说话方式。会话里的“系统提示词”仍然生效，作为业务说明追加在后面。</small>
                <el-button link type="primary" @click="resetStyle">恢复默认模板</el-button>
              </el-form-item>
            </el-form>
          </section>
          <section class="settings-panel">
            <h2>长度与格式</h2>
            <el-form label-position="top">
              <div class="form-row">
                <el-form-item label="群聊目标字数"><el-input-number v-model="settings.groupReplyChars" :min="30" :max="2000" :step="10" /></el-form-item>
                <el-form-item label="私聊目标字数"><el-input-number v-model="settings.privateReplyChars" :min="30" :max="4000" :step="10" /></el-form-item>
              </div>
              <el-form-item><el-checkbox v-model="settings.sanitizeMarkdown">清理标题、加粗等 Markdown 装饰和“希望对您有帮助”类套话（保留代码块）</el-checkbox></el-form-item>
              <el-form-item><el-checkbox v-model="settings.splitLongReplies">超过目标字数时按段落拆成多条发送，像真人分段打字</el-checkbox></el-form-item>
              <el-form-item label="最多拆成几条"><el-input-number v-model="settings.maxReplyParts" :min="1" :max="4" :disabled="!settings.splitLongReplies" /></el-form-item>
              <el-form-item label="单次输出 token 上限">
                <el-input-number v-model="settings.maxOutputTokens" :min="0" :max="8000" :step="100" />
                <small class="form-tip">0 表示不限制。推理模型请保持 0，否则可能因思考占用额度导致空回复。</small>
              </el-form-item>
            </el-form>
          </section>
          <section class="settings-panel span-2">
            <h2>示例对话 <small class="muted">从官方客服的真实回复里挑选，让语气和长度更像你们的客服</small></h2>
            <div v-for="(ex, index) in settings.examples" :key="index" class="example-row">
              <el-input v-model="ex.question" placeholder="用户问" />
              <el-input v-model="ex.answer" type="textarea" :autosize="{ minRows: 1, maxRows: 4 }" placeholder="客服答" />
              <el-button :icon="Delete" circle @click="settings.examples.splice(index, 1)" />
            </div>
            <el-button :icon="Plus" @click="settings.examples.push({ question: '', answer: '' })">添加示例</el-button>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane label="上下文与记忆" name="memory">
        <div v-if="settings" class="agent-grid">
          <section class="settings-panel">
            <h2>上下文</h2>
            <el-form label-position="top">
              <el-form-item label="历史消息 token 预算">
                <el-input-number v-model="settings.historyTokenBudget" :min="200" :max="32000" :step="100" />
                <small class="form-tip">按预算从新到旧装入历史；会话里的“上下文消息数”仍作为条数上限。</small>
              </el-form-item>
              <el-form-item><el-checkbox v-model="settings.groupThreadMode">群聊只看当前提问用户的对话线程（加上官方客服发言），不被别人的闲聊干扰</el-checkbox></el-form-item>
              <el-form-item><el-checkbox v-model="settings.rewriteQuery">检索前结合上下文改写问题（解决“那这个呢”检索不到）</el-checkbox></el-form-item>
              <el-form-item><el-checkbox v-model="settings.summaryEnabled">超出预算的历史自动滚动摘要</el-checkbox></el-form-item>
            </el-form>
          </section>
          <section class="settings-panel">
            <h2>长期记忆</h2>
            <el-form label-position="top">
              <el-form-item><el-checkbox v-model="settings.memoryEnabled">每轮对话后自动记住用户的关键信息（分组、接入方式、常见问题等）</el-checkbox></el-form-item>
              <el-form-item label="每次回复最多带入的记忆条数"><el-input-number v-model="settings.maxMemories" :min="1" :max="30" /></el-form-item>
              <el-alert type="info" :closable="false" show-icon title="QQ 群聊和私聊中同一个人的 ID 不同，记忆按“机器人 + 用户 ID”分别保存。可在“学习”页查看和修改。" />
            </el-form>
          </section>
          <section class="settings-panel">
            <h2>辅助模型</h2>
            <el-form label-position="top">
              <el-form-item label="辅助对话模型（改写、摘要、质检、记忆、挖掘）">
                <el-select v-model="settings.auxChatProfileId" clearable placeholder="默认使用会话的对话模型">
                  <el-option v-for="m in chatModels" :key="m.id" :label="`${m.name} · ${m.model}`" :value="m.id" />
                </el-select>
                <small class="form-tip">建议选一个便宜、快速的模型。</small>
              </el-form-item>
              <el-form-item label="Embedding 模型（记忆召回、缺口聚类）">
                <el-select v-model="settings.embeddingProfileId" clearable placeholder="默认使用默认 Embedding 配置">
                  <el-option v-for="m in embeddingModels" :key="m.id" :label="`${m.name} · ${m.model}`" :value="m.id" />
                </el-select>
              </el-form-item>
            </el-form>
          </section>
          <section class="settings-panel">
            <h2>学习与转人工</h2>
            <el-form label-position="top">
              <el-form-item><el-checkbox v-model="settings.evaluationEnabled">每轮自动质检：识别没答上的问题、用户的隐式负反馈</el-checkbox></el-form-item>
              <el-form-item><el-checkbox v-model="settings.staffMiningEnabled">从官方客服的群内回答中挖掘候选知识</el-checkbox></el-form-item>
              <el-form-item><el-checkbox v-model="settings.autoPublishStaff">官方客服来源且置信度达标时自动发布（默认需人工审核）</el-checkbox></el-form-item>
              <el-form-item label="自动发布置信度阈值"><el-slider v-model="settings.autoPublishMinConfidence" :min="0.5" :max="1" :step="0.05" :disabled="!settings.autoPublishStaff" show-input /></el-form-item>
              <el-form-item label="候选知识默认发布到">
                <el-select v-model="settings.learningKnowledgeBaseId" clearable placeholder="选择知识库"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
              </el-form-item>
              <el-form-item label="转人工后机器人对该用户静默（分钟）"><el-input-number v-model="settings.handoffPauseMinutes" :min="1" :max="1440" /></el-form-item>
            </el-form>
          </section>
        </div>
      </el-tab-pane>

      <el-tab-pane label="网站数据" name="live">
        <div v-if="settings" class="agent-grid">
          <section class="settings-panel">
            <h2>new-api 站点</h2>
            <el-form label-position="top">
              <el-form-item><el-switch v-model="settings.liveData.enabled" active-text="启用实时数据" /></el-form-item>
              <el-form-item label="网站地址"><el-input v-model="settings.liveData.baseUrl" placeholder="https://cooper-api.com" /></el-form-item>
              <el-form-item label="系统访问令牌（可选）">
                <el-input v-model="accessToken" type="password" show-password :placeholder="settings.liveData.hasAccessToken ? '已保存，留空不修改' : '公开接口无需填写'" />
                <small class="form-tip">new-api 后台“个人设置 → 系统访问令牌”。仅当价格页需要登录时填写，加密保存。</small>
                <el-button v-if="settings.liveData.hasAccessToken" link type="danger" @click="clearToken = true; accessToken = ''">清除已保存令牌</el-button>
              </el-form-item>
              <el-form-item label="用户 ID（配合令牌，New-Api-User）"><el-input v-model="settings.liveData.userId" /></el-form-item>
              <el-form-item label="刷新间隔（秒）"><el-input-number v-model="settings.liveData.refreshSeconds" :min="30" :max="3600" :step="30" /></el-form-item>
              <el-form-item><el-checkbox v-model="settings.liveData.injectSnapshot">把最新公告和异常分组放进每次回复的提示词</el-checkbox></el-form-item>
              <el-form-item><el-checkbox v-model="settings.liveData.syncAnnouncements">公告自动同步到知识库（内容变化自动更新、下线自动归档）</el-checkbox></el-form-item>
              <el-form-item label="公告同步到"><el-select v-model="settings.liveData.announcementKnowledgeBaseId" clearable :disabled="!settings.liveData.syncAnnouncements"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></el-form-item>
              <el-form-item label="报价附带说明"><el-input v-model="settings.liveData.priceNote" /></el-form-item>
            </el-form>
          </section>
          <LiveDataPanel ref="livePanel" />
        </div>
      </el-tab-pane>

      <el-tab-pane label="技能与工具" name="skills">
        <div v-if="settings" class="settings-panel tools-toggle">
          <el-checkbox v-model="settings.toolsEnabled">允许 AI 调用工具</el-checkbox>
          <span class="muted">最多连续调用</span>
          <el-input-number v-model="settings.maxToolSteps" :min="1" :max="8" size="small" />
          <span class="muted">轮（修改后点右上角保存设置）</span>
        </div>
        <SkillsPanel />
        <ToolsPanel />
      </el-tab-pane>

      <el-tab-pane label="效果评测" name="eval" lazy>
        <EvalPanel :chat-models="chatModels" :knowledge-bases="knowledgeBases" />
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Delete, Plus, Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import LiveDataPanel from '@/components/agent/LiveDataPanel.vue'
import SkillsPanel from '@/components/agent/SkillsPanel.vue'
import ToolsPanel from '@/components/agent/ToolsPanel.vue'
import EvalPanel from '@/components/agent/EvalPanel.vue'
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

const tab = ref('style')
const settings = ref<AgentSettings | null>(null)
const models = ref<ModelProfile[]>([])
const knowledgeBases = ref<KnowledgeBase[]>([])
const saving = ref(false)
const loadError = ref('')
const accessToken = ref('')
const clearToken = ref(false)
const livePanel = ref<InstanceType<typeof LiveDataPanel> | null>(null)
const chatModels = computed(() => models.value.filter(m => m.kind === 'chat'))
const embeddingModels = computed(() => models.value.filter(m => m.kind === 'embedding'))
const settingsTab = computed(() => tab.value !== 'eval')

onMounted(load)
async function load() {
  loadError.value = ''
  try {
    const [s, m, kbs] = await Promise.all([agentApi.settings.get(), api.models.list(), api.knowledge.list()])
    s.examples = s.examples || []
    settings.value = s
    models.value = m
    knowledgeBases.value = kbs
  } catch (error) {
    loadError.value = error instanceof Error ? error.message : '加载失败'
  }
}
function resetStyle() { if (settings.value) settings.value.stylePrompt = DEFAULT_STYLE }
async function save() {
  if (!settings.value) return
  saving.value = true
  try {
    const s = { ...settings.value, examples: settings.value.examples.filter(e => e.question.trim() && e.answer.trim()) }
    const token = accessToken.value.trim() ? accessToken.value.trim() : clearToken.value ? '' : undefined
    settings.value = await agentApi.settings.save(s, token)
    settings.value.examples = settings.value.examples || []
    accessToken.value = ''
    clearToken.value = false
    ElMessage.success('设置已保存')
    if (tab.value === 'live') livePanel.value?.load()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存失败')
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.agent-tabs :deep(.el-tabs__header) { margin-bottom: 18px; }
.agent-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; align-items: start; }
.span-2 { grid-column: span 2; }
.form-row { display: flex; gap: 16px; flex-wrap: wrap; }
.example-row { display: grid; grid-template-columns: 1fr 2fr auto; gap: 10px; margin-bottom: 10px; align-items: start; }
.tools-toggle { display: flex; align-items: center; gap: 10px; margin-bottom: 16px; flex-wrap: wrap; }
.muted { color: var(--muted); font-size: 12px; font-weight: 400; }
@media (max-width: 900px) { .agent-grid { grid-template-columns: 1fr; } .span-2 { grid-column: auto; } .example-row { grid-template-columns: 1fr; } }
</style>
