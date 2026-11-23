<template>
  <div class="content-page">
    <PageHeader title="知识审核" description="官方客服的回答、管理员的纠错、机器人没答上的问题，都会先到这里。审核通过后成为 FAQ，机器人就能用上。">
      <el-button :icon="Refresh" @click="reload">刷新</el-button>
    </PageHeader>
    <el-tabs v-model="tab" @tab-change="onTabChange">
      <el-tab-pane name="candidates">
        <template #label>待审核<span class="count-pill" :class="{ hot: stats?.pendingCandidates }">{{ stats?.pendingCandidates ?? 0 }}</span></template>
        <div class="toolbar"><el-segmented v-model="candidateStatus" :options="candidateStatusOptions" @change="loadTab" /><el-tag v-if="candidateConversation" closable round @close="clearConversationFilter">只看群：{{ candidateConversationName || candidateConversation }}</el-tag></div>
        <section class="table-panel">
          <el-table v-loading="loading" :data="candidates" :empty-text="candidateStatus === 'pending' ? '没有待审核的知识。在“群与会话 → 成员”里标记官方客服后，他们在群里的回答会自动出现在这里。' : '暂无记录'">
            <el-table-column label="问题 / 答案" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question }}</strong><span>{{ row.answer }}</span><small v-if="row.aliases?.length">相似问：{{ row.aliases.join('；') }}</small></div></template></el-table-column>
            <el-table-column label="来源" width="130"><template #default="{ row }"><div class="stacked-cell"><el-tag effect="plain" size="small">{{ sourceText[row.source] || row.source }}</el-tag><span>{{ row.knowledgeBaseName || '默认知识库' }}</span></div></template></el-table-column>
            <el-table-column label="可信度" width="90"><template #default="{ row }"><span :class="row.confidence >= 0.85 ? 'trend-good' : row.confidence < 0.6 ? 'danger-text' : ''">{{ Math.round(row.confidence * 100) }}%</span></template></el-table-column>
            <el-table-column label="时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
            <el-table-column label="操作" width="210" fixed="right"><template #default="{ row }"><template v-if="row.status === 'pending'"><el-button size="small" type="primary" @click="quickApprove(row)">通过</el-button><el-button size="small" @click="review(row)">修改后通过</el-button><el-button link type="danger" @click="reject(row)">驳回</el-button></template><span v-else class="muted">{{ row.status === 'approved' ? '已通过' : '已驳回' }}</span></template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane name="entries">
        <template #label>FAQ<span class="count-pill">{{ stats?.publishedEntries ?? 0 }}</span></template>
        <div class="toolbar">
          <el-input v-model="entryQuery" placeholder="搜索问题或答案" :prefix-icon="Search" clearable @change="loadTab" />
          <el-select v-model="entryKB" clearable placeholder="全部知识库" @change="loadTab"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
          <el-select v-model="entryStatus" @change="loadTab"><el-option label="已发布" value="published" /><el-option label="需复核" value="review" /><el-option label="已归档" value="archived" /><el-option label="全部" value="all" /></el-select>
          <span class="spacer" />
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

      <el-tab-pane name="gaps">
        <template #label>没答上的问题<span class="count-pill" :class="{ hot: stats?.openGaps }">{{ stats?.openGaps ?? 0 }}</span></template>
        <section class="table-panel">
          <el-table v-loading="loading" :data="gaps" empty-text="没有新的缺口。机器人因为缺少资料没答上的问题，会自动归类到这里，补上答案即可。">
            <el-table-column prop="question" label="用户问了但没答上的问题" min-width="360" />
            <el-table-column prop="occurrences" label="出现次数" width="100" sortable />
            <el-table-column label="最近出现" width="150"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></el-table-column>
            <el-table-column label="操作" width="160" fixed="right"><template #default="{ row }"><el-button size="small" type="primary" @click="answerGap(row)">补充答案</el-button><el-button link @click="ignoreGap(row)">忽略</el-button></template></el-table-column>
          </el-table>
        </section>
      </el-tab-pane>

      <el-tab-pane name="feedback">
        <template #label>反馈<span v-if="stats" class="count-pill" :class="{ hot: stats.negativeFeedback7d }" title="最近 7 天差评数">{{ stats.negativeFeedback7d }}</span></template>
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

      <el-tab-pane name="memories">
        <template #label>用户记忆<span class="count-pill">{{ stats?.activeMemories ?? 0 }}</span></template>
        <div class="toolbar"><el-input v-model="memoryQuery" placeholder="搜索记忆内容、昵称" :prefix-icon="Search" clearable @change="loadTab" /><el-input v-model="memoryUser" placeholder="用户 ID" clearable @change="loadTab" /></div>
        <section class="table-panel">
          <el-table v-loading="loading" :data="memories" empty-text="暂无记忆">
            <el-table-column label="用户" width="170"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.displayName || '—' }}</strong><span>{{ row.userId }}</span></div></template></el-table-column>
            <el-table-column label="记忆" min-width="320"><template #default="{ row }"><el-icon v-if="row.pinned" class="pin"><StarFilled /></el-icon>{{ row.content }}</template></el-table-column>
            <el-table-column label="类型" width="80"><template #default="{ row }">{{ kindText[row.kind] || row.kind }}</template></el-table-column>
            <el-table-column label="记忆向量" width="105">
              <template #default="{ row }">{{ row.hasEmbedding === true ? '已生成' : row.hasEmbedding === false ? '未生成' : '未知' }}</template>
            </el-table-column>
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
import { onMounted, reactive, ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useShellStore } from '@/stores/shell'
import { Plus, Refresh, Search, StarFilled } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import { api } from '@/api'
import { agentApi, feedbackSourceText, formatTime, ratingText, sourceText, type Candidate, type Entry, type Feedback, type Gap, type LearningStats, type Memory } from '@/api/agent'
import type { KnowledgeBase } from '@/types'

