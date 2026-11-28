import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
    {
      path: '/',
      component: () => import('@/layouts/MainLayout.vue'),
      children: [
        { path: '', redirect: '/overview' },
        { path: 'overview', name: 'overview', component: () => import('@/views/DashboardView.vue') },
        { path: 'handoffs', name: 'handoffs', component: () => import('@/views/HandoffView.vue') },
        { path: 'conversations', name: 'conversations', component: () => import('@/views/ConversationsView.vue') },
        { path: 'conversations/:id', name: 'conversation', component: () => import('@/views/ConversationsView.vue'), props: true },
        { path: 'operations', name: 'operations', component: () => import('@/views/OperationsView.vue') },
        { path: 'schedules', redirect: to => ({ path: '/operations', query: { ...to.query, tab: 'schedules' } }) },
        { path: 'blacklist', redirect: to => ({ path: '/operations', query: { ...to.query, tab: 'blacklist' } }) },
        { path: 'knowledge', name: 'knowledge', component: () => import('@/views/KnowledgeView.vue') },
        { path: 'memories', name: 'memories', component: () => import('@/views/MemoriesView.vue') },
        {
          path: 'learning',
          redirect: to =>
            to.query.tab === 'memories'
              ? { name: 'memories' }
              : { name: 'knowledge', query: { ...to.query, tab: to.query.tab || 'candidates' } },
        },
        { path: 'bots', name: 'bots', component: () => import('@/views/BotsView.vue') },
        { path: 'bots/:id/services', name: 'bot-services', component: () => import('@/views/SupportView.vue'), props: route => ({ botId: route.params.id }) },
        { path: 'bots/:id/services/:serviceId', name: 'bot-service', component: () => import('@/views/ServiceDetailView.vue'), props: route => ({ botId: route.params.id, serviceId: route.params.serviceId }) },
        { path: 'agent', redirect: '/bots' },
        { path: 'personalities', redirect: '/bots' },
        { path: 'studio', name: 'studio', component: () => import('@/views/StudioView.vue') },
        { path: 'shared-settings', redirect: to => ({ path: '/studio', query: { ...to.query, tab: 'rules' } }) },
        { path: 'evaluation', redirect: to => ({ path: '/studio', query: { ...to.query, tab: 'eval' } }) },
        { path: 'models', name: 'models', component: () => import('@/views/ModelsView.vue') },
        { path: 'messages', name: 'messages', component: () => import('@/views/MessagesView.vue') },
        { path: 'system', name: 'system', component: () => import('@/views/SystemView.vue') },
      ],
    },
    { path: '/:pathMatch(.*)*', redirect: '/overview' },
  ],
})

router.beforeEach((to) => {
  if (!to.meta.public && !sessionStorage.getItem('ai-bot-hub-demo-session')) return { name: 'login', query: { redirect: to.fullPath } }
  if (to.name === 'login' && sessionStorage.getItem('ai-bot-hub-demo-session')) return { name: 'overview' }
})

export default router
