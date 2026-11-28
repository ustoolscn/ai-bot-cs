<template>
  <div ref="pageRef" class="memory-universe-page">
    <!-- ==================== Tab 1: 3D 记忆星图 ==================== -->
    <template v-if="activeTab === 'subjects'">
      <section v-if="viewMode === '3d'" class="universe-stage">
        <MemoryConstellation3D
          ref="constellationRef"
          :subjects="subjects"
          :selected-id="selectedSubject?.id"
          @select="selectSubject"
        />

        <!-- 左上悬浮 HUD 指挥栏 -->
        <header class="hud-top-left">
          <div class="hud-title-row">
            <span class="holo-badge"><i /><span>3D MEMORY CONSTELLATION</span></span>
            <h1>记忆星图</h1>
            <el-segmented v-model="activeTab" :options="tabOptions" size="small" @change="onTabChange" />
          </div>
          <div class="hud-filters">
            <el-input v-model="subjectQuery" placeholder="搜索昵称 / 档案 / OpenID" :prefix-icon="Search" clearable size="small" style="width: 210px" @change="loadSubjects" />
            <el-select v-model="stageFilter" size="small" style="width: 132px" @change="loadSubjects">
              <el-option value="all" label="全部关系阶段" />
              <el-option value="stranger" label="初识" />
              <el-option value="acquaintance" label="脸熟" />
              <el-option value="familiar" label="熟人" />
              <el-option value="close_friend" label="老铁" />
            </el-select>
            <el-select v-model="botFilter" clearable placeholder="全部机器人" size="small" style="width: 148px" @change="loadSubjects">
              <el-option v-for="b in bots" :key="b.id" :label="b.name" :value="b.id" />
            </el-select>
            <el-button-group size="small">
              <el-button type="primary" @click="viewMode = '3d'">3D 星图</el-button>
              <el-button @click="viewMode = 'list'">分栏表格</el-button>
            </el-button-group>
            <el-button size="small" @click="resetUniverse">重置视角</el-button>
            <el-button size="small" :icon="Refresh" :loading="subjectsLoading" @click="reloadCurrent">刷新</el-button>
          </div>
        </header>

        <!-- 左下悬浮图例与指标 -->
        <aside class="hud-bottom-left">
          <div class="hud-stats">
            <div><span>人物总数</span><strong>{{ subjects.length }}</strong></div>
            <div><span>老铁 / 熟人</span><strong>{{ closeOrFamiliarCount }}</strong></div>
            <div><span>UnionID 贯通</span><strong>{{ unionCount }}</strong></div>
            <div><span>跨 Bot 共享</span><strong>{{ crossBotCount }}</strong></div>
          </div>
          <div class="hud-legend">
            <span><i style="background: #fbbf24" />老铁内环</span>
            <span><i style="background: #34d399" />熟人</span>
            <span><i style="background: #38bdf8" />脸熟</span>
            <span><i style="background: #818cf8" />初识外环</span>
            <span><i class="ring-dot" />金环 = UnionID</span>
          </div>
        </aside>

        <!-- 右侧全息检视舱 -->
        <Transition name="inspector-swap">
          <aside v-if="selectedSubject && inspectorOpen" class="hud-inspector">
            <header class="inspector-head">
              <div class="inspector-title">
                <strong>{{ selectedSubject.displayName || '未命名用户' }}</strong>
                <span class="stage-pill" :class="'stage-' + selectedSubject.relationshipStage">{{ stageLabel[selectedSubject.relationshipStage] }}</span>
              </div>
              <div class="inspector-actions">
                <el-button size="small" :loading="reflecting" @click="reflectNow">反思</el-button>
                <el-button size="small" :disabled="subjects.length < 2" @click="openMergeDialog">合并</el-button>
                <el-button size="small" type="danger" plain @click="clearSubjectProfile">清空</el-button>
                <el-button size="small" text @click="inspectorOpen = false">收起</el-button>
              </div>
            </header>

            <div class="inspector-body">
              <div class="rel-grid compact">
                <div class="rel-meter">
                  <div class="rel-label"><span>熟悉度</span><strong>{{ Math.round(selectedSubject.familiarity) }}</strong></div>
                  <el-progress :percentage="Math.round(selectedSubject.familiarity)" :show-text="false" :stroke-width="5" />
                </div>
                <div class="rel-meter">
                  <div class="rel-label"><span>亲近度</span><strong>{{ Math.round(selectedSubject.affinity) }}</strong></div>
                  <el-progress :percentage="Math.round(selectedSubject.affinity)" :show-text="false" :stroke-width="5" status="success" />
                </div>
                <div class="rel-meter">
                  <div class="rel-label"><span>信任度</span><strong>{{ Math.round(selectedSubject.trust) }}</strong></div>
                  <el-progress :percentage="Math.round(selectedSubject.trust)" :show-text="false" :stroke-width="5" status="warning" />
                </div>
              </div>

              <div class="links-chips">
                <span v-for="l in selectedSubject.links" :key="l.id" class="link-chip" :class="{ isUnion: l.identityType === 'qq_union_id' }">
                  <strong>{{ l.botName }}</strong>
                  <span>{{ linkTypeText[l.identityType] || l.identityType }}</span>
                  <span v-if="l.conversationName">· {{ l.conversationName }}</span>
                  <code>{{ l.identityValue.slice(0, 10) }}</code>
                  <button v-if="selectedSubject.links.length > 1" type="button" class="unlink-btn" @click="unlinkIdentity(l.id)">拆分</button>
                </span>
              </div>

              <div class="profile-tabs-bar">
                <el-segmented v-model="profileTab" size="small" :options="[{ label: '人格画像', value: 'persona' }, { label: '文字档案', value: 'text' }]" />
                <span class="spacer" />
                <el-button v-if="profileTab === 'text'" size="small" type="primary" :disabled="profileDraft === profileSaved" :loading="profileSaving" @click="saveProfile">
                  保存档案
                </el-button>
              </div>

              <div class="inspector-persona">
                <PersonaCard v-if="profileTab === 'persona'" :persona="selectedSubject.persona" />
                <div v-else class="profile-text">
                  <p class="muted">约 {{ profileTokens }} / 2000 token</p>
                  <div class="token-meter" :class="{ over: profileTokens > 2000 }"><i :style="{ transform: `scaleX(${Math.min(1, profileTokens / 2000)})` }" /></div>
                  <el-input v-model="profileDraft" type="textarea" :autosize="{ minRows: 4, maxRows: 10 }" placeholder="还没有档案。互动几轮后会自动反思总结，也可以直接编辑。" />
                </div>
              </div>

              <div class="inspector-mem-box">
                <div class="mem-table-head">
                  <strong>长期记忆 ({{ memories.length }})</strong>
                  <div class="mem-head-right">
                    <el-button v-if="selectedIds.length" size="small" type="danger" plain :loading="deleting" @click="deleteSelected('memories', '用户记忆')">删 {{ selectedIds.length }} 条</el-button>
                    <el-input v-model="memoryQuery" placeholder="筛选记忆" :prefix-icon="Search" clearable size="small" style="width: 150px" @change="loadSubjectMemories" />
                  </div>
                </div>
                <el-table row-key="id" max-height="260" v-loading="memoriesLoading" :data="memories" size="small" @selection-change="(rows: {id:string}[]) => selectRows(rows)">
                  <template #empty><EmptyState compact mood="sleepy" title="还没有记忆条目" description="多聊几轮后会自动记下事实与偏好。" /></template>
                  <el-table-column type="selection" reserve-selection width="36" />
                  <el-table-column label="记忆内容" min-width="170">
                    <template #default="{ row }">
                      <div class="mem-cell">
                        <span v-if="row.pinned" class="pill busy pinned">常驻</span>
                        <span>{{ $at(row.content) }}</span>
                        <div class="mem-sub" v-if="row.sourceBotName || row.emotionTag">
                          <span v-if="row.sourceBotName">{{ row.sourceBotName }}</span>
                          <span v-if="row.emotionTag" class="emo-badge">{{ row.emotionTag }}</span>
                        </div>
                      </div>
                    </template>
                  </el-table-column>
                  <el-table-column label="操作" width="84" fixed="right">
                    <template #default="{ row }">
                      <el-button link type="primary" size="small" @click="editMemory(row)">编辑</el-button>
                      <el-button link type="danger" size="small" @click="removeMemory(row)">删</el-button>
                    </template>
                  </el-table-column>
                </el-table>
              </div>
            </div>
          </aside>
        </Transition>

        <button v-if="selectedSubject && !inspectorOpen" type="button" class="reopen-pill" @click="inspectorOpen = true">
          展开「{{ selectedSubject.displayName || '当前人物' }}」档案
        </button>
      </section>

      <!-- 经典分栏列表视图 -->
      <div v-else class="content-page page-fill">
        <PageHeader title="记忆星图" description="全系统跨机器人共享的人物档案、关系演进、群情景记忆与主动关怀。">
          <el-segmented v-model="activeTab" :options="tabOptions" @change="onTabChange" />
          <el-button-group>
            <el-button @click="viewMode = '3d'">切换 3D 星图</el-button>
            <el-button type="primary" plain>分栏表格</el-button>
          </el-button-group>
          <el-button :icon="Refresh" @click="reloadCurrent">刷新</el-button>
        </PageHeader>

        <div class="toolbar">
          <el-input v-model="subjectQuery" placeholder="搜索人物昵称、档案或 OpenID" :prefix-icon="Search" clearable style="width: 260px" @change="loadSubjects" />
          <el-select v-model="stageFilter" style="width: 150px" @change="loadSubjects">
            <el-option value="all" label="全部关系阶段" />
            <el-option value="stranger" label="初识" />
            <el-option value="acquaintance" label="脸熟" />
            <el-option value="familiar" label="熟人" />
            <el-option value="close_friend" label="老铁" />
          </el-select>
          <el-select v-model="botFilter" clearable placeholder="全部机器人" style="width: 180px" @change="loadSubjects">
            <el-option v-for="b in bots" :key="b.id" :label="b.name" :value="b.id" />
          </el-select>
          <span class="spacer" />
          <el-button type="danger" plain :style="{ visibility: selectedIds.length ? 'visible' : 'hidden' }" :loading="deleting" @click="deleteSelected('memories', '用户记忆')">
            删除选中的 {{ selectedIds.length }} 条
          </el-button>
        </div>

        <div class="subject-layout fill">
          <aside class="panel subject-list" v-loading="subjectsLoading">
            <EmptyState v-if="!subjects.length" compact mood="sleepy" title="还没有人物档案" description="机器人和群友互动后，会自动沉淀全局人物与记忆。" />
            <ul v-else class="subject-items">
              <li v-for="s in subjects" :key="s.id" :class="{ active: selectedSubject?.id === s.id }" @click="selectSubject(s)">
                <div class="subject-item-top">
                  <strong>{{ s.displayName || '未命名用户' }}</strong>
                  <span class="stage-pill" :class="'stage-' + s.relationshipStage">{{ stageLabel[s.relationshipStage] || s.relationshipStage }}</span>
                </div>
                <div class="subject-item-meta">
                  <span>{{ s.interactionTurns }} 轮 · {{ s.memoryCount }} 条记忆</span>
                  <span v-if="s.moodState?.currentMood && s.moodState.currentMood !== '平静'" class="mood-chip">{{ s.moodState.currentMood }}</span>
                </div>
                <div class="subject-item-links">
                  <span v-for="bName in uniqueLinkBots(s)" :key="bName" class="tiny-tag">{{ bName }}</span>
                  <span v-if="s.links.some(l => l.identityType === 'qq_union_id')" class="tiny-tag union">UnionID 已通</span>
                </div>
              </li>
            </ul>
          </aside>

          <div v-if="selectedSubject" class="subject-detail">
            <section class="panel profile-panel">
              <header class="detail-head">
                <div>
                  <h2>{{ selectedSubject.displayName || '未命名用户' }}</h2>
                  <div class="head-sub">
                    <span class="stage-pill" :class="'stage-' + selectedSubject.relationshipStage">{{ stageLabel[selectedSubject.relationshipStage] }}</span>
                    <span>互动 {{ selectedSubject.interactionTurns }} 轮 / {{ selectedSubject.interactionDays }} 天</span>
                    <span v-if="selectedSubject.moodState?.currentMood">· 最近情绪：{{ selectedSubject.moodState.currentMood }}</span>
                    <span v-if="selectedSubject.lastReflectedAt">· 上次反思 {{ formatTime(selectedSubject.lastReflectedAt) }}</span>
                  </div>
                </div>
                <span class="spacer" />
                <el-button size="small" :loading="reflecting" @click="reflectNow">立即反思</el-button>
                <el-button size="small" :disabled="subjects.length < 2" @click="openMergeDialog">合并人物</el-button>
                <el-button size="small" type="danger" plain @click="clearSubjectProfile">清空画像</el-button>
              </header>

              <div class="rel-grid">
                <div class="rel-meter">
                  <div class="rel-label"><span>熟悉度</span><strong>{{ Math.round(selectedSubject.familiarity) }}</strong></div>
                  <el-progress :percentage="Math.round(selectedSubject.familiarity)" :show-text="false" :stroke-width="6" />
                </div>
                <div class="rel-meter">
                  <div class="rel-label"><span>亲近度</span><strong>{{ Math.round(selectedSubject.affinity) }}</strong></div>
                  <el-progress :percentage="Math.round(selectedSubject.affinity)" :show-text="false" :stroke-width="6" status="success" />
                </div>
                <div class="rel-meter">
                  <div class="rel-label"><span>信任度</span><strong>{{ Math.round(selectedSubject.trust) }}</strong></div>
                  <el-progress :percentage="Math.round(selectedSubject.trust)" :show-text="false" :stroke-width="6" status="warning" />
                </div>
              </div>

              <div class="links-strip">
                <span class="links-title">已关联身份（{{ selectedSubject.links.length }}）：</span>
                <div class="links-chips">
                  <span v-for="l in selectedSubject.links" :key="l.id" class="link-chip" :class="{ isUnion: l.identityType === 'qq_union_id' }">
                    <strong>{{ l.botName }}</strong>
                    <span>{{ linkTypeText[l.identityType] || l.identityType }}</span>
                    <span v-if="l.conversationName">· {{ l.conversationName }}</span>
                    <code>{{ l.identityValue.slice(0, 12) }}</code>
                    <button v-if="selectedSubject.links.length > 1" type="button" class="unlink-btn" title="拆分为独立人物" @click="unlinkIdentity(l.id)">拆分</button>
                  </span>
                </div>
              </div>

              <div class="profile-tabs-bar">
                <el-segmented v-model="profileTab" :options="[{ label: '人格画像', value: 'persona' }, { label: '文字档案', value: 'text' }]" />
                <span class="spacer" />
                <el-button v-if="profileTab === 'text'" size="small" type="primary" :disabled="profileDraft === profileSaved" :loading="profileSaving" @click="saveProfile">
                  保存档案
                </el-button>
              </div>

              <div class="profile-scroll">
                <PersonaCard v-if="profileTab === 'persona'" :persona="selectedSubject.persona" />
                <div v-else class="profile-text">
                  <p class="muted">慢通道反思后自动整合，回复时作为底层认知。约 {{ profileTokens }} / 2000 token</p>
                  <div class="token-meter" :class="{ over: profileTokens > 2000 }"><i :style="{ transform: `scaleX(${Math.min(1, profileTokens / 2000)})` }" /></div>
                  <el-input v-model="profileDraft" type="textarea" :autosize="{ minRows: 5, maxRows: 14 }" placeholder="还没有档案。互动几轮后会自动反思总结，也可以直接编辑。" />
                </div>
              </div>
            </section>

            <section class="panel table-panel">
              <div class="mem-table-head">
                <strong>跨会话长期记忆（{{ memories.length }}）</strong>
                <el-input v-model="memoryQuery" placeholder="筛选记忆内容" :prefix-icon="Search" clearable size="small" style="width: 220px" @change="loadSubjectMemories" />
              </div>
              <el-table row-key="id" height="100%" v-loading="memoriesLoading" :data="memories" @selection-change="(rows: {id:string}[]) => selectRows(rows)">
                <template #empty><EmptyState mood="sleepy" title="还没有记忆条目" description="多聊几轮后，AI 会自动记下事实、偏好与共同经历。" /></template>
                <el-table-column type="selection" reserve-selection width="44" />
                <el-table-column label="记忆内容" min-width="200">
                  <template #default="{ row }">
                    <div class="mem-cell">
                      <span v-if="row.pinned" class="pill busy pinned">常驻</span>
                      <span v-if="row.resolvedAt" class="pill good pinned">已解决</span>
                      <span>{{ $at(row.content) }}</span>
                      <div class="mem-sub" v-if="row.sourceConversationName || row.sourceBotName || row.emotionTag">
                        <span v-if="row.sourceBotName">{{ row.sourceBotName }}</span>
                        <span v-if="row.sourceConversationName">· {{ row.sourceConversationName }}</span>
                        <span v-if="row.emotionTag" class="emo-badge">{{ row.emotionTag }}</span>
                      </div>
                    </div>
                  </template>
                </el-table-column>
                <el-table-column label="类型 / 权重" width="120">
                  <template #default="{ row }">
                    <div class="stacked-cell">
                      <strong>{{ kindText[row.kind] || row.kind }}</strong>
                      <span>重要 {{ row.importance ?? 5 }} · 命中 {{ row.accessCount ?? 0 }}</span>
                    </div>
                  </template>
                </el-table-column>
                <el-table-column label="发生 / 更新" width="112">
                  <template #default="{ row }">
                    <div class="stacked-cell" :title="'更新于 ' + formatTime(row.updatedAt)">
                      <strong>{{ formatTime(row.eventAt || row.updatedAt).slice(5) }}</strong>
                      <span>{{ memorySourceText[row.source] || row.source }}</span>
                    </div>
                  </template>
                </el-table-column>
                <el-table-column label="操作" width="100" fixed="right">
                  <template #default="{ row }">
                    <el-button link type="primary" @click="editMemory(row)">编辑</el-button>
                    <el-button link type="danger" @click="removeMemory(row)">删除</el-button>
                  </template>
                </el-table-column>
              </el-table>
            </section>
          </div>

          <section v-else class="panel empty-detail">
            <EmptyState mood="thinking" title="选择左侧一位人物" description="查看他在所有机器人与群聊/私聊中共享的档案、人格画像、关系进度和长期记忆。" />
          </section>
        </div>
      </div>
    </template>

    <!-- ==================== Tab 2: 群情景记忆 ==================== -->
    <div v-else-if="activeTab === 'group'" class="content-page page-fill">
      <PageHeader title="记忆星图 · 群情景记忆" description="机器人定期沉淀各群热议话题、氛围与群梗。">
        <el-segmented v-model="activeTab" :options="tabOptions" @change="onTabChange" />
        <el-button :icon="Refresh" @click="reloadCurrent">刷新</el-button>
      </PageHeader>
      <div class="toolbar">
        <el-select v-model="selectedGroupId" filterable placeholder="选择群聊" style="width: 320px" @change="loadGroupMemories">
          <el-option v-for="g in groupConversations" :key="g.id" :value="g.id" :label="`${g.botName} · ${g.name}`" />
        </el-select>
        <el-button type="primary" plain :disabled="!selectedGroupId" :loading="digesting" @click="digestGroupNow">立即整理群记忆</el-button>
        <span class="spacer" />
        <span v-if="groupMemData.lastDigestAt" class="muted">上次整理：{{ formatTime(groupMemData.lastDigestAt) }}</span>
      </div>

      <section v-if="selectedGroupId" class="panel vibe-card cyber-panel">
        <strong>当前群氛围：</strong>
        <span>{{ groupMemData.groupVibe || '还没有整理出群氛围，群聊活跃后会自动提炼，或点击上方“立即整理群记忆”。' }}</span>
      </section>

      <section class="panel table-panel fill" v-loading="groupLoading">
        <el-table row-key="id" height="100%" :data="groupMemData.items">
          <template #empty><EmptyState mood="thinking" :title="selectedGroupId ? '暂无群情景记忆' : '先选择一个群聊'" description="机器人会定期总结群里近期讨论的热点话题、群梗和重要事件。" /></template>
          <el-table-column label="类型" width="100">
            <template #default="{ row }">{{ groupKindText[row.kind] || row.kind }}</template>
          </el-table-column>
          <el-table-column label="情景内容" prop="content" min-width="260" />
          <el-table-column label="重要度" width="90" prop="importance" />
          <el-table-column label="有效期至" width="150">
            <template #default="{ row }">{{ row.expiresAt ? formatTime(row.expiresAt) : '长期' }}</template>
          </el-table-column>
          <el-table-column label="操作" width="80" fixed="right">
            <template #default="{ row }"><el-button link type="danger" @click="removeGroupMemory(row.id)">删除</el-button></template>
          </el-table-column>
        </el-table>
      </section>
    </div>

    <!-- ==================== Tab 3: 主动关怀队列 ==================== -->
    <div v-else class="content-page page-fill">
      <PageHeader title="记忆星图 · 主动关怀队列" description="在低打扰窗口内，AI 自动安排的日程跟进与情感回访计划。">
        <el-segmented v-model="activeTab" :options="tabOptions" @change="onTabChange" />
        <el-button :icon="Refresh" @click="reloadCurrent">刷新</el-button>
      </PageHeader>
      <div class="toolbar">
        <el-select v-model="proactiveStatus" style="width: 160px" @change="loadProactive">
          <el-option value="all" label="全部状态" />
          <el-option value="pending" label="待发送" />
          <el-option value="sent" label="已发送" />
          <el-option value="skipped" label="已跳过" />
          <el-option value="cancelled" label="已取消" />
        </el-select>
      </div>
      <section class="panel table-panel fill" v-loading="proactiveLoading">
        <el-table row-key="id" height="100%" :data="proactiveList">
          <template #empty><EmptyState mood="sleepy" title="暂无主动关怀记录" description="当用户提到重要待办或近期日程时，AI 会在合适时机安排低打扰回访。" /></template>
          <el-table-column label="对象 / 会话" min-width="180">
            <template #default="{ row }">
              <div class="stacked-cell">
                <strong>{{ row.subjectName || '群友' }}</strong>
                <span>{{ row.botName }} · {{ row.conversationName }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="类型" width="110">
            <template #default="{ row }">{{ proactiveKindText[row.kind] || row.kind }}</template>
          </el-table-column>
          <el-table-column label="关怀事由" prop="topic" min-width="220" />
          <el-table-column label="计划时间" width="150">
            <template #default="{ row }">{{ formatTime(row.scheduledAt) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="160">
            <template #default="{ row }">
              <el-tag size="small" :type="row.status === 'sent' ? 'success' : row.status === 'pending' ? 'warning' : 'info'">{{ proactiveStatusText[row.status] || row.status }}</el-tag>
              <div v-if="row.skipReason" class="muted" style="font-size: 11px; margin-top: 2px">{{ row.skipReason }}</div>
            </template>
          </el-table-column>
          <el-table-column label="操作" width="90" fixed="right">
            <template #default="{ row }">
              <el-button v-if="row.status === 'pending'" link type="danger" @click="cancelTrigger(row.id)">取消</el-button>
            </template>
          </el-table-column>
        </el-table>
      </section>
    </div>

    <!-- 编辑记忆弹窗 -->
    <el-dialog v-model="memoryDialog" title="编辑记忆" width="560px">
      <el-form label-position="top">
        <div class="dlg-stack">
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>记忆正文</strong><span>调整机器人对用户的记忆表述</span></div>
            <el-form-item label="内容"><el-input v-model="memoryForm.content" type="textarea" :rows="3" /></el-form-item>
          </div>
          <div class="dlg-section">
            <div class="dlg-section-head"><strong>分类、权重与标签</strong><span>影响召回排序与展示样式</span></div>
            <div class="dialog-two">
              <el-form-item label="类型">
                <el-select v-model="memoryForm.kind" style="width: 100%"><el-option v-for="(v, k) in kindText" :key="k" :label="v" :value="k" /></el-select>
              </el-form-item>
              <el-form-item label="重要度 (1-10)">
                <el-input-number v-model="memoryForm.importance" :min="1" :max="10" style="width: 100%" />
              </el-form-item>
            </div>
            <el-form-item label="情绪标记"><el-input v-model="memoryForm.emotionTag" placeholder="例如：开心、委屈、共鸣（选填）" /></el-form-item>
            <div class="dialog-checks">
              <el-checkbox v-model="memoryForm.pinned">常驻（每次回复优先带上）</el-checkbox>
              <el-checkbox v-model="memoryForm.resolved">已解决（降低问题类记忆权重）</el-checkbox>
            </div>
          </div>
        </div>
      </el-form>
      <template #footer><el-button @click="memoryDialog = false">取消</el-button><el-button type="primary" @click="saveMemory">保存记忆</el-button></template>
    </el-dialog>

    <!-- 合并人物弹窗 -->
    <el-dialog v-model="mergeDialog" title="合并为同一人物" width="500px">
      <div class="dlg-stack">
        <div class="dlg-tip-box">将另一个人物的跨群/私聊身份、记忆与关系合并到「{{ selectedSubject?.displayName || '当前人物' }}」名下（不会丢失任何记录）。</div>
        <div class="dlg-section">
          <div class="dlg-section-head"><strong>选择目标人物</strong><span>合并后保留双方全部记忆条数</span></div>
          <el-select v-model="mergeTargetId" filterable placeholder="选择要合并进来的人物" style="width: 100%">
            <el-option
              v-for="s in subjects.filter(x => x.id !== selectedSubject?.id)"
              :key="s.id"
              :value="s.id"
              :label="`${s.displayName || '未命名'}（${s.interactionTurns}轮 / ${s.memoryCount}记忆）`"
            />
          </el-select>
        </div>
      </div>
      <template #footer>
        <el-button @click="mergeDialog = false">取消</el-button>
        <el-button type="primary" :disabled="!mergeTargetId" :loading="merging" @click="confirmMerge">确认合并</el-button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { Refresh, Search } from '@element-plus/icons-vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import PersonaCard from '@/components/memory/PersonaCard.vue'
import MemoryConstellation3D from '@/components/three/MemoryConstellation3D.vue'
import { useBulkDelete } from '@/composables/useBulkDelete'
import { useAdminEvents } from '@/composables/useAdminEvents'
import { api } from '@/api'
import { agentApi, formatTime, type GroupMemoryItem, type Memory, type ProactiveTrigger, type SubjectSummary } from '@/api/agent'
import type { Bot, Conversation } from '@/types'

const tabOptions = [
  { label: '3D 人物星图', value: 'subjects' },
  { label: '群情景记忆', value: 'group' },
  { label: '主动关怀队列', value: 'proactive' },
]
const activeTab = ref<'subjects' | 'group' | 'proactive'>('subjects')
const viewMode = ref<'3d' | 'list'>('3d')
const inspectorOpen = ref(true)
const constellationRef = ref<InstanceType<typeof MemoryConstellation3D> | null>(null)

const stageLabel: Record<string, string> = { stranger: '初识', acquaintance: '脸熟', familiar: '熟人', close_friend: '老铁' }
const linkTypeText: Record<string, string> = { qq_union_id: '全局 UnionID', member_openid: '群成员', user_openid: '私聊用户' }
const kindText: Record<string, string> = { fact: '事实', preference: '偏好', issue: '问题', episode: '共同经历', inside_joke: '老梗默契', social: '社交关系', profile: '资料' }
const memorySourceText: Record<string, string> = { extracted: '自动', reflection: '反思', tool: 'AI 记录', admin: '手动' }
const groupKindText: Record<string, string> = { topic: '话题', event: '事件', vibe: '氛围', meme: '群梗', incident: '异常动态' }
const proactiveKindText: Record<string, string> = { event_care: '日程关怀', issue_followup: '问题回访', milestone: '重要日子', group_warmup: '群内暖场' }
const proactiveStatusText: Record<string, string> = { pending: '待发送', sent: '已发送', skipped: '已跳过', failed: '发送失败', cancelled: '已取消' }

const bots = ref<Bot[]>([])
const subjects = ref<SubjectSummary[]>([])
const subjectsLoading = ref(false)
const subjectQuery = ref('')
const stageFilter = ref('all')
const botFilter = ref('')
const selectedSubject = ref<SubjectSummary | null>(null)
const profileTab = ref<'persona' | 'text'>('persona')
const profileDraft = ref('')
const profileSaved = ref('')
const profileSaving = ref(false)
const reflecting = ref(false)
const profileTokens = computed(() => {
  const text = profileDraft.value
  const cjk = (text.match(/[\u3400-\u9fff]/g) || []).length
  return Math.round(cjk + (text.length - cjk) / 4)
})

const closeOrFamiliarCount = computed(() => subjects.value.filter(s => s.relationshipStage === 'close_friend' || s.relationshipStage === 'familiar').length)
const unionCount = computed(() => subjects.value.filter(s => s.links.some(l => l.identityType === 'qq_union_id')).length)
const crossBotCount = computed(() => subjects.value.filter(s => new Set(s.links.map(l => l.botId)).size > 1).length)

const memories = ref<Memory[]>([])
const memoriesLoading = ref(false)
const memoryQuery = ref('')

function uniqueLinkBots(s: SubjectSummary) {
  return Array.from(new Set(s.links.map(l => l.botName).filter(Boolean))).slice(0, 3)
}

function resetUniverse() {
  constellationRef.value?.resetCamera()
}

async function loadSubjects() {
  subjectsLoading.value = true
  try {
    const list = await agentApi.subjects.list({
      q: subjectQuery.value || undefined,
      stage: stageFilter.value,
      botId: botFilter.value || undefined,
    })
    subjects.value = list
    if (selectedSubject.value) {
      const updated = list.find(x => x.id === selectedSubject.value!.id)
      if (updated) applySubject(updated)
      else {
        selectedSubject.value = list[0] || null
        if (selectedSubject.value) applySubject(selectedSubject.value)
      }
    } else if (list.length) {
      applySubject(list[0])
    }
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '加载人物列表失败')
  } finally {
    subjectsLoading.value = false
  }
}

