<template>
  <div class="content-page">
    <PageHeader title="知识库" description="先选一个知识库，再管理它的文档、问答、待审核、没答上的问题和反馈。问题和反馈归属到群或机器人绑定的知识库。">
      <el-button :icon="Refresh" @click="reload">刷新</el-button>
      <el-button :icon="Upload" @click="openImport">导入知识库</el-button>
      <el-button :icon="Plus" @click="createVisible = true">新建知识库</el-button>
    </PageHeader>

    <div class="knowledge-layout">
      <aside class="knowledge-list-panel">
        <el-input v-model="search" placeholder="搜索知识库" :prefix-icon="Search" clearable />
        <button v-for="kb in filteredBases" :key="kb.id" type="button" class="knowledge-list-item" :class="{ active: !sharedView && current?.id === kb.id }" @click="openKb(kb.id)">
          <span><strong>{{ kb.name }}</strong><small>{{ kb.documents }} 篇文档 · {{ kb.entries ?? 0 }} 条问答</small></span>
          <em v-if="todoByKb(kb.id)" class="count-pill hot" :title="todoTitle(kb.id)">{{ todoByKb(kb.id) }}</em>
        </button>
        <p v-if="!filteredBases.length" class="muted list-empty">{{ bases.length ? '没有匹配的知识库' : '还没有知识库' }}</p>

        <template v-if="unassignedGaps.length || unassignedFeedback.length">
          <div class="list-divider"><span>未归属到知识库</span></div>
          <button type="button" class="knowledge-list-item" :class="{ active: !!sharedView }" @click="openShared(unassignedGaps.length ? 'gaps' : 'feedback')">
            <span><strong>未归属</strong><small>{{ unassignedGaps.length }} 个没答上 · {{ unassignedFeedback.length }} 条反馈</small></span>
            <em v-if="unassignedUnread" class="count-pill hot" title="未读差评">{{ unassignedUnread }}</em>
          </button>
        </template>
      </aside>

      <!-- 没归属到任何知识库的问题和反馈（群和机器人都没绑知识库时） -->
      <section v-if="sharedView" key="unassigned" class="knowledge-detail panel-surface">
        <div class="knowledge-detail-head"><div><span class="scope-tag">未归属</span><h2>{{ sharedView === 'gaps' ? '没答上的问题' : '回答反馈' }}</h2><p>这些消息所在的群和机器人都没有绑定知识库。到“群与会话”里绑定后，新的问题和反馈会归到对应知识库。</p></div></div>
        <el-segmented :model-value="sharedView" class="unassigned-switch" :options="[{ label: `没答上 ${unassignedGaps.length}`, value: 'gaps' }, { label: `反馈 ${unassignedFeedback.length}`, value: 'feedback' }]" @update:model-value="(v: string | number | boolean) => openShared(v as 'gaps' | 'feedback')" />
        <template v-if="sharedView === 'gaps'">
        <div class="toolbar kb-toolbar"><span class="spacer" /><el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('gaps', '没答上的问题')">删除选中的 {{ selectedIds.length }} 条</el-button></div>
        <el-table row-key="id" v-loading="loading" :data="unassignedGaps" empty-text="没有新的问题" @selection-change="selectRows">
          <el-table-column type="selection" width="48" />
          <el-table-column prop="question" label="用户问了但没答上的问题" min-width="260" />
          <el-table-column prop="occurrences" label="出现次数" width="104" sortable align="right" />
          <el-table-column label="最近出现" width="140"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></el-table-column>
          <el-table-column label="操作" width="150" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="answerGap(row)">补充答案</el-button><el-button link @click="ignoreGap(row)">忽略</el-button></template></el-table-column>
        </el-table>
        </template>
        <template v-else>
        <div class="toolbar kb-toolbar">
          <el-segmented v-model="feedbackView" :options="[{ label: '全部', value: 'all' }, { label: '未读', value: 'unread' }]" />
          <el-select v-model="feedbackRating" clearable placeholder="全部评价" aria-label="按评价筛选"><el-option v-for="(v, k) in ratingText" :key="k" :label="v" :value="k" /></el-select>
          <span class="spacer" />
          <el-button v-if="selectedIds.length" :loading="markingRead" @click="markRead({ ids: [...selectedIds] })">选中的标为已读</el-button>
          <el-button v-else :disabled="!shownFeedback.some(f => !f.read)" :loading="markingRead" @click="markRead({ ids: shownFeedback.filter(f => !f.read).map(f => f.id) })">全部标为已读</el-button>
          <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('feedback', '反馈')">删除选中的 {{ selectedIds.length }} 条</el-button>
        </div>
        <el-table row-key="id" v-loading="loading" :data="shownFeedback" :row-class-name="({ row }: { row: Feedback }) => row.read ? '' : 'is-unread'" :empty-text="feedbackView === 'unread' ? '没有未读反馈' : '暂无反馈'" @selection-change="selectRows">
          <el-table-column type="selection" width="48" />
          <el-table-column width="72"><template #default="{ row }"><button v-if="!row.read" type="button" class="unread-dot" title="点击标为已读" @click.stop="markRead({ ids: [row.id] })"><i />未读</button><span v-else class="faint">已读</span></template></el-table-column>
          <el-table-column label="问题 / 回答" min-width="240"><template #default="{ row }"><div class="qa"><strong>{{ $at(row.question) || '—' }}</strong><span>{{ $at(row.answer) }}</span><small v-if="row.correctedAnswer">纠正：{{ $at(row.correctedAnswer) }}</small></div></template></el-table-column>
          <el-table-column label="评价" width="80"><template #default="{ row }"><el-tag size="small" :type="['good', 'helpful'].includes(row.rating) ? 'success' : 'danger'">{{ ratingText[row.rating] || row.rating }}</el-tag></template></el-table-column>
          <el-table-column label="会话 / 来源" width="140"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.conversationName || '—' }}</strong><span>{{ feedbackSourceText[row.source] || row.source }}</span></div></template></el-table-column>
          <el-table-column label="时间" width="140"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
        </el-table>
        </template>
      </section>

      <!-- 选中的知识库 -->
      <section v-else-if="current" :key="current.id" class="knowledge-detail panel-surface">
        <div class="knowledge-detail-head">
          <div><span class="scope-tag">{{ scopeText(current.scope) }}知识库</span><h2>{{ current.name }}</h2><p>{{ current.description || '没有说明' }}</p></div>
          <div class="kb-head-actions"><el-button :icon="Download" :loading="exporting" @click="exportKb">{{ exporting ? '正在打包' : '导出' }}</el-button><el-button :icon="Search" @click="testVisible = true">检索测试</el-button><el-button class="kb-delete" :icon="Delete" @click="openRemove">删除</el-button></div>
        </div>
        <div class="knowledge-summary">
          <div><span>文档</span><strong>{{ current.documents }}</strong></div>
          <div><span>问答</span><strong>{{ current.entries ?? 0 }}</strong></div>
          <div><span>分块</span><strong>{{ current.chunks.toLocaleString() }}</strong></div>
          <div><span>Embedding 模型</span><div class="kb-model-line"><strong class="truncate kb-model" :title="current.model">{{ current.model }}</strong><button type="button" class="summary-action" @click="openModelSwitch">更换</button></div></div>
          <div><span>索引进度</span><strong>{{ current.progress }}%</strong><div class="progress-line" :class="{ 'is-failed': current.status === 'failed' }"><i :style="{ width: current.progress + '%' }" /></div></div>
        </div>

        <el-tabs :model-value="kbTab" class="kb-tabs" @update:model-value="setTab">
          <el-tab-pane name="documents" label="文档">
            <KnowledgeDocuments v-if="kbTab === 'documents'" :base="current" @changed="refreshCurrent" />
          </el-tab-pane>

          <el-tab-pane name="entries">
            <template #label>问答<span class="count-pill">{{ current.entries ?? 0 }}</span></template>
            <template v-if="kbTab === 'entries'">
              <div class="toolbar kb-toolbar">
                <el-input v-model="entryQuery" placeholder="搜索问题或答案" :prefix-icon="Search" clearable @change="loadPane" />
                <el-select v-model="entryStatus" aria-label="按状态筛选" @change="loadPane"><el-option label="已发布" value="published" /><el-option label="需复核" value="review" /><el-option label="已归档" value="archived" /><el-option label="全部" value="all" /></el-select>
                <span class="spacer" />
                <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('entries', '问答')">删除选中的 {{ selectedIds.length }} 条</el-button>
                <el-button type="primary" :icon="Plus" @click="editEntry()">新建问答</el-button>
              </div>
              <el-table row-key="id" v-loading="loading" :data="entries" @selection-change="selectRows">
                <el-table-column type="selection" width="48" />
                <el-table-column label="问题 / 答案" min-width="220"><template #default="{ row }"><div class="qa-row"><div class="qa"><strong>{{ $at(row.question) }}</strong><span>{{ $at(row.answer) }}</span><small v-if="row.aliases?.length">相似问：{{ row.aliases.join('；') }}</small></div><a v-if="row.materials?.length" :href="row.materials[0].url" target="_blank" rel="noopener" class="qa-thumb" :title="`带 ${row.materials.length} 个素材，点开看第一个`" @click.stop><video v-if="row.materials[0].kind === 'video'" :src="row.materials[0].url" preload="metadata" muted /><img v-else :src="row.materials[0].url" alt="素材" loading="lazy"><span v-if="row.materials.length > 1" class="qa-thumb-count">{{ row.materials.length }}</span></a></div></template></el-table-column>
                <el-table-column label="命中 / 好评 / 差评" width="132" align="right"><template #default="{ row }"><span class="num">{{ row.hitCount }} / {{ row.helpfulCount }} / <span :class="{ 'danger-text': row.unhelpfulCount > row.helpfulCount }">{{ row.unhelpfulCount }}</span></span></template></el-table-column>
                <el-table-column label="状态 / 来源" width="110"><template #default="{ row }"><div class="stacked-cell"><span class="tag-line"><el-tag size="small" :type="row.status === 'published' ? (row.indexStatus === 'ready' ? 'success' : row.indexStatus === 'failed' ? 'danger' : 'warning') : row.status === 'review' ? 'danger' : 'info'">{{ entryStatusText(row) }}</el-tag></span><span>{{ sourceText[row.source] || row.source }}</span></div></template></el-table-column>
                <el-table-column label="操作" width="112" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="editEntry(row)">编辑</el-button><el-button link type="danger" @click="removeEntry(row)">删除</el-button></template></el-table-column>
                <template #empty><div class="table-empty"><el-icon><ChatLineSquare /></el-icon><strong>这个知识库还没有问答</strong><span>点“新建问答”手动添加，或通过待审核的知识补充</span></div></template>
              </el-table>
            </template>
          </el-tab-pane>

          <el-tab-pane name="candidates">
            <template #label>待审核<span class="count-pill" :class="{ hot: pendingByKb[current.id] }">{{ pendingByKb[current.id] || 0 }}</span></template>
            <template v-if="kbTab === 'candidates'">
              <div class="toolbar kb-toolbar">
                <el-segmented v-model="candidateStatus" :options="candidateStatusOptions" @change="loadPane" />
                <el-tag v-if="candidateConversation" closable @close="clearConversationFilter">只看：{{ candidateConversationName || candidateConversation }}</el-tag>
                <span class="spacer" />
                <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('candidates', '待审核记录')">删除选中的 {{ selectedIds.length }} 条</el-button>
              </div>
              <el-table row-key="id" v-loading="loading" :data="kbCandidates" @selection-change="selectRows">
                <el-table-column type="selection" width="48" />
                <el-table-column label="问题 / 答案" min-width="220"><template #default="{ row }"><div class="qa-row"><div class="qa"><strong>{{ $at(row.question) }}</strong><span>{{ $at(row.answer) }}</span><small v-if="row.aliases?.length">相似问：{{ row.aliases.join('；') }}</small></div><a v-if="row.materials?.length" :href="row.materials[0].url" target="_blank" rel="noopener" class="qa-thumb" :title="`带 ${row.materials.length} 个素材，点开看第一个`" @click.stop><video v-if="row.materials[0].kind === 'video'" :src="row.materials[0].url" preload="metadata" muted /><img v-else :src="row.materials[0].url" alt="素材" loading="lazy"><span v-if="row.materials.length > 1" class="qa-thumb-count">{{ row.materials.length }}</span></a></div></template></el-table-column>
                <el-table-column label="来源 / 时间" width="140"><template #default="{ row }"><div class="stacked-cell"><strong>{{ sourceText[row.source] || row.source }}</strong><span>{{ formatTime(row.createdAt) }}</span></div></template></el-table-column>
                <el-table-column label="可信度" width="80" align="right"><template #default="{ row }"><span class="num" :class="row.confidence >= 0.85 ? 'trend-good' : row.confidence < 0.6 ? 'danger-text' : ''">{{ Math.round(row.confidence * 100) }}%</span></template></el-table-column>
                <el-table-column label="操作" width="184" fixed="right"><template #default="{ row }"><template v-if="row.status === 'pending'"><el-button link type="primary" @click="quickApprove(row)">通过</el-button><el-button link type="primary" @click="review(row)">修改后通过</el-button><el-button link type="danger" @click="reject(row)">驳回</el-button></template><span v-else class="pill" :class="row.status === 'approved' ? 'good' : 'idle'">{{ row.status === 'approved' ? '已通过' : '已驳回' }}</span></template></el-table-column>
                <template #empty><div class="table-empty"><el-icon><Finished /></el-icon><strong>{{ candidateStatus === 'pending' ? '没有待审核的知识' : '暂无记录' }}</strong><span v-if="candidateStatus === 'pending'">在“群与会话 → 成员”里标记官方客服后，他们在群里的回答会出现在这里</span></div></template>
              </el-table>
            </template>
          </el-tab-pane>

          <el-tab-pane name="gaps">
            <template #label>没答上<span class="count-pill" :class="{ warn: gapsByKb[current.id] }">{{ gapsByKb[current.id] || 0 }}</span></template>
            <template v-if="kbTab === 'gaps'">
            <div class="toolbar kb-toolbar"><span class="spacer" /><el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('gaps', '没答上的问题')">删除选中的 {{ selectedIds.length }} 条</el-button></div>
            <el-table row-key="id" v-loading="loading" :data="kbGaps" empty-text="这个知识库没有没答上的问题" @selection-change="selectRows">
              <el-table-column type="selection" width="48" />
              <el-table-column prop="question" label="用户问了但没答上的问题" min-width="260" />
              <el-table-column prop="occurrences" label="出现次数" width="104" sortable align="right" />
              <el-table-column label="最近出现" width="140"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></el-table-column>
              <el-table-column label="操作" width="150" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="answerGap(row)">补充答案</el-button><el-button link @click="ignoreGap(row)">忽略</el-button></template></el-table-column>
            </el-table>
            </template>
          </el-tab-pane>

          <el-tab-pane name="feedback">
            <template #label>反馈<span class="count-pill" :class="{ hot: unreadByKb[current.id] }">{{ unreadByKb[current.id] || 0 }}</span></template>
            <template v-if="kbTab === 'feedback'">
            <div class="toolbar kb-toolbar">
              <el-segmented v-model="feedbackView" :options="[{ label: '全部', value: 'all' }, { label: '未读', value: 'unread' }]" />
              <el-select v-model="feedbackRating" clearable placeholder="全部评价" aria-label="按评价筛选"><el-option v-for="(v, k) in ratingText" :key="k" :label="v" :value="k" /></el-select>
              <span class="spacer" />
              <el-button v-if="selectedIds.length" :loading="markingRead" @click="markRead({ ids: [...selectedIds] })">选中的标为已读</el-button>
              <el-button v-else :disabled="!shownFeedback.some(f => !f.read)" :loading="markingRead" @click="markRead({ ids: shownFeedback.filter(f => !f.read).map(f => f.id) })">全部标为已读</el-button>
              <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('feedback', '反馈')">删除选中的 {{ selectedIds.length }} 条</el-button>
            </div>
            <el-table row-key="id" v-loading="loading" :data="shownFeedback" :row-class-name="({ row }: { row: Feedback }) => row.read ? '' : 'is-unread'" :empty-text="feedbackView === 'unread' ? '没有未读反馈' : '暂无反馈'" @selection-change="selectRows">
              <el-table-column type="selection" width="48" />
              <el-table-column width="72"><template #default="{ row }"><button v-if="!row.read" type="button" class="unread-dot" title="点击标为已读" @click.stop="markRead({ ids: [row.id] })"><i />未读</button><span v-else class="faint">已读</span></template></el-table-column>
              <el-table-column label="问题 / 回答" min-width="240"><template #default="{ row }"><div class="qa"><strong>{{ $at(row.question) || '—' }}</strong><span>{{ $at(row.answer) }}</span><small v-if="row.correctedAnswer">纠正：{{ $at(row.correctedAnswer) }}</small></div></template></el-table-column>
              <el-table-column label="评价" width="80"><template #default="{ row }"><el-tag size="small" :type="['good', 'helpful'].includes(row.rating) ? 'success' : 'danger'">{{ ratingText[row.rating] || row.rating }}</el-tag></template></el-table-column>
              <el-table-column label="会话 / 来源" width="140"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.conversationName || '—' }}</strong><span>{{ feedbackSourceText[row.source] || row.source }}</span></div></template></el-table-column>
              <el-table-column label="时间" width="140"><template #default="{ row }">{{ formatTime(row.createdAt) }}</template></el-table-column>
            </el-table>
            </template>
          </el-tab-pane>
        </el-tabs>
      </section>

      <section v-else class="panel"><EmptyState title="还没有知识库" description="知识库存放机器人回答时参考的资料。先新建一个，再往里面上传文档或添加问答。"><el-button type="primary" :icon="Plus" @click="createVisible = true">新建知识库</el-button></EmptyState></section>
    </div>

    <el-dialog v-model="entryDialog" :title="entryDialogTitle" width="640px">
      <el-form label-position="top">
        <el-form-item label="存入知识库"><el-select v-model="entryForm.knowledgeBaseId" :disabled="!!entryForm.id"><el-option v-for="kb in bases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></el-form-item>
        <el-form-item label="标准问题"><el-input v-model="entryForm.question" /></el-form-item>
        <el-form-item label="相似问法（每行一个）"><el-input v-model="entryForm.aliasesText" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="答案（建议口语化、简洁）"><el-input v-model="entryForm.answer" type="textarea" :rows="6" @paste="entryPicker?.fromClipboard($event)" /></el-form-item>
        <el-form-item label="演示素材（选填）"><MaterialPicker ref="entryPicker" v-model="entryForm.materials" hint="用户问到这条时，AI 先发答案，再按顺序发这些截图或录屏。拖动调整先后，可直接粘贴截图" /></el-form-item>
        <el-form-item v-if="entryForm.id" label="状态"><el-radio-group v-model="entryForm.status"><el-radio-button value="published">发布</el-radio-button><el-radio-button value="review">需复核</el-radio-button><el-radio-button value="archived">归档</el-radio-button></el-radio-group></el-form-item>
        <el-form-item v-if="entryForm.mode === 'candidate' && evidenceText" label="来源对话"><div class="content-box evidence">{{ evidenceText }}</div></el-form-item>
      </el-form>
      <template #footer><el-button @click="entryDialog = false">取消</el-button><el-button type="primary" :loading="savingEntry" @click="submitEntry">{{ entryForm.mode === 'candidate' ? '通过并发布' : entryForm.mode === 'gap' ? '保存答案' : '保存问答' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="removeVisible" title="删除知识库" width="480px" :close-on-click-modal="!removing">
      <div v-if="current" class="kb-remove">
        <p>将永久删除「<strong>{{ current.name }}</strong>」里的 {{ current.documents }} 篇文档和 {{ current.entries ?? 0 }} 条问答，以及它的待审核和没答上记录。删除后不能恢复，需要留底请先导出。</p>
        <div v-if="boundSummary.length" class="kb-remove-bound">
          <span>正在使用它的地方会自动解绑</span>
          <p v-for="group in boundSummary" :key="group.label"><em>{{ group.label }} {{ group.total }}</em>{{ group.names }}</p>
        </div>
        <el-form label-position="top" @submit.prevent="removeKb">
          <el-form-item label="输入知识库名称确认"><el-input v-model="removeConfirm" :placeholder="current.name" :disabled="removing" /></el-form-item>
        </el-form>
      </div>
      <template #footer><el-button :disabled="removing" @click="removeVisible = false">取消</el-button><el-button type="danger" :loading="removing" :disabled="removeConfirm.trim() !== current?.name" @click="removeKb">永久删除</el-button></template>
    </el-dialog>
    <el-dialog v-model="createVisible" title="新建知识库" width="520px">
      <el-form label-position="top">
        <el-form-item label="知识库名称"><el-input v-model="createForm.name" /></el-form-item>
        <el-form-item label="说明"><el-input v-model="createForm.description" type="textarea" :rows="3" /></el-form-item>
        <el-form-item label="Embedding 模型"><el-select v-model="createForm.embeddingProfileId"><el-option v-for="model in embeddingModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item>
      </el-form>
      <template #footer><el-button @click="createVisible = false">取消</el-button><el-button type="primary" @click="createKb">创建知识库</el-button></template>
    </el-dialog>

    <el-dialog v-model="importVisible" title="导入知识库" width="520px" :close-on-click-modal="!importing" :close-on-press-escape="!importing" :show-close="!importing">
      <el-form label-position="top">
        <el-form-item label="导出文件">
          <div class="import-file" :class="{ picked: importForm.file }">
            <el-icon><Document /></el-icon>
            <span class="truncate">{{ importForm.file ? `${importForm.file.name} · ${fileSize(importForm.file.size)}` : '选择从另一套系统导出的 .zip 文件' }}</span>
            <el-button size="small" :disabled="importing" @click="importPicker?.click()">{{ importForm.file ? '重新选择' : '选择文件' }}</el-button>
            <input ref="importPicker" type="file" accept=".zip,application/zip" hidden @change="pickImport">
          </div>
        </el-form-item>
        <el-form-item label="知识库名称"><el-input v-model="importForm.name" :disabled="importing" placeholder="留空则沿用文件里的名称" /></el-form-item>
        <el-form-item label="Embedding 模型"><el-select v-model="importForm.embeddingProfileId" :disabled="importing"><el-option v-for="model in embeddingModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item>
      </el-form>
      <p class="switch-note">导入后会生成一个新的知识库，不会覆盖已有的库。文档和问答会用这里选的模型重新索引，索引在后台进行，并按模型计费。</p>
      <div class="import-progress" :class="{ active: importing }" aria-live="polite">
        <div class="progress-line" :class="{ 'is-running': importing && importRatio >= 1 }"><i :style="{ width: Math.round(importRatio * 100) + '%' }" /></div>
        <span>{{ !importing ? '' : importRatio < 1 ? `正在上传 ${Math.round(importRatio * 100)}%` : '上传完成，正在导入…' }}</span>
      </div>
      <template #footer><el-button :disabled="importing" @click="importVisible = false">取消</el-button><el-button type="primary" :loading="importing" :disabled="!importForm.file || !importForm.embeddingProfileId" @click="runImport">导入</el-button></template>
    </el-dialog>

    <el-drawer v-model="testVisible" :title="`检索测试 · ${current?.name || ''}`" size="560px">
      <p class="muted drawer-hint">输入一个用户可能会问的问题，看看这个知识库能找到哪些内容。相似度越高越相关。</p>
      <el-form label-position="top">
        <el-form-item label="测试问题"><el-input v-model="testQuery" type="textarea" :rows="3" placeholder="输入一个真实用户问题，Ctrl/⌘ + Enter 开始检索" @keydown.enter.ctrl.exact.prevent="runTest" @keydown.enter.meta.exact.prevent="runTest" /></el-form-item>
        <el-button type="primary" :loading="testing" @click="runTest">开始检索</el-button>
      </el-form>
      <div v-if="testResults.length" class="test-results"><div v-for="result in testResults" :key="result.id || result.content"><strong>相似度 {{ (result.score * 100).toFixed(1) }}%<span v-if="result.hasImage" class="hit-image-tag">带截图</span></strong><p>{{ result.content }}</p><a v-if="result.hasImage && result.id && current" :href="api.knowledge.chunkImageUrl(current.id, result.id)" target="_blank" rel="noopener" class="hit-image"><img :src="api.knowledge.chunkImageUrl(current.id, result.id)" alt="命中片段的截图" loading="lazy"></a></div></div>
    </el-drawer>

    <el-dialog v-model="modelSwitchVisible" title="更换向量模型" width="480px">
      <el-form label-position="top">
        <el-form-item label="Embedding 模型"><el-select v-model="modelSwitchId"><el-option v-for="model in embeddingModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item>
      </el-form>
      <p class="switch-note">切换后会用新模型重新处理这个知识库的全部文档和问答，不用重新上传。处理完成前，只能查到已经处理好的部分；重新处理会按新模型计费。</p>
      <template #footer><el-button @click="modelSwitchVisible = false">取消</el-button><el-button type="primary" :loading="modelSwitching" :disabled="!modelSwitchId || modelSwitchId === current?.embeddingProfileId" @click="switchModel">切换并重建</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useShellStore } from '@/stores/shell'
