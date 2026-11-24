<template>
  <section class="settings-panel" v-loading="loading">
    <div class="panel-title">
      <h2>同步状态</h2>
      <el-tooltip :content="blockedReason" :disabled="!blockedReason" placement="top">
        <span><el-button size="small" :icon="Refresh" :loading="refreshing" :disabled="!!blockedReason" @click="refresh">立即同步</el-button></span>
      </el-tooltip>
    </div>
    <p v-if="blockedReason" class="muted" style="margin-bottom: 12px">{{ blockedReason }}</p>
    <template v-if="data">
      <dl class="detail-list">
        <div><dt>站点</dt><dd>{{ data.systemName || '—' }}</dd></div>
        <div><dt>最近成功同步</dt><dd>{{ formatTime(data.fetchedAt) }}<span v-if="data.possiblyStale && !data.lastError" class="stale-note">数据可能过时，改过网站后点“立即同步”</span></dd></div>
        <div><dt>最近尝试</dt><dd>{{ formatTime(data.lastAttemptAt) }}</dd></div>
        <div><dt>模型数量</dt><dd>{{ data.modelCount }}</dd></div>
      </dl>
      <p v-if="data.lastError" class="danger-text" style="margin-top: 8px; font-size: 13px">最近一次同步失败：{{ data.lastError }}</p>
      <h3>注入提示词的内容</h3>
      <div class="content-box">{{ data.promptPreview || '暂无数据' }}</div>
      <h3>全站服务状态</h3>
      <p class="muted status-note">这里是最近一次同步的结果。群里有人问状态或要状态图时，会当场重新查询。</p>
      <dl v-if="data.serviceStatus?.hour || data.serviceStatus?.day" class="detail-list">
        <template v-for="g in serviceWindows" :key="g.key">
          <div v-if="data.serviceStatus[g.key]">
            <dt>{{ g.label }}</dt>
            <dd>
              成功 {{ data.serviceStatus[g.key]!.overall.success_count }} / {{ data.serviceStatus[g.key]!.overall.request_count }} 次 ·
              成功率 {{ metric(data.serviceStatus[g.key]!.overall.success_rate, '%') }} ·
              首个 Token 延迟中位数 {{ metric(data.serviceStatus[g.key]!.overall.median_ttft_ms, ' ms') }}
              <small>（{{ formatTime(data.serviceStatus[g.key]!.generatedAt) }} 更新）</small>
            </dd>
          </div>
        </template>
      </dl>
      <span v-else class="muted">暂无服务状态统计</span>
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
        <el-table-column label="计费规则与单价" min-width="260">
          <template #default="{ row }">
            <template v-if="row.billing === 'per_token'">
              输入 {{ row.inputUsdPerMillionTokens }} · 输出 {{ row.outputUsdPerMillionTokens }}
              <span v-if="row.cacheReadUsdPerMillionTokens != null"> · 缓存读取 {{ row.cacheReadUsdPerMillionTokens }}</span>
              <span v-if="row.cacheWriteUsdPerMillionTokens != null"> · 缓存写入 {{ row.cacheWriteUsdPerMillionTokens }}</span>
              美元 / 百万 Token
            </template>
            <span v-else-if="row.billing === 'per_call'">{{ row.usdPerCall }} 美元 / 次</span>
            <details v-else-if="row.billing === 'tiered_expr'">
              <summary>阶梯计费 · 查看单价与条件</summary>
              <div>输入 p、输出 c、缓存读取 cr、缓存写入 cc 的系数单位为美元 / 百万 Token。公式结果乘分组倍率 {{ row.groupRatio }}。</div>
              <code class="price-expression">{{ row.baseBillingExpr }}</code>
            </details>
            <template v-else-if="row.billing === 'video_seconds'">
              <div v-for="(rate, resolution) in row.outputUsdPerSecond" :key="resolution">{{ resolution }} 输出 {{ rate }} 美元 / 秒</div>
              <div v-if="row.inputUsdPerSecond != null">输入视频另计 {{ row.inputUsdPerSecond }} 美元 / 秒</div>
              <div v-if="row.baseFps">基准帧率 {{ row.baseFps }} FPS</div>
            </template>
            <span v-else>暂无可用报价</span>
          </template>
        </el-table-column>
      </el-table>
      <h3>最新公告</h3>
      <div v-for="(a, i) in (data.announcements || []).slice(0, 5)" :key="i" class="content-box gap"><small class="muted">{{ formatTime(a.publishDate) }}</small><br />{{ a.content }}</div>
      <div v-if="data.notice" class="content-box">{{ data.notice }}</div>
    </template>
  </section>
</template>

<script setup lang="ts">
import { useAdminEvents } from '@/composables/useAdminEvents'
import { computed, onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { agentApi, formatTime, type LiveData } from '@/api/agent'

// blockedReason：设置有未保存的修改或实时数据未启用时，先不允许同步，避免后端按旧配置同步失败（400）。
const props = defineProps<{ blockedReason?: string; source?: { get: () => Promise<LiveData>; refresh: () => Promise<LiveData> } }>()
const api = () => props.source || agentApi.liveData

const serviceWindows = [{ key: 'hour', label: '最近 24 小时' }, { key: 'day', label: '最近 90 天' }] as const
function metric(value: number | null, unit: string) { return value == null ? '暂无数据' : value + unit }
const data = ref<LiveData | null>(null)
const loading = ref(false)
const refreshing = ref(false)
const filter = ref('')
const filteredModels = computed(() => (data.value?.models || []).filter(m => !filter.value || m.model.toLowerCase().includes(filter.value.toLowerCase())))
function statusText(s: number) { return ({ 1: '正常', 0: '异常', 2: '检测中', 3: '维护中' } as Record<number, string>)[s] || '未知' }
async function load() {
  loading.value = true
  try { data.value = await api().get() } catch (e) { ElMessage.error(e instanceof Error ? e.message : '加载失败') } finally { loading.value = false }
}
async function refresh() {
  if (props.blockedReason) return
  refreshing.value = true
  try { data.value = await api().refresh(); ElMessage.success('已同步') } catch (e) { ElMessage.error(e instanceof Error ? e.message : '同步失败') } finally { refreshing.value = false }
}
onMounted(load)
defineExpose({ load })
useAdminEvents(['live_data_snapshots'], load)
</script>

<style scoped>
.stale-note { margin-left: 8px; color: var(--warning); font-size: 12px; }
.price-expression { overflow-wrap: anywhere; white-space: pre-wrap; }
.panel-title { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
.panel-title h2 { margin: 0; }
h3 { font-size: 14px; font-weight: 600; margin: 20px 0 8px; }
.chips { display: flex; flex-wrap: wrap; gap: 6px; }
.gap { margin-bottom: 8px; }
.muted { color: var(--muted); font-size: 13px; }
.status-note { margin: -4px 0 8px; }
</style>