function applySubject(s: SubjectSummary) {
  selectedSubject.value = s
  profileDraft.value = s.profileContent || ''
  profileSaved.value = s.profileContent || ''
}

async function selectSubject(s: SubjectSummary) {
  applySubject(s)
  inspectorOpen.value = true
  await loadSubjectMemories()
}

async function loadSubjectMemories() {
  if (!selectedSubject.value) { memories.value = []; return }
  memoriesLoading.value = true
  try {
    memories.value = await agentApi.memories.list({ subjectId: selectedSubject.value.id, q: memoryQuery.value || undefined })
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '加载记忆失败')
  } finally {
    memoriesLoading.value = false
  }
}

watch(() => selectedSubject.value?.id, (id, prev) => {
  if (id && id !== prev) loadSubjectMemories()
})

async function saveProfile() {
  if (!selectedSubject.value) return
  profileSaving.value = true
  try {
    await agentApi.subjects.update(selectedSubject.value.id, { profileContent: profileDraft.value })
    profileSaved.value = profileDraft.value
    selectedSubject.value.profileContent = profileDraft.value
    ElMessage.success('档案已保存')
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '保存失败')
  } finally {
    profileSaving.value = false
  }
}

async function clearSubjectProfile() {
  if (!selectedSubject.value) return
  try {
    await ElMessageBox.confirm('清空这位人物的人格画像和文字档案？AI 之后会从后续对话重新沉淀。', '清空画像', { type: 'warning', confirmButtonText: '清空', cancelButtonText: '取消' })
  } catch { return }
  await agentApi.subjects.remove(selectedSubject.value.id, 'clear_profile')
  ElMessage.success('已清空画像')
  await loadSubjects()
}

