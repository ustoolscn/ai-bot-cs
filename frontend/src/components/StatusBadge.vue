<template><span class="status-badge" :class="`is-${status}`"><span class="status-dot" />{{ text || statusText(status) }}</span></template>
<script setup lang="ts">import { statusText } from '@/utils/format'; defineProps<{ status: string; text?: string }>()</script>
