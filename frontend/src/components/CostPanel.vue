<template>
  <section class="panel dash-card cost">
    <header>
      <h2>消费 <span class="muted">按当前价格估算</span></h2>
      <el-segmented v-model="range" :options="rangeOptions" size="small" />
    </header>

    <div class="cost-grid">
      <div class="cost-totals">
        <div><span>今日</span><strong class="money"><i>¥</i><CountUp :value="data?.today" :decimals="2" /></strong></div>
        <div><span>累计</span><strong class="money small"><i>¥</i><CountUp :value="data?.total" :decimals="2" /></strong></div>
      </div>

      <div class="cost-kinds">
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
        <el-segmented v-model="by" :options="byOptions" size="small" />
        <ul v-if="ranked.length">
          <li v-for="row in ranked" :key="row.id">
            <span class="r-name" :class="{ derived: derivedIds.has(row.id) }" :title="row.name">{{ row.name || '未命名' }}</span>
            <span class="r-bar"><i :style="{ transform: `scaleX(${rankMax ? row.cost / rankMax : 0})` }" /></span>
            <strong>{{ money(row.cost) }}</strong>
          </li>
        </ul>
        <p v-else class="muted none">这段时间还没有消费</p>
      </div>
    </div>

    <p v-if="data?.unpricedModels.length" class="unpriced">
      {{ data.unpricedModels.join('、') }} 还没填价格，按 0 计入，实际花费会更高。
      <el-button link type="primary" size="small" @click="$router.push('/models')">去填写</el-button>
    </p>
  </section>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
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
.cost { display: flex; flex-direction: column; gap: 14px; padding: 16px 20px; }
.cost > header { display: flex; align-items: center; justify-content: space-between; gap: 12px; min-height: 28px; flex-wrap: wrap; }
.cost h2 { display: flex; align-items: baseline; gap: 8px; font-size: 15px; font-weight: 600; }
.cost h2 .muted { font-size: 12px; font-weight: 400; }
.cost-grid { display: grid; grid-template-columns: minmax(140px, .7fr) minmax(0, 1.2fr) minmax(0, 1.3fr); gap: 28px; align-items: start; }

.cost-totals { display: flex; flex-direction: column; gap: 12px; }
.cost-totals div { display: flex; flex-direction: column; }
.cost-totals span { color: var(--muted); font-size: 12px; }
.money { font-size: 28px; font-weight: 600; line-height: 1.15; font-variant-numeric: tabular-nums; }
.money.small { font-size: 18px; color: var(--text-2); }
.money i { margin-right: 2px; font-size: .6em; font-style: normal; color: var(--muted); }
.money :deep(.count-up) { font-size: inherit; }

.stack { display: flex; gap: 2px; height: 8px; border-radius: 4px; overflow: hidden; background: var(--surface-3); }
.stack i { flex-basis: 0; min-width: 0; transition: flex-grow .6s var(--ease); }
.stack.empty i { display: none; }
.chat { background: var(--primary); }
.embedding { background: var(--success); }
.image { background: var(--warning); }
.cost-kinds ul, .cost-rank ul { margin: 12px 0 0; padding: 0; list-style: none; display: flex; flex-direction: column; gap: 8px; }
.cost-kinds li { display: grid; grid-template-columns: 8px 36px minmax(0, 1fr) auto; align-items: center; gap: 8px; font-size: 13px; }
.dot { width: 8px; height: 8px; border-radius: 2px; }
.k-label { color: var(--text-2); }
.k-usage { color: var(--muted); font-size: 12px; font-variant-numeric: tabular-nums; }
.cost-kinds strong, .cost-rank strong { font-weight: 600; font-variant-numeric: tabular-nums; text-align: right; }

.cost-rank li { display: grid; grid-template-columns: minmax(0, 1fr) minmax(48px, .8fr) 76px; align-items: center; gap: 10px; font-size: 13px; }
.r-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.r-name.derived { color: var(--muted); }
.r-bar { position: relative; height: 6px; border-radius: 3px; background: var(--surface-3); overflow: hidden; }
.r-bar i { position: absolute; inset: 0; border-radius: 3px; background: var(--primary); transform-origin: left; transition: transform .6s var(--ease); }
.none { margin: 14px 0 0; font-size: 13px; }

.unpriced { margin: 0; padding-top: 10px; border-top: 1px solid var(--line); color: var(--warning); font-size: 12px; }
@media (max-width: 1100px) { .cost-grid { grid-template-columns: 1fr 1fr; } .cost-rank { grid-column: 1 / -1; } }
@media (max-width: 640px) { .cost-grid { grid-template-columns: 1fr; } }
</style>
