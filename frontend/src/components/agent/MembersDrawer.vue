<template>
  <el-drawer :model-value="modelValue" :title="`成员 · ${conversationName}`" size="620px" @update:model-value="emit('update:modelValue', $event)">
    <el-alert type="info" :closable="false" show-icon title="标记为“官方客服”后：他们在群里的发言会进入机器人上下文，回答会被挖掘成候选知识。需要群聊全量消息事件才能收到未 @ 机器人的发言。" />
    <el-input v-model="filter" class="gap" placeholder="搜索昵称或 ID" clearable />
    <el-table v-loading="loading" :data="filtered" max-height="520">
      <el-table-column label="成员" min-width="200"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.displayName || '未知昵称' }}</strong><span>{{ row.userId }}</span></div></template></el-table-column>
      <el-table-column prop="messageCount" label="消息" width="70" />
      <el-table-column label="记忆" width="70"><template #default="{ row }"><el-button v-if="Number(row.memoryCount) > 0" link type="primary" @click="showMemories(row)">{{ row.memoryCount }}</el-button><span v-else>0</span></template></el-table-column>
      <el-table-column label="官方客服" width="90"><template #default="{ row }"><el-switch v-model="row.isStaff" @change="toggle(row)" /></template></el-table-column>
    </el-table>
    <el-dialog v-model="memDialog" :title="`记忆 · ${memUser}`" width="520px" append-to-body>
      <div v-for="m in mems" :key="m.id" class="content-box gap">{{ m.content }}<br /><small class="muted">{{ formatTime(m.updatedAt) }}</small></div>
    </el-dialog>
  </el-drawer>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { agentApi, formatTime, type Member, type Memory } from '@/api/agent'

const props = defineProps<{ modelValue: boolean; conversationId: string; conversationName: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
const members = ref<Member[]>([])
const loading = ref(false)
const filter = ref('')
const memDialog = ref(false)
const memUser = ref('')
const mems = ref<Memory[]>([])
const filtered = computed(() => members.value.filter(m => !filter.value || `${m.displayName}${m.userId}`.includes(filter.value)))
watch(() => [props.modelValue, props.conversationId], async () => {
  if (!props.modelValue || !props.conversationId) return
  loading.value = true
  try { members.value = await agentApi.members.list(props.conversationId) } catch (e) { ElMessage.error(e instanceof Error ? e.message : '加载失败') } finally { loading.value = false }
}, { immediate: true })
async function toggle(row: Member) {
  try { await agentApi.members.setStaff(props.conversationId, row.userId, row.isStaff); ElMessage.success(row.isStaff ? '已标记为官方客服' : '已取消') } catch (e) { row.isStaff = !row.isStaff; ElMessage.error(e instanceof Error ? e.message : '保存失败') }
}
async function showMemories(row: Member) { memUser.value = row.displayName || row.userId; mems.value = await agentApi.memories.list({ userId: row.userId }); memDialog.value = true }
</script>

<style scoped>
.gap { margin: 12px 0; }
.muted { color: var(--muted); font-size: 11px; }
</style>
