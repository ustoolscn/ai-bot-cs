<template>
  <div class="kb-universe-page">
    <!-- 1. 全屏底层：Three.js 3D 知识宇宙引擎 -->
    <KnowledgeUniverse3D
      v-if="current && mapData"
      ref="universeRef"
      :map-data="mapData"
      :mode="atlasMode"
      :color-mode="cityColorMode"
      :selection="inspectorSelection"
      :search-query="activeProbeQuery"
      :search-hits="searchHits"
      :cluster-filter="clusterFilter"
      :auto-rotate="autoRotate"
      :unread-feedback-count="unreadByKb[current.id] || 0"
      @select="onSelectNode"
      @navigate-drawer="openDrawerTab"
    />
    <div v-else class="kb-empty-backdrop">
      <div class="kb-empty-card">
        <div class="empty-pulse" />
        <h2>还没有知识库</h2>
        <p>创建一个知识库，开始构建你的 3D 语义星云、知识图谱与自动学习问答体系。</p>
        <div class="empty-actions">
          <el-button type="primary" :icon="Plus" @click="createVisible = true">新建知识库</el-button>
          <el-button :icon="Upload" @click="openImport">导入备份 (.zip)</el-button>
        </div>
      </div>
    </div>

    <!-- 2. 左上角 HUD 浮岛：知识库选择器 + 实时指标微条 -->
    <div class="hud-top-left">
      <div class="hud-card kb-switcher">
        <el-popover v-model:visible="kbPickerOpen" placement="bottom-start" :width="340" trigger="click" popper-class="kb-picker-popper">
          <template #reference>
            <button type="button" class="kb-pick-btn">
              <span class="kb-pick-dot" />
              <span class="kb-pick-name">{{ sharedView ? '未归属问题与反馈' : (current?.name || '选择知识库') }}</span>
              <em v-if="!sharedView && current && todoByKb(current.id)" class="count-pill hot">{{ todoByKb(current.id) }}</em>
              <el-icon class="kb-pick-arrow"><ArrowDown /></el-icon>
            </button>
          </template>
          <div class="kb-pick-pop">
            <el-input v-model="search" placeholder="搜索知识库…" :prefix-icon="Search" clearable size="small" />
            <div class="kb-pick-list">
              <button
                v-for="kb in filteredBases"
                :key="kb.id"
                type="button"
                class="kb-pick-item"
                :class="{ active: !sharedView && current?.id === kb.id }"
                @click="openKb(kb.id); kbPickerOpen = false"
              >
                <div>
                  <strong>{{ kb.name }}</strong>
                  <small>{{ kb.documents }} 文档 · {{ kb.entries ?? 0 }} 问答 · {{ kb.chunks }} 块</small>
                </div>
                <em v-if="todoByKb(kb.id)" class="count-pill hot">{{ todoByKb(kb.id) }}</em>
              </button>
              <button
                v-if="unassignedGaps.length || unassignedFeedback.length"
                type="button"
                class="kb-pick-item is-unassigned"
                :class="{ active: !!sharedView }"
                @click="openShared(unassignedGaps.length ? 'gaps' : 'feedback'); kbPickerOpen = false"
              >
                <div>
                  <strong>未归属池</strong>
                  <small>{{ unassignedGaps.length }} 缺口 · {{ unassignedFeedback.length }} 反馈</small>
                </div>
                <em v-if="unassignedUnread" class="count-pill hot">{{ unassignedUnread }}</em>
              </button>
            </div>
            <div class="kb-pick-foot">
              <el-button size="small" type="primary" :icon="Plus" @click="kbPickerOpen = false; createVisible = true">新建知识库</el-button>
              <el-button size="small" :icon="Upload" @click="kbPickerOpen = false; openImport()">导入</el-button>
            </div>
          </div>
        </el-popover>

        <div class="kb-top-actions">
          <el-tooltip content="刷新全景数据" placement="bottom"><button type="button" class="hud-icon-btn" @click="reload"><el-icon><Refresh /></el-icon></button></el-tooltip>
          <el-tooltip v-if="current" content="导出备份" placement="bottom"><button type="button" class="hud-icon-btn" :disabled="exporting" @click="exportKb"><el-icon><Download /></el-icon></button></el-tooltip>
          <el-tooltip v-if="current" content="删除知识库" placement="bottom"><button type="button" class="hud-icon-btn danger" @click="openRemove"><el-icon><Delete /></el-icon></button></el-tooltip>
        </div>
      </div>

      <div v-if="current" class="hud-card kb-stats-strip">
        <span><b>{{ current.documents }}</b> 文档</span>
        <span><b>{{ current.entries ?? 0 }}</b> 问答</span>
        <span><b>{{ current.chunks.toLocaleString() }}</b> 切片</span>
        <span><b>{{ current.progress }}%</b> 索引</span>
        <button type="button" class="model-link" :title="`点击更换向量模型：${current.model}`" @click="openModelSwitch">{{ current.model }}</button>
      </div>
    </div>

    <!-- 3. 顶部居中 HUD 语义检索探针舱 -->
    <div v-if="current" class="hud-top-center">
      <div class="hud-card probe-capsule">
        <el-icon class="probe-ico"><Search /></el-icon>
        <input
          v-model="testQuery"
          type="text"
          class="probe-input"
          placeholder="输入问题发射 3D 语义检索探针，实时聚焦命中星团…"
          @keydown.enter.prevent="runTest"
        >
        <button v-if="testQuery || searchHits.length" type="button" class="probe-clear" @click="clearProbe">清除</button>
        <button type="button" class="probe-go" :disabled="testing" @click="runTest">{{ testing ? '检索中…' : '发射探针' }}</button>
      </div>
    </div>

    <!-- 4. 右上角全景面板快速开关 -->
    <div v-if="current" class="hud-top-right">
      <button type="button" class="hud-card hud-toggle-btn" @click="inspectorOpen = !inspectorOpen">
        <span>{{ inspectorOpen ? '收起透视' : '全景透视' }}</span>
        <em v-if="searchHits.length" class="hit-pill">命中 {{ searchHits.length }}</em>
      </button>
    </div>

    <!-- 5. 右侧 HUD 节点透视浮舱 -->
    <Transition name="hud-slide-right">
      <div v-if="current && inspectorOpen && mapData" class="hud-right-inspector">
        <KnowledgeInspector
          :kb-id="current.id"
          :map-data="mapData"
          :selection="inspectorSelection"
          :search-hits="searchHits"
          :unread-feedback-count="unreadByKb[current.id] || 0"
          @select="onSelectNode"
          @close="inspectorOpen = false"
          @focus-cluster="focusCluster3D"
          @navigate-tab="openDrawerTab"
          @retry-doc="retryDocById"
          @delete-doc-index="deleteDocIndexById"
          @delete-doc="deleteDocById"
          @edit-entry="editEntryById"
          @delete-entry="deleteEntryById"
          @quick-approve="quickApproveById"
          @review-candidate="reviewCandidateById"
          @reject-candidate="rejectCandidateById"
          @answer-gap="answerGapById"
          @ignore-gap="ignoreGapById"
        />
      </div>
    </Transition>

    <!-- 6. 底部居中 HUD 悬浮 Dock 控制台 -->
    <div v-if="current" class="hud-bottom-dock">
      <div class="hud-card dock-bar">
        <div class="dock-seg">
          <button type="button" class="dock-btn" :class="{ active: atlasMode === 'galaxy' }" @click="atlasMode = 'galaxy'">3D 星系</button>
          <button type="button" class="dock-btn" :class="{ active: atlasMode === 'semantic' }" @click="atlasMode = 'semantic'">语义星云</button>
          <button type="button" class="dock-btn" :class="{ active: atlasMode === 'city' }" @click="atlasMode = 'city'">全息矩阵</button>
        </div>

        <template v-if="atlasMode === 'city'">
          <span class="dock-sep" />
          <div class="dock-seg">
            <button type="button" class="dock-btn" :class="{ active: cityColorMode === 'structure' }" @click="cityColorMode = 'structure'">结构</button>
            <button type="button" class="dock-btn" :class="{ active: cityColorMode === 'heat' }" @click="cityColorMode = 'heat'">热度</button>
          </div>
        </template>

        <span class="dock-sep" />
        <div class="dock-seg">
          <button type="button" class="dock-btn" :class="{ active: autoRotate }" @click="autoRotate = !autoRotate">{{ autoRotate ? '停止旋转' : '自动巡航' }}</button>
          <button type="button" class="dock-btn" @click="universeRef?.resetCamera()">重置视角</button>
        </div>

        <span class="dock-sep" />
        <div class="dock-seg dock-actions">
          <el-upload :show-file-list="false" accept=".txt,.md,.pdf,.docx,.png,.jpg,.jpeg,.webp" :before-upload="uploadQuick">
            <button type="button" class="dock-btn accent">+ 上传文档</button>
          </el-upload>
          <button type="button" class="dock-btn accent" @click="editEntry()">+ 新建问答</button>
        </div>

        <span class="dock-sep" />
        <div class="dock-seg">
          <button type="button" class="dock-btn" :class="{ active: drawerOpen && drawerTab === 'documents' }" @click="openDrawerTab('documents')">文档</button>
          <button type="button" class="dock-btn" :class="{ active: drawerOpen && drawerTab === 'entries' }" @click="openDrawerTab('entries')">问答</button>
          <button type="button" class="dock-btn" :class="{ active: drawerOpen && drawerTab === 'candidates' }" @click="openDrawerTab('candidates')">
            待审核<em v-if="pendingByKb[current.id]" class="dock-badge">{{ pendingByKb[current.id] }}</em>
          </button>
          <button type="button" class="dock-btn" :class="{ active: drawerOpen && drawerTab === 'gaps' }" @click="openDrawerTab('gaps')">
            没答上<em v-if="gapsByKb[current.id]" class="dock-badge warn">{{ gapsByKb[current.id] }}</em>
          </button>
          <button type="button" class="dock-btn" :class="{ active: drawerOpen && drawerTab === 'feedback' }" @click="openDrawerTab('feedback')">
            反馈<em v-if="unreadByKb[current.id]" class="dock-badge">{{ unreadByKb[current.id] }}</em>
          </button>
        </div>
      </div>
    </div>

    <!-- 7. 全息管理抽屉（按需滑出，承载 100% 明细管理表格与未归属池） -->
    <el-drawer
      v-model="drawerOpen"
      :title="drawerTitle"
      size="min(920px, 94vw)"
      append-to-body
      @closed="onDrawerClosed"
    >
      <template v-if="sharedView">
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
      </template>

      <template v-else-if="current">
        <div class="drawer-tab-switch">
          <el-segmented :model-value="drawerTab" :options="drawerTabOptions" @update:model-value="(v: string | number | boolean) => openDrawerTab(v as DrawerTab)" />
        </div>

        <KnowledgeDocuments v-if="drawerTab === 'documents'" :base="current" @changed="refreshCurrent" @inspect="openDocChunkDrawer" />

        <template v-else-if="drawerTab === 'entries'">
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

        <template v-else-if="drawerTab === 'candidates'">
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

        <template v-else-if="drawerTab === 'gaps'">
          <div class="toolbar kb-toolbar"><span class="spacer" /><el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('gaps', '没答上的问题')">删除选中的 {{ selectedIds.length }} 条</el-button></div>
          <el-table row-key="id" v-loading="loading" :data="kbGaps" empty-text="这个知识库没有没答上的问题" @selection-change="selectRows">
            <el-table-column type="selection" width="48" />
            <el-table-column prop="question" label="用户问了但没答上的问题" min-width="260" />
            <el-table-column prop="occurrences" label="出现次数" width="104" sortable align="right" />
            <el-table-column label="最近出现" width="140"><template #default="{ row }">{{ formatTime(row.lastSeenAt) }}</template></el-table-column>
            <el-table-column label="操作" width="150" fixed="right"><template #default="{ row }"><el-button link type="primary" @click="answerGap(row)">补充答案</el-button><el-button link @click="ignoreGap(row)">忽略</el-button></template></el-table-column>
          </el-table>
        </template>

        <template v-else-if="drawerTab === 'feedback'">
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
      </template>
    </el-drawer>

    <!-- 各种弹窗（问答、新建、删除、导入、切模型、文档切片） -->
    <el-dialog v-model="entryDialog" :title="entryDialogTitle" width="680px" append-to-body>
      <div class="dlg-tabs">
        <el-segmented
          v-model="entryDialogTab"
          :options="[
            { label: '问答正文', value: 'content' },
            { label: '演示素材与来源', value: 'media' },
          ]"
        />
      </div>
      <el-form label-position="top">
        <div v-show="entryDialogTab === 'content'" class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>基础归属与问题</strong><span>匹配标准问法与相近问法</span></div>
            <el-form-item label="存入知识库"><el-select v-model="entryForm.knowledgeBaseId" :disabled="!!entryForm.id"><el-option v-for="kb in bases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></el-form-item>
            <el-form-item label="标准问题"><el-input v-model="entryForm.question" /></el-form-item>
            <el-form-item label="相似问法（每行一个）"><el-input v-model="entryForm.aliasesText" type="textarea" :rows="3" /></el-form-item>
          </div>
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>标准回复与状态</strong><span>支持直接粘贴截图到演示素材 Tab</span></div>
            <el-form-item label="答案（建议口语化、简洁）"><el-input v-model="entryForm.answer" type="textarea" :rows="6" @paste="entryPicker?.fromClipboard($event)" /></el-form-item>
            <el-form-item v-if="entryForm.id" label="状态"><el-radio-group v-model="entryForm.status"><el-radio-button value="published">发布</el-radio-button><el-radio-button value="review">需复核</el-radio-button><el-radio-button value="archived">归档</el-radio-button></el-radio-group></el-form-item>
          </div>
        </div>
        <div v-show="entryDialogTab === 'media'" class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>演示素材（选填）</strong><span>回答后依次附带发送的截图或录屏</span></div>
            <el-form-item><MaterialPicker ref="entryPicker" v-model="entryForm.materials" hint="用户问到这条时，AI 先发答案，再按顺序发这些截图或录屏。拖动调整先后，可直接粘贴截图" /></el-form-item>
          </div>
          <div v-if="entryForm.mode === 'candidate' && evidenceText" class="dlg-section">
            <div class="dlg-section-head"><strong>来源对话证据</strong><span>提炼该候选知识的群聊上下文</span></div>
            <div class="content-box evidence">{{ evidenceText }}</div>
          </div>
        </div>
      </el-form>
      <template #footer><el-button @click="entryDialog = false">取消</el-button><el-button type="primary" :loading="savingEntry" @click="submitEntry">{{ entryForm.mode === 'candidate' ? '通过并发布' : entryForm.mode === 'gap' ? '保存答案' : '保存问答' }}</el-button></template>
    </el-dialog>

    <el-dialog v-model="removeVisible" title="删除知识库" width="500px" append-to-body :close-on-click-modal="!removing">
      <div v-if="current" class="kb-remove dlg-stack">
        <div class="dlg-section is-danger">
          <div class="dlg-section-head"><strong>高危操作确认</strong><span>此操作不可撤回</span></div>
          <p>将永久删除「<strong>{{ current.name }}</strong>」里的 {{ current.documents }} 篇文档和 {{ current.entries ?? 0 }} 条问答，以及它的待审核和没答上记录。需要留底请先导出。</p>
          <div v-if="boundSummary.length" class="kb-remove-bound">
            <span>正在使用它的地方会自动解绑</span>
            <p v-for="group in boundSummary" :key="group.label"><em>{{ group.label }} {{ group.total }}</em>{{ group.names }}</p>
          </div>
        </div>
        <div class="dlg-section">
          <el-form label-position="top" @submit.prevent="removeKb">
            <el-form-item label="输入知识库名称确认"><el-input v-model="removeConfirm" :placeholder="current.name" :disabled="removing" /></el-form-item>
          </el-form>
        </div>
      </div>
      <template #footer><el-button :disabled="removing" @click="removeVisible = false">取消</el-button><el-button type="danger" :loading="removing" :disabled="removeConfirm.trim() !== current?.name" @click="removeKb">永久删除</el-button></template>
    </el-dialog>

    <el-dialog v-model="createVisible" title="新建知识库" width="540px" append-to-body>
      <el-form label-position="top">
        <div class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>知识库信息</strong><span>名称与适用业务范围说明</span></div>
            <el-form-item label="知识库名称"><el-input v-model="createForm.name" /></el-form-item>
            <el-form-item label="说明"><el-input v-model="createForm.description" type="textarea" :rows="3" /></el-form-item>
          </div>
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>向量索引引擎</strong><span>决定文本与图片语义检索能力</span></div>
            <el-form-item label="Embedding 模型"><el-select v-model="createForm.embeddingProfileId"><el-option v-for="model in embeddingModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item>
          </div>
        </div>
      </el-form>
      <template #footer><el-button @click="createVisible = false">取消</el-button><el-button type="primary" @click="createKb">创建知识库</el-button></template>
    </el-dialog>

    <el-dialog v-model="importVisible" title="导入知识库" width="540px" append-to-body :close-on-click-modal="!importing" :close-on-press-escape="!importing" :show-close="!importing">
      <el-form label-position="top">
        <div class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>选择导出包</strong><span>上传 .zip 备份不会覆盖现有库</span></div>
            <el-form-item label="导出文件">
              <div class="import-file" :class="{ picked: importForm.file }">
                <el-icon><DocumentIcon /></el-icon>
                <span class="truncate">{{ importForm.file ? `${importForm.file.name} · ${fileSize(importForm.file.size)}` : '选择从另一套系统导出的 .zip 文件' }}</span>
                <el-button size="small" :disabled="importing" @click="importPicker?.click()">{{ importForm.file ? '重新选择' : '选择文件' }}</el-button>
                <input ref="importPicker" type="file" accept=".zip,application/zip" hidden @change="pickImport">
              </div>
            </el-form-item>
          </div>
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>目标知识库设置</strong><span>导入后在后台按所选模型重建向量索引</span></div>
            <el-form-item label="知识库名称"><el-input v-model="importForm.name" :disabled="importing" placeholder="留空则沿用文件里的名称" /></el-form-item>
            <el-form-item label="Embedding 模型"><el-select v-model="importForm.embeddingProfileId" :disabled="importing"><el-option v-for="model in embeddingModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item>
          </div>
        </div>
      </el-form>
      <div class="import-progress" :class="{ active: importing }" aria-live="polite" style="margin-top: 12px">
        <div class="progress-line" :class="{ 'is-running': importing && importRatio >= 1 }"><i :style="{ width: Math.round(importRatio * 100) + '%' }" /></div>
        <span>{{ !importing ? '' : importRatio < 1 ? `正在上传 ${Math.round(importRatio * 100)}%` : '上传完成，正在导入…' }}</span>
      </div>
      <template #footer><el-button :disabled="importing" @click="importVisible = false">取消</el-button><el-button type="primary" :loading="importing" :disabled="!importForm.file || !importForm.embeddingProfileId" @click="runImport">导入</el-button></template>
    </el-dialog>

    <el-drawer v-model="chunkDrawerOpen" :title="`文档切片预览 · ${chunkDrawerDoc?.name || ''}`" size="560px" append-to-body>
      <div v-loading="chunkDrawerLoading" class="test-results">
        <div v-for="ch in chunkDrawerList" :key="ch.id">
          <strong>切片 #{{ ch.chunkIndex + 1 }}<span v-if="ch.hasImage" class="hit-image-tag">带截图</span></strong>
          <p>{{ ch.content }}</p>
          <a v-if="ch.hasImage && ch.imageUrl" :href="ch.imageUrl" target="_blank" rel="noopener" class="hit-image"><img :src="ch.imageUrl" alt="切片截图" loading="lazy"></a>
        </div>
        <p v-if="!chunkDrawerLoading && !chunkDrawerList.length" class="muted">该文档还没有切片数据。</p>
      </div>
    </el-drawer>

    <el-dialog v-model="modelSwitchVisible" title="更换向量模型" width="500px" append-to-body>
      <div class="dlg-stack">
        <div class="dlg-section">
          <div class="dlg-section-head"><strong>目标 Embedding 模型</strong><span>支持切换不同维度向量</span></div>
          <el-form label-position="top">
            <el-form-item label="Embedding 模型"><el-select v-model="modelSwitchId"><el-option v-for="model in embeddingModels" :key="model.id" :label="`${model.name} · ${model.model}`" :value="model.id" /></el-select></el-form-item>
          </el-form>
        </div>
        <div class="dlg-tip-box">切换后会用新模型重新处理这个知识库的全部文档和问答，不用重新上传。处理完成前仅可检索已完成部分。</div>
      </div>
      <template #footer><el-button @click="modelSwitchVisible = false">取消</el-button><el-button type="primary" :loading="modelSwitching" :disabled="!modelSwitchId || modelSwitchId === current?.embeddingProfileId" @click="switchModel">切换并重建</el-button></template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useShellStore } from '@/stores/shell'
