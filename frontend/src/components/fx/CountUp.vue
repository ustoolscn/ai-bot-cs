<template><span class="count-up" :class="{ bump }">{{ text }}</span></template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useTween } from '@/composables/useTween'

const props = withDefaults(defineProps<{ value: number | null | undefined; decimals?: number; suffix?: string }>(), { decimals: 0, suffix: '' })
const target = computed(() => props.value ?? 0)
const tweened = useTween(target)
const text = computed(() => props.value == null ? '—' : tweened.value.toLocaleString('zh-CN', { minimumFractionDigits: props.decimals, maximumFractionDigits: props.decimals }) + props.suffix)
// 数值变化时短暂高亮，提示“这里刚变了”
const bump = ref(false)
watch(() => props.value, (now, before) => {
  if (before == null || now === before) return
  bump.value = false
  requestAnimationFrame(() => { bump.value = true })
})
</script>

<style scoped>
.count-up { font-variant-numeric: tabular-nums; border-radius: 4px; }
.bump { animation: bump 1.2s var(--ease); }
@keyframes bump { 0% { background: var(--primary-light); } 100% { background: transparent; } }
</style>
