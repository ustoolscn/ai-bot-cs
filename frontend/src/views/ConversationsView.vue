<template>
  <div class="content-page">
    <PageHeader title="群与会话" description="机器人加入的每个群和私聊都在这里。可以分别设置什么时候回复、由哪个客服接待、查哪些知识库。"><el-button :icon="Refresh" @click="load">刷新</el-button></PageHeader>
    <div class="toolbar">
      <el-input v-model="search" placeholder="搜索群名称或会话 ID" :prefix-icon="Search" clearable />
      <el-segmented v-model="type" :options="[{ label: '全部', value: 'all' }, { label: '群聊', value: 'group' }, { label: '私聊', value: 'c2c' }]" />
      <el-select v-model="trigger"><el-option label="全部回复方式" value="all" /><el-option label="被 @ 时回复" value="mention_only" /><el-option label="AI 判断后回复" value="smart" /><el-option label="已停用" value="disabled" /></el-select>
    </div>
    <section class="table-panel">
      <el-table :data="filtered" row-key="id" class="clickable-rows" empty-text="还没有会话。把机器人拉进 QQ 群或有人私聊它后，会自动出现在这里。" @row-click="edit">
        <el-table-column label="会话" min-width="240"><template #default="{ row }"><div class="cell-primary"><span class="entity-icon small" :class="{ purple: row.type !== 'group' }"><el-icon><component :is="row.type === 'group' ? ChatDotRound : User" /></el-icon></span><span><strong>{{ row.name }}</strong><small>{{ row.botName }} · {{ row.type === 'group' ? '群聊' : '私聊' }}</small></span></div></template></el-table-column>
        <el-table-column label="回复方式" width="160"><template #default="{ row }"><div class="trigger-status"><el-tag :type="row.triggerMode === 'disabled' ? 'info' : row.triggerMode === 'smart' ? 'warning' : 'primary'" effect="light">{{ triggerText(row.triggerMode) }}</el-tag><small v-if="row.type === 'group' && row.triggerMode === 'smart' && !row.hasFullMessageEvents">未收到群全量消息</small></div></template></el-table-column>
        <el-table-column label="知识库" min-width="190"><template #default="{ row }"><span :class="row.knowledgeBaseNames.length ? '' : 'faint'">{{ row.knowledgeBaseNames.join('、') || '未绑定' }}</span></template></el-table-column>
        <el-table-column label="成员 / 消息" width="120"><template #default="{ row }"><div class="stacked-cell"><strong>{{ row.memberCount }} 人</strong><span>{{ row.messageCount.toLocaleString() }} 条</span></div></template></el-table-column>
        <el-table-column prop="lastActiveAt" label="最近活跃" width="140" />
        <el-table-column label="操作" width="290" fixed="right"><template #default="{ row }"><el-button v-if="row.type === 'group'" link type="primary" :loading="syncingNames.has(row.id)" @click.stop="syncName(row)">同步群名</el-button><el-button link type="primary" @click.stop="edit(row)">配置</el-button><el-button link type="primary" @click.stop="compose=row">发送</el-button><el-button link type="primary" @click.stop="openMembers(row)">成员</el-button><el-button v-if="row.type === 'group'" link type="primary" @click.stop="guardGroup=row">群管理</el-button></template></el-table-column>
      </el-table>
    </section>
    <el-drawer v-model="drawer" :title="`配置 · ${editing.name || ''}`" size="520px">
      <el-form label-position="top">
        <div class="drawer-section">
          <h3>名称</h3>
          <p>可自定义显示名称。点击列表里的“同步群名”会用 QQ 当前的群名替换显示名称。</p>
          <el-input v-model="editing.name" />
        </div>
        <div class="drawer-section">
          <h3>接待客服</h3>
          <p>可以选择此 QQ 机器人下的一个 AI 客服。</p>
          <el-select v-model="editing.serviceId" :value-on-clear="''" clearable placeholder="使用默认人设" :loading="loadingServices">
            <el-option v-for="s in services" :key="s.id" :label="s.name" :value="s.id" />
          </el-select>
          <small v-if="serviceError" class="form-tip">{{ serviceError }}</small>
        </div>
        <div class="drawer-section">
          <h3>什么时候回复</h3>
          <p>开启 AI 判断后，每条收到的群消息都会先判断是否需要客服接话。</p>
          <el-radio-group v-model="editing.triggerMode"><el-radio-button value="mention_only">被 @ 时回复</el-radio-button><el-radio-button v-if="editing.type === 'group'" value="smart">AI 判断后回复</el-radio-button><el-radio-button value="disabled">停用</el-radio-button></el-radio-group>
          <el-alert v-if="editing.type === 'group' && editing.triggerMode === 'smart' && !editing.hasFullMessageEvents" style="margin-top: 12px" title="还没收到这个群的全量消息" description="“AI 判断后回复”需要在 QQ 开放平台订阅群聊全量消息，并由群主允许机器人接收群内全部消息。" type="warning" :closable="false" show-icon />
        </div>
        <div class="drawer-section">
          <h3>回复时参考的上下文</h3>
          <el-form-item label="带上最近几条消息"><el-slider v-model="editing.contextLimit" :min="1" :max="50" show-input /><small class="form-tip">包含当前这条；设为 1 表示只看当前问题。</small></el-form-item>
        </div>
        <div class="drawer-section">
          <h3>知识库与补充说明</h3>
          <p>机器人回答前会先查这里绑定的知识库。补充说明会接在“智能体”的说话风格后面。</p>
          <el-form-item label="绑定知识库"><el-select v-model="editing.knowledgeBaseIds" multiple placeholder="选择知识库"><el-option v-for="kb in knowledgeBases" :key="kb.id" :label="kb.name" :value="kb.id" /></el-select></el-form-item>
          <el-form-item label="这个群的补充说明"><el-input v-model="editing.systemPrompt" type="textarea" :rows="5" placeholder="例如：这是售后群，只回答退换货和保修问题" /></el-form-item>
        </div>
      </el-form>
      <template #footer><el-button @click="drawer = false">取消</el-button><el-button type="primary" @click="save">保存</el-button></template>
    </el-drawer>
    <QQCompose v-if="compose" :key="compose.id" :row="compose" @close="compose=null"/>
    <GroupGuardDrawer v-if="guardGroup" :group="guardGroup" @close="guardGroup=null"/>
    <MembersDrawer v-model="membersOpen" :conversation-id="membersConv.id" :conversation-name="membersConv.name" :is-group="membersConv.isGroup" @synced="load" :bot-id="membersConv.botId" :bot-name="membersConv.botName" />
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { botServices, type BotService } from "@/api/support"
import { computed, onMounted, reactive, ref } from 'vue'; import { ChatDotRound, Refresh, Search, User } from '@element-plus/icons-vue'; import { ElMessage } from 'element-plus'; import { api } from '@/api'; import PageHeader from '@/components/PageHeader.vue'; import MembersDrawer from '@/components/agent/MembersDrawer.vue'; import type { Conversation, KnowledgeBase } from '@/types'
import GroupGuardDrawer from "@/components/agent/GroupGuardDrawer.vue"
import QQCompose from '@/components/agent/QQCompose.vue'
const compose=ref<Conversation|null>(null)
const guardGroup=ref<Conversation|null>(null)
const conversations = ref<Conversation[]>([]); const knowledgeBases=ref<KnowledgeBase[]>([]); const search = ref(''); const type = ref('all'); const trigger = ref('all'); const drawer = ref(false); const editing = reactive<Conversation>({} as Conversation)
const filtered = computed(() => conversations.value.filter(c => (!search.value || `${c.name}${c.id}`.includes(search.value)) && (type.value === 'all' || c.type === type.value) && (trigger.value === 'all' || c.triggerMode === trigger.value)))
onMounted(load); async function load() { const [items,kbs]=await Promise.all([api.conversations.list(),api.knowledge.list()]);knowledgeBases.value=kbs;for (const item of items) item.knowledgeBaseNames=(item.knowledgeBaseIds || []).map(id=>kbs.find(kb=>kb.id===id)?.name || id);conversations.value=items }
function triggerText(value: string) { return ({ mention_only: '被 @ 时回复', smart: 'AI 判断后回复', disabled: '已停用' } as Record<string,string>)[value] }
const syncingNames = reactive(new Set<string>())
async function syncName(row: Conversation) {
  if (syncingNames.has(row.id)) return
  syncingNames.add(row.id)
  try {
    const { name } = await api.conversations.syncName(row.id)
    const current = conversations.value.find(item => item.id === row.id)
    if (current) current.name = name
    ElMessage.success(`已同步群名：${name}`)
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '群名同步失败')
  } finally {
    syncingNames.delete(row.id)
  }
}
const services = ref<BotService[]>([])
const loadingServices = ref(false)
const serviceError = ref('')
async function edit(row: Conversation) { Object.assign(editing, row, { knowledgeBaseNames: [...row.knowledgeBaseNames], knowledgeBaseIds: [...row.knowledgeBaseIds] }); drawer.value = true
  services.value = []
  loadingServices.value = true
  serviceError.value = ''
  try {
    const options = await botServices(row.botId).list()
    if (editing.id === row.id) services.value = options
  } catch (e) {
    if (editing.id === row.id) serviceError.value = e instanceof Error ? e.message : '客服加载失败'
  } finally { if (editing.id === row.id) loadingServices.value = false }
}
const membersOpen = ref(false); const membersConv = reactive({ isGroup:false, id: '', name: '', botId: '', botName: '' }); function openMembers(row: Conversation) { Object.assign(membersConv, {isGroup:row.type==='group', id: row.id, name: row.name, botId: row.botId, botName: row.botName }); membersOpen.value = true }
async function save() { editing.enabled=editing.triggerMode!=='disabled';await api.conversations.save(editing.id, editing); await load(); drawer.value = false; ElMessage.success('会话配置已保存') }
useAdminEvents(['conversations','messages','conversation_members','bots','knowledge_bases','conversation_knowledge_bases'], load)
</script>
