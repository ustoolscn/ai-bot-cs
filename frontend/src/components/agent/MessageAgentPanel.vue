<template>
  <div v-if="detail" class="agent-panel">
    <section v-if="run?.steps?.length || run?.rewrittenQuery" class="ap-section">
      <h3>Agent 步骤 <small>{{ run?.steps?.length || 0 }} 步 · 点一行看入参和结果</small></h3>
      <p v-if="run?.rewrittenQuery" class="ap-rewrite"><el-icon><EditPen /></el-icon><span>检索用的问题</span><strong>{{ run.rewrittenQuery }}</strong></p>
      <ol class="ap-steps">
        <li v-for="(s, i) in run?.steps || []" :key="i" :class="{ error: s.error, open: openStep === i }">
          <button type="button" class="ap-step" :aria-expanded="openStep === i" @click="openStep = openStep === i ? -1 : i">
            <el-icon class="ap-icon" :class="'is-' + s.type"><component :is="stepIcon(s.type)" /></el-icon>
            <span class="ap-name" :title="s.name">{{ s.name || stepType[s.type] || s.type }}</span>
            <span class="ap-track"><i :style="{ transform: `scaleX(${Math.max(0.02, s.latencyMs / maxStepMs)})` }" /></span>
            <span class="ap-ms">{{ stageSeconds(s.latencyMs) }}</span>
            <el-icon class="ap-chevron"><ArrowRight /></el-icon>
          </button>
          <div v-if="openStep === i" class="ap-body">
            <template v-if="s.input"><small>入参</small><pre>{{ s.input }}</pre></template>
            <template v-if="s.output"><small>结果</small><pre>{{ s.output }}</pre></template>
            <template v-if="s.error"><small>错误</small><pre class="err">{{ s.error }}</pre></template>
            <p v-if="!s.input && !s.output && !s.error" class="muted">这一步没有记录内容。</p>
          </div>
        </li>
      </ol>
    </section>

    <section v-if="perception || run?.directorNotes || recalled.length" class="ap-section">
      <h3>情绪感知与人设导演</h3>
      <div v-if="perception" class="chips ap-history">
        <el-tag v-if="perception.userMood" size="small" :type="moodTagType(perception.userMood)">情绪：{{ perception.userMood }}<template v-if="perception.moodIntensity != null"> ({{ Math.round(perception.moodIntensity * 100) }}%)</template></el-tag>
        <el-tag v-if="perception.replyMode" size="small" effect="plain">模式：{{ replyModeText[perception.replyMode] || perception.replyMode }}</el-tag>
        <el-tag v-if="perception.isChimeIn" size="small" type="warning" effect="plain">主动搭茬</el-tag>
        <el-tag v-if="perception.sarcasm" size="small" type="danger" effect="plain">疑似反话</el-tag>
        <el-tag v-if="perception.needEmpathyFirst" size="small" type="warning" effect="plain">共情优先</el-tag>
        <el-tag v-if="perception.memoryFocus && perception.memoryFocus !== 'all'" size="small" effect="plain">聚焦：{{ focusText[perception.memoryFocus] || perception.memoryFocus }}</el-tag>
        <el-tag v-if="perception.topicHint" size="small" type="info" effect="plain">话题：{{ perception.topicHint }}</el-tag>
      </div>
      <div v-if="recalled.length" class="ap-recalled">
        <small>本轮召回记忆（{{ recalled.length }} 条）</small>
        <ul>
          <li v-for="(m, idx) in recalled" :key="m.id || idx">
            <span class="ap-kind">{{ memKindText[m.kind] || m.kind }}</span>
            <span>{{ m.content }}</span>
            <small v-if="m.score != null" class="num">{{ (m.score * 100).toFixed(0) }}%</small>
          </li>
        </ul>
      </div>
      <div v-if="run?.directorNotes" class="ap-director">
        <button type="button" class="ap-director-toggle" @click="showDirector = !showDirector">
          <span>{{ showDirector ? '收起导演指令' : '查看本轮动态导演指令' }}</span>
          <el-icon :class="{ open: showDirector }"><ArrowRight /></el-icon>
        </button>
        <pre v-if="showDirector">{{ run.directorNotes }}</pre>
      </div>
    </section>

    <section v-if="evaluation || (detail.replies || []).length > 1" class="ap-section">
      <h3>回复结果</h3>
      <div v-if="evaluation" class="chips">
        <el-tag :type="evaluation.answered ? 'success' : 'danger'" size="small">{{ evaluation.answered ? '已解决' : '未解决' }}</el-tag>
        <el-tag v-if="evaluation.too_long" type="warning" size="small">回答偏长</el-tag>
        <el-tag v-if="evaluation.knowledge_gap" type="danger" size="small" effect="plain">缺口：{{ evaluation.knowledge_gap }}</el-tag>
      </div>
      <ol v-if="(detail.replies || []).length > 1" class="ap-replies">
        <li v-for="(r, i) in detail.replies" :key="r.id"><small>第 {{ i + 1 }} 段 · {{ r.eventType === 'HUMAN_REPLY' ? '人工' : 'AI' }} · <StatusBadge :status="replyStatus(r.status)" :text="replyText_[r.status]" /></small><p><MsgText :text="r.content" /></p></li>
      </ol>
    </section>

    <section class="ap-section">
      <h3>评价与纠错</h3>
      <div v-if="detail.feedback?.length" class="chips ap-history">
        <el-tag v-for="f in detail.feedback" :key="f.id" size="small" :type="['good', 'helpful'].includes(f.rating) ? 'success' : 'danger'" effect="plain">{{ feedbackSourceText[f.source] || f.source }}：{{ ratingText[f.rating] || f.rating }}</el-tag>
      </div>
      <div class="ap-form" @paste="correctPicker?.fromClipboard($event)">
        <div class="ap-ratings" role="radiogroup" aria-label="这条回复怎么样">
          <button v-for="r in ratings" :key="r" type="button" role="radio" :aria-checked="rating === r" class="ap-rating" :class="[{ active: rating === r }, r === 'good' ? 'is-good' : 'is-bad']" @click="rating = rating === r ? '' : r">{{ ratingText[r] }}</button>
        </div>
        <Transition name="ap-reveal">
          <div v-if="rating && rating !== 'good'" class="ap-correct">
            <el-input v-model="corrected" type="textarea" :rows="3" placeholder="正确的回答应该是…（选填，填了会生成候选知识）" />
            <MaterialPicker ref="correctPicker" v-model="correctMaterials" :disabled="!corrected.trim()" :hint="corrected.trim() ? '配上演示截图或录屏，以后 AI 答到这个问题会先发文字、再按顺序发素材。可直接粘贴截图' : '先写好正确答案，再添加演示素材'" />
          </div>
        </Transition>
        <div class="ap-actions">
          <div v-if="corrected.trim()" class="ap-options">
            <el-select v-model="kbID" placeholder="放进哪个知识库" style="width: 180px"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
            <el-checkbox v-model="publish">直接发布为问答</el-checkbox>
          </div>
          <el-button type="primary" :disabled="!rating" :loading="saving" @click="submitFeedback">提交评价</el-button>
        </div>
      </div>
    </section>

    <section class="ap-section">
      <h3>人工回复</h3>
      <div class="ap-form" @paste="replyPicker?.fromClipboard($event)">
        <el-input v-model="replyText" type="textarea" :rows="2" placeholder="以机器人身份补充回复（群聊 5 分钟、私聊 60 分钟内有效）" />
        <MaterialPicker ref="replyPicker" v-model="replyMaterials" hint="随回复按顺序发出的截图或录屏，可直接粘贴截图" />
        <div class="ap-actions"><el-checkbox v-model="learn" :disabled="!replyText.trim()">同时提交为候选知识</el-checkbox><el-button type="primary" plain :disabled="!replyText.trim() && !replyMaterials.length" :loading="replying" @click="sendReply">{{ replyMaterials.length ? `发送回复和 ${replyMaterials.length} 个素材` : '发送回复' }}</el-button></div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, ref, watch } from 'vue'