import { ArrowDown, ChatLineSquare, Delete, Document as DocumentIcon, Download, Finished, Plus, Refresh, Search, Upload } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import MaterialPicker from '@/components/MaterialPicker.vue'
import KnowledgeUniverse3D from '@/components/three/KnowledgeUniverse3D.vue'
import KnowledgeInspector from '@/components/knowledge/KnowledgeInspector.vue'
import { api, sanitizeServiceError } from '@/api'
import { ApiError } from '@/api/client'
import { useBulkDelete } from '@/composables/useBulkDelete'
import KnowledgeDocuments from './KnowledgeDocuments.vue'
import { agentApi, feedbackSourceText, formatTime, ratingText, sourceText, draftsOf, type Candidate, type Entry, type MaterialDraft, type Feedback, type Gap, type LearningStats } from '@/api/agent'
import type { KnowledgeBase, KnowledgeDocument, KnowledgeDocumentChunk, KnowledgeInspectorSelection, KnowledgeMapData, KnowledgeSearchHit, ModelProfile } from '@/types'

type DrawerTab = 'documents' | 'entries' | 'candidates' | 'gaps' | 'feedback'

const route = useRoute()
const router = useRouter()
const shell = useShellStore()

const drawerTabs: DrawerTab[] = ['documents', 'entries', 'candidates', 'gaps', 'feedback']
const queryTab = computed(() => String(route.query.tab || ''))
const sharedView = computed(() => (route.query.scope === 'unassigned' ? (queryTab.value === 'feedback' ? 'feedback' : 'gaps') : '') as '' | 'gaps' | 'feedback')
const drawerTab = computed<DrawerTab>(() => (drawerTabs.includes(queryTab.value as DrawerTab) ? (queryTab.value as DrawerTab) : 'documents'))
const queryKb = computed(() => typeof route.query.kb === 'string' ? route.query.kb : '')

