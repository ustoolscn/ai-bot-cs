<template>
  <div class="style-picker" role="radiogroup" aria-label="状态图样式">
    <div
      v-for="s in styles" :key="s.id" role="radio" tabindex="0" class="style-tile"
      :class="{ active: modelValue === s.id }" :aria-checked="modelValue === s.id" @click="emit('update:modelValue', s.id)" @keydown.enter.space.prevent="emit('update:modelValue', s.id)"
    >
      <span class="thumb" :class="{ dark: s.id === 'night' }">
        <iframe :srcdoc="docs[s.id]" sandbox="allow-scripts" tabindex="-1" aria-hidden="true" :title="s.name" />
      </span>
      <span class="tile-text"><strong>{{ s.name }}</strong><small>{{ s.hint }}</small></span>
      <el-button class="zoom" link type="primary" size="small" @click.stop="preview = s.id">放大看</el-button>
    </div>
    <button
      v-for="s in plain" :key="s.id" type="button" role="radio" class="style-tile plain"
      :class="{ active: modelValue === s.id }" :aria-checked="modelValue === s.id" @click="emit('update:modelValue', s.id)"
    >
      <span class="tile-text"><strong>{{ s.name }}</strong><small>{{ s.hint }}</small></span>
    </button>
  </div>

  <el-dialog :model-value="!!preview" :title="current ? current.name + ' · 示例数据' : ''" width="840px" @close="preview = ''">
    <div class="preview-frame" :class="{ dark: preview === 'night' }">
       <iframe v-if="preview" :srcdoc="docs[preview]" sandbox="allow-scripts allow-same-origin" :title="current?.name" @load="fit" />
    </div>
    <template #footer>
      <el-button @click="preview = ''">关闭</el-button>
      <el-button type="primary" @click="emit('update:modelValue', preview); preview = ''">用这个样式</el-button>
    </template>
  </el-dialog>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'

defineProps<{ modelValue: string }>()
const emit = defineEmits<{ 'update:modelValue': [value: string] }>()

// 与后端出图用的是同一份模板文件，这里只换成示例数据
const files = import.meta.glob('../../../../backend/internal/statuscard/templates/*.{html,json}', { query: '?raw', import: 'default', eager: true }) as Record<string, string>
const pick = (name: string) => Object.entries(files).find(([path]) => path.endsWith('/' + name))?.[1] ?? ''
const sample = pick('sample.json').replace(/<\//g, '<\\/')

const styles = [
  { id: 'brief', name: '简报', hint: '一句结论，各分组 24 小时柱状' },
  { id: 'table', name: '明细表', hint: '分组和模型的成功率、请求数、延迟' },
  { id: 'heatmap', name: '热力格', hint: '每小时一格，哪段时间出问题一眼看出' },
  { id: 'night', name: '夜间卡片', hint: '深色竖版，适合手机查看' },
]
const plain = [
  { id: 'page', name: '截取状态页网页', hint: '直接截下方填写的状态页地址' },
  { id: 'off', name: '不发图片', hint: '只用文字回答' },
]
const docs = Object.fromEntries(styles.map(s => [s.id, pick(s.id + '.html').replace('<head>', `<head><script>window.STATUS_DATA=${sample}<\/script>`)])) as Record<string, string>

const preview = ref('')
const current = computed(() => styles.find(s => s.id === preview.value))
// 弹窗里按卡片实际高度撑开，不出现内部滚动
function fit(event: Event) {
  const frame = event.target as HTMLIFrameElement
  const card = frame.contentDocument?.getElementById('card')
  if (card) frame.style.height = card.scrollHeight + 'px'
}
</script>

<style scoped>
.style-picker { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 12px; }
.style-tile { position: relative; display: flex; flex-direction: column; padding: 0; overflow: hidden; text-align: left; background: var(--surface); border: 1px solid var(--line); border-radius: 10px; cursor: pointer; transition: border-color .15s, box-shadow .15s; }
.style-tile:hover { border-color: var(--line-strong); }
.style-tile.active { border-color: var(--primary); box-shadow: 0 0 0 1px var(--primary); }
.style-tile:focus-visible { outline: 2px solid var(--primary); outline-offset: 2px; }
.thumb { position: relative; display: block; height: 150px; overflow: hidden; background: #fff; border-bottom: 1px solid var(--line); }
.thumb.dark { background: #16191f; }
.thumb iframe { position: absolute; top: 0; left: 0; width: 760px; height: 640px; border: 0; transform: scale(.27); transform-origin: 0 0; pointer-events: none; }
.tile-text { display: flex; flex-direction: column; gap: 2px; padding: 10px 12px 12px; }
.tile-text strong { font-size: 14px; font-weight: 600; color: var(--text); }
.tile-text small { font-size: 12px; line-height: 1.5; color: var(--muted); }
.zoom { position: absolute; top: 8px; right: 8px; padding: 2px 8px; background: var(--surface); border-radius: 6px; opacity: 0; transition: opacity .15s; }
.style-tile:hover .zoom, .style-tile:focus-within .zoom { opacity: 1; }
.style-tile.plain { grid-column: span 2; }
.preview-frame { max-height: 70vh; overflow: auto; background: #fff; border: 1px solid var(--line); border-radius: 8px; }
.preview-frame.dark { background: #16191f; }
.preview-frame iframe { display: block; width: 100%; min-height: 400px; border: 0; }
@media (max-width: 900px) { .style-picker { grid-template-columns: repeat(2, minmax(0, 1fr)); } .style-tile.plain { grid-column: span 1; } }
</style>
