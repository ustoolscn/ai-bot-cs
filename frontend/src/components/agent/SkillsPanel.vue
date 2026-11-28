<template>
  <section class="table-panel block">
    <div class="block-head">
      <div><h2>技能</h2><p>告诉 AI 遇到某类问题时该怎么处理。启用的技能会加入提示词。</p></div>
      <el-button type="primary" :icon="Plus" @click="edit()">新建技能</el-button>
    </div>
    <el-table v-loading="loading" :data="skills" empty-text="暂无技能">
      <el-table-column prop="name" label="技能" min-width="140" />
      <el-table-column prop="description" label="何时使用" min-width="260" />
      <el-table-column label="用到的工具" min-width="180"><template #default="{ row }"><el-tag v-for="t in row.tools" :key="t" size="small" effect="plain" class="tag">{{ t }}</el-tag></template></el-table-column>
      <el-table-column label="启用" width="80"><template #default="{ row }"><el-switch v-model="row.enabled" @change="quickSave(row)" /></template></el-table-column>
      <el-table-column label="操作" width="112" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="edit(row)">编辑</el-button><el-button link type="danger" @click="remove(row)">删除</el-button></template></el-table-column>
    </el-table>
    <el-dialog v-model="dialog" :title="editing.id ? '编辑技能' : '新建技能'" width="660px">
      <el-form label-position="top">
        <div class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>基础信息与触发条件</strong><span>定义技能名称与启用状态</span></div>
            <div class="form-grid">
              <el-form-item label="名称"><el-input v-model="editing.name" /></el-form-item>
              <el-form-item label="排序"><el-input-number v-model="editing.sortOrder" /></el-form-item>
            </div>
            <el-form-item label="何时使用"><el-input v-model="editing.description" type="textarea" :rows="2" placeholder="例如：用户询问模型价格、倍率时" /></el-form-item>
            <el-form-item><el-checkbox v-model="editing.enabled">启用该技能</el-checkbox></el-form-item>
          </div>
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>处理说明与关联工具</strong><span>启用的技能处理说明将写入系统提示词</span></div>
            <el-form-item label="处理说明"><el-input v-model="editing.instructions" type="textarea" :rows="6" placeholder="一步步写清楚应该怎么做、怎么说" /></el-form-item>
            <el-form-item label="用到的工具（说明用途，工具本身在下方统一启用）">
              <el-select v-model="editing.tools" multiple filterable allow-create style="width: 100%"><el-option v-for="t in toolNames" :key="t" :label="t" :value="t" /></el-select>
            </el-form-item>
          </div>
        </div>
      </el-form>
      <template #footer><el-button @click="dialog = false">取消</el-button><el-button type="primary" @click="save">保存技能</el-button></template>
    </el-dialog>
  </section>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { onMounted, reactive, ref } from 'vue'
import { Plus } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { agentApi, type Skill } from '@/api/agent'

const skills = ref<Skill[]>([])
const toolNames = ref<string[]>([])
const loading = ref(false)
const dialog = ref(false)
const editing = reactive<Skill>({ name: '', description: '', instructions: '', tools: [], enabled: true, sortOrder: 0 })

async function load() {
  loading.value = true
  try {
    const [s, t] = await Promise.all([agentApi.skills.list(), agentApi.tools.list()])
    skills.value = s
    toolNames.value = [...t.builtin.map(x => x.name), ...t.custom.map(x => x.name)]
  } finally { loading.value = false }
}
function edit(row?: Skill) {
  Object.assign(editing, row ? { ...row, tools: [...row.tools] } : { id: undefined, name: '', description: '', instructions: '', tools: [], enabled: true, sortOrder: (skills.value.length + 1) * 10 })
  dialog.value = true
}
async function save() {
  try { await agentApi.skills.save({ ...editing }); dialog.value = false; ElMessage.success('已保存'); await load() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '保存失败') }
}
async function quickSave(row: Skill) {
  try { await agentApi.skills.save(row) } catch (e) { ElMessage.error(e instanceof Error ? e.message : '保存失败'); await load() }
}
async function remove(row: Skill) {
  await ElMessageBox.confirm(`删除技能“${row.name}”？`, '确认', { type: 'warning' })
  await agentApi.skills.remove(row.id!)
  await load()
}
onMounted(load)
useAdminEvents(['skills'], load)
</script>

<style scoped>
.block { margin-bottom: 16px; }
.tag { margin: 2px 4px 2px 0; }
</style>