const drawerOpen = ref(false)
const kbPickerOpen = ref(false)
const universeRef = ref<InstanceType<typeof KnowledgeUniverse3D>>()

const bases = ref<KnowledgeBase[]>([])
const current = ref<KnowledgeBase>()
const models = ref<ModelProfile[]>([])
const defaultKB = ref('')
const stats = ref<LearningStats | null>(null)
const pendingCandidates = ref<Candidate[]>([])
const search = ref('')
const filteredBases = computed(() => bases.value.filter(k => !search.value || `${k.name}${k.description}`.includes(search.value)))
const embeddingModels = computed(() => models.value.filter(m => m.kind === 'embedding'))

// 3D 全景状态
const atlasMode = ref<'galaxy' | 'semantic' | 'city'>('galaxy')
const cityColorMode = ref<'structure' | 'heat'>('structure')
const autoRotate = ref(true)
const mapData = ref<KnowledgeMapData | null>(null)
const inspectorOpen = ref(false)
const inspectorSelection = ref<KnowledgeInspectorSelection>(null)
const clusterFilter = ref<number | null>(null)

function onSelectNode(sel: KnowledgeInspectorSelection) {
  inspectorSelection.value = sel
  inspectorOpen.value = sel !== null || searchHits.value.length > 0
}
function focusCluster3D(clusterId: number) {
  clusterFilter.value = clusterId
  atlasMode.value = 'semantic'
  setTimeout(() => universeRef.value?.focusCluster(clusterId), 80)
}