import { ChatLineSquare, Delete, Document, Download, Finished, Plus, Refresh, Search, Upload } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import MaterialPicker from '@/components/MaterialPicker.vue'
import { api } from '@/api'
import { ApiError } from '@/api/client'
import { useBulkDelete } from '@/composables/useBulkDelete'
import KnowledgeDocuments from './KnowledgeDocuments.vue'
import { agentApi, feedbackSourceText, formatTime, ratingText, sourceText, draftsOf, type Candidate, type Entry, type MaterialDraft, type Feedback, type Gap, type LearningStats } from '@/api/agent'
import type { KnowledgeBase, ModelProfile } from '@/types'

const route = useRoute()
const router = useRouter()
const shell = useShellStore()

// ---- 路由：?kb=<id>&tab=documents|entries|candidates|gaps|feedback；没归属到知识库的：?scope=unassigned&tab=gaps|feedback ----
const kbTabs = ['documents', 'entries', 'candidates', 'gaps', 'feedback']
const queryTab = computed(() => String(route.query.tab || ''))
const sharedView = computed(() => (route.query.scope === 'unassigned' ? (queryTab.value === 'feedback' ? 'feedback' : 'gaps') : '') as '' | 'gaps' | 'feedback')
const kbTab = computed(() => kbTabs.includes(queryTab.value) ? queryTab.value : 'documents')
const queryKb = computed(() => typeof route.query.kb === 'string' ? route.query.kb : '')

