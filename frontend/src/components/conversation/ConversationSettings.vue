<template>
  <section class="panel">
    <div class="subsection">
      <div class="subsection-head"><div><h3>显示名称</h3><p>只影响后台显示。群聊可以点右上角“同步群名”换成 QQ 当前的群名。</p></div></div>
      <el-input v-model="form.name" maxlength="60" style="max-width: 360px" />
    </div>

    <div class="subsection">
      <div class="subsection-head"><div><h3>接待客服</h3><p>从这个机器人下的 AI 客服里选一个。不选时使用“通用设置”里的默认人设。</p></div>
        <el-button link type="primary" @click="$router.push({ name: 'bot-services', params: { id: conversation.botId } })">管理 AI 客服</el-button>
      </div>
      <el-select v-model="form.serviceId" :value-on-clear="''" clearable placeholder="使用默认人设" :loading="loadingServices" style="width: 360px">
        <el-option v-for="s in services" :key="s.id" :label="s.name" :value="s.id" />
      </el-select>
    </div>

    <div class="subsection">
      <div class="subsection-head"><div><h3>什么时候回复</h3><p>{{ triggerHint }}</p></div></div>
      <el-radio-group v-model="form.triggerMode">
        <el-radio-button value="mention_only">被 @ 时回复</el-radio-button>
        <el-radio-button v-if="conversation.type === 'group'" value="smart">AI 判断后回复</el-radio-button>
        <el-radio-button value="disabled">停用</el-radio-button>
      </el-radio-group>
    </div>

    <div class="subsection">
      <div class="subsection-head"><div><h3>回复时参考几条消息</h3><p>包含当前这条；设为 1 表示只看当前问题。“通用设置”里的 token 预算仍然生效。</p></div></div>
      <el-slider v-model="form.contextLimit" :min="1" :max="50" show-input style="max-width: 480px" />
    </div>

    <div class="subsection">
      <div class="subsection-head"><div><h3>知识库与补充说明</h3><p>回答前会先查这里绑定的知识库。补充说明会接在客服的说话风格后面。</p></div></div>
      <el-form label-position="top">
        <el-form-item label="绑定知识库">
          <el-select v-model="form.knowledgeBaseIds" multiple placeholder="选择知识库" style="max-width: 480px"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select>
        </el-form-item>
        <el-form-item label="这个会话的补充说明" style="margin-bottom: 0">
          <el-input v-model="form.systemPrompt" type="textarea" :rows="5" placeholder="例如：这是售后群，只回答退换货和保修问题" />
        </el-form-item>
      </el-form>
    </div>

    <div class="panel-foot">
      <span class="note">{{ dirty ? '有未保存的修改' : '设置已是最新' }}</span>
      <el-button :disabled="!dirty" @click="reset">放弃修改</el-button>
      <el-button type="primary" :disabled="!dirty" :loading="saving" @click="save">保存接待设置</el-button>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import { botServices, type BotService } from '@/api/support'
import type { Conversation, KnowledgeBase } from '@/types'

const props = defineProps<{ conversation: Conversation }>()
const emit = defineEmits<{ saved: [] }>()

const fields = (c: Conversation) => ({
  name: c.name, serviceId: c.serviceId || '', triggerMode: c.triggerMode, contextLimit: c.contextLimit,
  knowledgeBaseIds: [...(c.knowledgeBaseIds || [])], systemPrompt: c.systemPrompt || '',
})
const form = reactive(fields(props.conversation))
const snapshot = ref(JSON.stringify(form))
const dirty = computed(() => JSON.stringify(form) !== snapshot.value)
const services = ref<BotService[]>([])
const knowledgeBases = ref<KnowledgeBase[]>([])
const loadingServices = ref(false)
const saving = ref(false)

const triggerHint = computed(() => form.triggerMode === 'smart'
  ? props.conversation.hasFullMessageEvents
    ? '每条群消息都会先由判断模型决定要不要接话。'
    : '还没收到这个群的全量消息。需要在 QQ 开放平台订阅群聊全量消息，并由群主允许机器人接收群内全部消息。'
  : form.triggerMode === 'disabled' ? '停用后机器人在这个会话里不回复任何消息。' : '只有被 @ 或私聊时才回复。')

function reset() { Object.assign(form, JSON.parse(snapshot.value)) }
watch(() => props.conversation, (value) => {
  if (dirty.value) return
  Object.assign(form, fields(value))
  snapshot.value = JSON.stringify(form)
})

onMounted(async () => {
  loadingServices.value = true
  const [kbs, list] = await Promise.allSettled([api.knowledge.list(), botServices(props.conversation.botId).list()])
  if (kbs.status === 'fulfilled') knowledgeBases.value = kbs.value
  if (list.status === 'fulfilled') services.value = list.value
  else ElMessage.error(list.reason instanceof Error ? list.reason.message : 'AI 客服加载失败')
  loadingServices.value = false
})

async function save() {
  if (saving.value) return
  saving.value = true
  try {
    await api.conversations.save(props.conversation.id, { ...props.conversation, ...form })
    snapshot.value = JSON.stringify(form)
    ElMessage.success('接待设置已保存')
    emit('saved')
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '保存失败')
  } finally { saving.value = false }
}
</script>