// 缺口与反馈归属
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
function candidateKb(c: Candidate) {
  if (c.knowledgeBaseId) return c.knowledgeBaseId
  if (c.knowledgeBaseName) return bases.value.find(b => b.name === c.knowledgeBaseName)?.id || defaultKB.value
  return defaultKB.value
}
const pendingByKb = computed(() => pendingCandidates.value.reduce<Record<string, number>>((acc, c) => { const id = candidateKb(c); acc[id] = (acc[id] || 0) + 1; return acc }, {}))

function openKb(id: string) {
  inspectorSelection.value = null
  inspectorOpen.value = false
  clusterFilter.value = null
  drawerOpen.value = false
  clearProbe()
  router.replace({ query: { kb: id } })
}
function openShared(view: 'gaps' | 'feedback') {
  drawerOpen.value = true
  router.replace({ query: { scope: 'unassigned', tab: view } })
}
function openDrawerTab(tab: DrawerTab) {
  if (!current.value) return
  drawerOpen.value = true
  router.replace({ query: { kb: current.value.id, tab } })
}
function onDrawerClosed() {
  if (sharedView.value) {
    if (current.value) router.replace({ query: { kb: current.value.id } })
    else if (bases.value[0]) router.replace({ query: { kb: bases.value[0].id } })
    else router.replace({ query: {} })
  } else if (current.value && route.query.tab) {
    router.replace({ query: { kb: current.value.id } })
  }
}

