<template>
  <div>
    <div class="knowledge-layout">
      <aside class="knowledge-list-panel">
        <el-input v-model="search" placeholder="搜索知识库" :prefix-icon="Search" clearable />
        <button v-for="kb in filtered" :key="kb.id" class="knowledge-list-item" :class="{ active: selected?.id === kb.id }" @click="selectKb(kb)">
          <span><strong>{{ kb.name }}</strong><small>{{ kb.documents }} 篇文档 · {{ kb.entries ?? 0 }} 条问答</small></span>
          <em :class="{ 'danger-text': kb.status === 'failed' }">{{ kb.status === 'failed' ? '有失败' : kb.progress + '%' }}</em>
        </button>
        <p v-if="!filtered.length" class="muted" style="padding: 8px 10px; font-size: 13px">{{ bases.length ? '没有匹配的知识库' : '还没有知识库' }}</p>
        <el-button :icon="Plus" style="margin-top: 8px" @click="createVisible = true">新建知识库</el-button>
      </aside>

      <section v-if="selected" class="knowledge-detail panel-surface">
        <div class="knowledge-detail-head">
          <div><span class="scope-tag">{{ scopeText(selected.scope) }}知识库</span><h2>{{ selected.name }}</h2><p>{{ selected.description || '没有说明' }}</p></div>
          <div>
            <el-button :icon="Search" @click="openTest">检索测试</el-button>
            <el-button :icon="Refresh" @click="retry">重新索引失败文档</el-button>
            <el-upload :show-file-list="false" :before-upload="upload"><el-button type="primary" :icon="Upload">上传文档</el-button></el-upload>
          </div>
        </div>
        <div class="knowledge-summary">
          <div><span>文档</span><strong>{{ selected.documents }}</strong></div>
          <div><span>知识问答</span><button type="button" class="summary-link" title="查看这个知识库的问答" @click="$router.push({ query: { tab: 'entries', kb: selected.id } })">{{ selected.entries ?? 0 }}</button></div>
          <div><span>分块</span><strong>{{ selected.chunks.toLocaleString() }}</strong></div>
          <div><span>Embedding 模型</span><strong class="truncate" style="font-size: 14px" :title="selected.model">{{ selected.model }}</strong></div>
          <div><span>索引进度</span><strong>{{ selected.progress }}%</strong><div class="progress-line" :class="{ 'is-failed': selected.status === 'failed' }"><i :style="{ width: selected.progress + '%' }" /></div></div>
        </div>
        <div class="section-title"><h3>文档</h3><span>支持 .txt、.md，单个文件不超过 10 MB</span></div>
        <el-table :data="selected.documentList">
          <el-table-column label="文件名" min-width="200"><template #default="{ row }"><div class="cell-primary"><span class="file-type">{{ row.type }}</span><span><strong>{{ row.name }}</strong><small>{{ row.size }}</small></span></div></template></el-table-column>
          <el-table-column label="分块" width="80" align="right"><template #default="{ row }"><span class="num">{{ row.chunks }}</span></template></el-table-column>
          <el-table-column label="状态" width="110"><template #default="{ row }"><StatusBadge :status="row.status" /></template></el-table-column>
          <el-table-column label="失败原因" min-width="140"><template #default="{ row }"><div v-if="row.lastError" class="document-error"><span>{{ row.lastError }}</span><el-button link type="primary" @click="showError(row.name, row.lastError)">查看</el-button></div><span v-else class="faint">—</span></template></el-table-column>
          <el-table-column prop="updatedAt" label="更新时间" width="150" />
          <el-table-column label="操作" width="180" fixed="right"><template #default="{ row }"><el-button v-if="row.status === 'failed' || row.status === 'unindexed' || row.lastError" link type="primary" @click="retryDocument(row.id)">重新索引</el-button><el-button v-if="row.chunks > 0" link @click="deleteIndex(row)">删除索引</el-button><el-button link type="danger" @click="deleteFile(row)">删除文件</el-button></template></el-table-column>
          <template #empty><div class="table-empty"><el-icon><Document /></el-icon><strong>还没有文档</strong><span>点右上角“上传文档”，支持 .txt 和 .md</span></div></template>
        </el-table>
      </section>
      <section v-else class="panel"><EmptyState title="还没有知识库" description="知识库存放机器人回答时参考的资料。先新建一个，再上传文档。"><el-button type="primary" :icon="Plus" @click="createVisible = true">新建知识库</el-button></EmptyState></section>
    </div>

    <el-dialog v-model="createVisible" title="新建知识库" width="520px"><el-form label-position="top"><el-form-item label="知识库名称"><el-input v-model="createForm.name" /></el-form-item><el-form-item label="说明"><el-input v-model="createForm.description" type="textarea" :rows="3" /></el-form-item><el-form-item label="Embedding 模型"><el-select v-model="createForm.embeddingProfileId"><el-option v-for="model in embeddingModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item></el-form><template #footer><el-button @click="createVisible = false">取消</el-button><el-button type="primary" @click="createKb">创建知识库</el-button></template></el-dialog>
    <el-drawer v-model="testVisible" title="检索测试" size="560px"><p class="muted" style="margin-bottom: 16px; font-size: 13px">输入一个用户可能会问的问题，看看知识库能找到哪些内容。相似度越高越相关。</p><el-form label-position="top"><el-form-item label="知识库"><el-select v-model="testKb"><el-option v-for="kb in bases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></el-form-item><el-form-item label="测试问题"><el-input v-model="testQuery" type="textarea" :rows="3" placeholder="输入一个真实用户问题，Ctrl/⌘ + Enter 开始检索" @keydown.enter.ctrl.exact.prevent="runTest" @keydown.enter.meta.exact.prevent="runTest" /></el-form-item><el-button type="primary" :loading="testing" @click="runTest">开始检索</el-button></el-form><div v-if="testResults.length" class="test-results"><div v-for="result in testResults" :key="result.content"><strong>相似度 {{ (result.score * 100).toFixed(1) }}%</strong><p>{{ result.content }}</p></div></div></el-drawer>
    <el-dialog v-model="errorVisible" :title="errorTitle" width="620px"><pre class="error-detail-text">{{ selectedError }}</pre><template #footer><el-button :icon="CopyDocument" @click="copyError">复制原因</el-button><el-button type="primary" @click="errorVisible = false">关闭</el-button></template></el-dialog>
  </div>
