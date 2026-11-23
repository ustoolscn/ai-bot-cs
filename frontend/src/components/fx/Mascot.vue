<template>
  <svg class="mascot" :class="[`mood-${mood}`, { floating: float, 'head-only': headOnly }]" :width="size" :height="size" :viewBox="headOnly ? '8 0 104 100' : '0 0 120 132'" role="img" :aria-label="label">
    <defs>
      <linearGradient :id="`${uid}-head`" x1="0" y1="0" x2="1" y2="1">
        <stop offset="0" stop-color="#ffa477" />
        <stop offset=".6" stop-color="#f2785c" />
        <stop offset="1" stop-color="#ec6478" />
      </linearGradient>
      <linearGradient :id="`${uid}-body`" x1="0" y1="0" x2="0" y2="1">
        <stop offset="0" stop-color="#ffc0a3" />
        <stop offset="1" stop-color="#f7967c" />
      </linearGradient>
    </defs>

    <!-- 身体与手臂 -->
    <g v-if="!headOnly">
      <ellipse cx="60" cy="128" rx="30" ry="3.5" fill="#3b2c22" opacity=".08" class="shadow" />
      <rect x="36" y="92" width="48" height="30" rx="15" :fill="`url(#${uid}-body)`" />
      <circle cx="60" cy="106" r="5" fill="#fffaf5" opacity=".9" />
      <path d="M58 106 l1.5 1.6 l3-3.2" fill="none" stroke="#f07a5a" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
      <rect x="24" y="96" width="14" height="9" rx="4.5" :fill="`url(#${uid}-body)`" class="arm-left" />
      <rect x="82" y="96" width="14" height="9" rx="4.5" :fill="`url(#${uid}-body)`" class="arm-right" />
    </g>

    <!-- 天线 -->
    <g class="antenna">
      <line x1="60" y1="24" x2="60" y2="12" stroke="#e8694f" stroke-width="3" stroke-linecap="round" />
      <circle cx="60" cy="10" r="5.5" fill="#f5c451" />
      <circle cx="58.3" cy="8.4" r="1.6" fill="#fff" opacity=".8" />
    </g>

    <!-- 头 -->
    <circle cx="16" cy="58" r="8" fill="#f5a28b" />
    <circle cx="104" cy="58" r="8" fill="#f5a28b" />
    <rect x="18" y="22" width="84" height="72" rx="32" :fill="`url(#${uid}-head)`" />
    <rect x="30" y="36" width="60" height="44" rx="21" fill="#fffaf5" />
    <ellipse cx="38" cy="30" rx="9" ry="4" fill="#fff" opacity=".28" transform="rotate(-18 38 30)" />

    <!-- 眼睛 -->
    <g v-if="mood === 'sleepy'" fill="none" stroke="#3b2c22" stroke-width="2.6" stroke-linecap="round">
      <path d="M41 57 Q47 61 53 57" />
      <path d="M67 57 Q73 61 79 57" />
    </g>
    <g v-else class="eyes">
      <ellipse class="eye" :cx="47" :cy="mood === 'thinking' ? 53 : 56" rx="5" ry="6.2" fill="#3b2c22" />
      <ellipse class="eye" :cx="73" :cy="mood === 'thinking' ? 53 : 56" rx="5" ry="6.2" fill="#3b2c22" />
      <circle :cx="48.8" :cy="mood === 'thinking' ? 50.5 : 53.5" r="1.7" fill="#fff" />
      <circle :cx="74.8" :cy="mood === 'thinking' ? 50.5 : 53.5" r="1.7" fill="#fff" />
    </g>

    <!-- 腮红 -->
    <ellipse cx="38.5" cy="67" rx="5.5" ry="3.2" fill="#ffad9e" opacity=".75" />
    <ellipse cx="81.5" cy="67" rx="5.5" ry="3.2" fill="#ffad9e" opacity=".75" />

    <!-- 嘴巴 -->
    <path v-if="mood === 'happy' || mood === 'wave'" d="M54 66 Q60 73 66 66" fill="none" stroke="#3b2c22" stroke-width="2.6" stroke-linecap="round" />
    <path v-else-if="mood === 'thinking'" d="M55 69 Q60 67 65 69" fill="none" stroke="#3b2c22" stroke-width="2.4" stroke-linecap="round" />
    <ellipse v-else cx="60" cy="69" rx="2.8" ry="2.2" fill="#3b2c22" />

    <!-- 情绪装饰 -->
    <g v-if="mood === 'sleepy'" class="zzz" fill="#9b8cf2" font-family="ui-rounded, sans-serif" font-weight="700">
      <text x="96" y="22" font-size="11">z</text>
      <text x="104" y="12" font-size="8">z</text>
    </g>
    <g v-if="mood === 'thinking'" class="think">
      <circle cx="100" cy="24" r="10" fill="#fff" stroke="#f0e4d4" stroke-width="1.5" />
      <text x="96.5" y="29" font-size="13" font-weight="800" fill="#f07a5a" font-family="ui-rounded, sans-serif">?</text>
    </g>
    <g v-if="mood === 'happy' && !headOnly" class="sparkles" fill="#f5c451">
      <path d="M8 30 l2 5 l5 2 l-5 2 l-2 5 l-2 -5 l-5 -2 l5 -2 z" />
      <path d="M108 86 l1.4 3.4 l3.4 1.4 l-3.4 1.4 l-1.4 3.4 l-1.4 -3.4 l-3.4 -1.4 l3.4 -1.4 z" />
    </g>
  </svg>
