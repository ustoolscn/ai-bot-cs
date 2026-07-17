<template>
  <div class="content-page">
    <PageHeader title="消息记录" description="仅展示实际触发机器人处理的消息，以及对应的模型、上下文和投递链路。">
      <el-button :icon="Download" @click="exportRows">导出记录</el-button>
      <el-button :icon="Refresh" @click="load">刷新</el-button>
    </PageHeader>
    <div class="toolbar">
      <el-input v-model="search" placeholder="搜索问题、回复、发送者或消息 ID" :prefix-icon="Search" clearable />
      <el-select v-model="status"><el-option label="全部状态" value="all" /><el-option label="处理成功" value="success" /><el-option label="处理失败" value="failed" /></el-select>
      <el-date-picker v-model="date" type="date" placeholder="选择日期" />
    </div>
    <el-alert v-if="loadError" class="page-error-alert" type="error" :closable="false" show-icon :title="loadError" />
    <section class="table-panel">
      <el-table v-loading="loading" :data="filtered" empty-text="暂无触发机器人处理的消息">
        <el-table-column prop="time" label="时间" width="170" />
        <el-table-column label="消息" min-width="340"><template #default="{ row }"><div class="message-preview"><strong>{{ row.question }}</strong><span>{{ row.attachments?.length ? `包含 ${row.attachments.length} 张图片 · ` : '' }}{{ row.answer || '未生成回复' }}</span></div></template></el-table-column>
        <el-table-column label="机器人 / 会话" min-width="180"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.botName }}</strong><span>{{ row.conversationName }}</span></div></template></el-table-column>
        <el-table-column prop="sender" label="发送者" width="100" />
        <el-table-column label="模型" width="160"><template #default="{ row }"><el-button class="model-detail-link" link type="primary" @click="open(row)">{{ row.model || '查看详情' }}</el-button><small class="table-sub">{{ row.tokens }} tokens</small></template></el-table-column>
        <el-table-column label="知识命中" width="100"><template #default="{ row }">{{ row.knowledgeHits }} 条</template></el-table-column>
        <el-table-column label="耗时" width="100"><template #default="{ row }">{{ formatDuration(row.latency) }}</template></el-table-column>
        <el-table-column label="状态" width="100"><template #default="{ row }"><StatusBadge :status="row.status" /></template></el-table-column>
        <el-table-column label="操作" width="90" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="open(row)">详情</el-button></template></el-table-column>
      </el-table>
    </section>

    <el-drawer v-model="drawer" title="消息处理详情" size="680px">
      <div v-if="selected" v-loading="detailLoading">
        <div class="trace-summary">
          <StatusBadge :status="selected.status" />
          <div class="trace-identity">
            <small>消息记录 ID</small>
            <el-tooltip :content="selected.traceId" placement="bottom"><strong>{{ selected.traceId }}</strong></el-tooltip>
          </div>
          <span>{{ selected.time }}</span>
        </div>

        <div class="detail-section"><h3>用户消息</h3><div class="content-box">{{ selected.question }}</div><div v-if="selected.attachments?.length" class="message-image-grid"><el-image v-for="(image,index) in selected.attachments" :key="image.previewUrl || index" :src="image.previewUrl" :preview-src-list="previewUrls(selected.attachments)" fit="cover" loading="lazy" /></div></div>

        <div class="detail-section">
          <h3>本次模型完整上下文 <small>（{{ selected.contextMessages?.length || 0 }} 条）</small></h3>
          <div v-if="selected.contextMessages?.length" class="context-message-list">
            <article v-for="(message, index) in selected.contextMessages" :key="`${index}-${message.role}`" class="context-message-item">
              <span :class="`role-${message.role}`">{{ roleText(message.role) }}</span>
              <div><pre>{{ message.content }}</pre><div v-if="message.parts?.length" class="message-image-grid compact"><el-image v-for="(image,imageIndex) in message.parts" :key="image.previewUrl || imageIndex" :src="image.previewUrl" :preview-src-list="previewUrls(message.parts)" fit="cover" loading="lazy" /></div></div>
            </article>
          </div>
          <el-empty v-else :image-size="54" description="历史记录没有上下文快照；新版本处理的消息会在这里完整展示" />
        </div>

        <div class="trace-flow">
          <div v-for="(step,index) in traceSteps" :key="step.title" class="trace-step" :class="step.status"><span>{{ index + 1 }}</span><div><strong>{{ step.title }}</strong><p>{{ step.description }}</p></div><em>{{ step.duration }}</em></div>
        </div>

        <div class="detail-section"><h3>机器人回复</h3><div class="content-box">{{ selected.answer || (selected.answerAttachments?.length ? '图片回复' : '处理尚未完成或失败，未生成回复。') }}</div><div v-if="selected.answerAttachments?.length" class="message-image-grid"><el-image v-for="(image,index) in selected.answerAttachments" :key="image.previewUrl || index" :src="image.previewUrl" :preview-src-list="previewUrls(selected.answerAttachments)" fit="cover" loading="lazy" /></div></div>

        <div v-if="selected.retrievedChunks?.length" class="detail-section"><h3>知识召回详情</h3><div class="retrieved-chunk-list"><article v-for="(chunk,index) in selected.retrievedChunks" :key="chunk.id || index"><strong>片段 {{ index + 1 }}<span v-if="chunk.score !== undefined"> · 相似度 {{ (chunk.score * 100).toFixed(1) }}%</span></strong><p>{{ chunk.content }}</p></article></div></div>

        <div class="detail-section"><h3>请求信息</h3><dl class="detail-list"><div><dt>事件类型</dt><dd>{{ selected.eventType }}</dd></div><div><dt>模型</dt><dd>{{ selected.model || '未调用' }}</dd></div><div><dt>输入 / 输出 Token</dt><dd>{{ selected.inputTokens }} / {{ selected.outputTokens }}</dd></div><div><dt>知识命中</dt><dd>{{ selected.knowledgeHits }} 条</dd></div><div><dt>QQ 消息 ID</dt><dd class="detail-id"><el-tooltip :content="selected.platformMessageId || '—'"><span>{{ selected.platformMessageId || '—' }}</span></el-tooltip></dd></div><div><dt>投递状态</dt><dd><StatusBadge :status="selected.deliveryStatus" /></dd></div><div v-if="selected.deliveryError"><dt>投递失败原因</dt><dd class="delivery-error">{{ selected.deliveryError }}</dd></div></dl></div>
      </div>
    </el-drawer>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Download, Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { api } from '@/api'
