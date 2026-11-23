<template>
  <div class="app-shell">
    <aside class="sidebar" :class="{ collapsed, 'mobile-open': mobileOpen }" aria-label="主导航">
      <div class="sidebar-head">
        <BrandLogo subtitle="QQ 客服管理" />
        <button v-if="collapsed" class="icon-button collapse-toggle" aria-label="展开侧栏" @click="toggleCollapsed"><el-icon><Expand /></el-icon></button>
      </div>
      <button class="sidebar-search" @click="openSearch"><el-icon><Search /></el-icon><span>搜索页面</span><kbd>{{ shortcutLabel }}</kbd></button>
      <nav class="sidebar-nav">
        <div v-for="group in navGroups" :key="group.title" class="nav-group">
          <div class="nav-group-title">{{ group.title }}</div>
          <el-tooltip v-for="item in group.items" :key="item.path" :content="item.label" placement="right" :disabled="!collapsed">
            <router-link :to="item.path" class="nav-link" @click="mobileOpen = false">
              <el-icon><component :is="item.icon" /></el-icon>
              <span>{{ item.label }}</span>
              <em v-if="item.badge && badgeCount(item.badge)" class="nav-badge" :class="{ soft: item.badge === 'candidates' }">{{ badgeCount(item.badge) }}</em>
            </router-link>
          </el-tooltip>
        </div>
      </nav>
      <router-link v-if="shell.openHandoffs" to="/handoffs" class="sidebar-tip" @click="mobileOpen = false">
        <Mascot mood="wave" head-only :size="34" />
        <span><strong>有 {{ shell.openHandoffs }} 位用户在等你</strong>去人工台看看吧</span>
      </router-link>
      <div v-else class="sidebar-tip">
        <Mascot mood="happy" head-only :size="34" />
        <span><strong>一切顺利</strong>小Q 正在认真回复中</span>
      </div>
      <div class="sidebar-foot">
        <el-dropdown trigger="click" placement="top-start" @command="handleCommand">
          <button class="user-menu" aria-label="用户菜单"><span class="avatar">{{ initial }}</span><span>{{ auth.username || 'admin' }}</span></button>
          <template #dropdown><el-dropdown-menu><el-dropdown-item command="system" :icon="Setting">系统状态</el-dropdown-item><el-dropdown-item divided command="logout" :icon="SwitchButton">退出登录</el-dropdown-item></el-dropdown-menu></template>
        </el-dropdown>
        <button v-if="!collapsed" class="icon-button collapse-toggle" title="收起侧栏" aria-label="收起侧栏" @click="toggleCollapsed"><el-icon><Fold /></el-icon></button>
      </div>
    </aside>
    <div v-if="mobileOpen" class="sidebar-backdrop" @click="mobileOpen = false" />

    <div class="main-area">
      <header class="mobile-bar">
        <button class="icon-button" aria-label="打开导航" @click="mobileOpen = true"><el-icon><Menu /></el-icon></button>
        <BrandLogo />
      </header>
      <div v-if="connection.lost && !bannerDismissed" class="connection-banner" role="status">
        <el-icon><WarningFilled /></el-icon>
        <span v-if="connection.usingMock">后端服务未连接，页面上显示的是示例数据，保存操作不会生效。</span>
        <span v-else>暂时连不上后端服务，数据可能不是最新的。请稍后点页面上的“刷新”重试。</span>
        <el-button text size="small" @click="bannerDismissed = true">知道了</el-button>
      </div>
      <main class="route-view"><router-view /></main>
    </div>

    <el-dialog v-model="searchVisible" width="560px" class="search-dialog" align-center :show-close="false" @opened="focusSearch">
      <el-input ref="searchInput" v-model="search" class="search-input" size="large" placeholder="输入页面名称或要做的事，例如“转人工”“上传文档”" :prefix-icon="Search" @keydown="onSearchKey" />
      <div class="search-results">
        <button v-for="(result, index) in filteredResults" :key="result.path" :class="{ active: index === activeIndex }" @click="openResult(result.path)" @mouseenter="activeIndex = index">
          <el-icon><component :is="result.icon" /></el-icon>
          <span><strong>{{ result.label }}</strong><small>{{ result.description }}</small></span>
          <span class="group-label">{{ result.group }}</span>
        </button>
        <div v-if="!filteredResults.length" class="search-empty">没有找到相关页面</div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Expand, Fold, Menu, Search, Setting, SwitchButton, WarningFilled } from '@element-plus/icons-vue'
import BrandLogo from '@/components/BrandLogo.vue'
import Mascot from '@/components/fx/Mascot.vue'
import { useAuthStore } from '@/stores/auth'
import { navGroups, navItems } from '@/router/nav'
import { connection } from '@/utils/connection'
import { useShellStore } from '@/stores/shell'

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const shell = useShellStore()
const mobileOpen = ref(false)
const collapsed = ref(localStorage.getItem('sidebar-collapsed') === '1')
const searchVisible = ref(false)
const search = ref('')
const activeIndex = ref(0)
const bannerDismissed = ref(false)
const searchInput = ref<{ focus: () => void } | null>(null)
const initial = computed(() => (auth.username || 'A').slice(0, 1).toUpperCase())
const shortcutLabel = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘K' : 'Ctrl K'

const filteredResults = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return navItems
  return navItems.filter(item => `${item.label}${item.description}${item.group}`.toLowerCase().includes(keyword))
})
watch(search, () => { activeIndex.value = 0 })

function badgeCount(kind: 'handoffs' | 'candidates') { return kind === 'handoffs' ? shell.openHandoffs : shell.pendingCandidates }
function toggleCollapsed() { collapsed.value = !collapsed.value; localStorage.setItem('sidebar-collapsed', collapsed.value ? '1' : '0') }
function openSearch() { search.value = ''; activeIndex.value = 0; searchVisible.value = true }
function focusSearch() { nextTick(() => searchInput.value?.focus()) }
function openResult(path: string) { searchVisible.value = false; mobileOpen.value = false; router.push(path) }
function onSearchKey(event: Event) {
  const key = (event as KeyboardEvent).key
  if (key === 'ArrowDown') { event.preventDefault(); activeIndex.value = Math.min(activeIndex.value + 1, filteredResults.value.length - 1) }
  if (key === 'ArrowUp') { event.preventDefault(); activeIndex.value = Math.max(activeIndex.value - 1, 0) }
  if (key === 'Enter' && filteredResults.value[activeIndex.value]) openResult(filteredResults.value[activeIndex.value].path)
}
function onGlobalKey(event: KeyboardEvent) {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') { event.preventDefault(); openSearch() }
}
async function handleCommand(command: string) {
  if (command === 'system') router.push('/system')
  if (command === 'logout') { await auth.logout(); router.push('/login') }
}

watch(() => route.path, (path) => {
  const item = navItems.find(entry => path.startsWith(entry.path))
  document.title = item ? `${item.label} · AI Bot Hub` : 'AI Bot Hub'
  shell.refreshCounts()
}, { immediate: true })

let timer: number | undefined
onMounted(() => { window.addEventListener('keydown', onGlobalKey); timer = window.setInterval(() => shell.refreshCounts(), 60000) })
onBeforeUnmount(() => { window.removeEventListener('keydown', onGlobalKey); window.clearInterval(timer) })
</script>