import { ArrowRight, Cpu, EditPen, SetUp } from '@element-plus/icons-vue'
import StatusBadge from '@/components/StatusBadge.vue'
import MsgText from '@/components/MsgText.vue'
import MaterialPicker from '@/components/MaterialPicker.vue'
import { stageSeconds } from '@/utils/stage-meta'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import { agentApi, feedbackSourceText, ratingText, type MaterialDraft, type MessageAgentDetail } from '@/api/agent'
import type { KnowledgeBase } from '@/types'

const props = defineProps<{ messageId: string }>()
const stepType: Record<string, string> = { rewrite: '改写', model: '模型', tool: '工具' }
const ratings = ['good', 'too_long', 'stiff', 'wrong', 'bad']
const detail = ref<MessageAgentDetail | null>(null)
const knowledgeBases = ref<KnowledgeBase[]>([])
const rating = ref('')
const corrected = ref('')
const correctMaterials = ref<MaterialDraft[]>([])
const replyMaterials = ref<MaterialDraft[]>([])
const correctPicker = ref<InstanceType<typeof MaterialPicker>>()
const replyPicker = ref<InstanceType<typeof MaterialPicker>>()
const kbID = ref('')
const publish = ref(false)
const saving = ref(false)
const replyText = ref('')
const learn = ref(true)
const replying = ref(false)
const run = computed(() => detail.value?.agentRuns?.[0])
const perception = computed(() => {
  const p = run.value?.perception
  return p && Object.keys(p).length ? p : null
})
const recalled = computed(() => run.value?.recalledMemories || [])
const showDirector = ref(false)
const replyModeText: Record<string, string> = { help: '解答', empathy: '共情陪伴', banter: '闲聊接梗', brief_ack: '简短搭话' }
const focusText: Record<string, string> = { facts: '事实偏好', episodes: '经历老梗', inside_jokes: '老梗默契', issues: '待决问题', none: '跳过' }
const memKindText: Record<string, string> = { profile: '档案', fact: '事实', preference: '偏好', issue: '问题', episode: '共同经历', inside_joke: '老梗', social: '关系' }
function moodTagType(mood: string) {
  if (['愤怒', '不满', '委屈', '焦虑', '着急'].includes(mood)) return 'danger'
  if (['疲惫', '困惑'].includes(mood)) return 'warning'
  if (['开心', '兴奋', '调侃'].includes(mood)) return 'success'
  return 'info'
}
const openStep = ref(-1)
const maxStepMs = computed(() => Math.max(1, ...(run.value?.steps || []).map(s => s.latencyMs || 0)))
const replyText_: Record<string, string> = { sent: '已发送', failed: '发送失败', pending: '待发送', processing: '发送中', expired: '已过期' }
function replyStatus(status: string) { return status === 'sent' ? 'success' : status === 'expired' ? 'warning' : status }
function stepIcon(type: string) { return type === 'tool' ? SetUp : type === 'rewrite' ? EditPen : Cpu }
const evaluation = computed(() => {
  const e = run.value?.evaluation as Record<string, unknown> | undefined
  return e && Object.keys(e).length ? e as { answered?: boolean; too_long?: boolean; knowledge_gap?: string } : null
})