const bases = ref<KnowledgeBase[]>([])
const current = ref<KnowledgeBase>()
const models = ref<ModelProfile[]>([])
const defaultKB = ref('')
const stats = ref<LearningStats | null>(null)
const pendingCandidates = ref<Candidate[]>([])
const search = ref('')
const filteredBases = computed(() => bases.value.filter(k => !search.value || `${k.name}${k.description}`.includes(search.value)))
const embeddingModels = computed(() => models.value.filter(m => m.kind === 'embedding'))

// 没答上的问题和反馈按“消息所在群/机器人绑定的知识库”归属；后端没给归属的算未归属
const gaps = ref<Gap[]>([])
const feedback = ref<Feedback[]>([])
function kbOf(row: { knowledgeBaseId?: string | null; knowledgeBaseName?: string }) {
  const id = row.knowledgeBaseId || (row.knowledgeBaseName ? bases.value.find(b => b.name === row.knowledgeBaseName)?.id : '') || ''
  return bases.value.some(b => b.id === id) ? id : ''
}
const isUnreadNegative = (f: Feedback) => !f.read && !['good', 'helpful'].includes(f.rating)
function countBy<T>(rows: T[], keep: (row: T) => boolean = () => true) {
  return rows.reduce<Record<string, number>>((acc, row) => { if (keep(row)) { const id = kbOf(row as never); acc[id] = (acc[id] || 0) + 1 } return acc }, {})
}
const gapsByKb = computed(() => countBy(gaps.value))
const unreadByKb = computed(() => countBy(feedback.value, isUnreadNegative))
const unassignedGaps = computed(() => gaps.value.filter(g => !kbOf(g)))
const unassignedFeedback = computed(() => feedback.value.filter(f => !kbOf(f)))
const unassignedUnread = computed(() => unassignedFeedback.value.filter(isUnreadNegative).length)
const kbGaps = computed(() => current.value ? gaps.value.filter(g => kbOf(g) === current.value!.id) : [])
const todoByKb = (id: string) => (pendingByKb.value[id] || 0) + (gapsByKb.value[id] || 0) + (unreadByKb.value[id] || 0)
function todoTitle(id: string) {
  return [[pendingByKb.value[id], '条待审核'], [gapsByKb.value[id], '个没答上'], [unreadByKb.value[id], '条未读差评']].filter(([n]) => n).map(([n, t]) => `${n} ${t}`).join('，')
}

