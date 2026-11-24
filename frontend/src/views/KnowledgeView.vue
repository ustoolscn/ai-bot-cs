<template>
  <div class="content-page">
    <PageHeader title="知识库" description="先选一个知识库，再管理它下面的文档、问答和待审核的知识。机器人回答时会查群里绑定的知识库。">
      <el-button :icon="Refresh" @click="reload">刷新</el-button>
      <el-button :icon="Plus" @click="createVisible = true">新建知识库</el-button>
    </PageHeader>

    <div class="knowledge-layout">
      <aside class="knowledge-list-panel">
        <el-input v-model="search" placeholder="搜索知识库" :prefix-icon="Search" clearable />
        <button v-for="kb in filteredBases" :key="kb.id" type="button" class="knowledge-list-item" :class="{ active: !sharedView && current?.id === kb.id }" @click="openKb(kb.id)">
          <span><strong>{{ kb.name }}</strong><small>{{ kb.documents }} 篇文档 · {{ kb.entries ?? 0 }} 条问答</small></span>
          <em v-if="pendingByKb[kb.id]" class="count-pill hot" :title="`${pendingByKb[kb.id]} 条待审核`">{{ pendingByKb[kb.id] }}</em>
        </button>
        <p v-if="!filteredBases.length" class="muted list-empty">{{ bases.length ? '没有匹配的知识库' : '还没有知识库' }}</p>

        <div class="list-divider"><span>所有知识库共用</span></div>
        <button type="button" class="knowledge-list-item" :class="{ active: sharedView === 'gaps' }" @click="openShared('gaps')">
          <span><strong>没答上的问题</strong><small>补上答案后存入所选知识库</small></span>
          <em v-if="stats?.openGaps" class="count-pill">{{ stats.openGaps }}</em>
        </button>
        <button type="button" class="knowledge-list-item" :class="{ active: sharedView === 'feedback' }" @click="openShared('feedback')">
          <span><strong>回答反馈</strong><small>用户和管理员对回答的评价</small></span>
          <em v-if="unreadFeedback" class="count-pill hot" title="未读差评">{{ unreadFeedback }}</em>
        </button>
      </aside>

      <!-- 共用：没答上的问题 -->
      <section v-if="sharedView === 'gaps'" class="knowledge-detail panel-surface">
        <div class="knowledge-detail-head"><div><h2>没答上的问题</h2><p>机器人因为缺少资料没答上的问题会自动归到这里。补充答案时选一个知识库存进去。</p></div></div>
        <div class="toolbar kb-toolbar"><span class="spacer" /><el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('gaps', '没答上的问题')">删除选中的 {{ selectedIds.length }} 条</el-button></div>
        <el-table row-key="id" v-loading="loading" :data="gaps" empty-text="没有新的问题" @selection-change="selectRows">
          <el-table-column type="selection" width="48" />
          <el-table-column prop="question" label="用户问了但没答上的问题" min-width="360" />
          <el-table-column prop="occurrences" label="出现次数" width="110" sortable align="right" />
          <el-table-column label="最近出现" width="150"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></el-table-column>
          <el-table-column label="操作" width="150" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="answerGap(row)">补充答案</el-button><el-button link @click="ignoreGap(row)">忽略</el-button></template></el-table-column>
        </el-table>
      </section>

      <!-- 共用：回答反馈 -->
      <section v-else-if="sharedView === 'feedback'" class="knowledge-detail panel-surface">
        <div class="knowledge-detail-head"><div><h2>回答反馈</h2><p>差评可以在消息记录里纠正答案，纠正后的答案会进入对应知识库的待审核。</p></div></div>
        <div class="toolbar kb-toolbar">
          <el-segmented v-model="feedbackView" :options="[{ label: '全部', value: 'all' }, { label: '未读', value: 'unread' }]" />
          <el-select v-model="feedbackRating" clearable placeholder="全部评价" aria-label="按评价筛选" @change="loadPane"><el-option v-for="(v, k) in ratingText" :key="k" :label="v" :value="k" /></el-select>
          <span class="spacer" />
          <el-button v-if="selectedIds.length" :loading="markingRead" @click="markRead({ ids: [...selectedIds] })">选中的标为已读</el-button>
          <el-button v-else :disabled="!feedback.some(f => !f.read)" :loading="markingRead" @click="markRead({ all: true })">全部标为已读</el-button>
          <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('feedback', '反馈')">删除选中的 {{ selectedIds.length }} 条</el-button>
        </div>
        <el-table row-key="id" v-loading="loading" :data="shownFeedback" :row-class-name="({ row }: { row: Feedback }) => row.read ? '' : 'is-unread'" :empty-text="feedbackView === 'unread' ? '没有未读反馈' : '暂无反馈'" @selection-change="selectRows">
          <el-table-column type="selection" width="48" />
          <el-table-column width="76"><template #default="{ row }"><button v-if="!row.read" type="button" class="unread-dot" title="点击标为已读" @click.stop="markRead({ ids: [row.id] })"><i />未读</button><span v-else class="faint">已读</span></template></el-table-column>
          <el-table-column label="问题 / 回答" min-width="360"><template #default="{ row }"><div class="qa"><strong>{{ row.question || '—' }}</strong><span>{{ row.answer }}</span><small v-if="row.correctedAnswer">纠正：{{ row.correctedAnswer }}</small></div></template></el-table-column>
          <el-table-column label="评价" width="90"><template #default="{ row }"><el-tag size="small" :type="['good', 'helpful'].includes(row.rating) ? 'success' : 'danger'">{{ ratingText[row.rating] || row.rating }}</el-tag></template></el-table-column>
          <el-table-column label="来源" width="100"><template #default="{ row }">{{ feedbackSourceText[row.source] || row.source }}</template></el-table-column>
          <el-table-column prop="conversationName" label="会话" width="140" />
          <el-table-column label="时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        </el-table>
      </section>

      <!-- 选中的知识库 -->
      <section v-else-if="current" class="knowledge-detail panel-surface">
        <div class="knowledge-detail-head">
          <div><span class="scope-tag">{{ scopeText(current.scope) }}知识库</span><h2>{{ current.name }}</h2><p>{{ current.description || '没有说明' }}</p></div>
          <div><el-button :icon="Search" @click="testVisible = true">检索测试</el-button></div>
        </div>
        <div class="knowledge-summary">
          <div><span>文档</span><strong>{{ current.documents }}</strong></div>
          <div><span>问答</span><strong>{{ current.entries ?? 0 }}</strong></div>
          <div><span>分块</span><strong>{{ current.chunks.toLocaleString() }}</strong></div>
          <div><span>Embedding 模型</span><strong class="truncate kb-model" :title="current.model">{{ current.model }}</strong></div>
          <div><span>索引进度</span><strong>{{ current.progress }}%</strong><div class="progress-line" :class="{ 'is-failed': current.status === 'failed' }"><i :style="{ width: current.progress + '%' }" /></div></div>
        </div>

        <el-tabs :model-value="kbTab" class="kb-tabs" @update:model-value="setTab">
          <el-tab-pane name="documents" label="文档">
            <KnowledgeDocuments v-if="kbTab === 'documents'" :base="current" @changed="refreshCurrent" />
          </el-tab-pane>

          <el-tab-pane name="entries">
            <template #label>问答<span class="count-pill">{{ current.entries ?? 0 }}</span></template>
            <template v-if="kbTab === 'entries'">
              <div class="toolbar kb-toolbar">
                <el-input v-model="entryQuery" placeholder="搜索问题或答案" :prefix-icon="Search" clearable @change="loadPane" />
                <el-select v-model="entryStatus" aria-label="按状态筛选" @change="loadPane"><el-option label="已发布" value="published" /><el-option label="需复核" value="review" /><el-option label="已归档" value="archived" /><el-option label="全部" value="all" /></el-select>
                <span class="spacer" />
                <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('entries', '问答')">删除选中的 {{ selectedIds.length }} 条</el-button>
                <el-button type="primary" :icon="Plus" @click="editEntry()">新建问答</el-button>
              </div>
              <el-table row-key="id" v-loading="loading" :data="entries" @selection-change="selectRows">
                <el-table-column type="selection" width="48" />
                <el-table-column label="问题 / 答案" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question }}</strong><span>{{ row.answer }}</span><small v-if="row.aliases?.length">相似问：{{ row.aliases.join('；') }}</small></div></template></el-table-column>
                <el-table-column label="来源" width="100"><template #default="{ row }">{{ sourceText[row.source] || row.source }}</template></el-table-column>
                <el-table-column label="命中 / 好评 / 差评" width="140" align="right"><template #default="{ row }"><span class="num">{{ row.hitCount }} / {{ row.helpfulCount }} / <span :class="{ 'danger-text': row.unhelpfulCount > row.helpfulCount }">{{ row.unhelpfulCount }}</span></span></template></el-table-column>
                <el-table-column label="状态" width="110"><template #default="{ row }"><el-tag size="small" :type="row.status === 'published' ? (row.indexStatus === 'ready' ? 'success' : row.indexStatus === 'failed' ? 'danger' : 'warning') : row.status === 'review' ? 'danger' : 'info'">{{ entryStatusText(row) }}</el-tag></template></el-table-column>
                <el-table-column label="操作" width="120" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="editEntry(row)">编辑</el-button><el-button link type="danger" @click="removeEntry(row)">删除</el-button></template></el-table-column>
                <template #empty><div class="table-empty"><el-icon><ChatLineSquare /></el-icon><strong>这个知识库还没有问答</strong><span>点“新建问答”手动添加，或通过待审核的知识补充</span></div></template>
              </el-table>
            </template>
          </el-tab-pane>

          <el-tab-pane name="candidates">
            <template #label>待审核<span class="count-pill" :class="{ hot: pendingByKb[current.id] }">{{ pendingByKb[current.id] || 0 }}</span></template>
            <template v-if="kbTab === 'candidates'">
              <div class="toolbar kb-toolbar">
                <el-segmented v-model="candidateStatus" :options="candidateStatusOptions" @change="loadPane" />
                <el-tag v-if="candidateConversation" closable @close="clearConversationFilter">只看：{{ candidateConversationName || candidateConversation }}</el-tag>
                <span class="spacer" />
                <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('candidates', '待审核记录')">删除选中的 {{ selectedIds.length }} 条</el-button>
              </div>
              <el-table row-key="id" v-loading="loading" :data="kbCandidates" @selection-change="selectRows">
                <el-table-column type="selection" width="48" />
                <el-table-column label="问题 / 答案" min-width="380"><template #default="{ row }"><div class="qa"><strong>{{ row.question }}</strong><span>{{ row.answer }}</span><small v-if="row.aliases?.length">相似问：{{ row.aliases.join('；') }}</small></div></template></el-table-column>
                <el-table-column label="来源" width="110"><template #default="{ row }">{{ sourceText[row.source] || row.source }}</template></el-table-column>
                <el-table-column label="可信度" width="90" align="right"><template #default="{ row }"><span class="num" :class="row.confidence >= 0.85 ? 'trend-good' : row.confidence < 0.6 ? 'danger-text' : ''">{{ Math.round(row.confidence * 100) }}%</span></template></el-table-column>
                <el-table-column label="时间" width="150"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
                <el-table-column label="操作" width="210" fixed="right"><template #default="{ row }"><template v-if="row.status === 'pending'"><el-button link type="primary" @click="quickApprove(row)">通过</el-button><el-button link type="primary" @click="review(row)">修改后通过</el-button><el-button link type="danger" @click="reject(row)">驳回</el-button></template><span v-else class="pill" :class="row.status === 'approved' ? 'good' : 'idle'">{{ row.status === 'approved' ? '已通过' : '已驳回' }}</span></template></el-table-column>
                <template #empty><div class="table-empty"><el-icon><Finished /></el-icon><strong>{{ candidateStatus === 'pending' ? '没有待审核的知识' : '暂无记录' }}</strong><span v-if="candidateStatus === 'pending'">在“群与会话 → 成员”里标记官方客服后，他们在群里的回答会出现在这里</span></div></template>
              </el-table>
            </template>
          </el-tab-pane>
        </el-tabs>
      </section>

      <section v-else class="panel"><EmptyState title="还没有知识库" description="知识库存放机器人回答时参考的资料。先新建一个，再往里面上传文档或添加问答。"><el-button type="primary" :icon="Plus" @click="createVisible = true">新建知识库</el-button></EmptyState></section>
    </div>

    <el-dialog v-model="entryDialog" :title="entryDialogTitle" width="640px">
      <el-form label-position="top">
        <el-form-item label="存入知识库"><el-select v-model="entryForm.knowledgeBaseId" :disabled="!!entryForm.id"><el-option v-for="kb in bases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></el-form-item>
        <el-form-item label="标准问题"><el-input v-model="entryForm.question" /></el-form-item>
        <el-form-item label="相似问法（每行一个）"><el-input v-model="entryForm.aliasesText" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="答案（建议口语化、简洁）"><el-input v-model="entryForm.answer" type="textarea" :rows="6" /></el-form-item>
        <el-form-item v-if="entryForm.id" label="状态"><el-radio-group v-model="entryForm.status"><el-radio-button value="published">发布</el-radio-button><el-radio-button value="review">需复核</el-radio-button><el-radio-button value="archived">归档</el-radio-button></el-radio-group></el-form-item>
        <el-form-item v-if="entryForm.mode === 'candidate' && evidenceText" label="来源对话"><div class="content-box evidence">{{ evidenceText }}</div></el-form-item>
      </el-form>
      <template #footer><el-button @click="entryDialog = false">取消</el-button><el-button type="primary" @click="submitEntry">{{ entryForm.mode === 'candidate' ? '通过并发布' : entryForm.mode === 'gap' ? '保存答案' : '保存问答' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="createVisible" title="新建知识库" width="520px">
      <el-form label-position="top">
        <el-form-item label="知识库名称"><el-input v-model="createForm.name" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="createForm.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="Embedding 模型"><el-select v-model="createForm.embeddingProfileId"><el-option v-for="model in embeddingModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item>
      </el-form>
      <template #footer><el-button @click="createVisible = false">取消</el-button><el-button type="primary" @click="createKb">创建知识库</el-button></template>
    </el-dialog>

    <el-drawer v-model="testVisible" :title="`检索测试 · ${current?.name || ''}`" size="560px">
      <p class="muted drawer-hint">输入一个用户可能会问的问题，看看这个知识库能找到哪些内容。相似度越高越相关。</p>
      <el-form label-position="top">
        <el-form-item label="测试问题"><el-input v-model="testQuery" type="textarea" :rows="3" placeholder="输入一个真实用户问题，Ctrl/⌘ + Enter 开始检索" @keydown.enter.ctrl.exact.prevent="runTest" @keydown.enter.meta.exact.prevent="runTest" /></el-form-item>
        <el-button type="primary" :loading="testing" @click="runTest">开始检索</el-button>
      </el-form>
      <div v-if="testResults.length" class="test-results"><div v-for="result in testResults" :key="result.content"><strong>相似度 {{ (result.score * 100).toFixed(1) }}%</strong><p>{{ result.content }}</p></div></div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useShellStore } from '@/stores/shell'
