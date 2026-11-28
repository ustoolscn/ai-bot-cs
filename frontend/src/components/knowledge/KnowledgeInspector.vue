<template>
  <aside class="inspector-panel">
    <div class="inspector-head">
      <strong>{{ headerTitle }}</strong>
      <div class="inspector-head-actions">
        <button v-if="selection" type="button" class="link-btn" @click="emit('select', null)">返回全景</button>
        <button type="button" class="link-btn" @click="emit('close')">收起</button>
      </div>
    </div>

    <div class="inspector-body">
      <Transition name="inspector-swap" mode="out-in">
      <div :key="selectionKey" class="inspector-swap-wrap">
      <!-- 1. 文档详情与切片预览 -->
      <template v-if="selection?.type === 'document' && activeDoc">
        <div class="ins-section">
          <div class="ins-title-row">
            <span class="file-badge">{{ activeDoc.type }}</span>
            <h3>{{ activeDoc.name }}</h3>
          </div>
          <p class="ins-meta">{{ fileSize(activeDoc.sizeBytes) }} · {{ activeDoc.chunkCount }} 分块<template v-if="activeDoc.imageCount"> · {{ activeDoc.imageCount }} 张图</template> · {{ formatTime(activeDoc.updatedAt) }}</p>
          <el-alert v-if="activeDoc.lastError" type="error" :closable="false" show-icon :title="activeDoc.lastError" class="ins-alert" />
          <div class="ins-actions">
            <el-button v-if="activeDoc.status === 'failed' || activeDoc.status === 'unindexed' || activeDoc.lastError" size="small" type="primary" @click="emit('retryDoc', activeDoc.id)">重新索引</el-button>
            <el-button v-if="activeDoc.chunkCount > 0" size="small" @click="emit('deleteDocIndex', activeDoc.id, activeDoc.name, activeDoc.chunkCount)">清除索引</el-button>
            <el-button size="small" type="danger" plain @click="emit('deleteDoc', activeDoc.id, activeDoc.name)">删除文档</el-button>
          </div>
        </div>

        <div class="ins-section">
          <div class="ins-subhead"><strong>文档切片预览</strong><span class="muted">{{ chunks.length }} 段</span></div>
          <div v-loading="loadingChunks" class="chunk-list">
            <article v-for="ch in chunks" :key="ch.id" class="chunk-card">
              <div class="chunk-meta"><strong>#{{ ch.chunkIndex + 1 }}</strong><span v-if="ch.hasImage" class="chip">含图</span></div>
              <p>{{ ch.content }}</p>
              <a v-if="ch.hasImage && ch.imageUrl" :href="ch.imageUrl" target="_blank" rel="noopener" class="chunk-img"><img :src="ch.imageUrl" alt="分块截图" loading="lazy"></a>
            </article>
            <p v-if="!loadingChunks && !chunks.length" class="muted empty-hint">该文档尚未切分出索引片段。</p>
          </div>
        </div>
      </template>

      <!-- 2. 问答条目详情 -->
      <template v-else-if="selection?.type === 'entry' && activeEntry">
        <div class="ins-section">
          <span class="ins-kicker">标准问答 · {{ sourceText[activeEntry.source] || activeEntry.source }}</span>
          <h3>{{ $at(activeEntry.question) }}</h3>
          <p v-if="activeEntry.aliases?.length" class="ins-meta">相似问：{{ activeEntry.aliases.join('；') }}</p>
          <div class="ins-box">{{ $at(activeEntry.answer) }}</div>
          <div class="ins-stats">
            <div><span>命中</span><strong>{{ activeEntry.hitCount }}</strong></div>
            <div><span>好评</span><strong>{{ activeEntry.helpfulCount }}</strong></div>
            <div><span>差评</span><strong :class="{ 'danger-text': activeEntry.unhelpfulCount > activeEntry.helpfulCount }">{{ activeEntry.unhelpfulCount }}</strong></div>
          </div>
          <div class="ins-actions">
            <el-button size="small" type="primary" @click="emit('editEntry', activeEntry.id)">编辑问答</el-button>
            <el-button size="small" type="danger" plain @click="emit('deleteEntry', activeEntry.id)">删除问答</el-button>
          </div>
        </div>
      </template>

      <!-- 3. 待审核候选详情 -->
      <template v-else-if="selection?.type === 'candidate' && activeCandidate">
        <div class="ins-section">
          <span class="ins-kicker">待审核候选 · 可信度 {{ Math.round(activeCandidate.confidence * 100) }}%</span>
          <h3>{{ $at(activeCandidate.question) }}</h3>
          <div class="ins-box">{{ $at(activeCandidate.answer) }}</div>
          <div class="ins-actions">
            <el-button size="small" type="primary" @click="emit('quickApprove', activeCandidate.id)">直接通过</el-button>
            <el-button size="small" @click="emit('reviewCandidate', activeCandidate.id)">修改后通过</el-button>
            <el-button size="small" type="danger" plain @click="emit('rejectCandidate', activeCandidate.id)">驳回</el-button>
          </div>
        </div>
      </template>

      <!-- 4. 知识缺口详情 -->
      <template v-else-if="selection?.type === 'gap' && activeGap">
        <div class="ins-section">
          <span class="ins-kicker warn">知识缺口 · 出现 {{ activeGap.occurrences }} 次</span>
          <h3>{{ activeGap.question }}</h3>
          <p class="ins-meta">最近一次未答上：{{ formatTime(activeGap.lastSeenAt) }}</p>
          <div class="ins-actions">
            <el-button size="small" type="primary" @click="emit('answerGap', activeGap.id)">立即补充答案</el-button>
            <el-button size="small" @click="emit('ignoreGap', activeGap.id)">忽略此缺口</el-button>
          </div>
        </div>
      </template>

      <!-- 5. 星图节点分块详情 -->
      <template v-else-if="selection?.type === 'point'">
        <div class="ins-section">
          <span class="ins-kicker">{{ pointKindText(selection.point.kind) }}<template v-if="clusterOf(selection.point.clusterId)"> · {{ clusterOf(selection.point.clusterId)?.label }}</template></span>
          <h3>{{ selection.point.title }}</h3>
          <div class="ins-box">{{ selection.point.snippet }}</div>
          <a v-if="selection.point.hasImage && selection.point.imageUrl" :href="selection.point.imageUrl" target="_blank" rel="noopener" class="chunk-img"><img :src="selection.point.imageUrl" alt="分块截图" loading="lazy"></a>
          <div class="ins-actions">
            <el-button v-if="selection.point.kind === 'document'" size="small" type="primary" @click="emit('select', { type: 'document', id: selection.point.sourceId })">查看所属文档全部分块</el-button>
            <el-button v-else-if="selection.point.kind === 'entry'" size="small" type="primary" @click="emit('select', { type: 'entry', id: selection.point.sourceId })">查看完整问答</el-button>
            <el-button v-else-if="selection.point.kind === 'gap'" size="small" type="primary" @click="emit('answerGap', selection.point.sourceId)">补充此问题答案</el-button>
          </div>
        </div>
      </template>

      <!-- 默认全景汇总：健康概览、检索探针命中列表、主题簇与待办直通 -->
      <template v-else>
        <div v-if="searchHits.length" class="ins-section">
          <div class="ins-subhead"><strong>检索探针命中</strong><span class="muted">前 {{ searchHits.length }} 条</span></div>
          <div class="hit-list">
            <button v-for="(hit, idx) in searchHits" :key="hit.id || idx" type="button" class="hit-card" @click="locateHit(hit)">
              <div class="hit-head"><strong>相似度 {{ (hit.score * 100).toFixed(1) }}%</strong><span v-if="hit.hasImage" class="chip">截图</span></div>
              <p>{{ hit.content }}</p>
            </button>
          </div>
        </div>

        <div class="ins-section">
          <div class="ins-subhead"><strong>核心主题聚类</strong><span class="muted">{{ mapData.clusters.length }} 个主题</span></div>
          <div v-if="mapData.clusters.length" class="cluster-list">
            <button v-for="cl in mapData.clusters" :key="cl.id" type="button" class="cluster-row" @click="emit('focusCluster', cl.id)">
              <span>{{ cl.label }}</span>
              <small>{{ cl.count }} 个知识块</small>
            </button>
          </div>
          <p v-else class="muted empty-hint">索引完成后将自动提取语义主题簇。</p>
        </div>

        <div class="ins-section">
          <div class="ins-subhead"><strong>待办与缺口直通</strong></div>
          <div class="todo-cards">
            <button type="button" class="todo-card" @click="emit('navigateTab', 'candidates')">
              <span>待审核候选</span><strong>{{ mapData.candidates.length }}</strong>
            </button>
            <button type="button" class="todo-card" @click="emit('navigateTab', 'gaps')">
              <span>没答上问题</span><strong :class="{ warn: mapData.gaps.length }">{{ mapData.gaps.length }}</strong>
            </button>
            <button type="button" class="todo-card" @click="emit('navigateTab', 'feedback')">
              <span>未读差评</span><strong :class="{ danger: unreadFeedbackCount }">{{ unreadFeedbackCount }}</strong>
            </button>
          </div>
        </div>

        <div class="ins-section">
          <div class="ins-subhead"><strong>已绑定范围</strong></div>
          <div v-if="mapData.boundConversations.length || mapData.boundAgents.length" class="bound-tags">
            <el-tag v-for="a in mapData.boundAgents" :key="'a:' + a.id" size="small" type="info">客服：{{ a.name }}</el-tag>
            <el-tag v-for="c in mapData.boundConversations" :key="'c:' + c.id" size="small">{{ c.name }}</el-tag>
          </div>
          <p v-else class="muted empty-hint">暂未绑定群聊或客服配置。</p>
        </div>
      </template>
      </div>
      </Transition>
    </div>
  </aside>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api } from '@/api'