const drawerTabOptions = computed(() => {
  const id = current.value?.id || ''
  return [
    { label: `文档 (${current.value?.documents ?? 0})`, value: 'documents' },
    { label: `问答 (${current.value?.entries ?? 0})`, value: 'entries' },
    { label: `待审核 (${pendingByKb.value[id] || 0})`, value: 'candidates' },
    { label: `没答上 (${gapsByKb.value[id] || 0})`, value: 'gaps' },
    { label: `反馈 (${unreadByKb.value[id] || 0})`, value: 'feedback' },
  ]
})
const drawerTitle = computed(() => sharedView.value ? '未归属问题与反馈' : `${current.value?.name || '知识库'} · 明细管理`)

// 列表数据
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

async function loadMapData(kbId: string) {
  try { mapData.value = await api.knowledge.map(kbId) }
  catch { /* 保留现有或由下次刷新加载 */ }
}

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

async function syncSelection(fresh = false) {
  if (sharedView.value) {
    drawerOpen.value = true
    await loadPane()
    return
  }
  if (drawerTabs.includes(queryTab.value as DrawerTab)) {
    drawerOpen.value = true
  }
  const busiest = (counts: Record<string, number>) => Object.entries(counts).filter(([id]) => id).sort((a, b) => b[1] - a[1])[0]?.[0] || ''
  if (!queryKb.value && queryTab.value === 'gaps' && !busiest(gapsByKb.value) && unassignedGaps.value.length) { openShared('gaps'); return }
  if (!queryKb.value && queryTab.value === 'feedback' && !busiest(unreadByKb.value) && unassignedUnread.value) { openShared('feedback'); return }
  const wanted = queryKb.value || current.value?.id
    || (queryTab.value === 'candidates' ? busiest(pendingByKb.value) : queryTab.value === 'gaps' ? busiest(gapsByKb.value) : queryTab.value === 'feedback' ? busiest(unreadByKb.value) : '')
    || bases.value[0]?.id || ''
  const base = bases.value.find(b => b.id === wanted) || bases.value[0]
  if (!base) { current.value = undefined; mapData.value = null; return }
  if (queryKb.value !== base.id) { router.replace({ query: { ...route.query, kb: base.id } }); return }
  if (fresh || current.value?.id !== base.id || !current.value.documentList) {
    try { current.value = await api.knowledge.get(base.id) } catch { current.value = base }
  }
  if (current.value) await loadMapData(current.value.id)
  if (drawerOpen.value) await loadPane()
}

async function refreshCurrent() {
  if (!current.value) return
  const [kbs, detail] = await Promise.all([api.knowledge.list(), api.knowledge.get(current.value.id).catch(() => current.value)])
  bases.value = kbs
  current.value = detail
  if (current.value) await loadMapData(current.value.id)
}

async function loadPane() {
  selectedIds.value = []
  loading.value = true
  try {
    if (sharedView.value === 'gaps' || (drawerOpen.value && drawerTab.value === 'gaps')) gaps.value = await agentApi.learning.gaps()
    else if (sharedView.value === 'feedback' || (drawerOpen.value && drawerTab.value === 'feedback')) feedback.value = await agentApi.learning.feedback()
    else if (current.value && drawerOpen.value && drawerTab.value === 'entries') entries.value = await agentApi.learning.entries({ knowledgeBaseId: current.value.id, status: entryStatus.value, q: entryQuery.value || undefined })
    else if (current.value && drawerOpen.value && drawerTab.value === 'candidates') candidates.value = await agentApi.learning.candidates(candidateStatus.value, candidateConversation.value || undefined)
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '加载失败')
  } finally { loading.value = false }
}
watch([queryKb, queryTab, candidateConversation], () => { if (bases.value.length || sharedView.value) syncSelection() })
watch(feedbackView, () => { selectedIds.value = [] })

function entryStatusText(row: Entry) {
  if (row.status === 'review') return '需复核'
  if (row.status === 'archived') return '已归档'
  return ({ ready: '已生效', indexed: '已生效', failed: '索引失败', pending: '索引中' } as Record<string, string>)[row.indexStatus] || row.indexStatus
}

// 快捷上传文档
async function uploadQuick(file: File) {
  if (!current.value) return false
  const acceptExts = ['.txt', '.md', '.pdf', '.docx', '.png', '.jpg', '.jpeg', '.webp']
  const ext = file.name.split('.').pop()?.toLowerCase() || ''
  if (!acceptExts.includes(`.${ext}`)) { ElMessage.error(ext === 'doc' ? '旧版 .doc 请另存为 .docx 后上传' : '仅支持 TXT、MD、PDF、DOCX、PNG、JPG、WEBP'); return false }
  if (file.size > 10 * 1024 * 1024) { ElMessage.error('单文件不能超过 10 MB'); return false }
  if (!/gemini-embedding/i.test(current.value.model) && ['png', 'jpg', 'jpeg', 'webp'].includes(ext)) {
    ElMessage.error('当前向量模型不支持图片，请先更换为图文向量模型'); return false
  }
  try {
    await api.knowledge.upload(current.value.id, file)
    ElMessage.success('已上传，正在 3D 宇宙中生成索引')
    await refreshCurrent()
  } catch (error) {
    ElMessage.error(sanitizeServiceError(error instanceof Error ? error.message : '上传失败') || '上传失败')
  }
  return false
}