import { ChatLineSquare, Finished, Plus, Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import { api } from '@/api'
import { useBulkDelete } from '@/composables/useBulkDelete'
import KnowledgeDocuments from './KnowledgeDocuments.vue'
import { agentApi, feedbackSourceText, formatTime, ratingText, sourceText, type Candidate, type Entry, type Feedback, type Gap, type LearningStats } from '@/api/agent'
import type { KnowledgeBase, ModelProfile } from '@/types'

const route = useRoute()
const router = useRouter()
const shell = useShellStore()

// ---- 路由：?kb=<id>&tab=documents|entries|candidates，或 ?tab=gaps|feedback（共用）----
const kbTabs = ['documents', 'entries', 'candidates']
const queryTab = computed(() => String(route.query.tab || ''))
const sharedView = computed(() => (['gaps', 'feedback'].includes(queryTab.value) ? queryTab.value : '') as '' | 'gaps' | 'feedback')
const kbTab = computed(() => kbTabs.includes(queryTab.value) ? queryTab.value : 'documents')
const queryKb = computed(() => typeof route.query.kb === 'string' ? route.query.kb : '')

const bases = ref<KnowledgeBase[]>([])
const current = ref<KnowledgeBase>()
const models = ref<ModelProfile[]>([])
const defaultKB = ref('')
const stats = ref<LearningStats | null>(null)
const pendingCandidates = ref<Candidate[]>([])
const search = ref('')
const filteredBases = computed(() => bases.value.filter(k => !search.value || `${k.name}${k.description}`.includes(search.value)))
const embeddingModels = computed(() => models.value.filter(m => m.kind === 'embedding'))
const unreadFeedback = computed(() => stats.value ? stats.value.unreadNegativeFeedback ?? stats.value.negativeFeedback7d : 0)

// 候选知识没写知识库时，归到“学习结果存入”的默认知识库
function candidateKb(c: Candidate) {
  if (c.knowledgeBaseId) return c.knowledgeBaseId
  if (c.knowledgeBaseName) return bases.value.find(b => b.name === c.knowledgeBaseName)?.id || defaultKB.value
  return defaultKB.value
}
const pendingByKb = computed(() => pendingCandidates.value.reduce<Record<string, number>>((acc, c) => { const id = candidateKb(c); acc[id] = (acc[id] || 0) + 1; return acc }, {}))

function openKb(id: string) { router.replace({ query: { kb: id, ...(kbTab.value !== 'documents' && !sharedView.value ? { tab: kbTab.value } : {}) } }) }
function openShared(view: 'gaps' | 'feedback') { router.replace({ query: { tab: view } }) }
function setTab(value: string | number) {
  if (!current.value) return
  const tab = String(value)
  router.replace({ query: { kb: current.value.id, ...(tab === 'documents' ? {} : { tab }) } })
}

// ---- 数据 ----
const loading = ref(false)
const entries = ref<Entry[]>([])
const entryQuery = ref('')
const entryStatus = ref('published')
const candidates = ref<Candidate[]>([])
const candidateStatus = ref('pending')
const candidateStatusOptions = [{ label: '待审核', value: 'pending' }, { label: '已通过', value: 'approved' }, { label: '已驳回', value: 'rejected' }, { label: '全部', value: 'all' }]
const candidateConversation = computed(() => typeof route.query.conversationId === 'string' ? route.query.conversationId : '')
const candidateConversationName = computed(() => typeof route.query.conversationName === 'string' ? route.query.conversationName : '')
const kbCandidates = computed(() => current.value ? candidates.value.filter(c => candidateKb(c) === current.value!.id) : [])
function clearConversationFilter() { router.replace({ query: { kb: current.value?.id, tab: 'candidates' } }) }
const gaps = ref<Gap[]>([])
const feedback = ref<Feedback[]>([])
const feedbackRating = ref('')
const feedbackView = ref<'all' | 'unread'>('all')
const shownFeedback = computed(() => feedbackView.value === 'unread' ? feedback.value.filter(f => !f.read) : feedback.value)

const { selectedIds, deleting, deleteSelected } = useBulkDelete(reload)
function selectRows(rows: { id: string }[]) { selectedIds.value = rows.map(r => r.id) }

async function reload() {
  const [s, kbs, settings, pending, ms] = await Promise.all([
    agentApi.learning.stats().catch(() => null), api.knowledge.list(), agentApi.settings.get().catch(() => null),
    agentApi.learning.candidates('pending').catch(() => []), api.models.list().catch(() => []),
  ])
  stats.value = s
  shell.refreshCounts(true)
  bases.value = kbs
  models.value = ms
  defaultKB.value = settings?.learningKnowledgeBaseId || kbs[0]?.id || ''
  pendingCandidates.value = pending
  if (!createForm.embeddingProfileId) createForm.embeddingProfileId = embeddingModels.value[0]?.id || ''
  await syncSelection(true)
}

// 选中哪个知识库：URL 指定的 > 当前的 > 有待审核的（从“待审核”链接进来时）> 第一个
async function syncSelection(fresh = false) {
  if (sharedView.value) { await loadPane(); return }
  const wanted = queryKb.value || current.value?.id
    || (kbTab.value === 'candidates' ? Object.entries(pendingByKb.value).sort((a, b) => b[1] - a[1])[0]?.[0] : '')
    || bases.value[0]?.id || ''
  const base = bases.value.find(b => b.id === wanted) || bases.value[0]
  if (!base) { current.value = undefined; return }
  // 地址里还没写知识库时先补上，交给下面的 watch 再加载一次
  if (queryKb.value !== base.id) { router.replace({ query: { ...route.query, kb: base.id } }); return }
  if (fresh || current.value?.id !== base.id || !current.value.documentList) {
    try { current.value = await api.knowledge.get(base.id) } catch { current.value = base }
  }
  await loadPane()
}
async function refreshCurrent() {
  if (!current.value) return
  const [kbs, detail] = await Promise.all([api.knowledge.list(), api.knowledge.get(current.value.id).catch(() => current.value)])
  bases.value = kbs
  current.value = detail
}

async function loadPane() {
  selectedIds.value = []
  loading.value = true
  try {
    if (sharedView.value === 'gaps') gaps.value = await agentApi.learning.gaps()
    else if (sharedView.value === 'feedback') feedback.value = await agentApi.learning.feedback(feedbackRating.value)
    else if (current.value && kbTab.value === 'entries') entries.value = await agentApi.learning.entries({ knowledgeBaseId: current.value.id, status: entryStatus.value, q: entryQuery.value || undefined })
    else if (current.value && kbTab.value === 'candidates') candidates.value = await agentApi.learning.candidates(candidateStatus.value, candidateConversation.value || undefined)
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally { loading.value = false }
}
watch([queryKb, queryTab, candidateConversation], () => { if (bases.value.length || sharedView.value) syncSelection() })
watch(feedbackView, () => { selectedIds.value = [] })

function scopeText(v: string) { return ({ global: '全局', bot: '机器人级', conversation: '会话级' } as Record<string, string>)[v] || '' }
function entryStatusText(row: Entry) {
  if (row.status === 'review') return '需复核'
  if (row.status === 'archived') return '已归档'
  return ({ ready: '已生效', indexed: '已生效', failed: '索引失败', pending: '索引中' } as Record<string, string>)[row.indexStatus] || row.indexStatus
}

// ---- 反馈已读 ----
const markingRead = ref(false)
async function markRead(target: { ids: string[] } | { all: true }) {
  markingRead.value = true
  try {
    await agentApi.learning.markFeedbackRead(target)
    const ids = 'ids' in target ? new Set(target.ids) : null
    feedback.value = feedback.value.map(f => !ids || ids.has(f.id) ? { ...f, read: true } : f)
    selectedIds.value = []
    stats.value = await agentApi.learning.stats()
    shell.refreshCounts(true)
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '标记失败') } finally { markingRead.value = false }
}

// ---- 问答 / 审核 / 补答 共用表单 ----
const entryDialog = ref(false)
const entryForm = reactive({ mode: 'entry' as 'entry' | 'candidate' | 'gap', id: '', refId: '', knowledgeBaseId: '', question: '', aliasesText: '', answer: '', status: 'published', evidence: [] as Array<Record<string, string>> })
const entryDialogTitle = computed(() => ({ entry: entryForm.id ? '编辑问答' : '新建问答', candidate: '审核候选知识', gap: '补充答案' })[entryForm.mode])
const evidenceText = computed(() => entryForm.evidence.map(e => e.context ? `${e.context}\n→ ${e.content}` : e.question ? `用户：${e.question}` : e.content || '').filter(Boolean).join('\n\n'))
function openForm(mode: 'entry' | 'candidate' | 'gap', values: Partial<typeof entryForm>) {
  Object.assign(entryForm, { mode, id: '', refId: '', knowledgeBaseId: current.value?.id || defaultKB.value, question: '', aliasesText: '', answer: '', status: 'published', evidence: [] }, values)
  entryDialog.value = true
}
function editEntry(row?: Entry) { openForm('entry', row ? { id: row.id, knowledgeBaseId: row.knowledgeBaseId, question: row.question, aliasesText: (row.aliases || []).join('\n'), answer: row.answer, status: row.status } : {}) }
function review(row: Candidate) { openForm('candidate', { refId: row.id, knowledgeBaseId: candidateKb(row), question: row.question, aliasesText: (row.aliases || []).join('\n'), answer: row.answer, evidence: row.evidence || [] }) }
function answerGap(row: Gap) { openForm('gap', { refId: row.id, question: row.question, knowledgeBaseId: defaultKB.value }) }
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
  const knowledgeBaseId = candidateKb(row)
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
  try { await agentApi.learning.reject(row.id, value || ''); await reload() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '操作失败') }
}
async function removeEntry(row: Entry) {
  try { await ElMessageBox.confirm('删除这条问答？删除后不能恢复。', '删除问答', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }) } catch { return }
  try { await agentApi.learning.deleteEntry(row.id); await reload() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '删除失败') }
}
async function ignoreGap(row: Gap) { try { await agentApi.learning.ignoreGap(row.id); await reload() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '操作失败') } }

