<template>
  <div class="persona">
    <EmptyState v-if="!persona" compact mood="thinking" title="还没有人格画像" description="这位用户多聊几轮后，AI 会从对话里慢慢总结出来。证据不足的维度会先空着。" />
    <template v-else>
      <div class="persona-head">
        <p class="persona-summary">{{ persona.summary || '还没有一句话画像' }}</p>
        <div class="persona-meta">
          <span v-if="persona.relationship?.mood" class="pill" :class="moodTone">{{ persona.relationship.mood }}<template v-if="trend"> <el-icon :class="'trend-' + persona.relationship.trend"><component :is="trend" /></el-icon></template></span>
          <span class="muted">基于 {{ persona.observedTurns ?? 0 }} 轮对话<template v-if="persona.updatedAt"> · {{ formatTime(persona.updatedAt) }}</template></span>
        </div>
      </div>

      <section class="persona-block">
        <h3>性格（大五人格）</h3>
        <div class="bigfive">
          <svg viewBox="0 0 240 220" class="radar" role="img" :aria-label="radarLabel">
            <polygon v-for="r in [0.25, 0.5, 0.75, 1]" :key="r" :points="ring(r)" class="radar-ring" />
            <line v-for="(_, i) in bigFiveKeys" :key="'l' + i" x1="120" y1="112" :x2="pt(i, 1)[0]" :y2="pt(i, 1)[1]" class="radar-axis" />
            <polygon :points="shape" class="radar-shape" />
            <circle v-for="(k, i) in bigFiveKeys" :key="'d' + k" :cx="pt(i, anim[i] / 100)[0]" :cy="pt(i, anim[i] / 100)[1]" r="3.5" class="radar-dot" :style="{ opacity: trait('bigFive', k) ? 0.35 + 0.65 * (trait('bigFive', k)!.confidence || 0) : 0 }" />
            <text v-for="(k, i) in bigFiveKeys" :key="'t' + k" :x="pt(i, 1.24)[0]" :y="pt(i, 1.24)[1]" class="radar-label" text-anchor="middle" dominant-baseline="middle">{{ bigFiveText[k][0] }}</text>
          </svg>
          <ul class="trait-list">
            <li v-for="k in bigFiveKeys" :key="k" :class="{ unknown: !trait('bigFive', k) }">
              <el-tooltip :disabled="!trait('bigFive', k)?.evidence" :content="trait('bigFive', k)?.evidence" placement="top" :show-after="200">
                <div class="trait-row">
                  <span class="trait-name">{{ bigFiveText[k][0] }}<small>{{ bigFiveText[k][1] }}</small></span>
                  <span class="bar"><i :style="{ transform: `scaleX(${(trait('bigFive', k)?.score ?? 0) / 100})`, opacity: barOpacity(trait('bigFive', k)) }" /></span>
                  <strong>{{ trait('bigFive', k) ? Math.round(trait('bigFive', k)!.score) : '—' }}</strong>
                </div>
              </el-tooltip>
            </li>
          </ul>
        </div>
      </section>

      <section class="persona-block">
        <h3>沟通风格</h3>
        <ul class="poles">
          <li v-for="k in commKeys" :key="k" :class="{ unknown: !trait('communication', k) }">
            <el-tooltip :disabled="!trait('communication', k)?.evidence" :content="trait('communication', k)?.evidence" placement="top" :show-after="200">
              <div class="pole-row">
                <span class="pole-l" :class="{ on: lean(k) < 0 }">{{ commText[k][0] }}</span>
                <span class="pole-track" :class="{ dashed: lowConfidence(trait('communication', k)) }">
                  <i class="pole-mid" />
                  <b v-if="trait('communication', k)" class="pole-mark" :style="{ left: trait('communication', k)!.score + '%', opacity: barOpacity(trait('communication', k)) }" />
                  <em v-else>还看不出来</em>
                </span>
                <span class="pole-r" :class="{ on: lean(k) > 0 }">{{ commText[k][1] }}</span>
              </div>
            </el-tooltip>
          </li>
        </ul>
      </section>

      <section class="persona-block two-col">
        <div>
          <h3>专业程度</h3>
          <el-tooltip :disabled="!persona.expertise?.evidence" :content="persona.expertise?.evidence" placement="top" :show-after="200">
            <div class="trait-row" :class="{ unknown: !persona.expertise }">
              <span class="trait-name">{{ expertiseLevel }}</span>
              <span class="bar"><i :style="{ transform: `scaleX(${(persona.expertise?.score ?? 0) / 100})`, opacity: barOpacity(persona.expertise) }" /></span>
              <strong>{{ persona.expertise ? Math.round(persona.expertise.score) : '—' }}</strong>
            </div>
          </el-tooltip>
          <div v-if="persona.expertise?.domains?.length" class="tags"><span v-for="d in persona.expertise.domains" :key="d" class="tag">{{ d }}</span></div>
        </div>
        <div>
          <h3>关系</h3>
          <el-tooltip v-for="r in relationRows" :key="r.key" :disabled="!r.value?.evidence" :content="r.value?.evidence" placement="top" :show-after="200">
            <div class="trait-row" :class="{ unknown: !r.value }">
              <span class="trait-name">{{ r.label }}</span>
              <span class="bar"><i :class="r.tone(r.value?.score)" :style="{ transform: `scaleX(${(r.value?.score ?? 0) / 100})`, opacity: barOpacity(r.value) }" /></span>
              <strong>{{ r.value ? Math.round(r.value.score) : '—' }}</strong>
            </div>
          </el-tooltip>
        </div>
      </section>

      <section v-if="tagGroups.length" class="persona-block tag-groups">
        <div v-for="g in tagGroups" :key="g.key" class="tag-group" :class="'is-' + g.key">
          <h3>{{ g.label }}</h3>
          <div class="tags"><span v-for="t in g.items" :key="t" class="tag">{{ t }}</span></div>
        </div>
      </section>
      <p class="persona-foot muted">分数 0–100，颜色越淡表示依据越少；把鼠标放到某一项上能看到 AI 的判断依据。不会推断健康、政治、宗教等敏感信息。</p>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Bottom, Right, Top } from '@element-plus/icons-vue'