// 透视面板回调动作
async function retryDocById(docId: string) {
  if (!current.value) return
  await api.knowledge.retryDocument(current.value.id, docId)
  ElMessage.success('文档已重新加入索引队列')
  await refreshCurrent()
}
async function deleteDocIndexById(docId: string, name: string, chunksCount: number) {
  if (!current.value) return
  try { await ElMessageBox.confirm(`只删除“${name}”生成的 ${chunksCount} 个向量分块，保留原文件？`, '删除索引', { type: 'warning', confirmButtonText: '删除索引', cancelButtonText: '取消' }) } catch { return }
  await api.knowledge.deleteDocumentIndex(current.value.id, docId)
  ElMessage.success('索引已删除，原文件仍保留')
  await refreshCurrent()
}
async function deleteDocById(docId: string, name: string) {
  if (!current.value) return
  try { await ElMessageBox.confirm(`删除“${name}”及其全部索引？此操作不可恢复。`, '删除文件', { type: 'warning', confirmButtonText: '删除文件', cancelButtonText: '取消' }) } catch { return }
  await api.knowledge.deleteDocument(current.value.id, docId)
  inspectorSelection.value = null
  ElMessage.success('文件及索引已删除')
  await refreshCurrent()
}

async function editEntryById(entryId: string) {
  if (!current.value) return
  let row = entries.value.find(e => e.id === entryId)
  if (!row) {
    const list = await agentApi.learning.entries({ knowledgeBaseId: current.value.id, status: 'all' }).catch(() => [])
    row = list.find(e => e.id === entryId)
  }
  if (row) editEntry(row)
}
async function deleteEntryById(entryId: string) {
  try { await ElMessageBox.confirm('删除这条问答？删除后不能恢复。', '删除问答', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }) } catch { return }
  await agentApi.learning.deleteEntry(entryId)
  inspectorSelection.value = null
  await reload()
}
async function quickApproveById(candId: string) {
  const row = pendingCandidates.value.find(c => c.id === candId)
  if (row) { await quickApprove(row); inspectorSelection.value = null }
}
function reviewCandidateById(candId: string) {
  const row = pendingCandidates.value.find(c => c.id === candId)
  if (row) review(row)
}
async function rejectCandidateById(candId: string) {
  const row = pendingCandidates.value.find(c => c.id === candId)
  if (row) { await reject(row); inspectorSelection.value = null }
}
function answerGapById(gapId: string) {
  const row = gaps.value.find(g => g.id === gapId)
  if (row) answerGap(row)
}
async function ignoreGapById(gapId: string) {
  const row = gaps.value.find(g => g.id === gapId)
  if (row) { await ignoreGap(row); inspectorSelection.value = null }
}

// 文档切片抽屉
const chunkDrawerOpen = ref(false), chunkDrawerLoading = ref(false), chunkDrawerDoc = ref<KnowledgeDocument | null>(null), chunkDrawerList = ref<KnowledgeDocumentChunk[]>([])
async function openDocChunkDrawer(doc: KnowledgeDocument) {
  if (!current.value) return
  chunkDrawerDoc.value = doc
  chunkDrawerOpen.value = true
  chunkDrawerLoading.value = true
  try { chunkDrawerList.value = await api.knowledge.documentChunks(current.value.id, doc.id) }
  catch { chunkDrawerList.value = [] }
  finally { chunkDrawerLoading.value = false }
}

// 反馈已读
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

// 问答 / 审核 / 补答 共用表单
const entryDialog = ref(false)
const entryDialogTab = ref<'content' | 'media'>('content')
const entryForm = reactive({ mode: 'entry' as 'entry' | 'candidate' | 'gap', id: '', refId: '', knowledgeBaseId: '', question: '', aliasesText: '', answer: '', status: 'published', evidence: [] as Array<Record<string, string>>, materials: [] as MaterialDraft[] })
const entryPicker = ref<InstanceType<typeof MaterialPicker>>()
const savingEntry = ref(false)
const entryDialogTitle = computed(() => ({ entry: entryForm.id ? '编辑问答' : '新建问答', candidate: '审核候选知识', gap: '补充答案' })[entryForm.mode])
const evidenceText = computed(() => entryForm.evidence.map(e => e.context ? `${e.context}\n→ ${e.content}` : e.question ? `用户：${e.question}` : e.content || '').filter(Boolean).join('\n\n'))
function openForm(mode: 'entry' | 'candidate' | 'gap', values: Partial<typeof entryForm>) {
  Object.assign(entryForm, { mode, id: '', refId: '', knowledgeBaseId: current.value?.id || defaultKB.value, question: '', aliasesText: '', answer: '', status: 'published', evidence: [], materials: [] }, values)
  entryDialogTab.value = 'content'
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

// 新建与删除知识库
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

const removeVisible = ref(false), removing = ref(false), removeConfirm = ref('')
function openRemove() {
  removeConfirm.value = ''
  removeVisible.value = true
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
    await api.knowledge.remove(kb.id).catch(e => { if (!(e instanceof ApiError && e.code === 'file_cleanup_pending')) throw e })
    removeVisible.value = false
    ElMessage.success(`已删除「${kb.name}」`)
    current.value = undefined
    mapData.value = null
    const { kb: _drop, ...rest } = route.query
    await router.replace({ query: rest })
    await reload()
  } catch (e) { ElMessage.error(e instanceof Error ? e.message : '删除失败') } finally { removing.value = false }
}

// 导出 / 导入
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

// 3D 语义探针
const testQuery = ref('')
const activeProbeQuery = ref('')
const testing = ref(false)
const searchHits = ref<KnowledgeSearchHit[]>([])
function clearProbe() {
  testQuery.value = ''
  activeProbeQuery.value = ''
  searchHits.value = []
}
async function runTest() {
  if (!current.value || !testQuery.value.trim()) { clearProbe(); return }
  testing.value = true
  try {
    searchHits.value = await api.knowledge.test(current.value.id, testQuery.value.trim())
    activeProbeQuery.value = testQuery.value.trim()
    inspectorSelection.value = null
    inspectorOpen.value = true
  } catch (e) {
    ElMessage.error(e instanceof Error ? e.message : '检索失败')
  } finally {
    testing.value = false
  }
}

// 更换向量模型
const modelSwitchVisible = ref(false), modelSwitchId = ref(''), modelSwitching = ref(false)
function openModelSwitch() { modelSwitchId.value = current.value?.embeddingProfileId || ''; modelSwitchVisible.value = true }
async function switchModel() {
  if (!current.value || !modelSwitchId.value) return
  modelSwitching.value = true
  try { await api.knowledge.changeModel(current.value.id, modelSwitchId.value); modelSwitchVisible.value = false; ElMessage.success('已切换，正在重建索引'); await refreshCurrent() }
  catch (e) { ElMessage.error(e instanceof Error ? e.message : '切换失败') } finally { modelSwitching.value = false }
}

