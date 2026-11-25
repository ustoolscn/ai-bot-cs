<template>
  <span class="msg-text"><template v-for="(p, i) in parts" :key="i"><img v-if="p.src" class="qface" :src="p.src" :alt="p.text" :title="p.text" loading="lazy" draggable="false" /><a v-else-if="p.href" class="msg-link" :href="p.href" target="_blank" rel="noopener noreferrer">{{ p.text }}</a><template v-else>{{ p.text }}</template></template></span>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { formatMentions } from '@/utils/mentions'
import { qfaceUrl } from '@/utils/qface'

// 消息文字：先换 @昵称、表情代码，再把网址做成链接、[呲牙] 这类认得的表情名画成 QQ 表情图；认不出的保持文字
const props = defineProps<{ text?: string | null; names?: Record<string, string> | null; fallback?: string }>()

type Part = { text: string; src?: string; href?: string }
// 网址只取 ASCII 可见字符，碰到中文、空白就结束；末尾的句读不算进网址
const URL = /(?:https?:\/\/|www\.)[!-~]+/gi
const URL_TAIL = /[.,;:!?'")\]}>]+$/
const FACE_NAME = /\[([^[\]\n]{1,8})\]/g

function faces(text: string, out: Part[]) {
  let last = 0
  for (const m of text.matchAll(FACE_NAME)) {
    const src = qfaceUrl(m[1])
    if (!src) continue
    if (m.index! > last) out.push({ text: text.slice(last, m.index) })
    out.push({ text: m[0], src })
    last = m.index! + m[0].length
  }
  if (last < text.length) out.push({ text: text.slice(last) })
}

const parts = computed(() => {
  const text = formatMentions(props.text, props.names) || props.fallback || ''
  const out: Part[] = []
  let last = 0
  for (const m of text.matchAll(URL)) {
    const url = m[0].replace(URL_TAIL, '')
    if (!url.includes('.')) continue
    faces(text.slice(last, m.index), out)
    out.push({ text: url, href: /^www\./i.test(url) ? `https://${url}` : url })
    last = m.index! + url.length
  }
  faces(text.slice(last), out)
  return out
})
</script>

<style scoped>
.qface { display: inline-block; width: 1.5em; height: 1.5em; margin: 0 1px; vertical-align: -0.38em; object-fit: contain; user-select: none; }
/* 网址紧跟前面的文字，排不下就按字符折行，不整段挤到下一行、也不在 / ? - 处乱断 */
.msg-link { color: var(--primary); text-decoration: none; word-break: break-all; }
.msg-link:hover { text-decoration: underline; }
</style>