async function reflectNow() {
  if (!selectedSubject.value) return
  reflecting.value = true
  try {
    await agentApi.subjects.reflect(selectedSubject.value.id)
    ElMessage.success('已完成深度反思整理')
    await loadSubjects()
    await loadSubjectMemories()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '反思失败')
  } finally {
    reflecting.value = false
  }
}

const mergeDialog = ref(false)
const mergeTargetId = ref('')
const merging = ref(false)
function openMergeDialog() {
  mergeTargetId.value = ''
  mergeDialog.value = true
}
async function confirmMerge() {
  if (!selectedSubject.value || !mergeTargetId.value) return
  merging.value = true
  try {
    await agentApi.subjects.merge(selectedSubject.value.id, mergeTargetId.value)
    mergeDialog.value = false
    ElMessage.success('已合并人物及全部跨会话记忆')
    await loadSubjects()
    await loadSubjectMemories()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '合并失败')
  } finally {
    merging.value = false
  }
}

async function unlinkIdentity(linkId: string) {
  if (!selectedSubject.value) return
  try {
    await ElMessageBox.confirm('将这条身份拆分为独立人物？对应机器人的记忆会一并迁出。', '拆分身份', { type: 'warning', confirmButtonText: '拆分', cancelButtonText: '取消' })
  } catch { return }
  await agentApi.subjects.unlink(selectedSubject.value.id, linkId)
  ElMessage.success('已拆分为独立人物')
  await loadSubjects()
  await loadSubjectMemories()
}