onMounted(reload)
useAdminEvents(['knowledge_bases', 'knowledge_documents', 'knowledge_entries', 'knowledge_candidates', 'knowledge_gaps', 'message_feedback', 'model_profiles'], reload)
</script>

<style scoped>
.kb-universe-page {
  position: relative;
  width: 100%;
  height: 100vh;
  height: 100dvh;
  min-height: 540px;
  overflow: hidden;
  background: #040814;
}

.kb-empty-backdrop {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: radial-gradient(circle at center, #0c1830 0%, #040814 75%);
}
.kb-empty-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  max-width: 440px;
  padding: 36px 32px;
  border: 1px solid rgba(56, 189, 248, 0.28);
  border-radius: 20px;
  background: rgba(10, 17, 32, 0.8);
  text-align: center;
  backdrop-filter: blur(18px);
}
.empty-pulse { width: 52px; height: 52px; border-radius: 50%; background: radial-gradient(circle, #38bdf8, transparent 70%); border: 1px solid rgba(56, 189, 248, 0.5); }
.kb-empty-card h2 { color: #f1f5f9; font-size: 18px; }
.kb-empty-card p { color: #94a3b8; font-size: 13px; line-height: 1.6; }
.empty-actions { display: flex; gap: 10px; margin-top: 8px; }

/* 通用 HUD 玻璃浮卡片 */
.hud-card {
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 14px;
  background: rgba(9, 15, 30, 0.76);
  box-shadow: 0 12px 36px rgba(0, 0, 0, 0.45), inset 0 1px 0 rgba(255, 255, 255, 0.08);
  backdrop-filter: blur(16px);
  color: #e2e8f0;
}

/* 左上角浮岛 */
.hud-top-left {
  position: absolute;
  top: 16px;
  left: 16px;
  z-index: 12;
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-width: calc(100vw - 32px);
}
.kb-switcher { display: flex; align-items: center; gap: 6px; padding: 6px 8px; }
.kb-pick-btn {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 32px;
  padding: 0 10px;
  border: 0;
  border-radius: 10px;
  background: transparent;
  color: #f8fafc;
  font-size: 14px;
  font-weight: 600;
  cursor: pointer;
  transition: background var(--dur-fast) var(--ease);
}
.kb-pick-btn:hover { background: rgba(56, 189, 248, 0.14); }
.kb-pick-dot { width: 8px; height: 8px; border-radius: 50%; background: #38bdf8; box-shadow: 0 0 10px #38bdf8; }
.kb-pick-name { max-width: 190px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.kb-pick-arrow { color: #94a3b8; font-size: 12px; }
.kb-top-actions { display: flex; align-items: center; gap: 2px; border-left: 1px solid rgba(148, 163, 184, 0.18); padding-left: 6px; }
.hud-icon-btn {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: #94a3b8;
  cursor: pointer;
  transition: background var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease);
}
.hud-icon-btn:hover { background: rgba(148, 163, 184, 0.16); color: #f8fafc; }
.hud-icon-btn.danger:hover { background: rgba(248, 113, 113, 0.18); color: #fca5a5; }

.kb-stats-strip {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  padding: 7px 12px;
  font-size: 12px;
  color: #94a3b8;
  font-variant-numeric: tabular-nums;
}
.kb-stats-strip b { color: #f1f5f9; font-weight: 600; margin-right: 2px; }
.model-link {
  max-width: 150px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  padding: 1px 7px;
  border: 1px solid rgba(56, 189, 248, 0.3);
  border-radius: 999px;
  background: rgba(56, 189, 248, 0.1);
  color: #7dd3fc;
  font-size: 11px;
  cursor: pointer;
}
.model-link:hover { border-color: #38bdf8; }

/* 顶部居中探针舱 */
.hud-top-center {
  position: absolute;
  top: 16px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 12;
  width: min(460px, calc(100vw - 560px));
  min-width: 280px;
}
.probe-capsule { display: flex; align-items: center; gap: 8px; height: 44px; padding: 0 6px 0 14px; border-radius: 999px; }
.probe-capsule:focus-within { border-color: rgba(56, 189, 248, 0.65); box-shadow: 0 0 24px rgba(56, 189, 248, 0.25); }
.probe-ico { color: #38bdf8; font-size: 15px; flex: none; }
.probe-input { flex: 1; min-width: 0; border: 0; outline: none; background: transparent; color: #f8fafc; font-size: 13px; }
.probe-input::placeholder { color: #64748b; }
.probe-clear { border: 0; background: transparent; color: #94a3b8; font-size: 12px; cursor: pointer; padding: 4px 6px; }
.probe-clear:hover { color: #f8fafc; }
.probe-go {
  flex: none;
  height: 32px;
  padding: 0 14px;
  border: 0;
  border-radius: 999px;
  background: linear-gradient(135deg, #0284c7, #2563eb 55%, #4f46e5);
  color: #ffffff;
  font-size: 12px;
  font-weight: 600;
  cursor: pointer;
}
.probe-go:hover { filter: brightness(1.1); }

/* 右上角透视开关 */
.hud-top-right { position: absolute; top: 16px; right: 16px; z-index: 12; }
.hud-toggle-btn { display: inline-flex; align-items: center; gap: 8px; height: 40px; padding: 0 14px; font-size: 13px; font-weight: 500; cursor: pointer; }
.hud-toggle-btn:hover { border-color: #38bdf8; color: #ffffff; }
.hit-pill { padding: 1px 7px; border-radius: 999px; background: rgba(56, 189, 248, 0.24); color: #7dd3fc; font-size: 11px; font-style: normal; font-weight: 700; }

/* 右侧透视浮舱 */
.hud-right-inspector {
  position: absolute;
  top: 68px;
  right: 16px;
  bottom: 84px;
  z-index: 11;
  width: 350px;
  max-width: calc(100vw - 32px);
  border: 1px solid rgba(148, 163, 184, 0.22);
  border-radius: 16px;
  background: rgba(9, 15, 30, 0.86);
  box-shadow: 0 20px 56px rgba(0, 0, 0, 0.6);
  backdrop-filter: blur(18px);
  overflow: hidden;
  display: flex;
}
.hud-right-inspector :deep(.inspector-panel) { width: 100%; border-left: 0; background: transparent; color: #e2e8f0; }
.hud-right-inspector :deep(.inspector-body) { max-height: none; flex: 1; }
.hud-right-inspector :deep(.inspector-head) { border-bottom-color: rgba(148, 163, 184, 0.16); color: #f8fafc; }
.hud-right-inspector :deep(.ins-section + .ins-section) { border-top-color: rgba(148, 163, 184, 0.14); }
.hud-right-inspector :deep(.ins-box),
.hud-right-inspector :deep(.chunk-card),
.hud-right-inspector :deep(.hit-card),
.hud-right-inspector :deep(.cluster-row),
.hud-right-inspector :deep(.todo-card) { background: rgba(17, 26, 46, 0.72); border-color: rgba(148, 163, 184, 0.18); color: #e2e8f0; }

/* 底部 Dock */
.hud-bottom-dock {
  position: absolute;
  bottom: 16px;
  left: 50%;
  transform: translateX(-50%);
  z-index: 12;
  max-width: calc(100vw - 32px);
}
.dock-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 10px;
  border-radius: 16px;
  overflow-x: auto;
  scrollbar-width: none;
}
.dock-bar::-webkit-scrollbar { display: none; }
.dock-seg { display: flex; align-items: center; gap: 4px; flex: none; }
.dock-sep { width: 1px; height: 20px; background: rgba(148, 163, 184, 0.22); flex: none; }
.dock-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 30px;
  padding: 0 11px;
  border: 1px solid transparent;
  border-radius: 10px;
  background: transparent;
  color: #cbd5e1;
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  cursor: pointer;
  transition: all var(--dur-fast) var(--ease);
}
.dock-btn:hover { background: rgba(148, 163, 184, 0.14); color: #ffffff; }
.dock-btn.active { background: rgba(56, 189, 248, 0.18); border-color: rgba(56, 189, 248, 0.42); color: #7dd3fc; font-weight: 600; }
.dock-btn.accent { color: #7dd3fc; background: rgba(56, 189, 248, 0.1); border-color: rgba(56, 189, 248, 0.24); }
.dock-btn.accent:hover { background: rgba(56, 189, 248, 0.22); }
.dock-badge { padding: 0 5px; border-radius: 999px; background: #ef4444; color: #fff; font-size: 10px; font-style: normal; font-weight: 700; line-height: 15px; }
.dock-badge.warn { background: #f59e0b; }

/* 知识库选择器 Popover 内容 */
.kb-pick-pop { display: flex; flex-direction: column; gap: 8px; }
.kb-pick-list { display: flex; flex-direction: column; gap: 2px; max-height: 280px; overflow-y: auto; }
.kb-pick-item { display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 8px 10px; border: 0; border-radius: 8px; background: transparent; text-align: left; cursor: pointer; color: var(--text); }
.kb-pick-item:hover { background: var(--surface-3); }
.kb-pick-item.active { background: var(--primary-light); color: var(--primary-text); }
.kb-pick-item strong { display: block; font-size: 13px; }
.kb-pick-item small { display: block; color: var(--muted); font-size: 11px; }
.kb-pick-item.is-unassigned { margin-top: 4px; border-top: 1px solid var(--line); }
.kb-pick-foot { display: flex; gap: 8px; padding-top: 8px; border-top: 1px solid var(--line); }

.drawer-tab-switch { margin-bottom: 14px; }
.unassigned-switch { align-self: flex-start; margin-bottom: 12px; }
.kb-toolbar { margin-bottom: 12px; }
.kb-toolbar > .el-input { width: 220px; }
.kb-toolbar > .el-select { width: 132px; }
.switch-note { margin: 0; color: var(--muted); font-size: 13px; line-height: 1.7; }
.import-file { display: flex; align-items: center; gap: 10px; width: 100%; min-width: 0; height: 44px; padding: 0 8px 0 12px; border: 1px dashed var(--line-strong); border-radius: var(--radius-sm); color: var(--muted); font-size: 13px; }
.import-file > span { flex: 1; min-width: 0; }
.import-file.picked { border-style: solid; background: var(--surface-2); color: var(--text); }
.import-progress { display: flex; align-items: center; gap: 12px; height: 20px; margin-top: 12px; opacity: 0; transition: opacity var(--dur) var(--ease); }
.import-progress.active { opacity: 1; }
.import-progress .progress-line { flex: 1; }
.import-progress span { flex: none; min-width: 120px; color: var(--text-2); font-size: 12px; text-align: right; font-variant-numeric: tabular-nums; }
.evidence { max-height: 220px; overflow: auto; font-size: 13px; }
.unread-dot { display: inline-flex; align-items: center; gap: 6px; padding: 2px 0; border: 0; background: none; color: var(--primary-text); font: inherit; font-size: 13px; font-weight: 600; cursor: pointer; }
.unread-dot i { width: 7px; height: 7px; border-radius: 50%; background: var(--primary); }
.qa-row { display: flex; align-items: flex-start; gap: 12px; }
.qa-row .qa { flex: 1; min-width: 0; }
.qa-thumb { flex: none; position: relative; display: block; width: 48px; height: 48px; margin: 2px 0; border: 1px solid var(--line); border-radius: var(--radius-xs); overflow: hidden; background: var(--surface-3); }
.qa-thumb video, .qa-thumb img { display: block; width: 100%; height: 100%; object-fit: cover; }
.qa-thumb-count { position: absolute; right: 2px; bottom: 2px; min-width: 16px; padding: 0 4px; border-radius: 8px; background: rgb(26 28 31 / .72); color: #fff; font-size: 12px; line-height: 16px; text-align: center; }
.hit-image-tag { margin-left: 8px; padding: 1px 6px; border-radius: var(--radius-xs); background: var(--surface-3); color: var(--text-2); font-weight: 500; }
.hit-image { display: block; margin-top: 8px; }
.hit-image img { display: block; max-width: 100%; max-height: 240px; border: 1px solid var(--line); border-radius: var(--radius-sm); object-fit: contain; }

.hud-slide-right-enter-active, .hud-slide-right-leave-active { transition: opacity 220ms var(--ease), transform 220ms var(--ease); }
.hud-slide-right-enter-from, .hud-slide-right-leave-to { opacity: 0; transform: translateX(24px); }

@media (max-width: 1120px) {
  .hud-top-center { top: 72px; left: 16px; transform: none; width: calc(100vw - 32px); }
  .hud-stats-strip { display: none; }
}
@media (max-width: 768px) {
  .kb-universe-page { height: calc(100dvh - 52px); }
  .hud-right-inspector { top: 124px; bottom: 72px; }
}
</style>
