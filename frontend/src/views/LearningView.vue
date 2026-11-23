<template>
  <div class="content-page">
    <PageHeader title="知识学习" description="官方客服回答、管理员纠错和没答上的问题，经审核后自动沉淀为 FAQ。">
      <el-button :icon="Refresh" @click="reload">刷新</el-button>
    </PageHeader>
    <div class="stat-row">
      <div v-for="s in statCards" :key="s.label" class="stat-card" @click="s.tab && (tab = s.tab)"><span>{{ s.label }}</span><strong>{{ s.value }}</strong></div>
    </div>
    <el-tabs v-model="tab" @tab-change="loadTab">
      <el-tab-pane :label="`待审核 (${stats?.pendingCandidates ?? 0})`" name="candidates">
        <div class="toolbar"><el-select v-model="candidateStatus" @change="loadTab"><el-option label="待审核" value="pending" /><el-option label="已通过" value="approved" /><el-option label="已驳回" value="rejected" /><el-option label="全部" value="all" /></el-select></div>
        <section class="table-panel">
          <el-table v-loading="loading" :data="candidates" empty-text="暂无候选知识。标记官方客服后，他们在群里的回答会自动出现在这里。">
            <el-table-column label="问题 / 答案" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question }}</strong><span>{{ row.answer }}</span><small v-if="row.aliases?.length">相似问：{{ row.aliases.join('；') }}</small></div></template></el-table-column>
            <el-table-column label="来源" width="110"><template #default="{ row }"><el-tag effect="plain" size="small">{{ sourceText[row.source] || row.source }}</el-tag></template></el-table-column>
            <el-table-column label="置信度" width="80"><template #default="{ row }">{{ Math.round(row.confidence * 100) }}%</template></el-table-column>
            <el-table-column label="时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
            <el-table-column label="操作" width="150" fixed="right"><template #default="{ row }"><template v-if="row.status === 'pending'"><el-button link type="primary" @click="review(row)">审核</el-button><el-button link type="danger" @click="reject(row)">驳回</el-button></template><span v-else class="muted">{{ row.status === 'approved' ? '已通过' : '已驳回' }}</span></template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane label="FAQ 条目" name="entries">
        <div class="toolbar">
          <el-input v-model="entryQuery" placeholder="搜索问题或答案" :prefix-icon="Search" clearable @change="loadTab" />
          <el-select v-model="entryKB" clearable placeholder="全部知识库" @change="loadTab"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
          <el-select v-model="entryStatus" @change="loadTab"><el-option label="已发布" value="published" /><el-option label="需复核" value="review" /><el-option label="已归档" value="archived" /><el-option label="全部" value="all" /></el-select>
          <el-button type="primary" :icon="Plus" @click="editEntry()">新建条目</el-button>
        </div>
        <section class="table-panel">
          <el-table v-loading="loading" :data="entries" empty-text="暂无条目">
            <el-table-column label="问题 / 答案" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question }}</strong><span>{{ row.answer }}</span></div></template></el-table-column>
            <el-table-column prop="knowledgeBaseName" label="知识库" width="120" />
            <el-table-column label="来源" width="100"><template #default="{ row }">{{ sourceText[row.source] || row.source }}</template></el-table-column>
            <el-table-column label="命中 / 好评 / 差评" width="140"><template #default="{ row }">{{ row.hitCount }} / {{ row.helpfulCount }} / <span :class="{ danger: row.unhelpfulCount > row.helpfulCount }">{{ row.unhelpfulCount }}</span></template></el-table-column>
            <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="row.status === 'published' ? (row.indexStatus === 'ready' ? 'success' : row.indexStatus === 'failed' ? 'danger' : 'warning') : row.status === 'review' ? 'danger' : 'info'">{{ entryStatusText(row) }}</el-tag></template></el-table-column>
            <el-table-column label="操作" width="120" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="editEntry(row)">编辑</el-button><el-button link type="danger" @click="removeEntry(row)">删除</el-button></template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane :label="`知识缺口 (${stats?.openGaps ?? 0})`" name="gaps">
        <section class="table-panel">
          <el-table v-loading="loading" :data="gaps" empty-text="暂无知识缺口。质检发现 AI 因缺少资料没答上的问题会聚类到这里。">
            <el-table-column prop="question" label="用户问了但没答上的问题" min-width="360" />
            <el-table-column prop="occurrences" label="出现次数" width="100" sortable />
            <el-table-column label="最近出现" width="150"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></el-table-column>
            <el-table-column label="操作" width="140" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="answerGap(row)">补充答案</el-button><el-button link @click="ignoreGap(row)">忽略</el-button></template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane label="反馈记录" name="feedback">
        <div class="toolbar"><el-select v-model="feedbackRating" clearable placeholder="全部评价" @change="loadTab"><el-option v-for="(v, k) in ratingText" :key="k" :label="v" :value="k" /></el-select></div>
        <section class="table-panel">
          <el-table v-loading="loading" :data="feedback" empty-text="暂无反馈">
            <el-table-column label="问题 / 回答" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question || '—' }}</strong><span>{{ row.answer }}</span><small v-if="row.correctedAnswer">纠正：{{ row.correctedAnswer }}</small></div></template></el-table-column>
            <el-table-column label="评价" width="90"><template #default="{ row }"><el-tag size="small" :type="['good', 'helpful'].includes(row.rating) ? 'success' : 'danger'">{{ ratingText[row.rating] || row.rating }}</el-tag></template></el-table-column>
            <el-table-column label="来源" width="100"><template #default="{ row }">{{ feedbackSourceText[row.source] || row.source }}</template></el-table-column>
            <el-table-column prop="conversationName" label="会话" width="140" />
            <el-table-column label="时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane label="用户记忆" name="memories">
        <div class="toolbar"><el-input v-model="memoryQuery" placeholder="搜索记忆内容、昵称" :prefix-icon="Search" clearable @change="loadTab" /><el-input v-model="memoryUser" placeholder="用户 ID" clearable @change="loadTab" /></div>
        <section class="table-panel">
          <el-table v-loading="loading" :data="memories" empty-text="暂无记忆">
            <el-table-column label="用户" width="170"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.displayName || '—' }}</strong><span>{{ row.userId }}</span></div></template></el-table-column>
            <el-table-column label="记忆" min-width="320"><template #default="{ row }"><el-icon v-if="row.pinned" class="pin"><StarFilled /></el-icon>{{ row.content }}</template></el-table-column>
            <el-table-column label="类型" width="80"><template #default="{ row }">{{ kindText[row.kind] || row.kind }}</template></el-table-column>
            <el-table-column label="来源" width="80"><template #default="{ row }">{{ memorySourceText[row.source] || row.source }}</template></el-table-column>
            <el-table-column label="更新" width="150"><template #default="{ row }">{{ formatTime(row.updatedAt) }}</template></el-table-column>
            <el-table-column label="操作" width="120" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="editMemory(row)">编辑</el-button><el-button link type="danger" @click="removeMemory(row)">删除</el-button></template></el-table-column>
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
      <template #footer><el-button @click="entryDialog = false">取消</el-button><el-button type="primary" @click="submitEntry">{{ entryForm.mode === 'candidate' ? '通过并发布' : '保存' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="memoryDialog" title="编辑记忆" width="520px">
      <el-form label-position="top">
        <el-form-item label="内容"><el-input v-model="memoryForm.content" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="类型"><el-select v-model="memoryForm.kind"><el-option v-for="(v, k) in kindText" :key="k" :label="v" :value="k" /></el-select></el-form-item>
        <el-checkbox v-model="memoryForm.pinned">常驻（每次回复都带上）</el-checkbox>
      </el-form>
      <template #footer><el-button @click="memoryDialog = false">取消</el-button><el-button type="primary" @click="saveMemory">保存</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Plus, Refresh, Search, StarFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import { api } from '@/api'