const { selectedIds, deleting, deleteSelected } = useBulkDelete(async () => { await loadSubjectMemories(); await loadSubjects() })
function selectRows(rows: { id: string }[]) {
  const visible = new Set(memories.value.map(row => row.id))
  selectedIds.value = rows.filter(row => visible.has(row.id)).map(row => row.id)
}
const memoryDialog = ref(false)
const memoryForm = reactive({ id: '', content: '', kind: 'fact', importance: 5, emotionTag: '', pinned: false, resolved: false })
function editMemory(row: Memory) {
  Object.assign(memoryForm, {
    id: row.id, content: row.content, kind: row.kind, importance: row.importance ?? 5,
    emotionTag: row.emotionTag || '', pinned: row.pinned, resolved: Boolean(row.resolvedAt),
  })
  memoryDialog.value = true
}
async function saveMemory() {
  await agentApi.memories.update(memoryForm.id, {
    content: memoryForm.content, kind: memoryForm.kind, pinned: memoryForm.pinned,
    importance: memoryForm.importance, emotionTag: memoryForm.emotionTag, resolved: memoryForm.resolved,
  })
  memoryDialog.value = false
  await loadSubjectMemories()
}
async function removeMemory(row: Memory) {
  try { await ElMessageBox.confirm('删除这条记忆？删除后不能恢复。', '删除记忆', { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }) } catch { return }
  await agentApi.memories.remove(row.id)
  await loadSubjectMemories()
  await loadSubjects()
}