import EmptyState from '@/components/EmptyState.vue'
import { formatTime, type Persona, type PersonaTrait } from '@/api/agent'

const props = defineProps<{ persona?: Persona | null }>()

const bigFiveKeys = ['openness', 'conscientiousness', 'extraversion', 'agreeableness', 'stability'] as const
const bigFiveText: Record<typeof bigFiveKeys[number], [string, string]> = {
  openness: ['开放性', '好奇、愿意尝试新东西'],
  conscientiousness: ['尽责性', '有条理、做事认真'],
  extraversion: ['外向性', '爱说话、主动'],
  agreeableness: ['宜人性', '友善、好沟通'],
  stability: ['情绪稳定', '不容易急躁'],
}
const commKeys = ['verbosity', 'formality', 'directness', 'patience', 'emotion', 'humor', 'decision'] as const
const commText: Record<typeof commKeys[number], [string, string]> = {
  verbosity: ['简短', '详细'], formality: ['随意', '正式'], directness: ['委婉', '直接'], patience: ['急躁', '耐心'],
  emotion: ['冷静', '情绪化'], humor: ['严肃', '爱开玩笑'], decision: ['凭感觉', '看数据'],
}

function trait(group: 'bigFive' | 'communication', key: string): PersonaTrait {
  const g = props.persona?.[group] as Record<string, PersonaTrait> | undefined
  const t = g?.[key]
  return t && typeof t.score === 'number' ? t : null
}
const lowConfidence = (t: PersonaTrait | undefined) => !t || (t.confidence ?? 0) < 0.4
const barOpacity = (t: PersonaTrait | undefined) => t ? 0.35 + 0.65 * Math.min(1, Math.max(0, t.confidence ?? 0)) : 0
function lean(k: typeof commKeys[number]) { const t = trait('communication', k); return !t ? 0 : t.score < 40 ? -1 : t.score > 60 ? 1 : 0 }

