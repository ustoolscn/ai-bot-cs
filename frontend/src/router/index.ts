import { createRouter, createWebHistory } from 'vue-router'

const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', name: 'login', component: () => import('@/views/LoginView.vue'), meta: { public: true } },
    { path: '/', component: () => import('@/layouts/MainLayout.vue'), children: [
      { path: '', redirect: '/overview' },
      { path: 'overview', name: 'overview', component: () => import('@/views/DashboardView.vue') },
      { path: 'bots', name: 'bots', component: () => import('@/views/BotsView.vue') },
      { path: 'conversations', name: 'conversations', component: () => import('@/views/ConversationsView.vue') },
      { path: 'messages', name: 'messages', component: () => import('@/views/MessagesView.vue') },
      { path: 'knowledge', name: 'knowledge', component: () => import('@/views/KnowledgeView.vue') },
      { path: 'models', name: 'models', component: () => import('@/views/ModelsView.vue') },
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
