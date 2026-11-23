<template>
  <div class="brand-logo" aria-label="AI Bot Hub">
    <span class="brand-mark"><Mascot head-only :size="28" /></span>
    <span class="brand-text">AI Bot Hub<small v-if="subtitle">{{ subtitle }}</small></span>
  </div>
</template>

<script setup lang="ts">
import Mascot from '@/components/fx/Mascot.vue'
defineProps<{ subtitle?: string }>()
</script>
