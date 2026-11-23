<template>
  <div class="ring" :style="{ width: `${size}px`, height: `${size}px` }">
    <svg :width="size" :height="size" :viewBox="`0 0 ${size} ${size}`">
      <defs>
        <linearGradient :id="`${uid}-g`" x1="0" y1="0" x2="1" y2="1">
          <stop offset="0" :stop-color="palette[0]" />
          <stop offset="1" :stop-color="palette[1]" />
        </linearGradient>
      </defs>
      <circle class="track" :cx="c" :cy="c" :r="r" :stroke-width="stroke" />
      <circle class="bar" :cx="c" :cy="c" :r="r" :stroke-width="stroke" :stroke="`url(#${uid}-g)`" :stroke-dasharray="circumference" :stroke-dashoffset="offset" :transform="`rotate(-90 ${c} ${c})`" />
      <circle v-if="animated > 0.5" class="knob" :cx="knob.x" :cy="knob.y" :r="stroke / 2 + 1.5" :fill="palette[1]" />
      <circle v-if="animated > 0.5" :cx="knob.x" :cy="knob.y" :r="Math.max(1.5, stroke / 4)" fill="#fff" />
    </svg>
    <div class="ring-center">
      <slot :value="animated">
        <strong :style="{ fontSize: `${Math.round(size * 0.22)}px` }">{{ animated.toFixed(decimals) }}<small>%</small></strong>
        <span v-if="label">{{ label }}</span>
      </slot>
    </div>
  </div>
</template>

<script setup lang="ts">
// 圆环进度：渐变描边 + 端点小圆点，数值用 requestAnimationFrame 平滑过渡。
import { computed, useId } from 'vue'
import { easeOutBack, useTween } from '@/composables/useTween'

const props = withDefaults(defineProps<{ percent: number | null | undefined; size?: number; stroke?: number; label?: string; tone?: 'coral' | 'mint' | 'lavender' | 'sky' | 'butter'; decimals?: number; delay?: number }>(), { size: 96, stroke: 9, tone: 'coral', decimals: 0, delay: 0 })
const tones = { coral: ['#ffa477', '#ec6478'], mint: ['#7fdcbc', '#2f9e7a'], lavender: ['#c4b9ff', '#8b7bef'], sky: ['#9fd4fb', '#4a9fe4'], butter: ['#ffe08a', '#f0a92c'] }
const palette = computed(() => tones[props.tone])
const uid = `ring-${useId()}`
const target = computed(() => Math.max(0, Math.min(100, Number(props.percent) || 0)))
const animated = useTween(target, { duration: 1300, delay: props.delay, easing: (t) => Math.min(1, easeOutBack(t)) })
const c = computed(() => props.size / 2)
const r = computed(() => props.size / 2 - props.stroke / 2 - 3)
const circumference = computed(() => 2 * Math.PI * r.value)
const offset = computed(() => circumference.value * (1 - animated.value / 100))
const knob = computed(() => {
  const angle = (animated.value / 100) * 2 * Math.PI - Math.PI / 2
  return { x: c.value + r.value * Math.cos(angle), y: c.value + r.value * Math.sin(angle) }
})
</script>

<style scoped>
.ring { position: relative; flex: none; }
.ring svg { display: block; overflow: visible; }
.track { fill: none; stroke: var(--surface-3); }
.bar { fill: none; stroke-linecap: round; }
.knob { filter: drop-shadow(0 2px 3px rgba(120, 80, 40, .25)); }
.ring-center { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; text-align: center; line-height: 1.1; }
.ring-center strong { font-family: var(--font-round); font-weight: 800; color: var(--text); font-variant-numeric: tabular-nums; }
.ring-center strong small { font-size: .55em; font-weight: 700; margin-left: 1px; color: var(--muted); }
.ring-center span { font-size: 12px; color: var(--muted); margin-top: 3px; }
</style>
