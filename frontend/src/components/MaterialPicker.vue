<template>
  <div ref="root" class="material-picker" :class="{ dragging: fileDrag, disabled }" @dragover="onFileDragOver" @dragleave.self="fileDrag = false" @drop="onFileDrop">
    <input ref="input" type="file" :accept="acceptAttr" multiple hidden @change="onChoose">
    <TransitionGroup tag="ol" name="mp-item" class="mp-list" :aria-label="`素材，共 ${items.length} 个，按顺序发送`">
      <li
        v-for="(item, i) in items" :key="item.key" class="mp-tile" :class="{ moving: dragKey === item.key }"
        :data-key="item.key" draggable="true" tabindex="0" :aria-label="`第 ${i + 1} 个：${nameOf(item)}。Alt 加左右方向键调整顺序，Delete 移除`"
        @dragstart="onTileDragStart(item.key, $event)" @dragenter.prevent="onTileDragEnter(i)" @dragover.prevent @dragend="dragKey = ''"
        @keydown.alt.left.prevent="moveByKey(i, i - 1)" @keydown.alt.right.prevent="moveByKey(i, i + 1)" @keydown.delete.prevent="remove(i)" @keydown.backspace.prevent="remove(i)"
      >
        <a :href="urlOf(item)" target="_blank" rel="noopener" class="mp-thumb" :title="`${nameOf(item)} · 点开看原${kindOf(item) === 'video' ? '视频' : '图'}`" draggable="false">
          <video v-if="kindOf(item) === 'video'" :src="urlOf(item)" :poster="item.existing?.posterUrl || undefined" preload="metadata" muted playsinline />
          <img v-else :src="urlOf(item)" :alt="nameOf(item)" loading="lazy" draggable="false">
          <span v-if="kindOf(item) === 'video'" class="mp-play"><el-icon><VideoPlay /></el-icon></span>
        </a>
        <span class="mp-index">{{ i + 1 }}</span>
        <span v-if="item.file" class="mp-new">新</span>
        <button type="button" class="mp-remove" :aria-label="`移除第 ${i + 1} 个`" :disabled="disabled" @click="remove(i)"><el-icon><Close /></el-icon></button>
      </li>
      <li v-if="items.length < max" key="__add" class="mp-add-wrap">
        <button type="button" class="mp-add" :disabled="disabled" @click="input?.click()">
          <el-icon><Plus /></el-icon>
          <span>{{ fileDrag ? '松开添加' : items.length ? '继续添加' : '添加素材' }}</span>
        </button>
      </li>
    </TransitionGroup>
    <p class="mp-hint">
      <span>{{ hint }}</span>
      <span class="mp-count" :class="{ full: items.length >= max }">{{ items.length }} / {{ max }}</span>
    </p>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
import { Close, Plus, VideoPlay } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { materialLimits, type Material, type MaterialDraft } from '@/api/agent'

const props = withDefaults(defineProps<{
  disabled?: boolean
  hint?: string
  max?: number
  /** 允许视频；人工回复等场景也一样 */
  allowVideo?: boolean
  maxImageBytes?: number
  maxVideoBytes?: number
}>(), { disabled: false, hint: '', max: materialLimits.max, allowVideo: true, maxImageBytes: materialLimits.imageBytes, maxVideoBytes: materialLimits.videoBytes })

/** 最终顺序里的每一项：要么是已保存的素材，要么是新选的文件 */
const items = defineModel<MaterialDraft[]>({ default: () => [] })
const input = ref<HTMLInputElement>()
const root = ref<HTMLElement>()
const fileDrag = ref(false)
const dragKey = ref('')
const imageTypes = ['image/png', 'image/jpeg']
const acceptAttr = computed(() => [...imageTypes, ...(props.allowVideo ? ['video/mp4'] : [])].join(','))
const hint = computed(() => props.hint || `按顺序发出，拖动调整先后。截图 PNG/JPEG 单张 ${props.maxImageBytes >> 20} MB 内${props.allowVideo ? `，录屏 MP4 单个 ${props.maxVideoBytes >> 20} MB 内` : ''}，可直接粘贴`)

// 新文件的本地预览地址，随列表增删创建和释放
const objectUrls = new Map<File, string>()
watch(items, list => {
  const alive = new Set(list.map(i => i.file).filter(Boolean) as File[])
  for (const [f, url] of objectUrls) if (!alive.has(f)) { URL.revokeObjectURL(url); objectUrls.delete(f) }
}, { deep: false })
onBeforeUnmount(() => { for (const url of objectUrls.values()) URL.revokeObjectURL(url); objectUrls.clear() })

