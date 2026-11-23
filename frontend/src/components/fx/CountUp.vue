<template><span class="count-up">{{ display }}</span></template>

<script setup lang="ts">
// 数字滚动：支持 "23,842"、"95.1%"、"1.28s"、"3 / 24" 这类带前后缀的文本，非数字原样显示。
import { computed } from 'vue'
import { useTween } from '@/composables/useTween'

const props = withDefaults(defineProps<{ value: number | string | null | undefined; duration?: number; delay?: number }>(), { duration: 1000, delay: 0 })
const parsed = computed(() => {
  if (props.value === null || props.value === undefined) return null
  const text = String(props.value)
  const match = text.match(/^([^\d-]*)(-?[\d,]*\.?\d+)(.*)$/)
  if (!match) return null
  const raw = match[2]
  return { prefix: match[1], suffix: match[3], number: Number(raw.replace(/,/g, '')), decimals: (raw.split('.')[1] || '').length, grouped: raw.includes(',') || Math.abs(Number(raw.replace(/,/g, ''))) >= 10000 }
})
const target = computed(() => parsed.value?.number ?? 0)
const tweened = useTween(target, { duration: props.duration, delay: props.delay })
const display = computed(() => {
  const p = parsed.value
  if (!p) return props.value ?? '—'
  const n = p.decimals ? tweened.value.toFixed(p.decimals) : String(Math.round(tweened.value))
  const formatted = p.grouped ? Number(n).toLocaleString('en-US', { minimumFractionDigits: p.decimals, maximumFractionDigits: p.decimals }) : n
  return `${p.prefix}${formatted}${p.suffix}`
})
</script>

<style scoped>
.count-up { font-variant-numeric: tabular-nums; }
</style>