// 候选知识没写知识库时，归到“学习结果存入”的默认知识库
function candidateKb(c: Candidate) {
  if (c.knowledgeBaseId) return c.knowledgeBaseId
  if (c.knowledgeBaseName) return bases.value.find(b => b.name === c.knowledgeBaseName)?.id || defaultKB.value
  return defaultKB.value
}
const pendingByKb = computed(() => pendingCandidates.value.reduce<Record<string, number>>((acc, c) => { const id = candidateKb(c); acc[id] = (acc[id] || 0) + 1; return acc }, {}))

function openKb(id: string) { router.replace({ query: { kb: id, ...(kbTab.value !== 'documents' && !sharedView.value ? { tab: kbTab.value } : {}) } }) }
function openShared(view: 'gaps' | 'feedback') { router.replace({ query: { scope: 'unassigned', tab: view } }) }
function setTab(value: string | number) {
  if (!current.value) return
  const tab = String(value)
  router.replace({ query: { kb: current.value.id, ...(tab === 'documents' ? {} : { tab }) } })
}

// ---- 数据 ----
const loading = ref(false)
const entries = ref<Entry[]>([])
const entryQuery = ref('')
const entryStatus = ref('published')
const candidates = ref<Candidate[]>([])
const candidateStatus = ref('pending')
const candidateStatusOptions = [{ label: '待审核', value: 'pending' }, { label: '已通过', value: 'approved' }, { label: '已驳回', value: 'rejected' }, { label: '全部', value: 'all' }]
const candidateConversation = computed(() => typeof route.query.conversationId === 'string' ? route.query.conversationId : '')
const candidateConversationName = computed(() => typeof route.query.conversationName === 'string' ? route.query.conversationName : '')
const kbCandidates = computed(() => current.value ? candidates.value.filter(c => candidateKb(c) === current.value!.id) : [])
function clearConversationFilter() { router.replace({ query: { kb: current.value?.id, tab: 'candidates' } }) }
const feedbackRating = ref('')
const feedbackView = ref<'all' | 'unread'>('all')
const shownFeedback = computed(() => feedback.value.filter(f =>
  kbOf(f) === (sharedView.value ? '' : current.value?.id)
  && (!feedbackRating.value || f.rating === feedbackRating.value)
  && (feedbackView.value !== 'unread' || !f.read)))

