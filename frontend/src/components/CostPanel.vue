<template>
  <section class="panel dash-card cost">
    <header>
      <h2>消费 <span class="muted">按当前价格估算</span></h2>
      <el-tooltip v-if="data?.unpricedModels.length" placement="top" :content="`${data.unpricedModels.join('、')} 还没填价格，按 0 计入，实际花费会更高。点击去填写。`">
        <button type="button" class="unpriced" @click="$router.push('/models')"><el-icon><WarningFilled /></el-icon>{{ data.unpricedModels.length }} 个模型未填价格</button>
      </el-tooltip>
      <span class="spacer" />
      <el-segmented v-model="range" :options="rangeOptions" size="small" />
    </header>

    <div class="cost-grid">
      <div class="cost-totals">
        <div><span>今日</span><strong class="money"><i>¥</i><CountUp :value="data?.today" :decimals="2" /></strong></div>
        <div><span>累计</span><strong class="money small"><i>¥</i><CountUp :value="data?.total" :decimals="2" /></strong></div>
      </div>

      <div class="cost-kinds">
        <div class="col-head"><span>按类型</span></div>
        <div class="stack" :class="{ empty: !rangeTotal }">
          <i v-for="k in kinds" :key="k.kind" :class="k.kind" :style="{ flexGrow: k.cost }" />
        </div>
        <ul>
          <li v-for="k in kinds" :key="k.kind">
            <span class="dot" :class="k.kind" />
            <span class="k-label">{{ k.label }}</span>
            <span class="k-usage">{{ k.usage }}</span>
            <strong>{{ money(k.cost) }}</strong>
          </li>
        </ul>
      </div>

      <div class="cost-rank">
        <div class="col-head"><span>花得最多</span><el-segmented v-model="by" :options="byOptions" size="small" /></div>
        <Transition name="fade" mode="out-in">
          <ul v-if="ranked.length" :key="by + range">
            <li v-for="row in ranked" :key="row.id">
              <span class="r-name" :class="{ derived: derivedIds.has(row.id) }" :title="row.name">{{ row.name || '未命名' }}</span>
              <span class="r-bar"><i :style="{ transform: `scaleX(${rankMax ? row.cost / rankMax : 0})` }" /></span>
              <strong>{{ money(row.cost) }}</strong>
            </li>
          </ul>
          <p v-else class="muted none">这段时间还没有消费</p>
        </Transition>
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'
import { api } from '@/api'
import CountUp from '@/components/fx/CountUp.vue'
import type { CostRange, CostSummary } from '@/types'

const range = ref<CostRange>('today')
const by = ref<'group' | 'bot'>('group')
const rangeOptions = [{ label: '今天', value: 'today' }, { label: '7 天', value: '7d' }, { label: '30 天', value: '30d' }, { label: '全部', value: 'all' }]
const byOptions = [{ label: '按群', value: 'group' }, { label: '按机器人', value: 'bot' }]
const data = ref<CostSummary>()
// 没有会话归属的调用由后端归到这两类
const derivedIds = new Set(['knowledge-index', 'system'])

function money(v: number | null | undefined) {
  if (v == null) return '—'
  return '¥' + v.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: v > 0 && v < 0.01 ? 4 : 2 })
}
function tokens(n: number) {
  if (n >= 1e8) return (n / 1e8).toFixed(2) + ' 亿'
  if (n >= 1e4) return (n / 1e4).toFixed(n >= 1e6 ? 0 : 1) + ' 万'
  return String(n)
}

const kinds = computed(() => {
  const rows = data.value?.byKind || []
  const get = (kind: string) => rows.find(r => r.kind === kind) || { cost: 0, inputTokens: 0, outputTokens: 0, images: 0 }
  const chat = get('chat'), emb = get('embedding'), img = get('image')
  return [
    { kind: 'chat', label: '对话', cost: chat.cost, usage: `${tokens(chat.inputTokens + chat.outputTokens)} token` },
    { kind: 'embedding', label: '向量', cost: emb.cost, usage: `${tokens(emb.inputTokens + emb.outputTokens)} token` },
    { kind: 'image', label: '生图', cost: img.cost, usage: `${img.images} 张` },
  ]
})
const rangeTotal = computed(() => kinds.value.reduce((sum, k) => sum + k.cost, 0))
const ranked = computed(() => ((by.value === 'group' ? data.value?.byConversation : data.value?.byBot) || []).filter(r => r.cost > 0).slice(0, 6))
const rankMax = computed(() => Math.max(0, ...ranked.value.map(r => r.cost)))