</template>

<script setup lang="ts">
// 吉祥物“小Q”：纯 SVG + CSS 动画（眨眼、天线摆动、挥手、漂浮）。
import { useId } from 'vue'

withDefaults(defineProps<{ mood?: 'happy' | 'sleepy' | 'thinking' | 'wave'; size?: number; float?: boolean; headOnly?: boolean; label?: string }>(), { mood: 'happy', size: 96, float: false, headOnly: false, label: '小Q' })
const uid = `mascot-${useId()}`
</script>

<style scoped>
.mascot { display: block; overflow: visible; flex: none; }
.mascot.floating { animation: mascot-float 3.6s ease-in-out infinite; }
.antenna { transform-origin: 60px 24px; animation: antenna 2.8s ease-in-out infinite; }
.eye { transform-box: fill-box; transform-origin: center; animation: blink 4.2s infinite; }
.arm-right { transform-box: fill-box; transform-origin: 0% 50%; }
.mood-wave .arm-right { animation: wave 1.2s ease-in-out infinite; }
.zzz text { animation: zzz 2.4s ease-in-out infinite; }
.zzz text:nth-child(2) { animation-delay: .6s; }
.think { animation: think 2.2s ease-in-out infinite; transform-box: fill-box; transform-origin: center; }
.sparkles path { transform-box: fill-box; transform-origin: center; animation: sparkle 2s ease-in-out infinite; }
.sparkles path:nth-child(2) { animation-delay: .9s; }
.shadow { transform-box: fill-box; transform-origin: center; }
.floating .shadow { animation: shadow 3.6s ease-in-out infinite; }

@keyframes blink { 0%, 90%, 100% { transform: scaleY(1); } 93% { transform: scaleY(.1); } }
@keyframes antenna { 0%, 100% { transform: rotate(-8deg); } 50% { transform: rotate(8deg); } }
@keyframes wave { 0%, 100% { transform: rotate(0deg); } 50% { transform: rotate(-38deg); } }
@keyframes mascot-float { 0%, 100% { transform: translateY(0); } 50% { transform: translateY(-6px); } }
@keyframes shadow { 0%, 100% { transform: scaleX(1); opacity: .08; } 50% { transform: scaleX(.8); opacity: .05; } }
@keyframes zzz { 0% { opacity: 0; transform: translate(0, 4px); } 40% { opacity: 1; } 100% { opacity: 0; transform: translate(4px, -6px); } }
@keyframes think { 0%, 100% { transform: translateY(0) scale(1); } 50% { transform: translateY(-3px) scale(1.06); } }
@keyframes sparkle { 0%, 100% { transform: scale(.4) rotate(0); opacity: .3; } 50% { transform: scale(1) rotate(45deg); opacity: 1; } }
@media (prefers-reduced-motion: reduce) { .mascot *, .mascot { animation: none !important; } }
</style>