// 雷达图：5 个轴从正上方顺时针
const pt = (i: number, r: number): [number, number] => {
  const a = -Math.PI / 2 + i * 2 * Math.PI / 5
  return [120 + Math.cos(a) * 82 * r, 112 + Math.sin(a) * 82 * r]
}
const ring = (r: number) => bigFiveKeys.map((_, i) => pt(i, r).join(',')).join(' ')
const anim = ref<number[]>([0, 0, 0, 0, 0])
const shape = computed(() => anim.value.map((v, i) => pt(i, Math.max(0.02, v / 100)).join(',')).join(' '))
const radarLabel = computed(() => bigFiveKeys.map(k => `${bigFiveText[k][0]} ${trait('bigFive', k)?.score ?? '未知'}`).join('，'))
let frame = 0
watch(() => bigFiveKeys.map(k => trait('bigFive', k)?.score ?? 0), to => {
  cancelAnimationFrame(frame)
  const from = [...anim.value]
  if (window.matchMedia?.('(prefers-reduced-motion: reduce)').matches) { anim.value = to; return }
  const start = performance.now()
  const step = (now: number) => {
    const t = Math.min(1, (now - start) / 700)
    const e = 1 - Math.pow(1 - t, 3)
    anim.value = from.map((f, i) => f + (to[i] - f) * e)
    if (t < 1) frame = requestAnimationFrame(step)
  }
  frame = requestAnimationFrame(step)
}, { immediate: true })
onBeforeUnmount(() => cancelAnimationFrame(frame))

const expertiseLevel = computed(() => {
  const s = props.persona?.expertise?.score
  if (s == null) return '还看不出来'
  return s < 25 ? '新手' : s < 50 ? '入门' : s < 75 ? '熟练' : '专家'
})
const goodBad = (s?: number) => s == null ? '' : s >= 60 ? 'is-good' : s < 40 ? 'is-bad' : 'is-mid'
const relationRows = computed(() => [
  { key: 'satisfaction', label: '满意度', value: props.persona?.relationship?.satisfaction ?? null, tone: goodBad },
  { key: 'trust', label: '信任度', value: props.persona?.relationship?.trust ?? null, tone: goodBad },
])
const moodTone = computed(() => ({ 满意: 'good', 平静: 'idle', 着急: 'warn', 不满: 'bad', 愤怒: 'bad' } as Record<string, string>)[props.persona?.relationship?.mood || ''] || 'idle')
const trend = computed(() => ({ up: Top, down: Bottom, flat: Right } as Record<string, unknown>)[props.persona?.relationship?.trend || ''])
const tagGroups = computed(() => [
  { key: 'roles', label: '身份', items: props.persona?.roles },
  { key: 'interests', label: '关注点', items: props.persona?.interests },
  { key: 'habits', label: '习惯', items: props.persona?.habits },
  { key: 'needs', label: '当前诉求', items: props.persona?.needs },
  { key: 'avoid', label: '沟通雷区', items: props.persona?.avoid },
  { key: 'tips', label: '沟通建议', items: props.persona?.tips },
].filter(g => g.items?.length) as Array<{ key: string; label: string; items: string[] }>)
</script>

<style scoped>
.persona { display: flex; flex-direction: column; gap: 18px; }
.persona-head { display: flex; flex-direction: column; gap: 6px; }
.persona-summary { margin: 0; color: var(--text); font-size: 16px; font-weight: 600; line-height: 1.5; text-wrap: pretty; }
.persona-meta { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; font-size: 12px; }
.trend-up { color: var(--success); } .trend-down { color: var(--danger); }
.persona-block { display: flex; flex-direction: column; gap: 10px; padding-top: 14px; border-top: 1px solid var(--line); animation: row-in var(--dur-slow) var(--ease) backwards; }
.persona-block:nth-of-type(2) { animation-delay: 40ms; } .persona-block:nth-of-type(3) { animation-delay: 80ms; } .persona-block:nth-of-type(4) { animation-delay: 120ms; }
.persona-block h3 { margin: 0; color: var(--text-2); font-size: 13px; font-weight: 600; }

