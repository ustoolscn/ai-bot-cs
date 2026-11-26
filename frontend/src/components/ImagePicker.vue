<template>
  <div class="image-picker" :class="{ dragging }" @dragover.prevent="dragging = true" @dragleave.self="dragging = false" @drop.prevent="onDrop">
    <input ref="input" type="file" accept="image/png,image/jpeg" hidden @change="onChoose">
    <Transition name="ip-swap" mode="out-in">
      <div v-if="preview" :key="preview" class="ip-preview">
        <a :href="preview" target="_blank" rel="noopener" class="ip-thumb" title="查看原图"><img :src="preview" :alt="file ? file.name : '已保存的配图'"></a>
        <div class="ip-meta">
          <strong>{{ file ? file.name : '已保存的配图' }}</strong>
          <span>{{ file ? sizeText(file.size) + ' · 保存后生效' : 'AI 回答时会把这张图一起发出去' }}</span>
          <div class="ip-actions">
            <el-button link type="primary" :disabled="disabled" @click="input?.click()">换一张</el-button>
            <el-button link type="danger" :disabled="disabled" @click="clear">{{ file && existingUrl && !removed ? '不换了' : '移除' }}</el-button>
          </div>
        </div>
      </div>
      <button v-else key="empty" type="button" class="ip-empty" :disabled="disabled" @click="input?.click()">
        <el-icon><Picture /></el-icon>
        <span><strong>{{ dragging ? '松开即可添加' : '添加图片' }}</strong><small>{{ hint }}</small></span>
      </button>
    </Transition>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { Picture } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'

const props = withDefaults(defineProps<{ existingUrl?: string | null; disabled?: boolean; hint?: string }>(), { existingUrl: null, disabled: false, hint: '演示截图、操作示意图，PNG 或 JPEG，5 MB 以内；也可以直接粘贴截图' })
const file = defineModel<File | null>('file', { default: null })
/** 编辑已有配图时，用户点了移除 */
const removed = defineModel<boolean>('removed', { default: false })
const input = ref<HTMLInputElement>()
const dragging = ref(false)
const objectUrl = ref('')

watch(file, f => {
  if (objectUrl.value) URL.revokeObjectURL(objectUrl.value)
  objectUrl.value = f ? URL.createObjectURL(f) : ''
}, { immediate: true })
onBeforeUnmount(() => { if (objectUrl.value) URL.revokeObjectURL(objectUrl.value) })

const preview = computed(() => objectUrl.value || (props.existingUrl && !removed.value ? props.existingUrl : ''))
function sizeText(n: number) { return n >= 1 << 20 ? `${(n / (1 << 20)).toFixed(1)} MB` : `${Math.max(1, Math.round(n / 1024))} KB` }

function accept(f: File | null | undefined) {
  if (!f || props.disabled) return false
  if (!['image/png', 'image/jpeg'].includes(f.type)) { ElMessage.warning('只支持 PNG 或 JPEG 图片'); return false }
  if (f.size > 5 << 20) { ElMessage.warning('图片超过 5 MB，请压缩后再传'); return false }
  file.value = f
  return true
}
function onChoose(e: Event) { const el = e.target as HTMLInputElement; accept(el.files?.[0]); el.value = '' }
function onDrop(e: DragEvent) { dragging.value = false; accept(e.dataTransfer?.files?.[0]) }
function clear() {
  if (file.value) { file.value = null; return }
  if (props.existingUrl) removed.value = true
}
/** 在表单上粘贴截图时调用；剪贴板里有图片才拦截，纯文字照常粘贴 */
function fromClipboard(e: ClipboardEvent) {
  const item = Array.from(e.clipboardData?.items || []).find(i => i.kind === 'file' && i.type.startsWith('image/'))
  if (!item) return
  e.preventDefault()
  const raw = item.getAsFile()
  if (raw && accept(new File([raw], raw.name && raw.name !== 'image.png' ? raw.name : `粘贴的截图.${raw.type === 'image/jpeg' ? 'jpg' : 'png'}`, { type: raw.type }))) ElMessage.success('已添加粘贴的图片')
}
defineExpose({ fromClipboard })
</script>

<style scoped>
.image-picker { width: 100%; box-sizing: border-box; border: 1px dashed var(--line-strong); border-radius: var(--radius-sm); transition: border-color var(--dur-fast) var(--ease), background var(--dur-fast) var(--ease); }
.image-picker.dragging { border-color: var(--primary); background: var(--primary-light); }
.image-picker:has(.ip-preview) { border-style: solid; border-color: var(--line); }
.ip-empty { display: flex; align-items: center; gap: 10px; width: 100%; padding: 10px 12px; border: 0; background: transparent; color: var(--text-2); font: inherit; text-align: left; cursor: pointer; transition: background var(--dur-fast) var(--ease); }
.ip-empty:hover:not(:disabled) { background: var(--surface-2); }
.ip-empty:focus-visible { outline: 2px solid var(--primary); outline-offset: -2px; border-radius: var(--radius-sm); }
.ip-empty:disabled { cursor: not-allowed; opacity: .6; }
.ip-empty .el-icon { flex: none; font-size: 18px; color: var(--muted); }
.ip-empty span { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.ip-empty strong { font-size: 13px; font-weight: 500; }
.ip-empty small { color: var(--muted); font-size: 12px; line-height: 1.5; }
.ip-preview { display: flex; align-items: center; gap: 12px; padding: 8px; }
.ip-thumb { flex: none; display: block; width: 72px; height: 72px; border-radius: var(--radius-xs, 6px); overflow: hidden; background: var(--surface-3); }
.ip-thumb img { display: block; width: 100%; height: 100%; object-fit: cover; transition: transform var(--dur) var(--ease); }
.ip-thumb:hover img { transform: scale(1.04); }
.ip-meta { display: flex; flex-direction: column; gap: 2px; min-width: 0; }
.ip-meta strong { overflow: hidden; font-size: 13px; font-weight: 500; text-overflow: ellipsis; white-space: nowrap; }
.ip-meta > span { color: var(--muted); font-size: 12px; }
.ip-actions { display: flex; gap: 4px; margin-top: 2px; }
.ip-actions .el-button { font-size: 12px; }
.ip-swap-enter-active, .ip-swap-leave-active { transition: opacity var(--dur-fast) var(--ease), transform var(--dur-fast) var(--ease); }
.ip-swap-enter-from, .ip-swap-leave-to { opacity: 0; transform: translateY(3px); }
@media (prefers-reduced-motion: reduce) { .ip-swap-enter-active, .ip-swap-leave-active, .ip-thumb img { transition: none; } }
</style>