// Tab 2: 群情景记忆
const groupConversations = ref<Conversation[]>([])
const selectedGroupId = ref('')
const groupLoading = ref(false)
const digesting = ref(false)
const groupMemData = ref<{ groupVibe: string; lastDigestAt?: string; items: GroupMemoryItem[] }>({ groupVibe: '', items: [] })

async function loadGroupConversations() {
  const list = await api.conversations.list()
  groupConversations.value = list.filter(c => c.type === 'group')
  if (!selectedGroupId.value && groupConversations.value.length) selectedGroupId.value = groupConversations.value[0].id
  if (selectedGroupId.value) await loadGroupMemories()
}
async function loadGroupMemories() {
  if (!selectedGroupId.value) return
  groupLoading.value = true
  try { groupMemData.value = await agentApi.groupMemories.list(selectedGroupId.value) }
  finally { groupLoading.value = false }
}
async function digestGroupNow() {
  if (!selectedGroupId.value) return
  digesting.value = true
  try {
    await agentApi.groupMemories.digest(selectedGroupId.value)
    ElMessage.success('群情景记忆整理完成')
    await loadGroupMemories()
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '整理失败')
  } finally {
    digesting.value = false
  }
}
async function removeGroupMemory(id: string) {
  await agentApi.groupMemories.remove(id)
  await loadGroupMemories()
}

