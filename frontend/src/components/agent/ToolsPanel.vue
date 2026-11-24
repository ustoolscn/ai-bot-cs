<template>
  <section class="table-panel block">
    <div class="block-head">
      <div><h2>工具</h2><p>内置工具随条件自动启用；自定义 HTTP 工具可对接你们自己的接口（如查订单、查余额）。</p></div>
      <el-button type="primary" :icon="Plus" @click="edit()">新建 HTTP 工具</el-button>
    </div>
    <el-table v-loading="loading" :data="builtin" empty-text="—">
      <el-table-column prop="name" label="内置工具" width="200" />
      <el-table-column prop="description" label="说明" min-width="320" />
      <el-table-column label="启用条件" width="200"><template #default="{ row }">{{ row.requiresLiveData ? '需启用网站数据' : row.requiresKnowledgeBase ? '会话绑定了知识库' : '始终可用' }}</template></el-table-column>
    </el-table>
    <el-table :data="custom" empty-text="暂无自定义工具">
      <el-table-column prop="name" label="自定义工具" width="200" />
      <el-table-column prop="description" label="说明" min-width="240" />
      <el-table-column label="接口" min-width="240"><template #default="{ row }"><code>{{ row.method }} {{ row.url }}</code></template></el-table-column>
      <el-table-column label="启用" width="70"><template #default="{ row }"><el-tag :type="row.enabled ? 'success' : 'info'" size="small">{{ row.enabled ? '是' : '否' }}</el-tag></template></el-table-column>
      <el-table-column label="操作" width="160" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="edit(row)">编辑</el-button><el-button link @click="openTest(row)">测试</el-button><el-button link type="danger" @click="remove(row)">删除</el-button></template></el-table-column>
    </el-table>

    <el-dialog v-model="dialog" :title="editing.id ? '编辑工具' : '新建 HTTP 工具'" width="680px">
      <el-form label-position="top">
        <el-form-item label="工具名（英文，模型调用时使用）"><el-input v-model="editing.name" placeholder="query_order" /></el-form-item>
        <el-form-item label="说明（告诉 AI 什么时候用、返回什么）"><el-input v-model="editing.description" type="textarea" :rows="2" /></el-form-item>
        <div class="form-row">
          <el-form-item label="方法"><el-select v-model="editing.method" style="width:110px"><el-option label="GET" value="GET" /><el-option label="POST" value="POST" /></el-select></el-form-item>
          <el-form-item label="接口地址" class="grow"><el-input v-model="editing.url" placeholder="https://api.example.com/orders/{order_id}" /></el-form-item>
        </div>
        <small class="form-tip">地址里的 {参数名} 会被替换；GET 其余参数拼到查询串，POST 以 JSON 发送。响应最多读取 4KB。</small>
        <el-form-item label="请求头（每行 名称: 值，加密保存）">
          <el-input v-model="headersText" type="textarea" :rows="3" :placeholder="editing.headerNames?.length ? `已保存：${editing.headerNames.join(', ')}（留空不修改）` : 'Authorization: Bearer xxx'" />
        </el-form-item>
        <el-form-item label="参数定义（JSON Schema）"><el-input v-model="paramsText" type="textarea" :rows="7" class="mono" /></el-form-item>
        <div class="form-row">
          <el-form-item label="超时（秒）"><el-input-number v-model="editing.timeoutSeconds" :min="1" :max="30" /></el-form-item>
          <el-form-item label=" "><el-checkbox v-model="editing.enabled">启用</el-checkbox></el-form-item>
        </div>
      </el-form>
      <template #footer><el-button @click="dialog = false">取消</el-button><el-button type="primary" @click="save">保存工具</el-button></template>
    </el-dialog>

    <el-dialog v-model="testDialog" title="测试工具" width="600px">
      <el-input v-model="testArgs" type="textarea" :rows="4" class="mono" />
      <div v-if="testResult" class="content-box result">{{ testResult }}</div>
      <template #footer><el-button type="primary" :loading="testing" @click="runTest">调用</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { onMounted, reactive, ref } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { agentApi, type BuiltinTool, type CustomTool } from '@/api/agent'

const builtin = ref<BuiltinTool[]>([])
const custom = ref<CustomTool[]>([])
const loading = ref(false)
const dialog = ref(false)
const editing = reactive<CustomTool>({ name: '', description: '', enabled: true, method: 'GET', url: '', timeoutSeconds: 10, parameters: {} })
const headersText = ref('')
const paramsText = ref('')
const testDialog = ref(false)
const testArgs = ref('{}')
const testResult = ref('')
const testing = ref(false)
let testID = ''

const defaultParams = JSON.stringify({ type: 'object', properties: { order_id: { type: 'string', description: '订单号' } }, required: ['order_id'] }, null, 2)

async function load() {
  loading.value = true
  try { const t = await agentApi.tools.list(); builtin.value = t.builtin; custom.value = t.custom } finally { loading.value = false }
}
function edit(row?: CustomTool) {
  Object.assign(editing, row ? { ...row } : { id: undefined, name: '', description: '', enabled: true, method: 'GET', url: '', timeoutSeconds: 10, parameters: {}, headerNames: [] })
  headersText.value = ''
  paramsText.value = row ? JSON.stringify(row.parameters, null, 2) : defaultParams
  dialog.value = true
}
function parseHeaders(): Record<string, string> | null {
  if (!headersText.value.trim()) return editing.id ? null : {}
  const out: Record<string, string> = {}
  for (const line of headersText.value.split('\n')) {
    const i = line.indexOf(':')
    if (i > 0) out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return out
}
async function save() {
  let params: Record<string, unknown>
  try { params = JSON.parse(paramsText.value || '{"type":"object","properties":{}}') } catch { ElMessage.error('参数定义不是合法 JSON'); return }
  try {
    await agentApi.tools.save({ ...editing, parameters: params }, parseHeaders())
    dialog.value = false
    ElMessage.success('已保存')
    await load()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '保存失败') }
}
async function remove(row: CustomTool) {
  await ElMessageBox.confirm(`删除工具“${row.name}”？`, '确认', { type: 'warning' })
  await agentApi.tools.remove(row.id!)
  await load()
}
function openTest(row: CustomTool) { testID = row.id!; testArgs.value = '{}'; testResult.value = ''; testDialog.value = true }
async function runTest() {
  let args: Record<string, unknown>
  try { args = JSON.parse(testArgs.value || '{}') } catch { ElMessage.error('参数不是合法 JSON'); return }
  testing.value = true
  try { testResult.value = (await agentApi.tools.test(testID, args)).result } catch (e) { testResult.value = e instanceof Error ? e.message : '调用失败' } finally { testing.value = false }
}
onMounted(load)
useAdminEvents(['tool_definitions'], load)
</script>

<style scoped>
.block { margin-bottom: 16px; }
.form-row { display: flex; gap: 12px; align-items: flex-start; }
.grow { flex: 1; }
.mono :deep(textarea) { font-family: ui-monospace, Menlo, monospace; font-size: 12px; }
.result { margin-top: 12px; max-height: 300px; overflow: auto; }
code { font-size: 13px; word-break: break-all; }
</style>
