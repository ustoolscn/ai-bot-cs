<template>
  <el-dialog :model-value="modelValue" :title="`拉黑 ${displayName || userId}`" width="460px" append-to-body @update:model-value="emit('update:modelValue', $event)" @open="reset">
    <blockquote v-if="quote" class="block-quote">{{ quote }}</blockquote>
    <p class="muted" style="margin-bottom: 16px">拉黑后，机器人不再回复他，他的发言也不会进入上下文、记忆和学习。</p>
    <el-form label-position="top">
      <el-form-item v-if="isGroup" label="拉黑范围">
        <el-radio-group v-model="form.scope">
          <el-radio value="group">只在{{ conversationName ? `「${conversationName}」` : '本群' }}</el-radio>
          <el-radio value="bot">{{ botName || '这个机器人' }} 所在的所有群</el-radio>
        </el-radio-group>
      </el-form-item>
      <el-form-item label="原因（选填）"><el-input v-model="form.reason" type="textarea" :rows="2" maxlength="1000" placeholder="例如：发广告、骚扰其他群友" /></el-form-item>
      <div v-if="isGroup" class="setting-row" style="padding: 0">
        <div><strong>发现时自动踢出群</strong><span>需要群主给机器人开启“移除群成员”权限，没有权限时会在黑名单里显示失败</span></div>
        <el-switch v-model="form.autoKick" />
      </div>
    </el-form>
    <template #footer><el-button @click="emit('update:modelValue', false)">取消</el-button><el-button type="danger" :loading="saving" @click="confirm">确认拉黑</el-button></template>
  </el-dialog>
</template>

<script setup lang="ts">
import { reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { agentApi } from '@/api/agent'

const props = withDefaults(defineProps<{
  modelValue: boolean
  botId: string
  botName?: string
  conversationId: string
  conversationName?: string
  isGroup?: boolean
  userId: string
  displayName?: string
  /** 触发拉黑的那条发言，弹窗里引用出来方便核对 */
  quote?: string
}>(), { isGroup: true })
const emit = defineEmits<{ 'update:modelValue': [boolean]; blocked: [] }>()
const saving = ref(false)
const form = reactive({ scope: 'group' as 'group' | 'bot', reason: '', autoKick: true })

function reset() { Object.assign(form, { scope: 'group', reason: '', autoKick: props.isGroup }) }
async function confirm() {
  if (!props.botId || !props.userId) { ElMessage.error('找不到这个人所属的机器人，请刷新后再试'); return }
  saving.value = true
  try {
    const res = await agentApi.blacklist.add({
      botId: props.botId,
      conversationId: !props.isGroup || form.scope === 'group' ? props.conversationId : undefined,
      userId: props.userId,
      reason: form.reason.trim() || undefined,
      autoKick: props.isGroup && form.autoKick,
    })
    ElMessage.success(res.autoKick ? '已拉黑，正在尝试踢出群' : '已拉黑')
    emit('update:modelValue', false)
    emit('blocked')
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '拉黑失败') } finally { saving.value = false }
}
</script>

<style scoped>
.block-quote { margin: 0 0 12px; padding: 8px 12px; border-left: 3px solid var(--line-strong); border-radius: 0 var(--radius-xs) var(--radius-xs) 0; background: var(--surface-2); color: var(--text); font-size: 13px; line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; max-height: 120px; overflow: auto; }
</style>