const { selectedIds, deleting, deleteSelected } = useBulkDelete(reload)
function selectRows(rows: { id: string }[]) { selectedIds.value = rows.map(r => r.id) }

async function reload() {
  const [s, kbs, settings, pending, ms, gapRows, feedbackRows] = await Promise.all([
    agentApi.learning.stats().catch(() => null), api.knowledge.list(), agentApi.settings.get().catch(() => null),
    agentApi.learning.candidates('pending').catch(() => []), api.models.list().catch(() => []),
    agentApi.learning.gaps().catch(() => gaps.value), agentApi.learning.feedback().catch(() => feedback.value),
  ])
  gaps.value = gapRows
  feedback.value = feedbackRows
  stats.value = s
  shell.refreshCounts(true)
  bases.value = kbs
  models.value = ms
  defaultKB.value = settings?.learningKnowledgeBaseId || kbs[0]?.id || ''
  pendingCandidates.value = pending
  if (!createForm.embeddingProfileId) createForm.embeddingProfileId = embeddingModels.value[0]?.id || ''
  await syncSelection(true)
}

// 选中哪个知识库：URL 指定的 > 当前的 > 有待审核的（从“待审核”链接进来时）> 第一个
async function syncSelection(fresh = false) {
  if (sharedView.value) { await loadPane(); return }
  // 从“没答上”“待审核”入口进来、没指定知识库时，先去待办最多的那个；全在未归属里就去未归属
  const busiest = (counts: Record<string, number>) => Object.entries(counts).filter(([id]) => id).sort((a, b) => b[1] - a[1])[0]?.[0] || ''
  if (!queryKb.value && kbTab.value === 'gaps' && !busiest(gapsByKb.value) && unassignedGaps.value.length) { openShared('gaps'); return }
  if (!queryKb.value && kbTab.value === 'feedback' && !busiest(unreadByKb.value) && unassignedUnread.value) { openShared('feedback'); return }
  const wanted = queryKb.value || current.value?.id
    || (kbTab.value === 'candidates' ? busiest(pendingByKb.value) : kbTab.value === 'gaps' ? busiest(gapsByKb.value) : kbTab.value === 'feedback' ? busiest(unreadByKb.value) : '')
    || bases.value[0]?.id || ''
  const base = bases.value.find(b => b.id === wanted) || bases.value[0]
  if (!base) { current.value = undefined; return }
  // 地址里还没写知识库时先补上，交给下面的 watch 再加载一次
  if (queryKb.value !== base.id) { router.replace({ query: { ...route.query, kb: base.id } }); return }
  if (fresh || current.value?.id !== base.id || !current.value.documentList) {
    try { current.value = await api.knowledge.get(base.id) } catch { current.value = base }
  }
  await loadPane()
}
async function refreshCurrent() {
  if (!current.value) return
  const [kbs, detail] = await Promise.all([api.knowledge.list(), api.knowledge.get(current.value.id).catch(() => current.value)])
  bases.value = kbs
  current.value = detail
}