let lastLoad = 0
async function load() {
  lastLoad = Date.now()
  try { data.value = await api.system.costs(range.value) } catch { /* 首页不因消费统计失败而报错 */ }
}
// 实时事件很频繁，后台刷新最多 15 秒一次
function refresh(force = false) { if (force || Date.now() - lastLoad > 15000) load() }
watch(range, load)
onMounted(load)
defineExpose({ refresh })
</script>

<style scoped>
.cost { display: flex; flex-direction: column; gap: 18px; padding: 18px 22px 22px; }
.cost > header { display: flex; align-items: center; gap: 12px; height: 32px; }
.cost h2 { display: flex; align-items: baseline; gap: 8px; font-size: 15px; font-weight: 600; white-space: nowrap; }
.cost h2 .muted { font-size: 12px; font-weight: 400; }
.unpriced { display: inline-flex; align-items: center; gap: 4px; height: 24px; padding: 0 8px; border: 0; border-radius: var(--radius-xs); background: var(--warning-light); color: var(--warning); font-size: 12px; cursor: pointer; transition: filter var(--dur-fast) var(--ease); }
.unpriced:hover { filter: brightness(.97); }
.cost-grid { display: grid; grid-template-columns: minmax(140px, .6fr) minmax(0, 1.2fr) minmax(0, 1.3fr); gap: 16px; align-items: stretch; }
.cost-totals, .cost-kinds, .cost-rank { padding: 14px 16px; border: 1px solid rgba(148, 163, 184, 0.14); border-radius: var(--radius-md); background: rgba(13, 22, 42, 0.65); }
.col-head { display: flex; align-items: center; justify-content: space-between; gap: 8px; height: 28px; margin-bottom: 8px; color: var(--muted); font-size: 12px; }

.cost-totals { display: flex; flex-direction: column; gap: 16px; padding-top: 2px; }
.cost-totals div { display: flex; flex-direction: column; }
.cost-totals span { color: var(--muted); font-size: 12px; }
.money { font-size: 28px; font-weight: 600; line-height: 1.2; font-variant-numeric: tabular-nums; }
.money.small { font-size: 18px; color: var(--text-2); }
.money i { margin-right: 2px; font-size: .6em; font-style: normal; color: var(--muted); }
.money :deep(.count-up) { font-size: inherit; }

.stack { display: flex; gap: 2px; height: 8px; border-radius: 4px; overflow: hidden; background: var(--surface-3); }
.stack i { flex-basis: 0; min-width: 0; transition: flex-grow .6s var(--ease); }
.stack.empty i { display: none; }
.chat { background: var(--primary); }
.embedding { background: var(--success); }
.image { background: var(--warning); }
.cost-kinds ul, .cost-rank ul { margin: 12px 0 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 10px; }
.cost-rank ul { margin-top: 0; }
.cost-kinds li { display: grid; grid-template-columns: 8px 36px minmax(0, 1fr) auto; align-items: center; gap: 8px; font-size: 13px; }
.dot { width: 8px; height: 8px; border-radius: 2px; }
.k-label { color: var(--text-2); }
.k-usage { color: var(--muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.cost-kinds strong, .cost-rank strong { font-weight: 600; font-variant-numeric: tabular-nums; text-align: right; }

.cost-rank li { display: grid; grid-template-columns: minmax(0, 1fr) minmax(48px, .9fr) 72px; align-items: center; gap: 12px; font-size: 13px; }
.r-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.r-name.derived { color: var(--muted); }
.r-bar { position: relative; height: 6px; border-radius: 3px; background: var(--surface-3); overflow: hidden; }
.r-bar i { position: absolute; inset: 0; border-radius: 3px; background: var(--primary); transform-origin: left; transition: transform .6s var(--ease); }
.none { margin: 4px 0 0; font-size: 13px; }
@media (max-width: 1100px) { .cost-grid { grid-template-columns: 1fr 1fr; } .cost-rank { grid-column: 1 / -1; } }
@media (max-width: 640px) { .cost-grid { grid-template-columns: 1fr; } .cost > header { flex-wrap: wrap; height: auto; } }
</style>
