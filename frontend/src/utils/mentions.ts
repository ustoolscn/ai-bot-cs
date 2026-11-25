import { reactive } from 'vue'
import { request } from '@/api/client'

// QQ 消息里的 @ 是 <@openid>（或 <@!openid>）。展示时换成 @昵称；查不到昵称就显示 @群成员，不露出 openid。
const MENTION = /<@!?([A-Za-z0-9_-]+)>/g

// 全站共用的昵称缓存：接口带回来的 mentionNames 会记进来，缺的再批量去后端查
const known = reactive(new Map<string, string>())
const missing = new Set<string>()
const asked = new Set<string>()
let timer: ReturnType<typeof setTimeout> | undefined

export function rememberMentionNames(names?: Record<string, string> | null) {
  if (!names) return
  for (const [id, name] of Object.entries(names)) if (name) known.set(id, name)
}

function flush() {
  timer = undefined
  const ids = [...missing].slice(0, 200)
  ids.forEach(id => { missing.delete(id); asked.add(id) })
  if (!ids.length) return
  request<Record<string, string>>({ method: 'POST', url: '/mention-names', data: { ids } })
    .then(rememberMentionNames)
    .catch(() => { /* 查不到就保持 @群成员 */ })
    .finally(() => { if (missing.size) timer = setTimeout(flush, 30) })
}

function lookup(id: string) {
  if (known.has(id) || asked.has(id)) return
  missing.add(id)
  timer ??= setTimeout(flush, 30)
}

// QQ 表情：<faceType=1,faceId="13",ext="base64({"text":"[呲牙]"})">，以及 <emoji:128512> 这类写法。
// 解不出名字时显示 [表情]，不露出代码。
const FACE = /<faceType=(\d+),\s*faceId="?([^",>]*)"?(?:,\s*ext="?([^">]*)"?)?\s*>/g
const EMOJI = /<emoji:(\d+)>/g
const faceCache = new Map<string, string>()
function decodeExt(ext: string) {
  if (faceCache.has(ext)) return faceCache.get(ext)!
  let name = ''
  try {
    const bytes = Uint8Array.from(atob(ext.replace(/-/g, '+').replace(/_/g, '/')), c => c.charCodeAt(0))
    const data = JSON.parse(new TextDecoder().decode(bytes)) as { text?: string }
    name = typeof data.text === 'string' ? data.text.trim() : ''
  } catch { /* 不是合法的 ext */ }
  faceCache.set(ext, name)
  return name
}
function fromCodePoint(id: string) {
  const n = Number(id)
  return Number.isInteger(n) && n > 0x2000 && n <= 0x10ffff ? String.fromCodePoint(n) : ''
}
export function formatFaces(text: string): string {
  return text
    .replace(FACE, (_, _type: string, id: string, ext?: string) => {
      const name = ext ? decodeExt(ext) : ''
      if (name) return /^\[.*\]$/.test(name) ? name : `[${name}]`
      return fromCodePoint(id) || '[表情]'
    })
    .replace(EMOJI, (_, id: string) => fromCodePoint(id) || '[表情]')
}

export function formatMentions(text: string | null | undefined, names?: Record<string, string> | null): string {
  if (!text) return text || ''
  return formatFaces(text).replace(MENTION, (_, id: string) => {
    const name = names?.[id] || known.get(id)
    if (!name) lookup(id)
    return `@${name || '群成员'}`
  })
}
