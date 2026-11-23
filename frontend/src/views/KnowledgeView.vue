<template>
  <div class="content-page">
    <PageHeader title="知识库" description="机器人回答时参考的资料：上传的文档、整理好的问答，以及从客服回答里学到、等你审核的知识。">
      <el-button :icon="Refresh" @click="reload">刷新</el-button>
    </PageHeader>
    <el-tabs :model-value="tab" @update:model-value="onTabChange">
      <el-tab-pane name="documents" label="文档"><KnowledgeDocuments v-if="tab === 'documents'" /></el-tab-pane>
      <el-tab-pane name="candidates">
        <template #label>待审核<span class="count-pill" :class="{ hot: stats?.pendingCandidates }">{{ stats?.pendingCandidates ?? 0 }}</span></template>
        <div class="toolbar"><el-segmented v-model="candidateStatus" :options="candidateStatusOptions" @change="loadTab" /><el-tag v-if="candidateConversation" closable @close="clearConversationFilter">只看：{{ candidateConversationName || candidateConversation }}</el-tag><span class="spacer" /><el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected(tab, names[tab])">删除选中的 {{ selectedIds.length }} 条</el-button></div>
        <section class="table-panel">
          <el-table row-key="id" :key="JSON.stringify([tab,candidateStatus,candidateConversation])" v-loading="loading" :data="candidates" @selection-change="(rows: {id:string}[]) => selectRows('candidates',rows)" :empty-text="candidateStatus === 'pending' ? '没有待审核的知识。在“群与会话 → 成员”里标记官方客服后，他们在群里的回答会自动出现在这里。' : '暂无记录'">
            <el-table-column type="selection" reserve-selection width="48" />
            <el-table-column label="问题 / 答案" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question }}</strong><span>{{ row.answer }}</span><small v-if="row.aliases?.length">相似问：{{ row.aliases.join('；') }}</small></div></template></el-table-column>
            <el-table-column label="来源" width="140"><template #default="{ row }"><div class="stacked-cell"><strong>{{ sourceText[row.source] || row.source }}</strong><span>{{ row.knowledgeBaseName || '默认知识库' }}</span></div></template></el-table-column>
            <el-table-column label="可信度" width="90" align="right"><template #default="{ row }"><span class="num" :class="row.confidence >= 0.85 ? 'trend-good' : row.confidence < 0.6 ? 'danger-text' : ''">{{ Math.round(row.confidence * 100) }}%</span></template></el-table-column>
            <el-table-column label="时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
            <el-table-column label="操作" width="210" fixed="right"><template #default="{ row }"><template v-if="row.status === 'pending'"><el-button link type="primary" @click="quickApprove(row)">通过</el-button><el-button link type="primary" @click="review(row)">修改后通过</el-button><el-button link type="danger" @click="reject(row)">驳回</el-button></template><span v-else class="pill" :class="row.status === 'approved' ? 'good' : 'idle'">{{ row.status === 'approved' ? '已通过' : '已驳回' }}</span></template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane name="entries">
        <template #label>知识问答<span class="count-pill">{{ stats?.publishedEntries ?? 0 }}</span></template>
        <div class="toolbar">
          <el-input v-model="entryQuery" placeholder="搜索问题或答案" :prefix-icon="Search" clearable @change="loadTab" />
          <el-select v-model="entryKB" clearable placeholder="全部知识库" @change="loadTab"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
          <el-select v-model="entryStatus" @change="loadTab"><el-option label="已发布" value="published" /><el-option label="需复核" value="review" /><el-option label="已归档" value="archived" /><el-option label="全部" value="all" /></el-select>
          <span class="spacer" /><el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected(tab, names[tab])">删除选中的 {{ selectedIds.length }} 条</el-button>
          <el-button type="primary" :icon="Plus" @click="editEntry()">新建问答</el-button>
        </div>
        <section class="table-panel">
          <el-table row-key="id" :key="JSON.stringify([tab,entryQuery,entryKB,entryStatus])" v-loading="loading" :data="entries" @selection-change="(rows: {id:string}[]) => selectRows('entries',rows)" empty-text="暂无条目">
            <el-table-column type="selection" reserve-selection width="48" />
            <el-table-column label="问题 / 答案" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question }}</strong><span>{{ row.answer }}</span></div></template></el-table-column>
            <el-table-column prop="knowledgeBaseName" label="知识库" width="120" />
            <el-table-column label="来源" width="100"><template #default="{ row }">{{ sourceText[row.source] || row.source }}</template></el-table-column>
            <el-table-column label="命中 / 好评 / 差评" width="140" align="right"><template #default="{ row }"><span class="num">{{ row.hitCount }} / {{ row.helpfulCount }} / <span :class="{ 'danger-text': row.unhelpfulCount > row.helpfulCount }">{{ row.unhelpfulCount }}</span></span></template></el-table-column>
            <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="row.status === 'published' ? (row.indexStatus === 'ready' ? 'success' : row.indexStatus === 'failed' ? 'danger' : 'warning') : row.status === 'review' ? 'danger' : 'info'">{{ entryStatusText(row) }}</el-tag></template></el-table-column>
            <el-table-column label="操作" width="120" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="editEntry(row)">编辑</el-button><el-button link type="danger" @click="removeEntry(row)">删除</el-button></template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane name="gaps">
        <template #label>没答上的问题<span class="count-pill" :class="{ hot: stats?.openGaps }">{{ stats?.openGaps ?? 0 }}</span></template>
        <div class="toolbar"><span class="muted" style="font-size: 13px">机器人因为缺少资料没答上的问题会自动归类到这里，补上答案就会进入知识库。</span><span class="spacer" /><el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected(tab, names[tab])">删除选中的 {{ selectedIds.length }} 条</el-button></div>
        <section class="table-panel">
          <el-table row-key="id" :key="JSON.stringify([tab])" v-loading="loading" :data="gaps" @selection-change="(rows: {id:string}[]) => selectRows('gaps',rows)" empty-text="没有新的问题">
            <el-table-column type="selection" reserve-selection width="48" />
            <el-table-column prop="question" label="用户问了但没答上的问题" min-width="360" />
            <el-table-column prop="occurrences" label="出现次数" width="110" sortable align="right" />
            <el-table-column label="最近出现" width="150"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></el-table-column>
            <el-table-column label="操作" width="150" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="answerGap(row)">补充答案</el-button><el-button link @click="ignoreGap(row)">忽略</el-button></template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane name="feedback">
        <template #label>反馈<span v-if="stats" class="count-pill" :class="{ hot: unreadFeedback }" title="未读差评数，标为已读后数字消失，记录仍保留">{{ unreadFeedback }}</span></template>
        <div class="toolbar"><el-segmented v-model="feedbackView" :options="[{ label: '全部', value: 'all' }, { label: '未读', value: 'unread' }]" /><el-select v-model="feedbackRating" clearable placeholder="全部评价" aria-label="按评价筛选" @change="loadTab"><el-option v-for="(v, k) in ratingText" :key="k" :label="v" :value="k" /></el-select><span class="spacer" /><el-button v-if="selectedIds.length" :loading="markingRead" @click="markRead({ ids: [...selectedIds] })">选中的标为已读</el-button><el-button v-else :disabled="!feedback.some(f => !f.read)" :loading="markingRead" @click="markRead({ all: true })">全部标为已读</el-button><el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected(tab, names[tab])">删除选中的 {{ selectedIds.length }} 条</el-button></div>
        <section class="table-panel">
          <el-table row-key="id" :key="JSON.stringify([tab,feedbackRating])" v-loading="loading" :data="shownFeedback" :row-class-name="({ row }: { row: Feedback }) => row.read ? '' : 'is-unread'" @selection-change="(rows: {id:string}[]) => selectRows('feedback',rows)" :empty-text="feedbackView === 'unread' ? '没有未读反馈' : '暂无反馈'">
            <el-table-column type="selection" reserve-selection width="48" />
            <el-table-column width="76"><template #default="{ row }"><button v-if="!row.read" type="button" class="unread-dot" title="点击标为已读" @click.stop="markRead({ ids: [row.id] })"><i />未读</button><span v-else class="faint">已读</span></template></el-table-column>
            <el-table-column label="问题 / 回答" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question || '—' }}</strong><span>{{ row.answer }}</span><small v-if="row.correctedAnswer">纠正：{{ row.correctedAnswer }}</small></div></template></el-table-column>
            <el-table-column label="评价" width="90"><template #default="{ row }"><el-tag size="small" :type="['good', 'helpful'].includes(row.rating) ? 'success' : 'danger'">{{ ratingText[row.rating] || row.rating }}</el-tag></template></el-table-column>
            <el-table-column label="来源" width="100"><template #default="{ row }">{{ feedbackSourceText[row.source] || row.source }}</template></el-table-column>
            <el-table-column prop="conversationName" label="会话" width="140" />
            <el-table-column label="时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

    </el-tabs>

    <el-dialog v-model="entryDialog" :title="entryDialogTitle" width="640px">
      <el-form label-position="top">
        <el-form-item label="知识库"><el-select v-model="entryForm.knowledgeBaseId" :disabled="!!entryForm.id"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></el-form-item>
        <el-form-item label="标准问题"><el-input v-model="entryForm.question" /></el-form-item>
        <el-form-item label="相似问法（每行一个）"><el-input v-model="entryForm.aliasesText" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="答案（建议口语化、简洁）"><el-input v-model="entryForm.answer" type="textarea" :rows="6" /></el-form-item>
        <el-form-item v-if="entryForm.id" label="状态"><el-radio-group v-model="entryForm.status"><el-radio-button value="published">发布</el-radio-button><el-radio-button value="review">需复核</el-radio-button><el-radio-button value="archived">归档</el-radio-button></el-radio-group></el-form-item>
        <el-form-item v-if="entryForm.mode === 'candidate' && evidenceText" label="来源对话"><div class="content-box evidence">{{ evidenceText }}</div></el-form-item>
      </el-form>
      <template #footer><el-button @click="entryDialog = false">取消</el-button><el-button type="primary" @click="submitEntry">{{ entryForm.mode === 'candidate' ? '通过并发布' : entryForm.mode === 'gap' ? '保存答案' : '保存问答' }}</el-button></template>
    </el-dialog>

  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { onMounted, reactive, ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useShellStore } from '@/stores/shell'
