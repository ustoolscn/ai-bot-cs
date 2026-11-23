import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
    { path: '/', component: () => import('@/layouts/MainLayout.vue'), children: [
      { path: '', redirect: '/overview' },
      { path: 'overview', name: 'overview', component: () => import('@/views/DashboardView.vue') },
      { path: 'schedules', name: 'schedules', component: () => import('@/views/SchedulesView.vue') },
      { path: 'bots', name: 'bots', component: () => import('@/views/BotsView.vue') },
      { path: 'conversations', name: 'conversations', component: () => import('@/views/ConversationsView.vue') },
      { path: 'blacklist', name: 'blacklist', component: () => import('@/views/BlacklistView.vue') },
      { path: 'messages', name: 'messages', component: () => import('@/views/MessagesView.vue') },
      { path: 'knowledge', name: 'knowledge', component: () => import('@/views/KnowledgeView.vue') },
      { path: 'models', name: 'models', component: () => import('@/views/ModelsView.vue') },
      { path: 'agent', name: 'agent', component: () => import('@/views/SupportView.vue'), props: { mode: 'agents' } },
      { path: 'personalities', name: 'personalities', component: () => import('@/views/SupportView.vue'), props: { mode: 'personalities' } },
      { path: 'bots/:id/services', name: 'bot-services', component: () => import('@/views/SupportView.vue'), props: route => ({ mode: 'services', botId: route.params.id }) },
      { path: 'shared-settings', name: 'shared-settings', component: () => import('@/views/AgentView.vue') },
      { path: 'memories', name: 'memories', component: () => import('@/views/MemoriesView.vue') },
      { path: 'learning', redirect: to => to.query.tab === 'memories'
        ? { name: 'memories' }
        : { name: 'knowledge', query: { ...to.query, tab: to.query.tab || 'candidates' } } },
      { path: 'evaluation', name: 'evaluation', component: () => import('@/views/EvaluationView.vue') },
      { path: 'handoffs', name: 'handoffs', component: () => import('@/views/HandoffView.vue') },
      { path: 'system', name: 'system', component: () => import('@/views/SystemView.vue') },
    ] },
    { path: '/:pathMatch(.*)*', redirect: '/overview' },
  ],
})

router.beforeEach((to) => {
  if (!to.meta.public && !sessionStorage.getItem('ai-bot-hub-demo-session')) return { name: 'login', query: { redirect: to.fullPath } }
  if (to.name === 'login' && sessionStorage.getItem('ai-bot-hub-demo-session')) return { name: 'overview' }
})

export default router