import { formatTime, sourceText } from '@/api/agent'
import type { KnowledgeDocumentChunk, KnowledgeInspectorSelection, KnowledgeMapData, KnowledgeMapPoint, KnowledgeSearchHit } from '@/types'

const props = defineProps<{
  kbId: string
  mapData: KnowledgeMapData
  selection: KnowledgeInspectorSelection
  searchHits: KnowledgeSearchHit[]
  unreadFeedbackCount: number
}>()

const emit = defineEmits<{
  select: [selection: KnowledgeInspectorSelection]
  close: []
  focusCluster: [clusterId: number]
  navigateTab: [tab: 'documents' | 'entries' | 'candidates' | 'gaps' | 'feedback']
  retryDoc: [docId: string]
  deleteDocIndex: [docId: string, name: string, chunks: number]
  deleteDoc: [docId: string, name: string]
  editEntry: [entryId: string]
  deleteEntry: [entryId: string]
  quickApprove: [candidateId: string]
  reviewCandidate: [candidateId: string]
  rejectCandidate: [candidateId: string]
  answerGap: [gapId: string]
  ignoreGap: [gapId: string]
}>()

const activeDoc = computed(() => {
  const sel = props.selection
  return sel?.type === 'document' ? props.mapData.documents.find(d => d.id === sel.id) : undefined
})
const activeEntry = computed(() => {
  const sel = props.selection
  return sel?.type === 'entry' ? props.mapData.entries.find(e => e.id === sel.id) : undefined
})
const activeCandidate = computed(() => {
  const sel = props.selection
  return sel?.type === 'candidate' ? props.mapData.candidates.find(c => c.id === sel.id) : undefined
})
const activeGap = computed(() => {
  const sel = props.selection
  return sel?.type === 'gap' ? props.mapData.gaps.find(g => g.id === sel.id) : undefined
})