import { Plus, Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import { api } from '@/api'
import { useBulkDelete } from '@/composables/useBulkDelete'
import KnowledgeDocuments from './KnowledgeDocuments.vue'
import { agentApi, feedbackSourceText, formatTime, ratingText, sourceText, type Candidate, type Entry, type Feedback, type Gap, type LearningStats } from '@/api/agent'
import type { KnowledgeBase } from '@/types'

const route = useRoute()
const router = useRouter()
const shell = useShellStore()
const tabs = ['documents', 'candidates', 'entries', 'gaps', 'feedback']
const tab = computed(() => tabs.includes(String(route.query.tab)) ? String(route.query.tab) : 'documents')
const candidateStatusOptions = [{ label: '待审核', value: 'pending' }, { label: '已通过', value: 'approved' }, { label: '已驳回', value: 'rejected' }, { label: '全部', value: 'all' }]
const loading = ref(false)
const stats = ref<LearningStats | null>(null)
const knowledgeBases = ref<KnowledgeBase[]>([])
const defaultKB = ref('')
const candidates = ref<Candidate[]>([])
const candidateStatus = ref('pending')
const candidateConversation = computed(() => typeof route.query.conversationId === 'string' ? route.query.conversationId : '')
const candidateConversationName = computed(() => typeof route.query.conversationName === 'string' ? route.query.conversationName : '')
function clearConversationFilter() { router.replace({ query: { tab: 'candidates' } }) }

const entries = ref<Entry[]>([])
const entryQuery = ref('')
const entryKB = ref(typeof route.query.kb === 'string' ? route.query.kb : '')
watch(() => route.query.kb, kb => { if (typeof kb === 'string') entryKB.value = kb })
const entryStatus = ref('published')
const gaps = ref<Gap[]>([])
const feedback = ref<Feedback[]>([])
const feedbackRating = ref('')
const feedbackView = ref<'all' | 'unread'>('all')
const shownFeedback = computed(() => feedbackView.value === 'unread' ? feedback.value.filter(f => !f.read) : feedback.value)
const unreadFeedback = computed(() => stats.value ? stats.value.unreadNegativeFeedback ?? stats.value.negativeFeedback7d : 0)
const markingRead = ref(false)
async function markRead(target: { ids: string[] } | { all: true }) {
  markingRead.value = true
  try {
    await agentApi.learning.markFeedbackRead(target)
    const ids = 'ids' in target ? new Set(target.ids) : null
    feedback.value = feedback.value.map(f => !ids || ids.has(f.id) ? { ...f, read: true } : f)
    selectedIds.value = []
    stats.value = await agentApi.learning.stats()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '标记失败') } finally { markingRead.value = false }
}
const { selectedIds, deleting, deleteSelected } = useBulkDelete(reload)
const names:Record<string,string>={candidates:'待审核记录',entries:'问答',gaps:'没答上的问题',feedback:'反馈'}
function selectRows(kind:string,rows:{id:string}[]){if(kind===tab.value){
 const visible = new Set(({candidates,entries,gaps,feedback}[kind as 'candidates'|'entries'|'gaps'|'feedback']).value.map(row=>row.id))
 selectedIds.value=rows.map(row=>row.id).filter(id=>visible.has(id))
 }}
