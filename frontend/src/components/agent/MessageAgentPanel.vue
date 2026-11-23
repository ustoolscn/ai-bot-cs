<template>
  <div v-if="detail" class="agent-panel">
    <div v-if="run?.rewrittenQuery" class="detail-section"><h3>问题改写</h3><div class="content-box">{{ run.rewrittenQuery }}</div></div>
    <div v-if="run?.steps?.length" class="detail-section">
      <h3>Agent 步骤</h3>
      <div class="steps">
        <article v-for="(s, i) in run.steps" :key="i" :class="{ error: s.error }">
          <header><el-tag size="small" :type="s.type === 'tool' ? 'warning' : s.type === 'rewrite' ? 'info' : 'primary'" effect="plain">{{ stepType[s.type] || s.type }}</el-tag><strong>{{ s.name }}</strong><em>{{ s.latencyMs }}ms</em></header>
          <pre v-if="s.input">入参：{{ s.input }}</pre>
          <pre v-if="s.output">{{ s.output }}</pre>
          <pre v-if="s.error" class="err">{{ s.error }}</pre>
        </article>
      </div>
    </div>
    <div v-if="evaluation" class="detail-section">
      <h3>自动质检</h3>
      <div class="chips">
        <el-tag :type="evaluation.answered ? 'success' : 'danger'" size="small">{{ evaluation.answered ? '已解决' : '未解决' }}</el-tag>
        <el-tag v-if="evaluation.too_long" type="warning" size="small">回答偏长</el-tag>
        <el-tag v-if="evaluation.knowledge_gap" type="danger" size="small" effect="plain">缺口：{{ evaluation.knowledge_gap }}</el-tag>
      </div>
    </div>
    <div v-if="(detail.replies || []).length > 1" class="detail-section">
      <h3>实际发送（{{ detail.replies!.length }} 条）</h3>
      <div v-for="r in detail.replies" :key="r.id" class="content-box reply-part"><small class="muted">{{ r.eventType === 'HUMAN_REPLY' ? '人工' : 'AI' }} · {{ r.status }}</small><br />{{ r.content }}</div>
    </div>
    <div class="detail-section">
      <h3>评价与纠错</h3>
      <div v-if="detail.feedback?.length" class="chips gap">
        <el-tag v-for="f in detail.feedback" :key="f.id" size="small" :type="['good', 'helpful'].includes(f.rating) ? 'success' : 'danger'" effect="plain">{{ feedbackSourceText[f.source] || f.source }}：{{ ratingText[f.rating] || f.rating }}</el-tag>
      </div>
      <el-radio-group v-model="rating" size="small">
        <el-radio-button v-for="r in ratings" :key="r" :value="r">{{ ratingText[r] }}</el-radio-button>
      </el-radio-group>
      <el-input v-if="rating && rating !== 'good'" v-model="corrected" class="gap-top" type="textarea" :rows="3" placeholder="正确的回答应该是…（可选，填写后生成候选知识）" />
      <div v-if="corrected.trim()" class="row gap-top">
        <el-select v-model="kbID" size="small" placeholder="知识库" style="width:180px"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
        <el-checkbox v-model="publish">直接发布为 FAQ</el-checkbox>
      </div>
      <el-button class="gap-top" size="small" type="primary" :disabled="!rating" :loading="saving" @click="submitFeedback">提交评价</el-button>
    </div>
    <div class="detail-section">
      <h3>人工回复</h3>
      <el-input v-model="replyText" type="textarea" :rows="2" placeholder="以机器人身份补充回复（群聊 5 分钟、私聊 60 分钟内有效）" />
      <div class="row gap-top"><el-checkbox v-model="learn">提交为候选知识</el-checkbox><el-button size="small" type="primary" :disabled="!replyText.trim()" :loading="replying" @click="sendReply">发送</el-button></div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, ref, watch } from 'vue'
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
watch(() => props.messageId, () => { rating.value = ''; corrected.value = ''; replyText.value = ''; load() }, { immediate: true })

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
.steps { display: flex; flex-direction: column; gap: 8px; }
.steps article { border: 1px solid var(--line); border-radius: var(--radius-sm); padding: 8px 10px; background: var(--surface-2); }
.steps article.error { border-color: var(--danger-light); }
.steps header { display: flex; align-items: center; gap: 8px; font-size: 12px; }
.steps header em { margin-left: auto; color: var(--muted); font-style: normal; }
.steps pre { margin: 6px 0 0; white-space: pre-wrap; word-break: break-all; font-size: 12px; color: var(--text-2); max-height: 180px; overflow: auto; }
.steps pre.err { color: var(--danger); }
.chips { display: flex; flex-wrap: wrap; gap: 6px; }
.gap { margin-bottom: 10px; }
.gap-top { margin-top: 10px; }
.row { display: flex; align-items: center; gap: 12px; justify-content: space-between; }
.reply-part { margin-bottom: 6px; }
.muted { color: var(--muted); font-size: 11px; }
</style>