// Tab 3: 主动关怀
const proactiveStatus = ref('all')
const proactiveLoading = ref(false)
const proactiveList = ref<ProactiveTrigger[]>([])
async function loadProactive() {
  proactiveLoading.value = true
  try { proactiveList.value = await agentApi.proactive.list(proactiveStatus.value) }
  finally { proactiveLoading.value = false }
}
async function cancelTrigger(id: string) {
  await agentApi.proactive.cancel(id)
  ElMessage.success('已取消该主动关怀')
  await loadProactive()
}

function onTabChange() { reloadCurrent() }
async function reloadCurrent() {
  if (activeTab.value === 'subjects') {
    await loadSubjects()
    await loadSubjectMemories()
  } else if (activeTab.value === 'group') {
    await loadGroupConversations()
  } else {
    await loadProactive()
  }
}

onMounted(async () => {
  bots.value = await api.bots.list().catch(() => [])
  await reloadCurrent()
})

useAdminEvents(['memory_subjects', 'user_identity_links', 'user_memories', 'group_memories', 'proactive_triggers'], () => {
  if (activeTab.value === 'subjects' && profileDraft.value === profileSaved.value) {
    loadSubjects()
    loadSubjectMemories()
  } else if (activeTab.value === 'group') {
    loadGroupMemories()
  } else if (activeTab.value === 'proactive') {
    loadProactive()
  }
})
</script>

