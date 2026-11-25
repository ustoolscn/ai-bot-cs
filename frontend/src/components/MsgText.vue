<template>
  <span class="msg-text"><template v-for="(p, i) in parts" :key="i"><img v-if="p.src" class="qface" :src="p.src" :alt="p.text" :title="p.text" loading="lazy" draggable="false" /><template v-else>{{ p.text }}</template></template></span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatMentions } from '@/utils/mentions'
import { qfaceUrl } from '@/utils/qface'

// 消息文字：先换 @昵称、表情代码，再把 [呲牙] 这类认得的表情名画成 QQ 表情图；认不出的保持文字
const props = defineProps<{ text?: string | null; names?: Record<string, string> | null; fallback?: string }>()

const FACE_NAME = /\[([^[\]\n]{1,8})\]/g
const parts = computed(() => {
  const text = formatMentions(props.text, props.names) || props.fallback || ''
  const out: { text: string; src?: string }[] = []
  let last = 0
  for (const m of text.matchAll(FACE_NAME)) {
    const src = qfaceUrl(m[1])
    if (!src) continue
    if (m.index! > last) out.push({ text: text.slice(last, m.index) })
    out.push({ text: m[0], src })
    last = m.index! + m[0].length
  }
  if (last < text.length) out.push({ text: text.slice(last) })
  return out
})
</script>

<style scoped>
.qface { display: inline-block; width: 1.5em; height: 1.5em; margin: 0 1px; vertical-align: -0.38em; object-fit: contain; user-select: none; }
</style>
