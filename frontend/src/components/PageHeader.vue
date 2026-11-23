<template>
  <div class="page-header">
    <div><h1>{{ title }}</h1><p v-if="description">{{ description }}</p></div>
    <div v-if="$slots.default" class="page-actions"><slot /></div>
  </div>
</template>
<script setup lang="ts">defineProps<{ title: string; description?: string }>()</script>
