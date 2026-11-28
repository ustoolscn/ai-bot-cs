<template>
  <div class="app-shell">
    <aside class="sidebar" :class="{ collapsed, 'mobile-open': mobileOpen }" aria-label="主导航">
      <div class="sidebar-head">
        <BrandLogo subtitle="QQ 客服控制中枢" />
        <button class="icon-button" :title="collapsed ? '展开侧栏' : '收起侧栏'" :aria-label="collapsed ? '展开侧栏' : '收起侧栏'" @click="toggleCollapsed">
          <el-icon><component :is="collapsed ? Expand : Fold" /></el-icon>
        </button>
      </div>
      <button class="sidebar-search" aria-label="搜索页面" @click="openSearch">
        <el-icon><Search /></el-icon><span>搜索功能或页面</span><kbd>{{ shortcutLabel }}</kbd>
      </button>
      <nav class="sidebar-nav">
        <div v-for="group in navGroups" :key="group.title" class="nav-group">
          <div class="nav-group-title">{{ group.title }}</div>
          <el-tooltip v-for="item in group.items" :key="item.path" :content="item.label" placement="right" :disabled="!collapsed">
            <router-link :to="item.path" class="nav-link" :class="{ 'router-link-active': isActive(item.path) }" @click="mobileOpen = false">
              <el-icon><component :is="item.icon" /></el-icon>
              <span>{{ item.label }}</span>
              <em v-if="item.badge && badgeCount(item.badge)" class="nav-badge" :class="{ soft: item.badge === 'candidates' }">{{ badgeCount(item.badge) }}</em>
            </router-link>
          </el-tooltip>
        </div>
      </nav>
      <div class="sidebar-foot">
        <el-dropdown trigger="click" placement="top-start" @command="handleCommand">
          <button class="user-menu" aria-label="用户菜单"><span class="avatar">{{ initial }}</span><span>{{ auth.username || 'admin' }}</span></button>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="monitor" :icon="Monitor">实时运维监控</el-dropdown-item>
              <el-dropdown-item command="settings" :icon="Setting">系统参数与安全</el-dropdown-item>
              <el-dropdown-item divided command="logout" :icon="SwitchButton">退出登录</el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
        <el-tooltip v-if="connection.lost || reconnecting" :content="connectionTip" placement="top">
          <span class="mock-flag">{{ connection.usingMock ? '示例数据' : '未连接' }}</span>
        </el-tooltip>
        <span v-else class="live-pulse" title="实时事件通道在线" />
      </div>
    </aside>
    <div v-if="mobileOpen" class="sidebar-backdrop" @click="mobileOpen = false" />

    <div class="main-area">
      <header class="mobile-bar">
        <button class="icon-button" aria-label="打开导航" @click="mobileOpen = true"><el-icon><Menu /></el-icon></button>
        <BrandLogo />
        <span class="spacer" />
        <span v-if="connection.lost || reconnecting" class="mock-flag">{{ connection.usingMock ? '示例数据' : '未连接' }}</span>
        <span v-else class="live-pulse" title="实时事件通道在线" />
      </header>
      <main class="route-view">
        <router-view v-slot="{ Component, route: current }">
          <Transition name="page" mode="out-in">
            <component :is="Component" :key="current.matched[1]?.path || current.path" />
          </Transition>
        </router-view>
      </main>
    </div>

    <el-dialog v-model="searchVisible" width="560px" class="search-dialog" align-center :show-close="false" @opened="focusSearch">
      <el-input ref="searchInput" v-model="search" class="search-input" size="large" placeholder="输入页面名称或要做的事，例如“禁言”“代理”“评测”" :prefix-icon="Search" @keydown="onSearchKey" />
      <div class="search-results">
        <button v-for="(result, index) in filteredResults" :key="result.path" :class="{ active: index === activeIndex }" @click="openResult(result.path)" @mouseenter="activeIndex = index">
          <el-icon><component :is="result.icon" /></el-icon>
          <span><strong>{{ result.label }}</strong><small>{{ result.description }}</small></span>
          <span class="group-label">{{ result.group }}</span>
        </button>
        <div v-if="!filteredResults.length" class="search-empty">没有找到相关页面，换个词试试</div>
      </div>
    </el-dialog>
  </div>
</template>

<script setup lang="ts">
import { useAdminEvents, reconnecting } from '@/composables/useAdminEvents'
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { Expand, Fold, Menu, Monitor, Search, Setting, SwitchButton } from '@element-plus/icons-vue'
import BrandLogo from '@/components/BrandLogo.vue'
import { useAuthStore } from '@/stores/auth'
import { navGroups, navItems } from '@/router/nav'
import { connection } from '@/utils/connection'
import { useSpotlight } from '@/utils/spotlight'
import { useShellStore } from '@/stores/shell'

useSpotlight()

const router = useRouter()
const route = useRoute()
const auth = useAuthStore()
const shell = useShellStore()
const mobileOpen = ref(false)
const collapsed = ref(localStorage.getItem('sidebar-collapsed') === '1')
const searchVisible = ref(false)
const search = ref('')
const activeIndex = ref(0)
const searchInput = ref<{ focus: () => void } | null>(null)
const initial = computed(() => (auth.username || 'A').slice(0, 1).toUpperCase())
const shortcutLabel = /Mac|iPhone|iPad/.test(navigator.platform) ? '⌘K' : 'Ctrl K'
const connectionTip = computed(() => connection.usingMock
  ? '后端服务未连接，页面显示的是示例数据，保存操作不会生效。'
  : connection.lost ? '暂时连不上后端服务，数据可能不是最新的。' : '实时连接已断开，正在重新连接。')

watch(() => connection.lost, (lost) => { if (lost) ElMessage.warning({ message: connectionTip.value, duration: 5000, grouping: true }) })

const filteredResults = computed(() => {
  const keyword = search.value.trim().toLowerCase()
  if (!keyword) return navItems
  return navItems.filter(item => `${item.label}${item.description}${item.group}${item.keywords || ''}`.toLowerCase().includes(keyword))
})
watch(search, () => { activeIndex.value = 0 })

const extraMatches: Record<string, string[]> = {
  '/bots': ['/bots'],
  '/conversations': ['/conversations'],
  '/operations': ['/operations', '/schedules', '/blacklist'],
  '/studio': ['/studio', '/shared-settings', '/evaluation'],
}
function isActive(path: string) {
  return (extraMatches[path] || [path]).some(prefix => route.path === prefix || route.path.startsWith(prefix + '/'))
}
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
  if (command === 'monitor') router.push({ path: '/system', query: { tab: 'monitor' } })
  if (command === 'settings') router.push({ path: '/system', query: { tab: 'settings' } })
  if (command === 'logout') { await auth.logout(); router.push('/login') }
}

watch(() => route.path, () => {
  const item = navItems.find(entry => isActive(entry.path))
  document.title = item ? `${item.label} · AI Bot Hub` : 'AI Bot Hub'
  shell.refreshCounts()
}, { immediate: true })

onMounted(() => { window.addEventListener('keydown', onGlobalKey) })
onBeforeUnmount(() => { window.removeEventListener('keydown', onGlobalKey) })
useAdminEvents(['handoffs', 'knowledge_candidates'], () => shell.refreshCounts(true))
</script>

<style scoped>
.live-pulse { width: 8px; height: 8px; flex: none; border-radius: 50%; background: var(--success); box-shadow: 0 0 10px var(--success); animation: pulse-dot 2.4s ease-in-out infinite; }
@keyframes pulse-dot { 0%, 100% { opacity: 1; transform: scale(1); } 50% { opacity: 0.45; transform: scale(0.82); } }
</style>
