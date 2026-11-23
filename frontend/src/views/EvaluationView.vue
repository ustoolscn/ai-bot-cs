<template>
  <div class="content-page">
    <PageHeader title="效果评测" description="用一组固定问题检验客服的回答。调整人设、模型或知识库前后各跑一次，就能看出是变好还是变差。" />
    <EvalPanel :chat-models="chatModels" :knowledge-bases="knowledgeBases" />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import PageHeader from '@/components/PageHeader.vue'
import EvalPanel from '@/components/agent/EvalPanel.vue'
import { api } from '@/api'
import type { KnowledgeBase, ModelProfile } from '@/types'

const models = ref<ModelProfile[]>([])
const knowledgeBases = ref<KnowledgeBase[]>([])
const chatModels = computed(() => models.value.filter(m => m.kind === 'chat'))
onMounted(async () => { [models.value, knowledgeBases.value] = await Promise.all([api.models.list(), api.knowledge.list()]) })
</script>