</template>
<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, reactive, ref } from 'vue'; import { CopyDocument, Document, Plus, Refresh, Search, Upload } from '@element-plus/icons-vue'; import { ElMessage, ElMessageBox } from 'element-plus'; import { api, sanitizeServiceError } from '@/api'; import EmptyState from '@/components/EmptyState.vue'; import StatusBadge from '@/components/StatusBadge.vue'; import type { KnowledgeBase, KnowledgeDocument, ModelProfile } from '@/types'
const bases=ref<KnowledgeBase[]>([]), models=ref<ModelProfile[]>([]), selected=ref<KnowledgeBase>(), search=ref(''), createVisible=ref(false), testVisible=ref(false), errorVisible=ref(false), errorTitle=ref('失败原因'), selectedError=ref(''), testKb=ref(''), testQuery=ref('机器人如何接收 QQ 消息？'), testing=ref(false), testResults=ref<Array<{content:string;score:number}>>([]); function openTest(){if(selected.value)testKb.value=selected.value.id;testVisible.value=true}
const createForm=reactive({name:'',description:'',embeddingProfileId:''}); const filtered=computed(()=>bases.value.filter(k=>!search.value||`${k.name}${k.description}`.includes(search.value)));const embeddingModels=computed(()=>models.value.filter(model=>model.kind==='embedding'))
onMounted(load);async function load(){[bases.value,models.value]=await Promise.all([api.knowledge.list(),api.models.list()]);if(!createForm.embeddingProfileId)createForm.embeddingProfileId=embeddingModels.value[0]?.id||'';if(!selected.value&&bases.value[0])await selectKb(bases.value[0]);testKb.value=testKb.value||bases.value[0]?.id||''};async function selectKb(kb:KnowledgeBase){try{selected.value=await api.knowledge.get(kb.id)}catch{selected.value=kb}}function scopeText(v:string){return ({global:'全局',bot:'机器人级',conversation:'会话级'} as Record<string,string>)[v]};function showError(name:string,error:string){errorTitle.value=`${name} · 失败原因`;selectedError.value=error;errorVisible.value=true}async function copyError(){await navigator.clipboard?.writeText(selectedError.value);ElMessage.success('失败原因已复制')}async function retry(){if(!selected.value)return;const documents=selected.value.documentList.filter(document=>document.status==='failed'||document.status==='unindexed');if(!documents.length){ElMessage.info('当前没有需要索引的文档');return}await Promise.all(documents.map(document=>api.knowledge.retryDocument(selected.value!.id,document.id)));await selectKb(selected.value);ElMessage.success('文档已重新加入索引队列')};async function retryDocument(documentId:string){if(!selected.value)return;await api.knowledge.retryDocument(selected.value.id,documentId);await selectKb(selected.value);ElMessage.success('文档已重新加入索引队列')};async function deleteIndex(document:KnowledgeDocument){if(!selected.value)return;try{await ElMessageBox.confirm(`只删除“${document.name}”生成的 ${document.chunks} 个向量分块，保留原文件？`,'删除索引',{type:'warning',confirmButtonText:'删除索引',cancelButtonText:'取消'})}catch{return}await api.knowledge.deleteDocumentIndex(selected.value.id,document.id);await selectKb(selected.value);ElMessage.success('索引已删除，原文件仍保留')};async function deleteFile(document:KnowledgeDocument){if(!selected.value)return;try{await ElMessageBox.confirm(`删除“${document.name}”及其全部索引？此操作不可恢复。`,'删除文件',{type:'warning',confirmButtonText:'删除文件',cancelButtonText:'取消'})}catch{return}await api.knowledge.deleteDocument(selected.value.id,document.id);await selectKb(selected.value);ElMessage.success('文件及索引已删除')};async function upload(file:File){if(!selected.value)return false;if(!/\.(txt|md)$/i.test(file.name)){ElMessage.error('仅支持 TXT 和 Markdown 文件');return false}if(file.size>10*1024*1024){ElMessage.error('单文件不能超过 10 MB');return false}try{await api.knowledge.upload(selected.value.id,file);await selectKb(selected.value);ElMessage.success('文档已上传，正在索引')}catch(error){ElMessage.error(sanitizeServiceError(error instanceof Error?error.message:'文档上传失败')||'文档上传失败')}return false};async function createKb(){if(!createForm.name.trim()||!createForm.embeddingProfileId){ElMessage.error('请填写名称并选择 Embedding 模型');return}const result=await api.knowledge.create(createForm.name,createForm.description,createForm.embeddingProfileId);createVisible.value=false;await load();const created=bases.value.find(base=>base.id===result.id)||bases.value[0];if(created)await selectKb(created);Object.assign(createForm,{name:'',description:'',embeddingProfileId:embeddingModels.value[0]?.id||''});ElMessage.success('知识库已创建')};async function runTest(){if(!testKb.value||!testQuery.value)return;testing.value=true;try{testResults.value=await api.knowledge.test(testKb.value,testQuery.value)}finally{testing.value=false}}
useAdminEvents(['knowledge_documents','knowledge_bases','model_profiles'], async () => { await load(); if (selected.value) await selectKb(selected.value) })
</script>


<style scoped>
.summary-link { display: block; padding: 0; border: 0; background: none; color: var(--primary-text); font: inherit; margin-top: 2px; font-size: 16px; font-weight: 600; font-variant-numeric: tabular-nums; cursor: pointer; }
.summary-link:hover { text-decoration: underline; }
.summary-link:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; border-radius: 4px; }
</style>
