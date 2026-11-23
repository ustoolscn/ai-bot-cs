<template>
  <div class="eval-grid">
    <section class="table-panel">
      <div class="block-head">
        <div><h2>跑一次评测</h2><p>用下面的用例完整试跑一遍（不会真的发到 QQ，转人工也不会真正执行）。改提示词或模型前后各跑一次，对比得分和字数。</p></div>
      </div>
      <div class="run-form">
        <el-select v-model="runForm.chatProfileId" placeholder="对话模型（默认用当前设置）" clearable><el-option v-for="m in chatModels" :key="m.id" :label="`${m.name} · ${m.model}`" :value="m.id" /></el-select>
        <el-select v-model="runForm.knowledgeBaseIds" multiple placeholder="使用的知识库" collapse-tags><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
        <el-input v-model="runForm.name" placeholder="给这次评测起个名字，例如“缩短回复后”" />
        <el-button type="primary" :icon="VideoPlay" :disabled="!enabledCount" @click="startRun">开始评测（{{ enabledCount }} 条用例）</el-button>
      </div>
      <el-table class="clickable-rows" :data="runs" max-height="440" empty-text="还没有跑过评测" @row-click="openRun">
        <el-table-column label="名称" min-width="150"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.name || '未命名' }}</strong><span>{{ formatTime(row.createdAt) }} · {{ row.chatProfileName }}</span></div></template></el-table-column>
        <el-table-column label="平均分（满分 10）" width="170"><template #default="{ row, $index }"><span class="score-bar"><i :style="{ width: `${(row.avgScore ?? 0) * 10}%` }" /></span><strong>{{ row.avgScore != null ? row.avgScore.toFixed(1) : '—' }}</strong> <small v-if="delta($index, 'avgScore')" :class="delta($index, 'avgScore')!.startsWith('+') ? 'trend-good' : 'trend-bad'">{{ delta($index, 'avgScore') }}</small></template></el-table-column>
        <el-table-column label="平均字数" width="110"><template #default="{ row, $index }">{{ row.avgLength != null ? Math.round(row.avgLength) : '—' }} <small v-if="delta($index, 'avgLength')" class="muted">{{ delta($index, 'avgLength') }}</small></template></el-table-column>
        <el-table-column label="状态" width="110"><template #default="{ row }"><ProgressRing v-if="row.status === 'running' || row.status === 'pending'" :percent="row.total ? row.completed / row.total * 100 : 0" :size="34" :stroke="4" tone="butter"><small class="run-count">{{ row.completed }}</small></ProgressRing><el-tag v-else size="small" :type="row.status === 'completed' ? 'success' : 'danger'">{{ statusText[row.status] || row.status }}</el-tag></template></el-table-column>
      </el-table>
    </section>
    <section class="table-panel">
      <div class="block-head">
        <div><h2>评测用例</h2><p>每条用例是一个问题和期望答到的要点。</p></div>
        <div class="actions"><el-button @click="fromEntries">从已发布 FAQ 生成</el-button><el-button type="primary" :icon="Plus" @click="editCase()">新增用例</el-button></div>
      </div>
      <el-table v-loading="loading" :data="cases" max-height="520" empty-text="还没有用例。可以点“从已发布 FAQ 生成”快速导入。">
        <el-table-column label="问题 / 期望要点" min-width="260"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.question }}</strong><span>{{ row.expected }}</span></div></template></el-table-column>
        <el-table-column label="启用" width="70"><template #default="{ row }"><el-switch v-model="row.enabled" size="small" @change="agentApi.eval.saveCase(row)" /></template></el-table-column>
        <el-table-column label="操作" width="110"><template #default="{ row }"><el-button link type="primary" @click="editCase(row)">编辑</el-button><el-button link type="danger" @click="removeCase(row)">删除</el-button></template></el-table-column>
      </el-table>
    </section>

    <el-dialog v-model="caseDialog" :title="editingCase.id ? '编辑用例' : '新增用例'" width="560px">
      <el-form label-position="top">
        <el-form-item label="用户会怎么问"><el-input v-model="editingCase.question" type="textarea" :rows="2" /></el-form-item>
        <el-form-item label="期望答到的要点"><el-input v-model="editingCase.expected" type="textarea" :rows="4" placeholder="用分号分隔多个要点" /></el-form-item>
        <el-checkbox v-model="editingCase.enabled">参与评测</el-checkbox>
      </el-form>
      <template #footer><el-button @click="caseDialog = false">取消</el-button><el-button type="primary" @click="saveCase">保存</el-button></template>
    </el-dialog>

    <el-drawer v-model="resultDrawer" :title="`评测结果 · ${currentRun?.name || ''}`" size="720px">
      <article v-for="r in results" :key="r.id" class="result-item">
        <div class="result-head"><strong>{{ r.question }}</strong><el-tag :type="(r.score ?? 0) >= 7 ? 'success' : (r.score ?? 0) >= 4 ? 'warning' : 'danger'">{{ r.score != null ? r.score : '—' }} 分</el-tag></div>
        <div class="content-box">{{ r.answer || r.error }}</div>
        <small class="muted">{{ r.verdict }} · {{ r.latencyMs }}ms<template v-if="r.toolsUsed?.length"> · 工具：{{ r.toolsUsed.join('、') }}</template></small>
      </article>
      <EmptyState v-if="!results.length" title="这次评测还没有结果" />
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Plus, VideoPlay } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { agentApi, formatTime, type EvalCase, type EvalResult, type EvalRun } from '@/api/agent'
import EmptyState from '@/components/EmptyState.vue'
import ProgressRing from '@/components/fx/ProgressRing.vue'
import type { KnowledgeBase, ModelProfile } from '@/types'