import { agentApi, feedbackSourceText, formatTime, ratingText, sourceText, type Candidate, type Entry, type Feedback, type Gap, type LearningStats, type Memory } from '@/api/agent'
import type { KnowledgeBase } from '@/types'

const kindText: Record<string, string> = { fact: '事实', preference: '偏好', issue: '问题', profile: '资料' }
const memorySourceText: Record<string, string> = { extracted: '自动', tool: 'AI 记录', admin: '手动' }
const tab = ref('candidates')
const loading = ref(false)
const stats = ref<LearningStats | null>(null)
const knowledgeBases = ref<KnowledgeBase[]>([])
const defaultKB = ref('')
const candidates = ref<Candidate[]>([])
const candidateStatus = ref('pending')
const entries = ref<Entry[]>([])
const entryQuery = ref('')
const entryKB = ref('')
const entryStatus = ref('published')
const gaps = ref<Gap[]>([])
const feedback = ref<Feedback[]>([])
const feedbackRating = ref('')
const memories = ref<Memory[]>([])
const memoryQuery = ref('')
const memoryUser = ref('')

const statCards = computed(() => [
  { label: '待审核候选', value: stats.value?.pendingCandidates ?? '—', tab: 'candidates' },
  { label: '已发布 FAQ', value: stats.value?.publishedEntries ?? '—', tab: 'entries' },
  { label: '待补知识缺口', value: stats.value?.openGaps ?? '—', tab: 'gaps' },
  { label: '7 天负反馈 / 总反馈', value: stats.value ? `${stats.value.negativeFeedback7d} / ${stats.value.feedback7d}` : '—', tab: 'feedback' },
  { label: '用户记忆', value: stats.value?.activeMemories ?? '—', tab: 'memories' },
])

