<template>
  <div class="content-page">
    <PageHeader :title="(botName || '机器人') + ' 的 AI 客服'" :back="{ label: '机器人与客服', to: '/bots' }">
      <el-button type="primary" :icon="Plus" @click="open('new')">添加 AI 客服</el-button>
    </PageHeader>
    <section v-loading="busy" class="table-panel">
      <el-table :data="services" row-key="id" class="clickable-rows" @row-click="(row: BotService) => open(row.id)">
        <template #empty>
          <EmptyState v-if="error" mood="thinking" title="加载失败" :description="error"><el-button @click="load()">重新加载</el-button></EmptyState>
          <EmptyState v-else title="还没有 AI 客服" description="添加后到群设置里选择由谁接待。"><el-button type="primary" :icon="Plus" @click="open('new')">添加 AI 客服</el-button></EmptyState>
        </template>
        <el-table-column label="客服名称" min-width="160"><template #default="{ row }"><strong style="font-weight: 500">{{ row.name }}</strong></template></el-table-column>
        <el-table-column label="职责" min-width="240"><template #default="{ row }"><span class="truncate">{{ row.instructions || '—' }}</span></template></el-table-column>
        <el-table-column label="负责网站" min-width="200">
          <template #default="{ row }"><span v-if="row.settings?.liveData?.enabled" class="mono truncate">{{ row.settings.liveData.baseUrl || '—' }}</span><span v-else class="faint">未接入</span></template>
        </el-table-column>
        <el-table-column label="对话模型" min-width="160">
          <template #default="{ row }">{{ row.chatProfileId ? models.find(m => m.id === row.chatProfileId)?.name || '模型不可用' : '系统默认' }}</template>
        </el-table-column>
        <el-table-column width="80" align="right"><template #default="{ row }"><el-button link type="primary" @click.stop="open(row.id)">设置</el-button></template></el-table-column>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Plus } from '@element-plus/icons-vue'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import { useAdminEvents } from '@/composables/useAdminEvents'
import { api } from '@/api'
import { botServices, type BotService } from '@/api/support'
import type { ModelProfile } from '@/types'

const props = defineProps<{ botId: string }>()
const router = useRouter()
const services = ref<BotService[]>([])
const models = ref<ModelProfile[]>([])
const botName = ref('')
const busy = ref(true)
const error = ref('')

async function load() {
  busy.value = true
  error.value = ''
  try {
    const [list, bots, profiles] = await Promise.all([botServices(props.botId).list(), api.bots.list(), api.models.list()])
    services.value = list
    models.value = profiles
    botName.value = bots.find(b => b.id === props.botId)?.name || ''
  } catch (e) {
    error.value = e instanceof Error ? e.message : '加载失败'
  } finally { busy.value = false }
}
function open(id: string) { router.push({ name: 'bot-service', params: { id: props.botId, serviceId: id } }) }
watch(() => props.botId, load, { immediate: true })
useAdminEvents(['model_profiles', 'bot_services', 'bots'], load)
</script>