async function load() {
  if (!props.messageId) return
  try {
    const [d, kbs, settings] = await Promise.all([agentApi.messages.detail(props.messageId), knowledgeBases.value.length ? Promise.resolve(knowledgeBases.value) : api.knowledge.list(), agentApi.settings.get().catch(() => null)])
    detail.value = d
    knowledgeBases.value = kbs
    if (!kbID.value) kbID.value = settings?.learningKnowledgeBaseId || kbs[0]?.id || ''
  } catch { detail.value = null }
}
watch(() => props.messageId, () => { openStep.value = -1; rating.value = ''; corrected.value = ''; correctMaterials.value = []; replyText.value = ''; replyMaterials.value = []; load() }, { immediate: true })

async function submitFeedback() {
  saving.value = true
  try {
    const r = await agentApi.messages.feedback(props.messageId, { rating: rating.value, correctedAnswer: corrected.value.trim() || undefined, publish: publish.value, knowledgeBaseId: kbID.value || undefined }, corrected.value.trim() ? correctMaterials.value : undefined)
    ElMessage.success(r.entryId ? '已发布为 FAQ' : r.candidateId ? '已提交候选知识' : '已记录')
    rating.value = ''; corrected.value = ''; correctMaterials.value = []
    await load()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '提交失败') } finally { saving.value = false }
}
async function sendReply() {
  replying.value = true
  try { await agentApi.messages.reply(props.messageId, replyText.value.trim(), learn.value && !!replyText.value.trim(), replyMaterials.value); replyText.value = ''; replyMaterials.value = []; ElMessage.success('已加入发送队列'); setTimeout(load, 1500) } catch (e) { ElMessage.error(e instanceof Error ? e.message : '发送失败') } finally { replying.value = false }
}
useAdminEvents(['messages','agent_runs','model_calls','tool_calls','outbox_tasks','message_feedback'], load)
</script>

