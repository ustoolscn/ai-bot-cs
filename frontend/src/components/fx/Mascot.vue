<template>
  <svg class="mascot" :width="size" :height="size" :viewBox="headOnly ? '8 0 104 100' : '0 0 120 132'" role="img" :aria-label="label">
    <!-- 身体 -->
    <g v-if="!headOnly">
      <rect x="36" y="92" width="48" height="30" rx="15" fill="#c9dbf3" />
      <rect x="24" y="96" width="14" height="9" rx="4.5" fill="#c9dbf3" />
      <rect x="82" y="96" width="14" height="9" rx="4.5" fill="#c9dbf3" />
    </g>
    <!-- 天线 -->
    <line x1="60" y1="24" x2="60" y2="12" stroke="#1f6bd1" stroke-width="3" stroke-linecap="round" />
    <circle cx="60" cy="10" r="5" fill="#1f6bd1" />
    <!-- 头 -->
    <circle cx="16" cy="58" r="8" fill="#9fbfe9" />
    <circle cx="104" cy="58" r="8" fill="#9fbfe9" />
    <rect x="18" y="22" width="84" height="72" rx="32" fill="#1f6bd1" />
    <rect x="30" y="36" width="60" height="44" rx="21" fill="#fff" />
    <!-- 眼睛 -->
    <g v-if="mood === 'sleepy'" fill="none" stroke="#1b2130" stroke-width="2.6" stroke-linecap="round">
      <path d="M41 57 Q47 61 53 57" />
      <path d="M67 57 Q73 61 79 57" />
    </g>
    <g v-else fill="#1b2130">
      <ellipse cx="47" :cy="mood === 'thinking' ? 53 : 56" rx="5" ry="6.2" />
      <ellipse cx="73" :cy="mood === 'thinking' ? 53 : 56" rx="5" ry="6.2" />
    </g>
    <!-- 嘴巴 -->
    <path v-if="mood === 'happy' || mood === 'wave'" d="M54 66 Q60 72 66 66" fill="none" stroke="#1b2130" stroke-width="2.6" stroke-linecap="round" />
    <path v-else-if="mood === 'thinking'" d="M55 69 Q60 67 65 69" fill="none" stroke="#1b2130" stroke-width="2.4" stroke-linecap="round" />
    <ellipse v-else cx="60" cy="69" rx="2.8" ry="2.2" fill="#1b2130" />
  </svg>
</template>

<script setup lang="ts">
// 吉祥物“小Q”：静态 SVG，只用在登录页、品牌标识和空状态。
withDefaults(defineProps<{ mood?: 'happy' | 'sleepy' | 'thinking' | 'wave'; size?: number; headOnly?: boolean; label?: string }>(), { mood: 'happy', size: 96, headOnly: false, label: '小Q' })
</script>

<style scoped>
.mascot { display: block; flex: none; }
</style>
