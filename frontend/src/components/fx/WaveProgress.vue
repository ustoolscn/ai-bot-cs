<template>
  <div class="wave" :style="{ width: `${size}px`, height: `${size}px` }">
    <svg :width="size" :height="size" viewBox="0 0 100 100">
      <defs>
        <clipPath :id="`${uid}-clip`"><circle cx="50" cy="50" r="44" /></clipPath>
        <linearGradient :id="`${uid}-g`" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0" :stop-color="palette[0]" />
          <stop offset="1" :stop-color="palette[1]" />
        </linearGradient>
      </defs>
      <circle cx="50" cy="50" r="48" fill="none" :stroke="palette[0]" stroke-width="2" stroke-dasharray="3 5" class="orbit" />
      <circle cx="50" cy="50" r="44" fill="var(--surface-2)" />
      <g :clip-path="`url(#${uid}-clip)`">
        <g :transform="`translate(0 ${level})`">
          <path class="w back" :d="wavePath" :fill="palette[0]" opacity=".45" />
          <path class="w front" :d="wavePath" :fill="`url(#${uid}-g)`" />
        </g>
        <circle v-for="b in bubbles" :key="b.x" class="bubble" :cx="b.x" cy="96" :r="b.r" fill="#fff" :style="{ animationDelay: `${b.d}s`, animationDuration: `${b.t}s` }" />
      </g>
    </svg>
    <div class="wave-center">
      <strong :style="{ fontSize: `${Math.round(size * 0.2)}px` }">{{ Math.round(animated) }}<small>%</small></strong>
      <span v-if="label">{{ label }}</span>
    </div>
  </div>
</template>

<script setup lang="ts">
// 水波进度：两层波浪平移形成流动感，水位随百分比上升，还有小气泡往上冒。
import { computed, useId } from 'vue'
import { useTween } from '@/composables/useTween'

const props = withDefaults(defineProps<{ percent: number | null | undefined; size?: number; label?: string; tone?: 'coral' | 'mint' | 'sky' }>(), { size: 120, tone: 'mint' })
const tones = { coral: ['#ffb08f', '#f2735a'], mint: ['#8fe3c6', '#3fb48d'], sky: ['#a8d8fc', '#4a9fe4'] }
const palette = computed(() => tones[props.tone])
const uid = `wave-${useId()}`
const target = computed(() => Math.max(0, Math.min(100, Number(props.percent) || 0)))
const animated = useTween(target, { duration: 1600 })
const level = computed(() => 94 - (animated.value / 100) * 88)
// 宽 200 的波浪，左右各一个周期，平移 100 正好无缝循环
const wavePath = 'M0 0 Q12.5 -6 25 0 T50 0 T75 0 T100 0 T125 0 T150 0 T175 0 T200 0 V120 H0 Z'
const bubbles = [{ x: 34, r: 1.6, d: 0, t: 3.2 }, { x: 58, r: 2.2, d: 1.1, t: 3.8 }, { x: 70, r: 1.3, d: 2.2, t: 2.9 }]
</script>

<style scoped>
.wave { position: relative; flex: none; }
.wave svg { display: block; overflow: visible; }
.w { animation: wave-move 3.2s linear infinite; }
.w.back { animation-duration: 5s; animation-direction: reverse; }
.orbit { transform-origin: 50px 50px; animation: orbit 18s linear infinite; }
.bubble { opacity: 0; animation: bubble 3.2s ease-in infinite; }
.wave-center { position: absolute; inset: 0; display: flex; flex-direction: column; align-items: center; justify-content: center; line-height: 1.1; }
.wave-center strong { font-family: var(--font-round); font-weight: 800; color: var(--text); text-shadow: 0 1px 0 rgba(255, 255, 255, .8); font-variant-numeric: tabular-nums; }
.wave-center strong small { font-size: .55em; margin-left: 1px; }
.wave-center span { font-size: 12px; color: var(--text-2); margin-top: 3px; text-shadow: 0 1px 0 rgba(255, 255, 255, .8); }
@keyframes wave-move { from { transform: translateX(0); } to { transform: translateX(-100px); } }
@keyframes orbit { to { transform: rotate(360deg); } }
@keyframes bubble { 0% { opacity: 0; transform: translateY(0); } 20% { opacity: .8; } 100% { opacity: 0; transform: translateY(-60px); } }
@media (prefers-reduced-motion: reduce) { .w, .orbit, .bubble { animation: none; } }
</style>
