<template><span class="live-dot" :class="[tone, { active }]" aria-hidden="true"><i /></span></template>

<script setup lang="ts">
// 状态点：只有真实进行中时才呼吸扩散，空闲时静止。
withDefaults(defineProps<{ active?: boolean; tone?: 'primary' | 'success' | 'warning' | 'danger' | 'muted' }>(), { active: false, tone: 'success' })
</script>

<style scoped>
.live-dot { position: relative; display: inline-grid; place-items: center; width: 10px; height: 10px; flex: none; color: var(--success); }
.live-dot i { width: 8px; height: 8px; border-radius: 50%; background: currentColor; }
.live-dot.active::before { content: ''; position: absolute; inset: 0; border-radius: 50%; background: currentColor; animation: ripple 1.6s var(--ease) infinite; }
.primary { color: var(--primary); } .warning { color: #d18a12; } .danger { color: var(--danger); } .muted { color: var(--line-strong); }
@keyframes ripple { 0% { transform: scale(.8); opacity: .55; } 100% { transform: scale(2.6); opacity: 0; } }
</style>
