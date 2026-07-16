<template>
  <div class="app-shell">
    <header class="topbar">
      <BrandLogo class="topbar-brand" />
      <nav class="global-nav" aria-label="主导航">
        <router-link v-for="item in navItems" :key="item.path" :to="item.path">{{ item.label }}</router-link>
      </nav>
      <div class="topbar-actions">
        <el-tooltip content="搜索消息、机器人和知识库"><el-button text circle aria-label="搜索" @click="searchVisible = true"><el-icon><Search /></el-icon></el-button></el-tooltip>
        <el-button text circle aria-label="通知" @click="$router.push('/overview#alerts')"><el-icon><Bell /></el-icon></el-button>
        <el-tooltip content="系统设置"><el-button text circle aria-label="系统设置" @click="$router.push('/system')"><el-icon><Setting /></el-icon></el-button></el-tooltip>
        <el-dropdown trigger="click" @command="handleCommand">
          <button class="user-menu" aria-label="用户菜单"><span class="avatar">A</span><span class="user-name">{{ auth.username || 'admin' }}</span><el-icon><ArrowDown /></el-icon></button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item command="system">系统设置</el-dropdown-item><el-dropdown-item divided command="logout">退出登录</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
      </div>
      <el-button class="mobile-menu" text circle @click="mobileOpen = !mobileOpen"><el-icon><Menu /></el-icon></el-button>
    </header>
    <div v-if="mobileOpen" class="mobile-nav">
      <router-link v-for="item in [...navItems, { path: '/messages', label: '消息' }, { path: '/system', label: '系统' }]" :key="item.path" :to="item.path" @click="mobileOpen = false">{{ item.label }}</router-link>
    </div>
    <main class="route-view"><router-view /></main>

    <el-dialog v-model="searchVisible" title="全局搜索" width="560px" class="search-dialog">
      <el-input v-model="search" size="large" autofocus placeholder="搜索消息、机器人、会话或知识库" :prefix-icon="Search" />
      <div class="search-results">
        <button v-for="result in filteredResults" :key="result.path" @click="openResult(result.path)"><el-icon><component :is="result.icon" /></el-icon><span><strong>{{ result.title }}</strong><small>{{ result.subtitle }}</small></span><el-icon class="result-arrow"><ArrowRight /></el-icon></button>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ArrowDown, ArrowRight, Bell, ChatDotRound, Cpu, Files, Menu, Search, Setting } from '@element-plus/icons-vue'
import BrandLogo from '@/components/BrandLogo.vue'
import { useAuthStore } from '@/stores/auth'

const router = useRouter()
const auth = useAuthStore()
const mobileOpen = ref(false)
const searchVisible = ref(false)
const search = ref('')
const navItems = [
  { path: '/overview', label: '概览' }, { path: '/bots', label: '机器人' }, { path: '/conversations', label: '会话' }, { path: '/knowledge', label: '知识' }, { path: '/models', label: '模型' },
]
const results = [
  { title: '消息记录', subtitle: '查看完整处理链路', path: '/messages', icon: ChatDotRound },
  { title: '机器人管理', subtitle: '配置 QQ 机器人与回调', path: '/bots', icon: Cpu },
  { title: '知识库管理', subtitle: '管理文档与向量索引', path: '/knowledge', icon: Files },
  { title: '系统状态', subtitle: '任务队列与服务配置', path: '/system', icon: Setting },
]
const filteredResults = computed(() => !search.value ? results : results.filter((item) => `${item.title}${item.subtitle}`.includes(search.value)))
function openResult(path: string) { searchVisible.value = false; router.push(path) }
async function handleCommand(command: string) { if (command === 'system') router.push('/system'); if (command === 'logout') { await auth.logout(); router.push('/login') } }
</script>