async function loadPane() {
  selectedIds.value = []
  loading.value = true
  try {
    if (sharedView.value === 'gaps' || kbTab.value === 'gaps') gaps.value = await agentApi.learning.gaps()
    else if (sharedView.value === 'feedback' || kbTab.value === 'feedback') feedback.value = await agentApi.learning.feedback()
    else if (current.value && kbTab.value === 'entries') entries.value = await agentApi.learning.entries({ knowledgeBaseId: current.value.id, status: entryStatus.value, q: entryQuery.value || undefined })
    else if (current.value && kbTab.value === 'candidates') candidates.value = await agentApi.learning.candidates(candidateStatus.value, candidateConversation.value || undefined)
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally { loading.value = false }
}
watch([queryKb, queryTab, candidateConversation], () => { if (bases.value.length || sharedView.value) syncSelection() })
watch(feedbackView, () => { selectedIds.value = [] })

function scopeText(v: string) { return ({ global: '全局', bot: '机器人级', conversation: '会话级' } as Record<string, string>)[v] || '' }
function entryStatusText(row: Entry) {
  if (row.status === 'review') return '需复核'
  if (row.status === 'archived') return '已归档'
  return ({ ready: '已生效', indexed: '已生效', failed: '索引失败', pending: '索引中' } as Record<string, string>)[row.indexStatus] || row.indexStatus
}

// ---- 反馈已读 ----
const markingRead = ref(false)
async function markRead(target: { ids: string[] } | { all: true }) {
  markingRead.value = true
  try {
    await agentApi.learning.markFeedbackRead(target)
    const ids = 'ids' in target ? new Set(target.ids) : null
    feedback.value = feedback.value.map(f => !ids || ids.has(f.id) ? { ...f, read: true } : f)
    selectedIds.value = []
    stats.value = await agentApi.learning.stats()
    shell.refreshCounts(true)
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '标记失败') } finally { markingRead.value = false }
}