// ---- 新建知识库 ----
const createVisible = ref(false)
const createForm = reactive({ name: '', description: '', embeddingProfileId: '' })
async function createKb() {
  if (!createForm.name.trim() || !createForm.embeddingProfileId) { ElMessage.error('请填写名称并选择 Embedding 模型'); return }
  try {
    const result = await api.knowledge.create(createForm.name, createForm.description, createForm.embeddingProfileId)
    createVisible.value = false
    Object.assign(createForm, { name: '', description: '', embeddingProfileId: embeddingModels.value[0]?.id || '' })
    ElMessage.success('知识库已创建')
    await router.replace({ query: { kb: result.id } })
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '创建失败') }
}

// ---- 检索测试（针对当前知识库）----
const testVisible = ref(false), testQuery = ref(''), testing = ref(false), testResults = ref<Array<{ content: string; score: number }>>([])
async function runTest() {
  if (!current.value || !testQuery.value.trim()) return
  testing.value = true
  try { testResults.value = await api.knowledge.test(current.value.id, testQuery.value) }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '检索失败') } finally { testing.value = false }
}

onMounted(reload)
useAdminEvents(['knowledge_bases', 'knowledge_documents', 'knowledge_entries', 'knowledge_candidates', 'knowledge_gaps', 'message_feedback', 'model_profiles'], reload)
</script>

