<template>
  <div>
    <div class="toolbar kb-toolbar">
      <span class="muted kb-hint">支持 .txt、.md，单个文件不超过 10 MB</span>
      <span class="spacer" />
      <el-button :icon="Refresh" @click="retry">重新索引失败文档</el-button>
      <el-upload :show-file-list="false" :before-upload="upload"><el-button type="primary" :icon="Upload">上传文档</el-button></el-upload>
    </div>
    <el-table :data="base.documentList">
      <el-table-column label="文件名" min-width="220"><template #default="{ row }"><div class="cell-primary"><span class="file-type">{{ row.type }}</span><span><strong>{{ row.name }}</strong><small>{{ row.size }}</small></span></div></template></el-table-column>
      <el-table-column label="分块" width="80" align="right"><template #default="{ row }"><span class="num">{{ row.chunks }}</span></template></el-table-column>
      <el-table-column label="状态" width="110"><template #default="{ row }"><StatusBadge :status="row.status" /></template></el-table-column>
      <el-table-column label="失败原因" min-width="140"><template #default="{ row }"><div v-if="row.lastError" class="document-error"><span>{{ row.lastError }}</span><el-button link type="primary" @click="showError(row.name, row.lastError)">查看</el-button></div><span v-else class="faint">—</span></template></el-table-column>
      <el-table-column prop="updatedAt" label="更新时间" width="150" />
      <el-table-column label="操作" width="190" fixed="right"><template #default="{ row }"><el-button v-if="row.status === 'failed' || row.status === 'unindexed' || row.lastError" link type="primary" @click="retryDocument(row.id)">重新索引</el-button><el-button v-if="row.chunks > 0" link @click="deleteIndex(row)">删除索引</el-button><el-button link type="danger" @click="deleteFile(row)">删除文件</el-button></template></el-table-column>
      <template #empty><div class="table-empty"><el-icon><Document /></el-icon><strong>这个知识库还没有文档</strong><span>点右上角“上传文档”，支持 .txt 和 .md</span></div></template>
    </el-table>
    <el-dialog v-model="errorVisible" :title="errorTitle" width="620px"><pre class="error-detail-text">{{ selectedError }}</pre><template #footer><el-button :icon="CopyDocument" @click="copyError">复制原因</el-button><el-button type="primary" @click="errorVisible = false">关闭</el-button></template></el-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { CopyDocument, Document, Refresh, Upload } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { api, sanitizeServiceError } from '@/api'
import StatusBadge from '@/components/StatusBadge.vue'
import type { KnowledgeBase, KnowledgeDocument } from '@/types'

const props = defineProps<{ base: KnowledgeBase }>()
const emit = defineEmits<{ changed: [] }>()
const errorVisible = ref(false), errorTitle = ref('失败原因'), selectedError = ref('')

function showError(name: string, error: string) { errorTitle.value = `${name} · 失败原因`; selectedError.value = error; errorVisible.value = true }
async function copyError() { await navigator.clipboard?.writeText(selectedError.value); ElMessage.success('失败原因已复制') }
async function retry() {
  const documents = props.base.documentList.filter(d => d.status === 'failed' || d.status === 'unindexed')
  if (!documents.length) { ElMessage.info('当前没有需要索引的文档'); return }
  await Promise.all(documents.map(d => api.knowledge.retryDocument(props.base.id, d.id)))
  emit('changed'); ElMessage.success('文档已重新加入索引队列')
}
async function retryDocument(documentId: string) { await api.knowledge.retryDocument(props.base.id, documentId); emit('changed'); ElMessage.success('文档已重新加入索引队列') }
async function deleteIndex(document: KnowledgeDocument) {
  try { await ElMessageBox.confirm(`只删除“${document.name}”生成的 ${document.chunks} 个向量分块，保留原文件？`, '删除索引', { type: 'warning', confirmButtonText: '删除索引', cancelButtonText: '取消' }) } catch { return }
  await api.knowledge.deleteDocumentIndex(props.base.id, document.id); emit('changed'); ElMessage.success('索引已删除，原文件仍保留')
}
async function deleteFile(document: KnowledgeDocument) {
  try { await ElMessageBox.confirm(`删除“${document.name}”及其全部索引？此操作不可恢复。`, '删除文件', { type: 'warning', confirmButtonText: '删除文件', cancelButtonText: '取消' }) } catch { return }
  await api.knowledge.deleteDocument(props.base.id, document.id); emit('changed'); ElMessage.success('文件及索引已删除')
}
async function upload(file: File) {
  if (!/\.(txt|md)$/i.test(file.name)) { ElMessage.error('仅支持 TXT 和 Markdown 文件'); return false }
  if (file.size > 10 * 1024 * 1024) { ElMessage.error('单文件不能超过 10 MB'); return false }
  try { await api.knowledge.upload(props.base.id, file); emit('changed'); ElMessage.success('文档已上传，正在索引') }
  catch (error) { ElMessage.error(sanitizeServiceError(error instanceof Error ? error.message : '文档上传失败') || '文档上传失败') }
  return false
}
</script>