// ---- 问答 / 审核 / 补答 共用表单 ----
const entryDialog = ref(false)
const entryForm = reactive({ mode: 'entry' as 'entry' | 'candidate' | 'gap', id: '', refId: '', knowledgeBaseId: '', question: '', aliasesText: '', answer: '', status: 'published', evidence: [] as Array<Record<string, string>>, materials: [] as MaterialDraft[] })
const entryPicker = ref<InstanceType<typeof MaterialPicker>>()
const savingEntry = ref(false)
const entryDialogTitle = computed(() => ({ entry: entryForm.id ? '编辑问答' : '新建问答', candidate: '审核候选知识', gap: '补充答案' })[entryForm.mode])
const evidenceText = computed(() => entryForm.evidence.map(e => e.context ? `${e.context}\n→ ${e.content}` : e.question ? `用户：${e.question}` : e.content || '').filter(Boolean).join('\n\n'))
function openForm(mode: 'entry' | 'candidate' | 'gap', values: Partial<typeof entryForm>) {
  Object.assign(entryForm, { mode, id: '', refId: '', knowledgeBaseId: current.value?.id || defaultKB.value, question: '', aliasesText: '', answer: '', status: 'published', evidence: [], materials: [] }, values)
  entryDialog.value = true
}
function editEntry(row?: Entry) { openForm('entry', row ? { id: row.id, knowledgeBaseId: row.knowledgeBaseId, question: row.question, aliasesText: (row.aliases || []).join('\n'), answer: row.answer, status: row.status, materials: draftsOf(row.materials) } : {}) }
function review(row: Candidate) { openForm('candidate', { refId: row.id, knowledgeBaseId: candidateKb(row), question: row.question, aliasesText: (row.aliases || []).join('\n'), answer: row.answer, evidence: row.evidence || [], materials: draftsOf(row.materials) }) }
function answerGap(row: Gap) { openForm('gap', { refId: row.id, question: row.question, knowledgeBaseId: kbOf(row) || current.value?.id || defaultKB.value }) }
async function submitEntry() {
  const aliases = entryForm.aliasesText.split('\n').map(s => s.trim()).filter(Boolean)
  if (!entryForm.knowledgeBaseId || !entryForm.question.trim() || !entryForm.answer.trim()) { ElMessage.warning('知识库、问题和答案必填'); return }
  savingEntry.value = true
  try {
    if (entryForm.mode === 'candidate') await agentApi.learning.approve(entryForm.refId, { knowledgeBaseId: entryForm.knowledgeBaseId, question: entryForm.question, aliases, answer: entryForm.answer }, entryForm.materials)
    else if (entryForm.mode === 'gap') await agentApi.learning.answerGap(entryForm.refId, { knowledgeBaseId: entryForm.knowledgeBaseId, question: entryForm.question, aliases, answer: entryForm.answer }, entryForm.materials)
    else if (entryForm.id) await agentApi.learning.updateEntry(entryForm.id, { question: entryForm.question, aliases, answer: entryForm.answer, status: entryForm.status }, entryForm.materials)
    else await agentApi.learning.createEntry({ knowledgeBaseId: entryForm.knowledgeBaseId, question: entryForm.question, aliases, answer: entryForm.answer }, entryForm.materials)
    entryDialog.value = false
    ElMessage.success('已保存，索引完成后生效')
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '保存失败') } finally { savingEntry.value = false }
}
async function quickApprove(row: Candidate) {
  const knowledgeBaseId = candidateKb(row)
  if (!knowledgeBaseId) { review(row); return }
  try {
    await agentApi.learning.approve(row.id, { knowledgeBaseId, question: row.question, aliases: row.aliases || [], answer: row.answer })
    ElMessage.success('已通过，索引完成后机器人就能用上')
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '操作失败') }
}
async function reject(row: Candidate) {
  let value = ''
  try { value = (await ElMessageBox.prompt('驳回原因（选填）', '驳回候选知识', { inputValue: '', confirmButtonText: '驳回', cancelButtonText: '取消' })).value } catch { return }
  try { await agentApi.learning.reject(row.id, value || ''); await reload() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '操作失败') }
}
async function removeEntry(row: Entry) {
  try { await ElMessageBox.confirm('删除这条问答？删除后不能恢复。', '删除问答', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }) } catch { return }
  try { await agentApi.learning.deleteEntry(row.id); await reload() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '删除失败') }
}
async function ignoreGap(row: Gap) { try { await agentApi.learning.ignoreGap(row.id); await reload() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '操作失败') } }

// ---- 新建知识库 ----
const createVisible = ref(false)
const createForm = reactive({ name: '', description: '', embeddingProfileId: '' })
async function createKb() {
  if (!createForm.name.trim() || !createForm.embeddingProfileId) { ElMessage.error('请填写名称并选择 Embedding 模型'); return }
  try {
    const result = await api.knowledge.create(createForm.name, createForm.description, createForm.embeddingProfileId)
    createVisible.value = false
    Object.assign(createForm, { name: '', description: '', embeddingProfileId: embeddingModels.value[0]?.id || '' })
    ElMessage.success('知识库已创建')
    await router.replace({ query: { kb: result.id } })
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '创建失败') }
}

// ---- 整库删除 ----
const removeVisible = ref(false), removing = ref(false), removeConfirm = ref('')
function openRemove() {
  removeConfirm.value = ''
  removeVisible.value = true
  // 绑定关系可能刚变过，打开时取一次最新的
  const id = current.value?.id
  if (id) api.knowledge.get(id).then(d => { if (current.value?.id === id) current.value = d }).catch(() => {})
}
const boundSummary = computed(() => {
  const kb = current.value
  const list = (label: string, items: Array<{ name: string }> = []) => ({ label, total: items.length, names: items.slice(0, 5).map(i => i.name).join('、') + (items.length > 5 ? ' 等' : '') })
  return [list('会话', kb?.boundConversations), list('智能体', kb?.boundAgents)].filter(g => g.total)
})
async function removeKb() {
  const kb = current.value
  if (!kb || removeConfirm.value.trim() !== kb.name || removing.value) return
  removing.value = true
  try {
    // 库已删掉、只是磁盘原件还在后台清理时，后端返回 file_cleanup_pending，按删除成功处理
    await api.knowledge.remove(kb.id).catch(e => { if (!(e instanceof ApiError && e.code === 'file_cleanup_pending')) throw e })
    removeVisible.value = false
    ElMessage.success(`已删除「${kb.name}」`)
    current.value = undefined
    const { kb: _drop, ...rest } = route.query
    await router.replace({ query: rest })
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '删除失败') } finally { removing.value = false }
}

// ---- 导出 / 导入（迁移到另一套同样的系统）----
const exporting = ref(false)
async function exportKb() {
  if (!current.value) return
  exporting.value = true
  try {
    const { blob, filename } = await api.knowledge.exportArchive(current.value.id)
    const a = document.createElement('a'); a.href = URL.createObjectURL(blob); a.download = filename.startsWith('knowledge-') ? `${current.value.name}.zip` : filename; a.click(); URL.revokeObjectURL(a.href)
    ElMessage.success('已导出，在另一套系统的知识库页点“导入知识库”即可')
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '导出失败') } finally { exporting.value = false }
}
const importVisible = ref(false), importing = ref(false), importRatio = ref(0)
const importPicker = ref<HTMLInputElement>()
const importForm = reactive<{ file: File | null; name: string; embeddingProfileId: string }>({ file: null, name: '', embeddingProfileId: '' })
function fileSize(bytes: number) { return bytes >= 1048576 ? `${(bytes / 1048576).toFixed(1)} MB` : `${Math.max(1, Math.round(bytes / 1024))} KB` }
function openImport() {
  Object.assign(importForm, { file: null, name: '', embeddingProfileId: importForm.embeddingProfileId || embeddingModels.value[0]?.id || '' })
  importRatio.value = 0
  importVisible.value = true
}
function pickImport(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file) return
  if (!file.name.toLowerCase().endsWith('.zip')) { ElMessage.error('请选择知识库导出的 .zip 文件'); return }
  if (file.size > 1024 ** 3) { ElMessage.error('文件超过 1 GB，无法导入'); return }
  importForm.file = file
}
async function runImport() {
  if (!importForm.file || !importForm.embeddingProfileId) return
  importing.value = true
  importRatio.value = 0
  try {
    const result = await api.knowledge.importArchive(importForm.file, importForm.embeddingProfileId, importForm.name, ratio => { importRatio.value = ratio })
    importVisible.value = false
    ElMessage.success(`已导入“${result.name}”：${result.documentCount} 篇文档、${result.entryCount} 条问答，正在后台索引`)
    await router.replace({ query: { kb: result.id } })
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '导入失败') } finally { importing.value = false }
}

