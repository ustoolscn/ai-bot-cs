<template>
  <div class="eval-grid">
    <section class="table-panel">
      <div class="block-head">
        <div><h2>评测用例</h2><p>修改提示词、模型或风格前后各跑一次，对比得分和平均长度。</p></div>
        <div class="actions"><el-button @click="fromEntries">从已发布 FAQ 生成</el-button><el-button type="primary" :icon="Plus" @click="editCase()">新增</el-button></div>
      </div>
      <el-table v-loading="loading" :data="cases" max-height="420" empty-text="暂无用例">
        <el-table-column prop="question" label="问题" min-width="200" />
        <el-table-column prop="expected" label="期望要点" min-width="220" show-overflow-tooltip />
        <el-table-column label="启用" width="70"><template #default="{ row }"><el-switch v-model="row.enabled" size="small" @change="agentApi.eval.saveCase(row)" /></template></el-table-column>
        <el-table-column label="操作" width="110"><template #default="{ row }"><el-button link type="primary" @click="editCase(row)">编辑</el-button><el-button link type="danger" @click="removeCase(row)">删除</el-button></template></el-table-column>
      </el-table>
    </section>
    <section class="table-panel">
      <div class="block-head">
        <div><h2>评测运行</h2><p>试运行完整流程（不会发送 QQ 消息，转人工等操作不会真正执行）。</p></div>
      </div>
      <div class="run-form">
        <el-select v-model="runForm.chatProfileId" placeholder="对话模型" clearable><el-option v-for="m in chatModels" :key="m.id" :label="`${m.name} · ${m.model}`" :value="m.id" /></el-select>
        <el-select v-model="runForm.knowledgeBaseIds" multiple placeholder="使用的知识库" collapse-tags><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
        <el-input v-model="runForm.name" placeholder="名称（可选）" />
        <el-button type="primary" :icon="VideoPlay" @click="startRun">开始评测</el-button>
      </div>
      <el-table :data="runs" max-height="380" empty-text="暂无运行" @row-click="openRun">
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column prop="chatProfileName" label="模型" min-width="120" />
        <el-table-column label="进度" width="100"><template #default="{ row }">{{ row.completed }}/{{ row.total }}</template></el-table-column>
        <el-table-column label="平均分" width="80"><template #default="{ row }">{{ row.avgScore != null ? row.avgScore.toFixed(1) : '—' }}</template></el-table-column>
        <el-table-column label="平均字数" width="90"><template #default="{ row }">{{ row.avgLength != null ? Math.round(row.avgLength) : '—' }}</template></el-table-column>
        <el-table-column label="状态" width="90"><template #default="{ row }"><el-tag size="small" :type="row.status === 'completed' ? 'success' : row.status === 'failed' ? 'danger' : 'warning'">{{ statusText[row.status] || row.status }}</el-tag></template></el-table-column>
      </el-table>
    </section>

    <el-dialog v-model="caseDialog" title="评测用例" width="560px">
      <el-form label-position="top">
        <el-form-item label="问题"><el-input v-model="editingCase.question" type="textarea" :rows="2" /></el-form-item>
        <el-form-item label="期望要点"><el-input v-model="editingCase.expected" type="textarea" :rows="4" /></el-form-item>
        <el-checkbox v-model="editingCase.enabled">启用</el-checkbox>
      </el-form>
      <template #footer><el-button @click="caseDialog = false">取消</el-button><el-button type="primary" @click="saveCase">保存</el-button></template>
    </el-dialog>

    <el-drawer v-model="resultDrawer" :title="`评测结果 · ${currentRun?.name || ''}`" size="720px">
      <article v-for="r in results" :key="r.id" class="result-item">
        <div class="result-head"><strong>{{ r.question }}</strong><el-tag :type="(r.score ?? 0) >= 7 ? 'success' : (r.score ?? 0) >= 4 ? 'warning' : 'danger'">{{ r.score != null ? r.score : '—' }} 分</el-tag></div>
        <div class="content-box">{{ r.answer || r.error }}</div>
        <small class="muted">{{ r.verdict }} · {{ r.latencyMs }}ms<template v-if="r.toolsUsed?.length"> · 工具：{{ r.toolsUsed.join('、') }}</template></small>
      </article>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, reactive, ref } from 'vue'
import { Plus, VideoPlay } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { agentApi, type EvalCase, type EvalResult, type EvalRun } from '@/api/agent'
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
.eval-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; align-items: start; }
.block-head { display: flex; justify-content: space-between; align-items: center; padding: 16px 18px; border-bottom: 1px solid var(--line); gap: 12px; }
.block-head h2 { font-size: 15px; margin: 0 0 4px; }
.block-head p { margin: 0; color: var(--muted); font-size: 12px; }
.actions { display: flex; gap: 8px; }
.run-form { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; padding: 14px 18px; }
.result-item { margin-bottom: 18px; }
.result-head { display: flex; justify-content: space-between; gap: 10px; margin-bottom: 8px; }
.muted { color: var(--muted); font-size: 12px; }
@media (max-width: 1100px) { .eval-grid { grid-template-columns: 1fr; } }
</style>