const headerTitle = computed(() => {
  if (props.selection?.type === 'document') return '文档透视'
  if (props.selection?.type === 'entry') return '问答透视'
  if (props.selection?.type === 'candidate') return '候选知识审核'
  if (props.selection?.type === 'gap') return '知识缺口处理'
  if (props.selection?.type === 'point') return '知识块详情'
  return '知识库全景面板'
})

const selectionKey = computed(() => {
  const sel = props.selection
  if (!sel) return 'overview'
  return sel.type === 'point' ? `point:${sel.point.id}` : `${sel.type}:${sel.id}`
})

const chunks = ref<KnowledgeDocumentChunk[]>([])
const loadingChunks = ref(false)
watch(
  () => [props.kbId, props.selection?.type === 'document' ? props.selection.id : ''] as const,
  async ([kbId, docId]) => {
    if (!kbId || !docId) { chunks.value = []; return }
    loadingChunks.value = true
    try { chunks.value = await api.knowledge.documentChunks(kbId, docId) }
    catch { chunks.value = [] }
    finally { loadingChunks.value = false }
  },
  { immediate: true },
)

function locateHit(hit: KnowledgeSearchHit) {
  if (hit.id) {
    const pt = props.mapData.points.find(p => p.id === hit.id)
    if (pt) { emit('select', { type: 'point', point: pt }); return }
  }
  if (hit.entryId) { emit('select', { type: 'entry', id: hit.entryId }); return }
  if (hit.documentId) { emit('select', { type: 'document', id: hit.documentId }) }
}

function clusterOf(id: number) { return props.mapData.clusters.find(c => c.id === id) }
function pointKindText(kind: KnowledgeMapPoint['kind']) { return kind === 'entry' ? '问答片段' : kind === 'gap' ? '知识缺口' : '文档片段' }
function fileSize(bytes: number) { return bytes >= 1048576 ? `${(bytes / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB` }
</script>