function urlOf(item: MaterialDraft) {
  if (item.existing) return item.existing.url
  if (!objectUrls.has(item.file!)) objectUrls.set(item.file!, URL.createObjectURL(item.file!))
  return objectUrls.get(item.file!)!
}
function kindOf(item: MaterialDraft): Material['kind'] { return item.existing ? item.existing.kind : item.file!.type.startsWith('video/') ? 'video' : 'image' }
function nameOf(item: MaterialDraft) { return item.existing?.filename || item.file?.name || (kindOf(item) === 'video' ? '视频' : '图片') }

let seq = 0
function add(files: File[]) {
  if (props.disabled || !files.length) return 0
  const room = props.max - items.value.length
  if (room <= 0) { ElMessage.warning(`最多 ${props.max} 个素材`); return 0 }
  const ok: MaterialDraft[] = []
  const skipped: string[] = []
  let total = items.value.reduce((sum, it) => sum + (it.file?.size ?? it.existing?.sizeBytes ?? 0), 0)
  for (const f of files) {
    const video = f.type === 'video/mp4'
    if (!imageTypes.includes(f.type) && !(video && props.allowVideo)) { skipped.push(`${f.name}：格式不支持`); continue }
    const limit = video ? props.maxVideoBytes : props.maxImageBytes
    if (f.size > limit) { skipped.push(`${f.name}：超过 ${limit >> 20} MB`); continue }
    if (ok.length >= room) { skipped.push(`${f.name}：超过 ${props.max} 个上限`); continue }
    if (total + f.size > materialLimits.totalBytes) { skipped.push(`${f.name}：整组超过 ${materialLimits.totalBytes >> 20} MB`); continue }
    total += f.size
    ok.push({ key: `new-${Date.now()}-${seq++}`, file: f })
  }
  if (ok.length) items.value = [...items.value, ...ok]
  if (skipped.length) ElMessage.warning(skipped.length === 1 ? `没有添加 ${skipped[0]}` : `有 ${skipped.length} 个没有添加：${skipped.slice(0, 2).join('；')}${skipped.length > 2 ? '…' : ''}`)
  return ok.length
}
function remove(i: number) { if (!props.disabled) items.value = items.value.filter((_, j) => j !== i) }
function move(from: number, to: number) {
  if (props.disabled || to < 0 || to >= items.value.length || from === to) return
  const next = [...items.value]
  const [it] = next.splice(from, 1)
  next.splice(to, 0, it)
  items.value = next
}

/** 键盘调整顺序后焦点跟着这一项走，可以连续按 */
async function moveByKey(from: number, to: number) {
  const key = items.value[from]?.key
  move(from, to)
  await nextTick()
  root.value?.querySelector<HTMLElement>(`[data-key="${key}"]`)?.focus()
}
function onChoose(e: Event) { const el = e.target as HTMLInputElement; add(Array.from(el.files || [])); el.value = '' }
function isFileDrag(e: DragEvent) { return !dragKey.value && Array.from(e.dataTransfer?.types || []).includes('Files') }
function onFileDragOver(e: DragEvent) { if (isFileDrag(e)) { e.preventDefault(); fileDrag.value = true } }
function onFileDrop(e: DragEvent) {
  if (!isFileDrag(e)) return
  e.preventDefault(); fileDrag.value = false
  add(Array.from(e.dataTransfer?.files || []))
}
function onTileDragStart(key: string, e: DragEvent) {
  dragKey.value = key
  e.dataTransfer?.setData('text/plain', key)
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move'
}
function onTileDragEnter(i: number) {
  if (!dragKey.value) return
  const from = items.value.findIndex(it => it.key === dragKey.value)
  if (from !== -1 && from !== i) move(from, i)
}
/** 在表单上粘贴时调用；剪贴板里有图片或视频才拦截，纯文字照常粘贴 */
function fromClipboard(e: ClipboardEvent) {
  const files = Array.from(e.clipboardData?.items || []).filter(i => i.kind === 'file').map(i => i.getAsFile()).filter(Boolean) as File[]
  if (!files.length) return
  e.preventDefault()
  const named = files.map((f, n) => f.name && f.name !== 'image.png' ? f : new File([f], `粘贴的截图${files.length > 1 ? n + 1 : ''}.${f.type.split('/')[1]?.replace('jpeg', 'jpg') || 'png'}`, { type: f.type }))
  const added = add(named)
  if (added) ElMessage.success(added > 1 ? `已添加 ${added} 个粘贴的素材` : '已添加粘贴的截图')
}
defineExpose({ fromClipboard })
</script>