<style scoped>
.agent-panel { display: flex; flex-direction: column; gap: 24px; }
.ap-section h3 { margin: 0 0 10px; font-size: 14px; font-weight: 600; }
.ap-section h3 small { margin-left: 6px; color: var(--muted); font-size: 12px; font-weight: 400; }
.ap-rewrite { display: flex; align-items: baseline; gap: 8px; margin: 0 0 10px; font-size: 13px; }
.ap-rewrite .el-icon { align-self: center; color: var(--muted); }
.ap-rewrite span { flex: none; color: var(--muted); }
.ap-rewrite strong { font-weight: 500; overflow-wrap: anywhere; }
.ap-steps { margin: 0; padding: 0; list-style: none; border: 1px solid var(--line); border-radius: var(--radius-sm); overflow: hidden; }
.ap-steps li + li { border-top: 1px solid var(--line); }
.ap-step { display: grid; grid-template-columns: 16px minmax(0, 168px) minmax(0, 1fr) 64px 14px; align-items: center; gap: 10px; width: 100%; padding: 8px 12px; border: 0; background: var(--surface); color: var(--text); font: inherit; font-size: 13px; text-align: left; cursor: pointer; transition: background .12s var(--ease); }
.ap-step:hover, .open .ap-step { background: var(--surface-2); }
.ap-step:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; }
.ap-icon { font-size: 15px; color: var(--primary-text); }
.ap-icon.is-tool { color: var(--warning); }
.ap-icon.is-rewrite { color: var(--muted); }
.error .ap-icon, .error .ap-ms { color: var(--danger); }
.ap-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ap-track { position: relative; height: 6px; border-radius: 3px; background: var(--surface-3); overflow: hidden; }
.ap-track i { position: absolute; inset: 0; border-radius: 3px; background: var(--primary); transform-origin: left; transition: transform .5s var(--ease); }
.error .ap-track i { background: var(--danger); }
.ap-ms { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
.ap-chevron { color: var(--faint); transition: transform .2s var(--ease); }
.open .ap-chevron { transform: rotate(90deg); }
.ap-body { display: flex; flex-direction: column; gap: 4px; padding: 4px 12px 12px 38px; background: var(--surface-2); }
.ap-body small { margin-top: 4px; color: var(--muted); font-size: 12px; }
.ap-body pre { margin: 0; max-height: 220px; overflow: auto; font: 12px/1.6 var(--font-mono); color: var(--text-2); white-space: pre-wrap; word-break: break-all; }
.ap-body pre.err { color: var(--danger); }
.ap-body .muted { margin: 0; font-size: 13px; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; }
.ap-history { margin-bottom: 12px; }
.ap-replies { display: flex; flex-direction: column; gap: 8px; margin: 10px 0 0; padding: 0; list-style: none; }
.ap-replies li { padding: 8px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); }
.ap-replies small { display: flex; align-items: center; gap: 4px; color: var(--muted); font-size: 12px; }
.ap-replies small :deep(.status-badge) { font-size: 12px; }
.ap-replies p { margin: 4px 0 0; font-size: 13px; line-height: 1.7; white-space: pre-wrap; overflow-wrap: anywhere; }
.ap-form { display: flex; flex-direction: column; gap: 12px; }
.ap-ratings { display: flex; flex-wrap: wrap; gap: 8px; }
.ap-rating { height: 32px; padding: 0 14px; border: 1px solid var(--line-strong); border-radius: var(--radius-sm); background: var(--surface); color: var(--text-2); font: inherit; font-size: 13px; cursor: pointer; transition: background .12s var(--ease), border-color .12s var(--ease), color .12s var(--ease); }
.ap-rating:hover { background: var(--surface-2); }
.ap-rating:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
.ap-rating.active.is-good { border-color: var(--success); background: var(--success-light); color: var(--success); }
.ap-rating.active.is-bad { border-color: var(--danger); background: var(--danger-light); color: var(--danger); }
.ap-actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; }
.ap-actions > .el-button:only-child { margin-left: auto; }
.ap-correct { display: flex; flex-direction: column; gap: 8px; }
.ap-reveal-enter-active, .ap-reveal-leave-active { transition: opacity var(--dur) var(--ease), transform var(--dur) var(--ease); }
.ap-reveal-enter-from, .ap-reveal-leave-to { opacity: 0; transform: translateY(-4px); }
.ap-options { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
.ap-recalled { margin-top: 8px; padding: 10px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); }
.ap-recalled > small { display: block; margin-bottom: 6px; color: var(--muted); font-size: 12px; }
.ap-recalled ul { margin: 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 4px; }
.ap-recalled li { display: flex; align-items: baseline; gap: 8px; font-size: 12px; line-height: 1.5; }
.ap-recalled li .ap-kind { flex: none; padding: 1px 6px; border-radius: var(--radius-xs); background: var(--surface-3); color: var(--text-2); }
.ap-recalled li small { margin-left: auto; color: var(--muted); }
.ap-director { margin-top: 8px; }
.ap-director-toggle { display: inline-flex; align-items: center; gap: 6px; padding: 0; border: 0; background: none; color: var(--primary-text); font: inherit; font-size: 12px; cursor: pointer; }
.ap-director-toggle .el-icon { transition: transform .2s var(--ease); }
.ap-director-toggle .el-icon.open { transform: rotate(90deg); }
.ap-director pre { margin: 6px 0 0; padding: 10px 12px; border-radius: var(--radius-sm); background: var(--surface-2); font: 12px/1.6 var(--font-mono); color: var(--text-2); white-space: pre-wrap; }
@media (prefers-reduced-motion: reduce) { .ap-track i, .ap-chevron, .ap-reveal-enter-active, .ap-reveal-leave-active { transition: none; } }
</style>
