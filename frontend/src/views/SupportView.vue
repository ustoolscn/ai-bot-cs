<template>
  <div ref="pageRef" class="content-page">
    <PageHeader :title="(botName || '机器人') + ' 的 AI 客服'" :back="{ label: '机器人与客服', to: '/bots' }" description="一个机器人可以有多个 AI 客服，各自负责不同的网站或业务。在群的接待设置里选择由谁接待。">
      <el-button type="primary" :icon="Plus" @click="open('new')">添加 AI 客服</el-button>
    </PageHeader>
    <section class="holo-hero is-compact">
      <HoloBanner3D variant="bots" class="holo-hero-bg" />
      <div class="holo-hero-scrim" aria-hidden="true" />
      <div class="holo-hero-content">
        <div class="holo-hero-title-row">
          <span class="holo-badge"><i /><span>AI SERVICE PERSONAS</span></span>
          <strong style="font-size: 16px">{{ botName || '机器人' }} · {{ services.length }} 个专属 AI 客服</strong>
        </div>
        <p class="holo-hero-desc">每个客服拥有独立的人格设定、陪伴导演层、知识库边界与网站实时查询接口。点击任意行进入深度编排。</p>
      </div>
    </section>
    <section v-loading="busy" class="table-panel cyber-panel">
      <el-table :data="services" row-key="id" class="clickable-rows" @row-click="(row: BotService) => open(row.id)">
        <template #empty>
          <EmptyState v-if="error" mood="thinking" title="加载失败" :description="error"><el-button @click="load()">重新加载</el-button></EmptyState>
          <EmptyState v-else title="还没有 AI 客服" description="添加后到群设置里选择由谁接待。"><el-button type="primary" :icon="Plus" @click="open('new')">添加 AI 客服</el-button></EmptyState>
        </template>
        <el-table-column label="客服名称" min-width="160"><template #default="{ row }"><strong class="svc-name">{{ row.name }}</strong></template></el-table-column>
        <el-table-column label="职责" min-width="240"><template #default="{ row }"><span class="truncate">{{ row.instructions || '—' }}</span></template></el-table-column>
        <el-table-column label="负责网站" min-width="200">
          <template #default="{ row }"><span v-if="row.settings?.liveData?.enabled" class="mono truncate">{{ row.settings.liveData.baseUrl || '—' }}</span><span v-else class="faint">未接入</span></template>
        </el-table-column>
        <el-table-column label="对话模型" min-width="160">
          <template #default="{ row }">{{ row.chatProfileId ? models.find(m => m.id === row.chatProfileId)?.name || '模型不可用' : '系统默认' }}</template>
        </el-table-column>
        <el-table-column width="64" align="right"><template #default><el-icon class="row-chevron"><ArrowRight /></el-icon></template></el-table-column>
      </el-table>
    </section>
  </div>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { Plus, ArrowRight } from '@element-plus/icons-vue'
import PageHeader from '@/components/PageHeader.vue'
import EmptyState from '@/components/EmptyState.vue'
import HoloBanner3D from '@/components/three/HoloBanner3D.vue'
import { useAdminEvents } from '@/composables/useAdminEvents'
import { api } from '@/api'
import { botServices, type BotService } from '@/api/support'
import { animateStagger } from '@/utils/motion'
import type { ModelProfile } from '@/types'

const pageRef = ref<HTMLElement>()

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
  } finally { busy.value = false; nextTick(() => animateStagger(pageRef.value)) }
}
function open(id: string) { router.push({ name: 'bot-service', params: { id: props.botId, serviceId: id } }) }
watch(() => props.botId, load, { immediate: true })
useAdminEvents(['model_profiles', 'bot_services', 'bots'], load)
</script>

<style scoped>
.svc-name { font-weight: 500; }
</style>