<style scoped>
.memory-universe-page { height: 100%; display: flex; flex-direction: column; min-height: 0; }
.universe-stage { position: relative; flex: 1; min-height: 0; height: 100vh; height: 100dvh; overflow: hidden; }

.hud-top-left {
  position: absolute;
  top: 16px;
  left: 16px;
  z-index: 10;
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: calc(100% - 32px);
  padding: 12px 14px;
  border: 1px solid rgba(56, 189, 248, 0.25);
  border-radius: 14px;
  background: rgba(7, 13, 27, 0.82);
  backdrop-filter: blur(18px);
  box-shadow: 0 16px 36px rgba(0, 0, 0, 0.5);
}
.hud-title-row { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; }
.hud-title-row h1 {
  margin: 0;
  font-size: 18px;
  font-weight: 700;
  background: linear-gradient(120deg, #fff 20%, #7dd3fc 70%, #a5b4fc);
  -webkit-background-clip: text;
  -webkit-text-fill-color: transparent;
}
.hud-filters { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; }

.hud-bottom-left {
  position: absolute;
  left: 16px;
  bottom: 16px;
  z-index: 10;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 10px 14px;
  border: 1px solid rgba(148, 163, 184, 0.2);
  border-radius: 12px;
  background: rgba(7, 13, 27, 0.82);
  backdrop-filter: blur(14px);
}
.hud-stats { display: flex; gap: 16px; }
.hud-stats > div { display: flex; flex-direction: column; gap: 1px; }
.hud-stats span { font-size: 11px; color: var(--muted); }
.hud-stats strong { font-size: 16px; color: var(--text); font-variant-numeric: tabular-nums; }
.hud-legend { display: flex; flex-wrap: wrap; gap: 10px; font-size: 11px; color: var(--text-2); }
.hud-legend span { display: inline-flex; align-items: center; gap: 5px; }
.hud-legend i { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
.hud-legend .ring-dot { border: 2px solid #fbbf24; background: transparent; }

.hud-inspector {
  position: absolute;
  top: 16px;
  right: 16px;
  bottom: 16px;
  z-index: 12;
  width: min(440px, calc(100vw - 32px));
  display: flex;
  flex-direction: column;
  border: 1px solid rgba(56, 189, 248, 0.28);
  border-radius: 16px;
  background: rgba(8, 14, 28, 0.88);
  backdrop-filter: blur(22px);
  box-shadow: 0 24px 60px rgba(0, 0, 0, 0.72);
  overflow: hidden;
}
.inspector-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 16px;
  border-bottom: 1px solid var(--line);
}
.inspector-title { display: flex; align-items: center; gap: 8px; min-width: 0; }
.inspector-title strong { font-size: 15px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.inspector-actions { display: flex; align-items: center; gap: 4px; flex: none; }
.inspector-actions .el-button + .el-button { margin-left: 0; }
.inspector-body { flex: 1; min-height: 0; overflow-y: auto; padding: 14px 16px; display: flex; flex-direction: column; gap: 12px; }
.inspector-mem-box { border: 1px solid var(--line); border-radius: var(--radius-sm); overflow: hidden; background: rgba(12, 20, 38, 0.6); }
.mem-head-right { display: flex; align-items: center; gap: 6px; }
.reopen-pill {
  position: absolute;
  right: 16px;
  bottom: 16px;
  z-index: 12;
  padding: 8px 16px;
  border: 1px solid rgba(56, 189, 248, 0.45);
  border-radius: 999px;
  background: rgba(10, 17, 32, 0.9);
  color: #7dd3fc;
  font-size: 13px;
  font-weight: 600;
  cursor: pointer;
  backdrop-filter: blur(12px);
}

.subject-layout { display: grid; grid-template-columns: 310px minmax(0, 1fr); gap: 16px; min-height: 0; }
.subject-list { padding: 10px; overflow-y: auto; min-height: 0; }
.subject-items { list-style: none; margin: 0; padding: 0; display: flex; flex-direction: column; gap: 6px; }
.subject-items li { padding: 10px 12px; border: 1px solid var(--line); border-radius: var(--radius-sm); cursor: pointer; transition: background .12s var(--ease), border-color .12s var(--ease); }
.subject-items li:hover { background: var(--surface-2); }
.subject-items li.active { border-color: var(--primary); background: var(--primary-light); }
.subject-item-top { display: flex; align-items: center; justify-content: space-between; gap: 8px; }
.subject-item-top strong { font-size: 14px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.subject-item-meta { display: flex; align-items: center; justify-content: space-between; margin-top: 4px; font-size: 12px; color: var(--muted); }
.subject-item-links { display: flex; flex-wrap: wrap; gap: 4px; margin-top: 6px; }
.tiny-tag { padding: 1px 6px; border-radius: var(--radius-xs); background: var(--surface-3); font-size: 11px; color: var(--text-2); }
.tiny-tag.union { background: var(--success-light); color: var(--success); }
.mood-chip { padding: 1px 6px; border-radius: var(--radius-xs); background: var(--warning-light); color: var(--warning); font-size: 11px; }

.stage-pill { flex: none; padding: 2px 8px; border-radius: 999px; font-size: 11px; font-weight: 600; background: var(--surface-3); color: var(--text-2); }
.stage-pill.stage-acquaintance { background: rgba(56, 189, 248, 0.16); color: var(--primary-text); }
.stage-pill.stage-familiar { background: var(--success-light); color: var(--success); }
.stage-pill.stage-close_friend { background: var(--warning-light); color: var(--warning); }

.subject-detail { display: grid; grid-template-rows: minmax(280px, auto) minmax(240px, 1fr); gap: 16px; min-height: 0; }
.empty-detail { display: flex; align-items: center; justify-content: center; }
.profile-panel { display: flex; flex-direction: column; gap: 12px; padding: 16px 20px; min-height: 0; }
.detail-head { display: flex; align-items: flex-start; gap: 10px; flex-wrap: wrap; }
.detail-head h2 { margin: 0; font-size: 17px; }
.head-sub { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin-top: 4px; font-size: 12px; color: var(--muted); }

.rel-grid { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; padding: 10px 12px; border-radius: var(--radius-sm); background: var(--surface-2); }
.rel-grid.compact { padding: 8px 10px; gap: 10px; }
.rel-label { display: flex; justify-content: space-between; font-size: 12px; margin-bottom: 4px; color: var(--text-2); }

.links-strip { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: 12px; }
.links-title { color: var(--muted); flex: none; }
.links-chips { display: flex; flex-wrap: wrap; gap: 6px; font-size: 12px; }
.link-chip { display: inline-flex; align-items: center; gap: 6px; padding: 3px 8px; border: 1px solid var(--line); border-radius: var(--radius-xs); background: var(--surface-2); }
.link-chip.isUnion { border-color: var(--success); }
.link-chip code { font-family: var(--font-mono); color: var(--muted); font-size: 11px; }
.unlink-btn { border: 0; background: none; color: var(--danger); cursor: pointer; font-size: 11px; padding: 0 2px; }

.profile-tabs-bar { display: flex; align-items: center; gap: 10px; }
.profile-scroll { max-height: 280px; overflow-y: auto; padding-right: 4px; }
.profile-text { display: flex; flex-direction: column; gap: 8px; }
.token-meter { position: relative; height: 4px; border-radius: 2px; background: var(--surface-3); overflow: hidden; }
.token-meter i { position: absolute; inset: 0; background: var(--primary); transform-origin: left; transition: transform .4s var(--ease); }
.token-meter.over i { background: var(--danger); }

.table-panel { display: flex; flex-direction: column; min-height: 0; padding: 0; overflow: hidden; }
.table-panel > .el-table { flex: 1; min-height: 0; }
.mem-table-head { display: flex; align-items: center; justify-content: space-between; padding: 10px 14px; border-bottom: 1px solid var(--line); gap: 8px; }
.mem-cell { display: flex; flex-direction: column; gap: 4px; }
.mem-sub { display: flex; flex-wrap: wrap; gap: 6px; font-size: 11px; color: var(--muted); }
.emo-badge { padding: 0 5px; border-radius: 4px; background: var(--warning-light); color: var(--warning); }
.pinned { margin-right: 6px; }

.vibe-card { padding: 12px 16px; font-size: 13px; margin-bottom: 12px; }
.dialog-two { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
.dialog-checks { display: flex; gap: 16px; }

@media (max-width: 1100px) {
  .subject-layout { grid-template-columns: 1fr; }
  .subject-list { max-height: 260px; }
  .rel-grid { grid-template-columns: 1fr; }
  .hud-bottom-left { display: none; }
}
</style>
