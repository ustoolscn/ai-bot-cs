<template>
  <span class="avatar-image" :class="tone ? 'tone-' + tone : ''" :style="{ width: size + 'px', height: size + 'px', fontSize: Math.round(size * 0.42) + 'px' }" :title="name">
    <img v-if="src && !broken" :src="src" :alt="name || ''" loading="lazy" referrerpolicy="no-referrer" @error="broken = true">
    <template v-else>{{ initial }}</template>
  </span>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'

// 头像：有图显示图，图挂了或没有图显示名字首字
const props = withDefaults(defineProps<{ src?: string; name?: string; size?: number; tone?: 'primary' | 'success' | 'group' | 'c2c' | '' }>(), { src: '', name: '', size: 32, tone: '' })
const broken = ref(false)
watch(() => props.src, () => { broken.value = false })
const initial = computed(() => Array.from((props.name || '').replace(/^[@\s【\[(（]+/, ''))[0] || '?')
</script>

<style scoped>
.avatar-image { display: inline-grid; flex: none; place-items: center; overflow: hidden; border-radius: 50%; background: var(--surface-3); color: var(--text-2); font-weight: 600; line-height: 1; user-select: none; }
.avatar-image img { width: 100%; height: 100%; object-fit: cover; }
.tone-primary { background: var(--primary); color: #fff; }
.tone-success { background: var(--success); color: #fff; }
.tone-group { background: var(--primary-light); color: var(--primary-text); }
.tone-c2c { background: var(--success-light); color: var(--success); }
</style>