<style scoped>
.material-picker { width: 100%; box-sizing: border-box; padding: 8px; border: 1px dashed var(--line-strong); border-radius: var(--radius-sm); transition: border-color var(--dur-fast) var(--ease), background var(--dur-fast) var(--ease); }
.material-picker:has(.mp-tile) { border-style: solid; border-color: var(--line); }
.material-picker.dragging { border-style: dashed; border-color: var(--primary); background: var(--primary-light); }
.material-picker.disabled { opacity: .6; }
.mp-list { position: relative; display: flex; flex-wrap: wrap; gap: 8px; margin: 0; padding: 0; list-style: none; }
.mp-tile, .mp-add-wrap { position: relative; flex: none; width: 76px; height: 76px; }
.mp-tile { border-radius: var(--radius-xs); cursor: grab; }
.mp-tile:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
.mp-tile.moving { opacity: .4; }
.mp-thumb { display: block; width: 100%; height: 100%; border: 1px solid var(--line); border-radius: var(--radius-xs); overflow: hidden; background: var(--surface-3); }
.mp-thumb img, .mp-thumb video { display: block; width: 100%; height: 100%; object-fit: cover; pointer-events: none; transition: transform var(--dur) var(--ease); }
.mp-tile:hover .mp-thumb img, .mp-tile:hover .mp-thumb video { transform: scale(1.04); }
.mp-play { position: absolute; inset: 0; display: grid; place-items: center; color: #fff; font-size: 24px; pointer-events: none; filter: drop-shadow(0 1px 2px rgb(0 0 0 / .45)); }
.mp-index { position: absolute; left: 4px; top: 4px; min-width: 18px; height: 18px; padding: 0 5px; box-sizing: border-box; border-radius: 9px; background: rgb(26 28 31 / .72); color: #fff; font-size: 12px; line-height: 18px; text-align: center; font-variant-numeric: tabular-nums; pointer-events: none; }
.mp-new { position: absolute; left: 4px; bottom: 4px; padding: 0 5px; border-radius: 4px; background: var(--primary); color: #fff; font-size: 12px; line-height: 18px; pointer-events: none; }
.mp-remove { position: absolute; right: -6px; top: -6px; display: grid; place-items: center; width: 22px; height: 22px; padding: 0; border: 1px solid var(--line-strong); border-radius: 50%; background: var(--surface); color: var(--text-2); font-size: 12px; cursor: pointer; opacity: 0; transform: scale(.85); transition: opacity var(--dur-fast) var(--ease), transform var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease), border-color var(--dur-fast) var(--ease); }
.mp-tile:hover .mp-remove, .mp-tile:focus-within .mp-remove { opacity: 1; transform: scale(1); }
.mp-remove:hover { border-color: var(--danger); color: var(--danger); }
.mp-remove:focus-visible { opacity: 1; transform: scale(1); outline: 2px solid var(--primary); outline-offset: 1px; }
@media (hover: none) { .mp-remove { opacity: 1; transform: none; } }
.mp-add { display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 4px; width: 100%; height: 100%; padding: 0; border: 1px dashed var(--line-strong); border-radius: var(--radius-xs); background: var(--surface); color: var(--muted); font: inherit; font-size: 12px; cursor: pointer; transition: background var(--dur-fast) var(--ease), border-color var(--dur-fast) var(--ease), color var(--dur-fast) var(--ease); }
.mp-add .el-icon { font-size: 18px; }
.mp-add:hover:not(:disabled) { border-color: var(--primary); color: var(--primary-text); background: var(--primary-light); }
.mp-add:active:not(:disabled) { transform: scale(.97); }
.mp-add:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
.mp-add:disabled { cursor: not-allowed; }
.mp-hint { display: flex; justify-content: space-between; gap: 12px; margin: 8px 0 0; color: var(--muted); font-size: 12px; line-height: 1.5; }
.mp-count { flex: none; font-variant-numeric: tabular-nums; transition: color var(--dur-fast) var(--ease); }
.mp-count.full { color: var(--warning); }
.mp-item-move { transition: transform var(--dur) var(--ease); }
.mp-item-enter-active, .mp-item-leave-active { transition: opacity var(--dur) var(--ease), transform var(--dur) var(--ease); }
.mp-item-enter-from, .mp-item-leave-to { opacity: 0; transform: scale(.9); }
.mp-item-leave-active { position: absolute; }
@media (prefers-reduced-motion: reduce) { .mp-item-move, .mp-item-enter-active, .mp-item-leave-active, .mp-thumb img, .mp-thumb video, .mp-remove { transition: none; } }
</style>