.bigfive { display: grid; grid-template-columns: 200px minmax(0, 1fr); align-items: center; gap: 16px; }
.radar { width: 100%; height: auto; overflow: visible; }
.radar-ring { fill: none; stroke: var(--line-strong); stroke-width: 1; }
.radar-axis { stroke: var(--line); stroke-width: 1; }
.radar-shape { fill: var(--primary); fill-opacity: .14; stroke: var(--primary); stroke-width: 1.5; stroke-linejoin: round; }
.radar-dot { fill: var(--primary); transition: opacity var(--dur-slow) var(--ease); }
.radar-label { fill: var(--muted); font-size: 12px; }

.trait-list, .poles { display: flex; flex-direction: column; gap: 6px; margin: 0; padding: 0; list-style: none; }
.trait-row { display: grid; grid-template-columns: minmax(92px, auto) minmax(60px, 1fr) 32px; align-items: center; gap: 10px; min-height: 28px; font-size: 13px; }
.trait-name { display: flex; flex-direction: column; color: var(--text); line-height: 1.3; }
.trait-name small { color: var(--muted); font-size: 12px; }
.trait-row strong { color: var(--text); font-weight: 600; text-align: right; font-variant-numeric: tabular-nums; }
.unknown .trait-row strong, .trait-row.unknown strong { color: var(--faint); font-weight: 400; }
.bar { position: relative; height: 6px; border-radius: 3px; background: var(--surface-3); overflow: hidden; }
.bar i { position: absolute; inset: 0; border-radius: 3px; background: var(--primary); transform-origin: left; transition: transform .7s var(--ease), opacity var(--dur-slow) var(--ease); }
.bar i.is-good { background: var(--success); } .bar i.is-mid { background: var(--warning); } .bar i.is-bad { background: var(--danger); }

.pole-row { display: grid; grid-template-columns: 56px minmax(80px, 1fr) 56px; align-items: center; gap: 10px; min-height: 26px; font-size: 13px; }
.pole-l { color: var(--muted); text-align: right; transition: color var(--dur) var(--ease); }
.pole-r { color: var(--muted); transition: color var(--dur) var(--ease); }
.pole-l.on, .pole-r.on { color: var(--text); font-weight: 600; }
.pole-track { position: relative; height: 6px; border-radius: 3px; background: var(--surface-3); }
.pole-track.dashed { background: repeating-linear-gradient(90deg, var(--surface-3) 0 6px, transparent 6px 10px); }
.pole-mid { position: absolute; top: -3px; bottom: -3px; left: 50%; width: 1px; background: var(--line-strong); }
.pole-mark { position: absolute; top: 50%; width: 14px; height: 14px; border: 2px solid var(--surface); border-radius: 50%; background: var(--primary); box-shadow: 0 0 0 1px var(--primary); transform: translate(-50%, -50%); transition: left .7s var(--ease), opacity var(--dur-slow) var(--ease); }
.pole-track em { position: absolute; top: 50%; left: 50%; padding: 0 6px; background: var(--surface); color: var(--faint); font-size: 12px; font-style: normal; white-space: nowrap; transform: translate(-50%, -50%); }

.two-col { display: grid; grid-template-columns: 1fr 1fr; gap: 20px; }
.two-col > div { display: flex; flex-direction: column; gap: 8px; }
.tag-groups { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 14px 20px; }
.tag-group { display: flex; flex-direction: column; gap: 6px; }
.tags { display: flex; flex-wrap: wrap; gap: 6px; }
.tag { display: inline-flex; align-items: center; min-height: 24px; padding: 2px 8px; border-radius: var(--radius-xs); background: var(--surface-3); color: var(--text-2); font-size: 12px; line-height: 1.5; }
.is-avoid .tag { background: var(--danger-light); color: var(--danger); }
.is-tips .tag { background: var(--success-light); color: var(--success); }
.is-needs .tag { background: var(--warning-light); color: var(--warning); }
.persona-foot { margin: 0; font-size: 12px; line-height: 1.6; }

@media (max-width: 720px) { .bigfive, .two-col { grid-template-columns: 1fr; } .radar { max-width: 220px; justify-self: center; } }
@media (prefers-reduced-motion: reduce) { .bar i, .pole-mark { transition: none; } }
</style>