defineProps<{ chatModels: ModelProfile[]; knowledgeBases: KnowledgeBase[] }>()
const statusText: Record<string, string> = { pending: '排队', running: '运行中', completed: '完成', failed: '失败' }
const cases = ref<EvalCase[]>([])
const runs = ref<EvalRun[]>([])
const loading = ref(false)
const caseDialog = ref(false)
const editingCase = reactive<EvalCase>({ question: '', expected: '', enabled: true })
const runForm = reactive({ name: '', chatProfileId: '', knowledgeBaseIds: [] as string[] })
const resultDrawer = ref(false)
const results = ref<EvalResult[]>([])
const currentRun = ref<EvalRun | null>(null)
let timer: ReturnType<typeof setInterval> | undefined
const enabledCount = computed(() => cases.value.filter(c => c.enabled).length)
// 与下一行（更早的一次）已完成评测对比
function delta(index: number, key: 'avgScore' | 'avgLength') {
  const current = runs.value[index]?.[key]
  const previous = runs.value.slice(index + 1).find(r => r.status === 'completed' && r[key] != null)?.[key]
  if (current == null || previous == null) return ''
  const diff = key === 'avgScore' ? Number((current - previous).toFixed(1)) : Math.round(current - previous)
  return diff === 0 ? '' : `${diff > 0 ? '+' : ''}${diff}`
}

async function load() {
  loading.value = true
  try { [cases.value, runs.value] = await Promise.all([agentApi.eval.cases(), agentApi.eval.runs()]) } finally { loading.value = false }
}
function editCase(row?: EvalCase) { Object.assign(editingCase, row ? { ...row } : { id: undefined, question: '', expected: '', enabled: true }); caseDialog.value = true }
async function saveCase() { await agentApi.eval.saveCase({ ...editingCase }); caseDialog.value = false; await load() }
async function removeCase(row: EvalCase) { await ElMessageBox.confirm('删除该用例？', '确认', { type: 'warning' }); await agentApi.eval.removeCase(row.id!); await load() }
async function fromEntries() { const r = await agentApi.eval.fromEntries(); ElMessage.success(`新增 ${r.created} 条用例`); await load() }
async function startRun() {
  try { await agentApi.eval.createRun({ ...runForm }); ElMessage.success('已开始评测'); await load() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '创建失败') }
}
async function openRun(row: EvalRun) { currentRun.value = row; results.value = await agentApi.eval.results(row.id); resultDrawer.value = true }
onMounted(() => {
  load()
  timer = setInterval(async () => { if (runs.value.some(r => r.status === 'pending' || r.status === 'running')) runs.value = await agentApi.eval.runs() }, 4000)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<style scoped>
.score-bar { display: inline-block; width: 64px; height: 8px; border-radius: 99px; background: var(--surface-3); overflow: hidden; vertical-align: middle; margin-right: 8px; }
.score-bar i { display: block; height: 100%; border-radius: 99px; background: linear-gradient(90deg, #ffc27a, #f2735a); animation: grow 1s var(--ease-bounce) both; transform-origin: left; }
@keyframes grow { from { transform: scaleX(0); } }
.run-count { font: 700 11px/1 var(--font-round); }
.run-form { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; padding: 16px 20px; border-bottom: 1px solid var(--line); }
.result-item { margin-bottom: 18px; }
.result-head { display: flex; justify-content: space-between; gap: 10px; margin-bottom: 8px; }
.result-item small { display: block; margin-top: 6px; font-size: 13px; }
@media (max-width: 620px) { .run-form { grid-template-columns: 1fr; } }
</style>
