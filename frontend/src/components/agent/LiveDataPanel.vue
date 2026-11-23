<template>
  <section class="settings-panel" v-loading="loading">
    <div class="panel-title">
      <h2>同步状态</h2>
      <el-button size="small" :icon="Refresh" :loading="refreshing" @click="refresh">立即同步</el-button>
    </div>
    <template v-if="data">
      <dl class="detail-list">
        <div><dt>站点</dt><dd>{{ data.systemName || '—' }}</dd></div>
        <div><dt>最近成功同步</dt><dd>{{ formatTime(data.fetchedAt) }}</dd></div>
        <div><dt>最近尝试</dt><dd>{{ formatTime(data.lastAttemptAt) }}</dd></div>
        <div><dt>模型数量</dt><dd>{{ data.modelCount }}</dd></div>
      </dl>
      <el-alert v-if="data.lastError" class="gap" :title="data.lastError" type="warning" :closable="false" show-icon />
      <h3>注入提示词的内容</h3>
      <div class="content-box">{{ data.promptPreview || '暂无数据' }}</div>
      <h3>分组状态</h3>
      <div class="chips">
        <el-tag v-for="m in data.monitors || []" :key="m.category + m.name" :type="m.status === 1 ? 'success' : m.status === 3 ? 'info' : 'danger'" effect="plain">{{ m.name }} · {{ statusText(m.status) }}</el-tag>
        <span v-if="!data.monitors?.length" class="muted">未获取到监控状态（站点未启用 Uptime Kuma 时为空）</span>
      </div>
      <h3>分组倍率</h3>
      <div class="chips">
        <el-tag v-for="g in data.groups || []" :key="g.group" effect="plain">{{ g.group }}<template v-if="g.ratio !== undefined"> × {{ g.ratio }}</template><template v-if="g.description"> · {{ g.description }}</template></el-tag>
      </div>
      <h3>模型价格（默认分组，美元）</h3>
      <el-input v-model="filter" size="small" placeholder="筛选模型" clearable class="gap" />
      <el-table :data="filteredModels" size="small" max-height="320">
        <el-table-column prop="model" label="模型" min-width="180" />
        <el-table-column label="输入 / 1M" width="100"><template #default="{ row }">{{ row.billing === 'per_call' ? '—' : row.inputUsdPerMillionTokens }}</template></el-table-column>
        <el-table-column label="输出 / 1M" width="100"><template #default="{ row }">{{ row.billing === 'per_call' ? '—' : row.outputUsdPerMillionTokens }}</template></el-table-column>
        <el-table-column label="按次" width="80"><template #default="{ row }">{{ row.billing === 'per_call' ? row.usdPerCall : '—' }}</template></el-table-column>
      </el-table>
      <h3>最新公告</h3>
      <div v-for="(a, i) in (data.announcements || []).slice(0, 5)" :key="i" class="content-box gap"><small class="muted">{{ formatTime(a.publishDate) }}</small><br />{{ a.content }}</div>
      <div v-if="data.notice" class="content-box">{{ data.notice }}</div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { agentApi, formatTime, type LiveData } from '@/api/agent'

const data = ref<LiveData | null>(null)
const loading = ref(false)
const refreshing = ref(false)
const filter = ref('')
const filteredModels = computed(() => (data.value?.models || []).filter(m => !filter.value || m.model.toLowerCase().includes(filter.value.toLowerCase())))
function statusText(s: number) { return ({ 1: '正常', 0: '异常', 2: '检测中', 3: '维护中' } as Record<number, string>)[s] || '未知' }
async function load() {
  loading.value = true
  try { data.value = await agentApi.liveData.get() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '加载失败') } finally { loading.value = false }
}
async function refresh() {
  refreshing.value = true
  try { data.value = await agentApi.liveData.refresh(); ElMessage.success('已同步') } catch (e) { ElMessage.error(e instanceof Error ? e.message : '同步失败') } finally { refreshing.value = false }
}
onMounted(load)
defineExpose({ load })
</script>

<style scoped>
.panel-title { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.panel-title h2 { margin: 0; }
h3 { font-size: 13px; margin: 18px 0 8px; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; }
.gap { margin-bottom: 8px; }
.muted { color: var(--muted); font-size: 12px; }
</style>