const kindText: Record<string, string> = { fact: '事实', preference: '偏好', issue: '问题', profile: '资料' }
const memorySourceText: Record<string, string> = { extracted: '自动', tool: 'AI 记录', admin: '手动' }
const route = useRoute()
const router = useRouter()
const shell = useShellStore()
const tabs = ['candidates', 'entries', 'gaps', 'feedback', 'memories']
const tab = ref(tabs.includes(String(route.query.tab)) ? String(route.query.tab) : 'candidates')
const candidateStatusOptions = [{ label: '待审核', value: 'pending' }, { label: '已通过', value: 'approved' }, { label: '已驳回', value: 'rejected' }, { label: '全部', value: 'all' }]
const loading = ref(false)
const stats = ref<LearningStats | null>(null)
const knowledgeBases = ref<KnowledgeBase[]>([])
const defaultKB = ref('')
const candidates = ref<Candidate[]>([])
const candidateStatus = ref('pending')
const candidateConversation = computed(() => typeof route.query.conversationId === 'string' ? route.query.conversationId : '')
const candidateConversationName = computed(() => typeof route.query.conversationName === 'string' ? route.query.conversationName : '')
function clearConversationFilter() { router.replace({ query: {} }) }
watch(candidateConversation, () => { if (tab.value === 'candidates') loadTab() })
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

function onTabChange() { router.replace({ query: tab.value === 'candidates' ? {} : { tab: tab.value } }); loadTab() }
watch(() => route.query.tab, (value) => { const next = tabs.includes(String(value)) ? String(value) : 'candidates'; if (next !== tab.value) { tab.value = next; loadTab() } })

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
.danger { color: var(--danger); font-weight: 600; }
.pin { color: #e6a23c; margin-right: 4px; vertical-align: -2px; }
.evidence { max-height: 220px; overflow: auto; font-size: 13px; }
.stacked-cell .el-tag { margin-bottom: 2px; }
</style>
