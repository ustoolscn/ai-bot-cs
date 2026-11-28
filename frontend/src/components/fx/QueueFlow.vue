<template>
  <div class="queue-flow">
    <template v-for="(node, index) in nodes" :key="node.label">
      <component :is="node.to ? 'button' : 'div'" class="node" :class="[node.tone, { busy: node.count > 0, link: node.to }]" :type="node.to ? 'button' : undefined" :title="node.to ? `查看${node.label}的消息` : undefined" @click="node.to && emit('select', node)">
        <strong><CountUp :value="node.count" /></strong>
        <span>{{ node.label }}<template v-if="node.to"> ›</template></span>
      </component>
      <span v-if="index < nodes.length - 1" class="pipe" :class="{ moving: node.count > 0 || nodes[index + 1].count > 0 }"><i /></span>
    </template>
  </div>
</template>

<script setup lang="ts">
// 任务队列：节点之间有任务流动时，连线上的点在移动；数量变化时数字过渡。
import CountUp from './CountUp.vue'

type QueueNode = { label: string; count: number; tone?: string; to?: string }
defineProps<{ nodes: QueueNode[] }>()
const emit = defineEmits<{ select: [node: QueueNode] }>()
</script>

<style scoped>
.queue-flow { display: flex; align-items: center; gap: 0; height: 100%; }
.node { display: flex; flex: 1 1 0; flex-direction: column; align-items: center; justify-content: center; gap: 2px; min-width: 64px; min-height: 76px; padding: 10px 8px; border: 1px solid var(--line); border-radius: var(--radius-sm); background: var(--surface-2); transition: border-color .3s var(--ease), background .3s var(--ease); }
.node.link { font: inherit; color: inherit; cursor: pointer; }
.node.link:hover { filter: brightness(.97); }
.node.link:active { transform: scale(.97); }
.node strong { font-size: 22px; font-weight: 600; line-height: 1.2; }
.node > span { color: var(--muted); font-size: 12px; white-space: nowrap; }
.node.busy { border-color: var(--primary-soft); background: var(--primary-light); box-shadow: 0 0 18px var(--primary-light); }
.node.busy strong { color: var(--primary-text); }
.node.danger.busy { border-color: color-mix(in srgb, var(--danger) 42%, transparent); background: var(--danger-light); box-shadow: 0 0 18px var(--danger-light); }
.node.danger.busy strong { color: var(--danger); }
.pipe { position: relative; flex: 0 1 28px; min-width: 12px; height: 2px; background: var(--line); overflow: hidden; }
.pipe.moving { background: transparent; overflow: visible; }
.pipe.moving i { position: absolute; inset: 0 0 0 -12px; background: repeating-linear-gradient(90deg, var(--primary) 0 6px, transparent 6px 12px); filter: drop-shadow(0 0 4px var(--primary)); animation: march .6s linear infinite; }
@keyframes march { from { transform: translateX(-12px); } to { transform: translateX(0); } }
</style>