<style scoped>
.inspector-panel { display: flex; flex-direction: column; width: 340px; min-width: 0; border-left: 1px solid var(--line); background: var(--surface); }
.inspector-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; height: 47px; padding: 0 14px; border-bottom: 1px solid var(--line); }
.inspector-head strong { font-size: 13px; font-weight: 600; }
.inspector-head-actions { display: flex; align-items: center; gap: 10px; }
.link-btn { padding: 0; border: 0; background: none; color: var(--primary-text); font-size: 12px; cursor: pointer; }
.link-btn:hover { text-decoration: underline; }
.inspector-body { flex: 1; max-height: 560px; overflow-y: auto; padding: 14px; }
.inspector-swap-wrap { display: flex; flex-direction: column; gap: 16px; }
.ins-section { display: flex; flex-direction: column; gap: 8px; }
.ins-section + .ins-section { padding-top: 14px; border-top: 1px solid var(--line); }
.ins-title-row { display: flex; align-items: center; gap: 8px; }
.ins-title-row h3, .ins-section h3 { margin: 0; font-size: 14px; font-weight: 600; line-height: 1.4; overflow-wrap: anywhere; }
.file-badge { flex: none; padding: 2px 6px; border-radius: 4px; background: var(--primary-light); color: var(--primary-text); font-size: 11px; font-weight: 700; }
.ins-meta { margin: 0; color: var(--muted); font-size: 12px; }
.ins-kicker { color: var(--primary-text); font-size: 12px; font-weight: 600; }
.ins-kicker.warn { color: var(--warning); }
.ins-alert { margin: 2px 0; }
.ins-box { padding: 10px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); color: var(--text-2); font-size: 13px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
.ins-stats { display: grid; grid-template-columns: repeat(3, 1fr); border: 1px solid var(--line); border-radius: var(--radius-sm); }
.ins-stats > div { padding: 6px 10px; border-left: 1px solid var(--line); }
.ins-stats > div:first-child { border-left: 0; }
.ins-stats span { display: block; color: var(--muted); font-size: 11px; }
.ins-stats strong { font-size: 14px; font-weight: 600; font-variant-numeric: tabular-nums; }
.ins-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.ins-actions .el-button + .el-button { margin-left: 0; }

.ins-subhead { display: flex; align-items: center; justify-content: space-between; font-size: 12px; }
.ins-subhead strong { font-size: 12px; color: var(--text-2); }
.chunk-list { display: flex; flex-direction: column; gap: 8px; }
.chunk-card { padding: 8px 10px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); }
.chunk-meta { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; font-size: 11px; color: var(--muted); }
.chunk-card p { margin: 0; font-size: 12px; line-height: 1.55; color: var(--text-2); white-space: pre-wrap; overflow-wrap: anywhere; }
.chunk-img { display: block; margin-top: 6px; }
.chunk-img img { display: block; max-width: 100%; max-height: 140px; border-radius: var(--radius-xs); border: 1px solid var(--line); }
.chip { padding: 1px 6px; border-radius: 4px; background: var(--surface-3); color: var(--text-2); font-size: 11px; }

.hit-list, .cluster-list { display: flex; flex-direction: column; gap: 6px; }
.hit-card, .cluster-row { width: 100%; text-align: left; padding: 8px 10px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); cursor: pointer; transition: border-color var(--dur-fast) var(--ease); }
.hit-card:hover, .cluster-row:hover { border-color: var(--primary); }
.hit-head { display: flex; align-items: center; justify-content: space-between; margin-bottom: 4px; font-size: 12px; color: var(--primary-text); }
.hit-card p { margin: 0; font-size: 12px; color: var(--text-2); display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; }
.cluster-row { display: flex; align-items: center; justify-content: space-between; font-size: 12px; color: var(--text); }
.cluster-row small { color: var(--muted); font-variant-numeric: tabular-nums; }

.todo-cards { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; }
.todo-card { padding: 8px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); text-align: left; cursor: pointer; }
.todo-card:hover { border-color: var(--primary); }
.todo-card span { display: block; color: var(--muted); font-size: 11px; }
.todo-card strong { display: block; margin-top: 2px; font-size: 16px; font-weight: 600; color: var(--text); font-variant-numeric: tabular-nums; }
.todo-card strong.warn { color: var(--warning); }
.todo-card strong.danger { color: var(--danger); }

.bound-tags { display: flex; flex-wrap: wrap; gap: 6px; }
.empty-hint { margin: 0; font-size: 12px; }

@media (max-width: 1120px) {
  .inspector-panel { width: 100%; border-left: 0; border-top: 1px solid var(--line); }
  .inspector-body { max-height: 420px; }
}
</style>
