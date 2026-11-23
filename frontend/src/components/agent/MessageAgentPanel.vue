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

    <section v-if="evaluation || (detail.replies || []).length > 1" class="ap-section">
      <h3>回复结果</h3>
      <div v-if="evaluation" class="chips">
        <el-tag :type="evaluation.answered ? 'success' : 'danger'" size="small">{{ evaluation.answered ? '已解决' : '未解决' }}</el-tag>
        <el-tag v-if="evaluation.too_long" type="warning" size="small">回答偏长</el-tag>
        <el-tag v-if="evaluation.knowledge_gap" type="danger" size="small" effect="plain">缺口：{{ evaluation.knowledge_gap }}</el-tag>
      </div>
      <ol v-if="(detail.replies || []).length > 1" class="ap-replies">
        <li v-for="(r, i) in detail.replies" :key="r.id"><small>第 {{ i + 1 }} 段 · {{ r.eventType === 'HUMAN_REPLY' ? '人工' : 'AI' }} · <StatusBadge :status="replyStatus(r.status)" :text="replyText_[r.status]" /></small><p>{{ r.content }}</p></li>
      </ol>
    </section>

    <section class="ap-section">
      <h3>评价与纠错</h3>
      <div v-if="detail.feedback?.length" class="chips ap-history">
        <el-tag v-for="f in detail.feedback" :key="f.id" size="small" :type="['good', 'helpful'].includes(f.rating) ? 'success' : 'danger'" effect="plain">{{ feedbackSourceText[f.source] || f.source }}：{{ ratingText[f.rating] || f.rating }}</el-tag>
      </div>
      <div class="ap-form">
        <div class="ap-ratings" role="radiogroup" aria-label="这条回复怎么样">
          <button v-for="r in ratings" :key="r" type="button" role="radio" :aria-checked="rating === r" class="ap-rating" :class="[{ active: rating === r }, r === 'good' ? 'is-good' : 'is-bad']" @click="rating = rating === r ? '' : r">{{ ratingText[r] }}</button>
        </div>
        <el-input v-if="rating && rating !== 'good'" v-model="corrected" type="textarea" :rows="3" placeholder="正确的回答应该是…（选填，填了会生成候选知识）" />
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
      <div class="ap-form">
        <el-input v-model="replyText" type="textarea" :rows="2" placeholder="以机器人身份补充回复（群聊 5 分钟、私聊 60 分钟内有效）" />
        <div class="ap-actions"><el-checkbox v-model="learn">同时提交为候选知识</el-checkbox><el-button type="primary" plain :disabled="!replyText.trim()" :loading="replying" @click="sendReply">发送回复</el-button></div>
      </div>
    </section>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, ref, watch } from 'vue'
import { ArrowRight, Cpu, EditPen, SetUp } from '@element-plus/icons-vue'
import StatusBadge from '@/components/StatusBadge.vue'
import { stageSeconds } from '@/utils/stage-meta'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import { agentApi, feedbackSourceText, ratingText, type MessageAgentDetail } from '@/api/agent'
import type { KnowledgeBase } from '@/types'

const props = defineProps<{ messageId: string }>()
const stepType: Record<string, string> = { rewrite: '改写', model: '模型', tool: '工具' }
const ratings = ['good', 'too_long', 'stiff', 'wrong', 'bad']
const detail = ref<MessageAgentDetail | null>(null)
const knowledgeBases = ref<KnowledgeBase[]>([])
const rating = ref('')
const corrected = ref('')
const kbID = ref('')
const publish = ref(false)
const saving = ref(false)
const replyText = ref('')
const learn = ref(true)
const replying = ref(false)
const run = computed(() => detail.value?.agentRuns?.[0])
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
watch(() => props.messageId, () => { openStep.value = -1; rating.value = ''; corrected.value = ''; replyText.value = ''; load() }, { immediate: true })

async function submitFeedback() {
  saving.value = true
  try {
    const r = await agentApi.messages.feedback(props.messageId, { rating: rating.value, correctedAnswer: corrected.value.trim() || undefined, publish: publish.value, knowledgeBaseId: kbID.value || undefined })
    ElMessage.success(r.entryId ? '已发布为 FAQ' : r.candidateId ? '已提交候选知识' : '已记录')
    rating.value = ''; corrected.value = ''
    await load()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '提交失败') } finally { saving.value = false }
}
async function sendReply() {
  replying.value = true
  try { await agentApi.messages.reply(props.messageId, replyText.value.trim(), learn.value); replyText.value = ''; ElMessage.success('已加入发送队列'); setTimeout(load, 1500) } catch (e) { ElMessage.error(e instanceof Error ? e.message : '发送失败') } finally { replying.value = false }
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
.ap-options { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
@media (prefers-reduced-motion: reduce) { .ap-track i, .ap-chevron { transition: none; } }
</style>