async function reload() {
  const [s, kbs, settings] = await Promise.all([agentApi.learning.stats(), api.knowledge.list(), agentApi.settings.get().catch(() => null)])
  stats.value = s
  knowledgeBases.value = kbs
  defaultKB.value = settings?.learningKnowledgeBaseId || kbs[0]?.id || ''
  await loadTab()
}
async function loadTab() {
  loading.value = true
  try {
    if (tab.value === 'candidates') candidates.value = await agentApi.learning.candidates(candidateStatus.value)
    if (tab.value === 'entries') entries.value = await agentApi.learning.entries({ knowledgeBaseId: entryKB.value || undefined, status: entryStatus.value, q: entryQuery.value || undefined })
    if (tab.value === 'gaps') gaps.value = await agentApi.learning.gaps()
    if (tab.value === 'feedback') feedback.value = await agentApi.learning.feedback(feedbackRating.value)
    if (tab.value === 'memories') memories.value = await agentApi.memories.list({ q: memoryQuery.value || undefined, userId: memoryUser.value || undefined })
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
const entryDialogTitle = computed(() => ({ entry: entryForm.id ? '编辑 FAQ 条目' : '新建 FAQ 条目', candidate: '审核候选知识', gap: '补充答案' })[entryForm.mode])
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
async function reject(row: Candidate) {
  const { value } = await ElMessageBox.prompt('驳回原因（可选）', '驳回候选', { inputValue: '' })
  await agentApi.learning.reject(row.id, value || '')
  await reload()
}
async function removeEntry(row: Entry) { await ElMessageBox.confirm('删除该条目？', '确认', { type: 'warning' }); await agentApi.learning.deleteEntry(row.id); await reload() }
async function ignoreGap(row: Gap) { await agentApi.learning.ignoreGap(row.id); await reload() }

// Memories
const memoryDialog = ref(false)
const memoryForm = reactive({ id: '', content: '', kind: 'fact', pinned: false })
function editMemory(row: Memory) { Object.assign(memoryForm, { id: row.id, content: row.content, kind: row.kind, pinned: row.pinned }); memoryDialog.value = true }
async function saveMemory() { await agentApi.memories.update(memoryForm.id, { content: memoryForm.content, kind: memoryForm.kind, pinned: memoryForm.pinned }); memoryDialog.value = false; await loadTab() }
async function removeMemory(row: Memory) { await ElMessageBox.confirm('删除这条记忆？', '确认', { type: 'warning' }); await agentApi.memories.remove(row.id); await loadTab() }

onMounted(reload)
</script>

<style scoped>
.stat-row { display: grid; grid-template-columns: repeat(5, 1fr); gap: 12px; margin-bottom: 18px; }
.stat-card { background: #fff; border: 1px solid var(--line); border-radius: 7px; padding: 14px 16px; cursor: pointer; }
.stat-card span { display: block; color: var(--muted); font-size: 12px; margin-bottom: 6px; }
.stat-card strong { font-size: 22px; }
.qa strong { display: block; font-size: 13px; margin-bottom: 4px; }
.qa span { display: block; color: #4a5568; font-size: 12px; white-space: pre-wrap; line-height: 1.6; }
.qa small { display: block; color: var(--muted); margin-top: 4px; }
.muted { color: var(--muted); font-size: 12px; }
.danger { color: #d64545; font-weight: 600; }
.pin { color: #e6a23c; margin-right: 4px; vertical-align: -2px; }
.evidence { max-height: 220px; overflow: auto; font-size: 12px; }
@media (max-width: 900px) { .stat-row { grid-template-columns: repeat(2, 1fr); } }
</style>