// ---- 检索测试（针对当前知识库）----
const testVisible = ref(false), testQuery = ref(''), testing = ref(false), testResults = ref<Array<{ id?: string; content: string; score: number; hasImage?: boolean }>>([])

// ---- 更换向量模型（会重建索引）----
const modelSwitchVisible = ref(false), modelSwitchId = ref(''), modelSwitching = ref(false)
function openModelSwitch() { modelSwitchId.value = current.value?.embeddingProfileId || ''; modelSwitchVisible.value = true }
async function switchModel() {
  if (!current.value || !modelSwitchId.value) return
  modelSwitching.value = true
  try { await api.knowledge.changeModel(current.value.id, modelSwitchId.value); modelSwitchVisible.value = false; ElMessage.success('已切换，正在重建索引'); await refreshCurrent() }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '切换失败') } finally { modelSwitching.value = false }
}
async function runTest() {
  if (!current.value || !testQuery.value.trim()) return
  testing.value = true
  try { testResults.value = await api.knowledge.test(current.value.id, testQuery.value) }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '检索失败') } finally { testing.value = false }
}

onMounted(reload)
useAdminEvents(['knowledge_bases', 'knowledge_documents', 'knowledge_entries', 'knowledge_candidates', 'knowledge_gaps', 'message_feedback', 'model_profiles'], reload)
</script>

<style scoped>
.list-empty { padding: 8px 10px; font-size: 13px; }
.count-pill.warn { background: var(--warning); color: #fff; }
.unassigned-switch { align-self: flex-start; margin-bottom: 12px; }
.list-divider { margin: 8px 0 4px; padding: 12px 10px 4px; border-top: 1px solid var(--line); color: var(--faint); font-size: 12px; }
.knowledge-list-item .count-pill { margin-left: 0; animation: pop-in var(--dur) var(--ease); }
.kb-model { font-size: 14px; }
.kb-model-line { display: flex; align-items: baseline; gap: 6px; min-width: 0; }
.kb-model-line .kb-model { min-width: 0; }
.summary-action { flex: none; padding: 0; border: 0; background: none; color: var(--primary-text); font-size: 12px; cursor: pointer; }
.summary-action:hover { text-decoration: underline; }
.switch-note { margin: 0; color: var(--muted); font-size: 13px; line-height: 1.7; }
.import-file { display: flex; align-items: center; gap: 10px; width: 100%; min-width: 0; height: 44px; padding: 0 8px 0 12px; border: 1px dashed var(--line-strong); border-radius: var(--radius-sm); color: var(--muted); font-size: 13px; transition: border-color var(--dur-fast) var(--ease), background var(--dur-fast) var(--ease); }
.import-file > span { flex: 1; min-width: 0; }
.import-file.picked { border-style: solid; background: var(--surface-2); color: var(--text); }
/* 进度行固定占位，开始导入时只淡入，不推动弹窗高度 */
.import-progress { display: flex; align-items: center; gap: 12px; height: 20px; margin-top: 12px; opacity: 0; transition: opacity var(--dur) var(--ease); }
.import-progress.active { opacity: 1; }
.import-progress .progress-line { flex: 1; }
.import-progress span { flex: none; min-width: 120px; color: var(--text-2); font-size: 12px; text-align: right; font-variant-numeric: tabular-nums; }
.hit-image-tag { margin-left: 8px; padding: 1px 6px; border-radius: var(--radius-xs); background: var(--surface-3); color: var(--text-2); font-weight: 500; }
.hit-image { display: block; margin-top: 8px; }
.hit-image img { display: block; max-width: 100%; max-height: 240px; border: 1px solid var(--line); border-radius: var(--radius-sm); object-fit: contain; background: var(--surface-2); animation: rise var(--dur-slow) var(--ease) backwards; }
.kb-tabs { padding: 0 20px 20px; }
.kb-tabs :deep(.el-tabs__header) { margin-bottom: 12px; }
.kb-toolbar { margin-bottom: 12px; }
.kb-toolbar > .el-input { width: 220px; }
.kb-toolbar > .el-select { width: 132px; }
.tag-line { overflow: visible !important; }
.knowledge-detail > .kb-toolbar { padding: 0 20px; }
.knowledge-detail > .el-table { border-top: 1px solid var(--line); }
.kb-hint { font-size: 13px; }
.drawer-hint { margin-bottom: 16px; font-size: 13px; }
.evidence { max-height: 220px; overflow: auto; font-size: 13px; }
.unread-dot { display: inline-flex; align-items: center; gap: 6px; padding: 2px 0; border: 0; background: none; color: var(--primary-text); font: inherit; font-size: 13px; font-weight: 600; cursor: pointer; }
.unread-dot i { width: 7px; height: 7px; border-radius: 50%; background: var(--primary); }
.unread-dot:hover { text-decoration: underline; }
:deep(.is-unread .qa strong) { font-weight: 600; }
.qa-row { display: flex; align-items: flex-start; gap: 12px; }
.qa-row .qa { flex: 1; min-width: 0; }
.qa-thumb { flex: none; display: block; width: 48px; height: 48px; margin: 2px 0; border: 1px solid var(--line); border-radius: var(--radius-xs); overflow: hidden; background: var(--surface-3); }
.qa-thumb { position: relative; }
.qa-thumb video { display: block; width: 100%; height: 100%; object-fit: cover; }
.qa-thumb-count { position: absolute; right: 2px; bottom: 2px; min-width: 16px; padding: 0 4px; box-sizing: border-box; border-radius: 8px; background: rgb(26 28 31 / .72); color: #fff; font-size: 12px; line-height: 16px; text-align: center; font-variant-numeric: tabular-nums; }
.qa-thumb img { display: block; width: 100%; height: 100%; object-fit: cover; transition: transform var(--dur) var(--ease); }
.qa-thumb:hover img { transform: scale(1.06); }
.faint { font-size: 13px; }
</style>