watch([tab,candidateStatus,candidateConversation,entryQuery,entryKB,entryStatus,feedbackRating,feedbackView],()=>{selectedIds.value=[]},{flush:'sync'})
function onTabChange(value: string | number) {
  if (value !== tab.value) router.replace({ query: value === 'documents' ? {} : { tab: String(value) } })
}
watch([tab, candidateConversation], loadTab)

async function reload() {
  const [s, kbs, settings] = await Promise.all([agentApi.learning.stats(), api.knowledge.list(), agentApi.settings.get().catch(() => null)])
  stats.value = s
  shell.refreshCounts(true)
  knowledgeBases.value = kbs
  defaultKB.value = settings?.learningKnowledgeBaseId || kbs[0]?.id || ''
  await loadTab()
}
async function loadTab() {
  loading.value = true
  try {
    if (tab.value === 'candidates') candidates.value = await agentApi.learning.candidates(candidateStatus.value, candidateConversation.value || undefined)
    if (tab.value === 'entries') entries.value = await agentApi.learning.entries({ knowledgeBaseId: entryKB.value || undefined, status: entryStatus.value, q: entryQuery.value || undefined })
    if (tab.value === 'gaps') gaps.value = await agentApi.learning.gaps()
    if (tab.value === 'feedback') feedback.value = await agentApi.learning.feedback(feedbackRating.value)
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally {
    loading.value = false
  }
}
function entryStatusText(row: Entry) {
  if (row.status === 'review') return '需复核'
  if (row.status === 'archived') return '已归档'
  return ({ ready: '已生效', failed: '索引失败', pending: '索引中' } as Record<string, string>)[row.indexStatus] || row.indexStatus
}

// Entry / candidate / gap dialog
const entryDialog = ref(false)
const entryForm = reactive({ mode: 'entry' as 'entry' | 'candidate' | 'gap', id: '', refId: '', knowledgeBaseId: '', question: '', aliasesText: '', answer: '', status: 'published', evidence: [] as Array<Record<string, string>> })
const entryDialogTitle = computed(() => ({ entry: entryForm.id ? '编辑问答' : '新建问答', candidate: '审核候选知识', gap: '补充答案' })[entryForm.mode])
const evidenceText = computed(() => entryForm.evidence.map(e => e.context ? `${e.context}\n→ ${e.content}` : e.question ? `用户：${e.question}` : e.content || '').filter(Boolean).join('\n\n'))
function openForm(mode: 'entry' | 'candidate' | 'gap', values: Partial<typeof entryForm>) {
  Object.assign(entryForm, { mode, id: '', refId: '', knowledgeBaseId: defaultKB.value, question: '', aliasesText: '', answer: '', status: 'published', evidence: [] }, values)
  entryDialog.value = true
}
function editEntry(row?: Entry) { openForm('entry', row ? { id: row.id, knowledgeBaseId: row.knowledgeBaseId, question: row.question, aliasesText: (row.aliases || []).join('\n'), answer: row.answer, status: row.status } : {}) }
function review(row: Candidate) { openForm('candidate', { refId: row.id, knowledgeBaseId: row.knowledgeBaseId || defaultKB.value, question: row.question, aliasesText: (row.aliases || []).join('\n'), answer: row.answer, evidence: row.evidence || [] }) }
function answerGap(row: Gap) { openForm('gap', { refId: row.id, question: row.question }) }
async function submitEntry() {
  const aliases = entryForm.aliasesText.split('\n').map(s => s.trim()).filter(Boolean)
  if (!entryForm.knowledgeBaseId || !entryForm.question.trim() || !entryForm.answer.trim()) { ElMessage.warning('知识库、问题和答案必填'); return }
  try {
    if (entryForm.mode === 'candidate') await agentApi.learning.approve(entryForm.refId, { knowledgeBaseId: entryForm.knowledgeBaseId, question: entryForm.question, aliases, answer: entryForm.answer })
    else if (entryForm.mode === 'gap') await agentApi.learning.answerGap(entryForm.refId, { knowledgeBaseId: entryForm.knowledgeBaseId, question: entryForm.question, aliases, answer: entryForm.answer })
    else if (entryForm.id) await agentApi.learning.updateEntry(entryForm.id, { question: entryForm.question, aliases, answer: entryForm.answer, status: entryForm.status })
    else await agentApi.learning.createEntry({ knowledgeBaseId: entryForm.knowledgeBaseId, question: entryForm.question, aliases, answer: entryForm.answer })
    entryDialog.value = false
    ElMessage.success('已保存，索引完成后生效')
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '保存失败') }
}
async function quickApprove(row: Candidate) {
  const knowledgeBaseId = row.knowledgeBaseId || defaultKB.value
  if (!knowledgeBaseId) { review(row); return }
  try {
    await agentApi.learning.approve(row.id, { knowledgeBaseId, question: row.question, aliases: row.aliases || [], answer: row.answer })
    ElMessage.success('已通过，索引完成后机器人就能用上')
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '操作失败') }
}
async function reject(row: Candidate) {
  let value = ''
  try { value = (await ElMessageBox.prompt('驳回原因（选填）', '驳回候选知识', { inputValue: '', confirmButtonText: '驳回', cancelButtonText: '取消' })).value } catch { return }
  await agentApi.learning.reject(row.id, value || '')
  await reload()
}
async function removeEntry(row: Entry) { try { await ElMessageBox.confirm('删除这条问答？删除后不能恢复。', '删除问答', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }) } catch { return } await agentApi.learning.deleteEntry(row.id); await reload() }
async function ignoreGap(row: Gap) { await agentApi.learning.ignoreGap(row.id); await reload() }

onMounted(reload)
useAdminEvents(['knowledge_entries','knowledge_candidates','knowledge_gaps','message_feedback'], reload)
</script>

<style scoped>
.evidence { max-height: 220px; overflow: auto; font-size: 13px; }
.unread-dot { display: inline-flex; align-items: center; gap: 6px; padding: 2px 0; border: 0; background: none; color: var(--primary-text); font: inherit; font-size: 13px; font-weight: 600; cursor: pointer; }
.unread-dot i { width: 7px; height: 7px; border-radius: 50%; background: var(--primary); }
.unread-dot:hover { text-decoration: underline; }
.unread-dot:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; border-radius: 4px; }
:deep(.is-unread .qa strong) { font-weight: 600; }
.faint { font-size: 13px; }
</style>