import PageHeader from '@/components/PageHeader.vue'
import StatusBadge from '@/components/StatusBadge.vue'
import type { MessageAttachment, MessageRecord, Status } from '@/types'
import { formatDuration } from '@/utils/format'

const messages = ref<MessageRecord[]>([])
const search = ref('')
const status = ref('all')
const date = ref('')
const drawer = ref(false)
const detailLoading = ref(false)
const selected = ref<MessageRecord>()
const loading = ref(false)
const loadError = ref('')
const filtered = computed(() => messages.value.filter(m => (!search.value || `${m.question}${m.answer}${m.sender}${m.traceId}`.includes(search.value)) && (status.value === 'all' || m.status === status.value)))
const traceSteps = computed(() => {
  if (!selected.value) return []
  const message = selected.value
  const pending = message.status === 'pending' || message.status === 'processing'
  const failed = message.status === 'failed'
  const stageStatus = (completed: boolean): Status => failed ? 'failed' : completed ? 'success' : pending ? 'pending' : 'warning'
  return [
    { title: 'QQ 事件接收', description: message.eventType, duration: '—', status: 'success' as Status },
    { title: '上下文构建', description: `实际发送给模型 ${message.contextMessages?.length || 0} 条消息`, duration: duration(message.contextLatency), status: stageStatus(Boolean(message.contextMessages?.length) || message.contextLatency > 0) },
    { title: '知识检索', description: `召回 ${message.knowledgeHits} 条知识片段`, duration: duration(message.retrievalLatency), status: stageStatus(message.retrievalLatency > 0 || message.knowledgeHits > 0) },
    { title: '模型生成', description: message.model || '尚未调用', duration: duration(message.modelLatency), status: stageStatus(Boolean(message.answer) || Boolean(message.answerAttachments?.length) || message.modelLatency > 0) },
    { title: 'QQ 消息投递', description: message.deliveryStatus === 'success' ? '发送成功' : message.deliveryStatus === 'failed' ? '发送失败' : '等待投递', duration: duration(message.deliveryLatency), status: message.deliveryStatus },
  ]
})

onMounted(load)
async function load() {
  loading.value = true
  loadError.value = ''
  try {
    const result = await api.messages.list()
    messages.value = Array.isArray(result) ? result : []
  } catch (error) {
    messages.value = []
    loadError.value = error instanceof Error ? `消息记录加载失败：${error.message}` : '消息记录加载失败，请稍后重试。'
  } finally { loading.value = false }
}
async function open(row: MessageRecord) {
  selected.value = row
  drawer.value = true
  detailLoading.value = true
  try { selected.value = await api.messages.detail(row.id) }
  catch (error) { ElMessage.error(error instanceof Error ? error.message : '读取消息详情失败') }
  finally { detailLoading.value = false }
}
function duration(value: number) { return value > 0 ? formatDuration(value) : '—' }
function previewUrls(parts?: MessageAttachment[]) { return (parts || []).flatMap(part => part.previewUrl ? [part.previewUrl] : []) }
function roleText(role: string) { return ({ system: '系统', developer: '开发者', user: '用户', assistant: '助手' } as Record<string, string>)[role] || role }
function exportRows() { const csv = ['时间,机器人,会话,问题,状态,Token', ...filtered.value.map(m => `"${m.time}","${m.botName}","${m.conversationName}","${m.question.replaceAll('"','""')}","${m.status}","${m.tokens}"`)].join('\n'); const a=document.createElement('a'); a.href=URL.createObjectURL(new Blob(['\ufeff'+csv],{type:'text/csv'})); a.download='messages.csv'; a.click(); URL.revokeObjectURL(a.href); ElMessage.success('消息记录已导出') }
</script>
