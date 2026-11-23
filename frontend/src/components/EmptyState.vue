<template>
  <div class="empty-state">
    <Mascot :mood="mood" :size="compact ? 64 : 88" float />
    <strong>{{ title }}</strong>
    <p v-if="description">{{ description }}</p>
    <slot />
  </div>
</template>
<script setup lang="ts">
import Mascot from '@/components/fx/Mascot.vue'
withDefaults(defineProps<{ title: string; description?: string; mood?: 'happy' | 'sleepy' | 'thinking' | 'wave'; compact?: boolean }>(), { mood: 'sleepy', compact: false })
</script>