<style scoped>
.list-empty { padding: 8px 10px; font-size: 13px; }
.list-divider { margin: 12px 0 4px; padding: 12px 10px 0; border-top: 1px solid var(--line); color: var(--faint); font-size: 12px; }
.knowledge-list-item .count-pill { margin-left: 0; }
.kb-model { font-size: 14px; }
.kb-tabs { padding: 4px 20px 20px; }
.kb-tabs :deep(.el-tabs__header) { margin-bottom: 12px; }
.kb-toolbar { margin-bottom: 12px; }
.knowledge-detail > .kb-toolbar { padding: 0 20px; }
.knowledge-detail > .el-table { border-top: 1px solid var(--line); }
.kb-hint { font-size: 13px; }
.drawer-hint { margin-bottom: 16px; font-size: 13px; }
.evidence { max-height: 220px; overflow: auto; font-size: 13px; }
.unread-dot { display: inline-flex; align-items: center; gap: 6px; padding: 2px 0; border: 0; background: none; color: var(--primary-text); font: inherit; font-size: 13px; font-weight: 600; cursor: pointer; }
.unread-dot i { width: 7px; height: 7px; border-radius: 50%; background: var(--primary); }
.unread-dot:hover { text-decoration: underline; }
:deep(.is-unread .qa strong) { font-weight: 600; }
.faint { font-size: 13px; }
</style>
