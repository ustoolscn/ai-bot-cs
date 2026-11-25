<template>
  <div>
    <div class="toolbar kb-toolbar">
      <span class="muted kb-hint">TXT、MD、PDF、DOCX、PNG、JPG、WEBP，单个不超过 10 MB</span>
      <span class="spacer" />
      <el-button :icon="Refresh" @click="retry">重新索引失败文档</el-button>
      <el-upload :show-file-list="false" :accept="accept" :before-upload="upload"><el-button type="primary" :icon="Upload">上传文档</el-button></el-upload>
    </div>
    <el-table :data="base.documentList" row-key="id">
      <el-table-column label="文件名" min-width="200"><template #default="{ row }"><div class="cell-primary"><span class="file-type">{{ row.type }}</span><span><strong>{{ row.name }}</strong><small>{{ row.size }} · {{ row.chunks }} 个分块</small></span></div></template></el-table-column>
      <el-table-column label="状态" width="130"><template #default="{ row }"><div class="stacked-cell"><StatusBadge :status="row.status" /><button v-if="row.lastError" type="button" class="error-link" :title="row.lastError" @click="showError(row.name, row.lastError)">查看失败原因</button></div></template></el-table-column>
      <el-table-column prop="updatedAt" label="更新时间" width="110" />
      <el-table-column label="操作" width="216" fixed="right"><template #default="{ row }"><el-button v-if="row.status === 'failed' || row.status === 'unindexed' || row.lastError" link type="primary" @click="retryDocument(row.id)">重新索引</el-button><el-button v-if="row.chunks > 0" link @click="deleteIndex(row)">删除索引</el-button><el-button link type="danger" @click="deleteFile(row)">删除文件</el-button></template></el-table-column>
      <template #empty><div class="table-empty"><el-icon><Document /></el-icon><strong>这个知识库还没有文档</strong><span>支持文本、PDF、Word 和图片</span></div></template>
    </el-table>
    <el-dialog v-model="imageVisible" title="上传图片" width="520px" @closed="clearImage">
      <div class="image-upload">
        <img v-if="imagePreview" :src="imagePreview" :alt="imageFile?.name">
        <el-input v-model="imageDescription" type="textarea" :autosize="{ minRows: 3, maxRows: 8 }" maxlength="4000" resize="none" placeholder="图片说明（可选）：品名、颜色、型号等。留空则识别图中文字" />
      </div>
      <template #footer><el-button @click="imageVisible = false">取消</el-button><el-button type="primary" :loading="uploading" @click="confirmImage">上传</el-button></template>
    </el-dialog>
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
const accept = '.txt,.md,.pdf,.docx,.png,.jpg,.jpeg,.webp'
const imageVisible = ref(false), imageFile = ref<File | null>(null), imagePreview = ref(''), imageDescription = ref(''), uploading = ref(false)

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
  const ext = file.name.split('.').pop()?.toLowerCase() || ''
  if (!accept.split(',').includes(`.${ext}`)) { ElMessage.error(ext === 'doc' ? '旧版 .doc 请另存为 .docx 后上传' : '仅支持 TXT、MD、PDF、DOCX、PNG、JPG、WEBP'); return false }
  if (file.size > 10 * 1024 * 1024) { ElMessage.error('单文件不能超过 10 MB'); return false }
  if (file.type.startsWith('image/') || ['png', 'jpg', 'jpeg', 'webp'].includes(ext)) {
    imageFile.value = file; imagePreview.value = URL.createObjectURL(file); imageDescription.value = ''; imageVisible.value = true
    return false
  }
  await send(file)
  return false
}
async function send(file: File, description = '') {
  try { await api.knowledge.upload(props.base.id, file, description); emit('changed'); ElMessage.success('已上传，正在索引'); return true }
  catch (error) { ElMessage.error(sanitizeServiceError(error instanceof Error ? error.message : '上传失败') || '上传失败'); return false }
}
async function confirmImage() {
  if (!imageFile.value) return
  uploading.value = true
  const done = await send(imageFile.value, imageDescription.value.trim())
  uploading.value = false
  if (done) imageVisible.value = false
}
function clearImage() { if (imagePreview.value) URL.revokeObjectURL(imagePreview.value); imageFile.value = null; imagePreview.value = '' }
</script>

<style scoped>
.error-link { align-self: flex-start; padding: 0; border: 0; background: none; color: var(--danger); font-size: 12px; cursor: pointer; }
.error-link:hover { text-decoration: underline; }
.image-upload { display: flex; flex-direction: column; gap: 12px; }
.image-upload img { display: block; max-width: 100%; max-height: 280px; margin: 0 auto; border-radius: var(--radius-sm); background: var(--surface-2); object-fit: contain; animation: rise var(--dur-slow) var(--ease) backwards; }
</style>
