<template>
  <header class="page-header">
    <button v-if="back" type="button" class="page-back" @click="goBack"><el-icon><ArrowLeft /></el-icon>{{ back.label }}</button>
    <div class="page-header-row">
      <h1>{{ title }}</h1>
      <div v-if="$slots.default" class="page-actions"><slot /></div>
    </div>
    <p v-if="description">{{ description }}</p>
  </header>
</template>
<script setup lang="ts">
import { ArrowLeft } from '@element-plus/icons-vue'
import { useRouter, type RouteLocationRaw } from 'vue-router'
const props = defineProps<{ title: string; description?: string; back?: { label: string; to: RouteLocationRaw } }>()
const router = useRouter()
function goBack() { if (props.back) router.push(props.back.to) }
</script>
