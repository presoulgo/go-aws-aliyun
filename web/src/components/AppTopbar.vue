<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from './AppIcon.vue'
import ChangePasswordDialog from './ChangePasswordDialog.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { formatDateTime, formatShortTime, fromNow } from '@/utils/format'
import { roleLabel, type Tone } from '@/utils/status'

const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()

const keyword = ref('')
const searchInput = ref<HTMLInputElement>()
const menuOpen = ref(false)
const menuRoot = ref<HTMLElement>()
const pwdOpen = ref(false)

const user = computed(() => auth.user)
const initial = computed(() => (user.value?.username || '?').slice(0, 1).toUpperCase())

const pill = computed<{ tone: Tone; text: string; title: string }>(() => {
  const s = app.syncStatus
  if (!s) return { tone: 'off', text: '同步状态加载中', title: '' }
  const last = s.last_finished_at ? `最近完成：${formatDateTime(s.last_finished_at)}` : ''
  if (s.running > 0) return { tone: 'busy', text: `正在同步 · ${s.running} 个账号`, title: last }
  if (s.accounts === 0) return { tone: 'off', text: '尚未接入云账号', title: '' }
  switch (s.state) {
    case 'ok':
      return { tone: 'ok', text: `同步正常 · ${fromNow(s.last_finished_at, app.now)}`, title: last }
    case 'partial':
      return { tone: 'warn', text: `部分同步失败 · ${s.problem_accounts} 个账号`, title: last }
    case 'failed':
      return { tone: 'err', text: '同步失败 · 请检查凭证', title: last }
    default:
      return { tone: 'off', text: '等待首次同步', title: '' }
  }
})

function search() {
  const q = keyword.value.trim()
  if (!q) return
  void router.push({ path: '/resources', query: { q } })
  keyword.value = ''
  searchInput.value?.blur()
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === '/' && !e.ctrlKey && !e.metaKey && !e.altKey) {
    const el = document.activeElement as HTMLElement | null
    const typing = el && (el.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(el.tagName))
    if (!typing) {
      e.preventDefault()
      searchInput.value?.focus()
    }
  } else if (e.key === 'Escape' && menuOpen.value) {
    menuOpen.value = false
  }
}

function onDocClick(e: MouseEvent) {
  if (menuOpen.value && menuRoot.value && !menuRoot.value.contains(e.target as Node)) menuOpen.value = false
}

function openPassword() {
  menuOpen.value = false
  pwdOpen.value = true
}

function logout() {
  menuOpen.value = false
  auth.reset()
  app.stopPolling()
  void router.replace({ name: 'login' })
}

onMounted(() => {
  window.addEventListener('keydown', onKeydown)
  document.addEventListener('click', onDocClick)
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKeydown)
  document.removeEventListener('click', onDocClick)
})
</script>

<template>
  <header class="topbar">
    <label class="search">
      <AppIcon name="search" :size="16" />
      <input
        ref="searchInput"
        v-model="keyword"
        type="search"
        aria-label="全局搜索资源"
        placeholder="搜索实例 ID、IP、名称或标签"
        @keydown.enter="search"
        @keydown.esc="searchInput?.blur()"
      />
      <kbd>/</kbd>
    </label>
    <el-tooltip
      v-if="app.meta.demo"
      content="演示模式：云账号、资源和监控数据均为模拟生成，不会调用云厂商 API"
      placement="bottom"
    >
      <span class="demo-badge" tabindex="0"><AppIcon name="info" :size="14" />演示模式</span>
    </el-tooltip>
    <RouterLink to="/accounts" class="sync-pill" :class="pill.tone" :title="pill.title">
      <span class="dot" />
      {{ pill.text }}
    </RouterLink>
    <div class="divider" />
    <div ref="menuRoot" class="user">
      <button
        type="button"
        class="user-btn"
        :class="{ open: menuOpen }"
        aria-label="用户菜单"
        aria-haspopup="menu"
        :aria-expanded="menuOpen"
        @click="menuOpen = !menuOpen"
      >
        <span class="avatar">{{ initial }}</span>
        <span class="user-text">
          <span class="user-name">{{ user?.username }}</span>
          <span class="user-role">{{ roleLabel[user?.role ?? ''] ?? user?.role }}</span>
        </span>
        <AppIcon name="chevronDown" :size="14" class="caret" />
      </button>
      <Transition name="menu">
        <div v-if="menuOpen" class="menu" role="menu">
          <div class="menu-head">
            <span class="menu-name">
              {{ user?.username }}
              <span v-if="user?.display_name && user.display_name !== user.username" class="menu-display">
                {{ user.display_name }}
              </span>
            </span>
            <span class="menu-meta">
              角色：{{ roleLabel[user?.role ?? ''] }}<template v-if="user?.last_login_at">
                · 登录于 {{ formatShortTime(user.last_login_at, app.now) }}</template
              >
            </span>
          </div>
          <button type="button" role="menuitem" class="menu-item" @click="openPassword">
            <AppIcon name="lock" :size="16" />修改密码
          </button>
          <button type="button" role="menuitem" class="menu-item danger" @click="logout">
            <AppIcon name="logout" :size="16" />退出登录
          </button>
        </div>
      </Transition>
    </div>
    <ChangePasswordDialog v-model="pwdOpen" />
  </header>
</template>

<style scoped>
.topbar {
  position: sticky;
  top: 0;
  z-index: 20;
  height: 64px;
  flex-shrink: 0;
  padding: 0 28px;
  display: flex;
  align-items: center;
  gap: 20px;
  background: var(--ys-surface);
  border-bottom: 1px solid var(--ys-border);
}

.search {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 380px;
  min-width: 200px;
  height: 38px;
  padding: 0 12px;
  border: 1px solid #dde1e7;
  border-radius: 8px;
  background: var(--ys-bg-subtle);
  color: var(--ys-text-muted);
  cursor: text;
  transition:
    border-color 0.15s,
    background-color 0.15s;
}

.search:focus-within {
  border-color: var(--ys-primary);
  background: #fff;
}

.search input {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: none;
  background: transparent;
  color: var(--ys-text);
  font-family: inherit;
  font-size: 13px;
}

.search input::placeholder {
  color: var(--ys-text-muted);
}

.search input::-webkit-search-cancel-button {
  display: none;
}

kbd {
  font-family: var(--ys-font-mono);
  font-size: 11px;
  padding: 1px 6px;
  border: 1px solid #dde1e7;
  border-radius: 4px;
  background: #fff;
  color: var(--ys-text-muted);
}

.demo-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 4px 10px;
  border: 1px solid var(--ys-border);
  border-radius: 999px;
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
  font-size: 12px;
  white-space: nowrap;
  cursor: default;
}

.sync-pill {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 999px;
  font-size: 12px;
  white-space: nowrap;
}

.sync-pill .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
}

.sync-pill.ok {
  background: var(--ys-ok-bg);
  color: var(--ys-ok-fg);
}
.sync-pill.ok .dot {
  background: var(--ys-ok-dot);
}
.sync-pill.warn {
  background: var(--ys-warn-bg);
  color: var(--ys-warn-fg);
}
.sync-pill.warn .dot {
  background: var(--ys-warn-dot);
}
.sync-pill.err {
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
}
.sync-pill.err .dot {
  background: var(--ys-err-dot);
}
.sync-pill.busy {
  background: var(--ys-busy-bg);
  color: var(--ys-busy-fg);
}
.sync-pill.busy .dot {
  background: var(--ys-busy-dot);
  animation: pulse 1.2s ease-in-out infinite;
}
.sync-pill.off {
  background: var(--ys-off-bg);
  color: var(--ys-off-fg);
}
.sync-pill.off .dot {
  background: var(--ys-off-dot);
}

@keyframes pulse {
  50% {
    opacity: 0.35;
  }
}

.divider {
  width: 1px;
  height: 24px;
  background: var(--ys-border);
}

.user {
  position: relative;
}

.user-btn {
  display: flex;
  align-items: center;
  gap: 10px;
  height: 44px;
  padding: 0 8px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--ys-text);
  cursor: pointer;
}

.user-btn:hover,
.user-btn.open {
  background: var(--ys-bg-muted);
}

.avatar {
  width: 32px;
  height: 32px;
  border-radius: 50%;
  background: #dde4f8;
  color: var(--ys-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 600;
}

.user-text {
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  line-height: 1.25;
}

.user-name {
  font-size: 13px;
  font-weight: 500;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.user-role {
  font-size: 11px;
  color: var(--ys-text-muted);
}

.caret {
  color: var(--ys-text-muted);
}

.menu {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  z-index: 30;
  width: 216px;
  padding: 6px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  border: 1px solid var(--ys-border);
  border-radius: 10px;
  background: #fff;
  box-shadow: var(--ys-shadow-pop);
}

.menu-head {
  padding: 8px 10px 10px;
  margin-bottom: 4px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  border-bottom: 1px solid var(--ys-divider);
}

.menu-name {
  font-size: 13px;
  font-weight: 600;
}

.menu-display {
  margin-left: 4px;
  font-weight: 400;
  color: var(--ys-text-secondary);
}

.menu-meta {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.menu-item {
  height: 36px;
  padding: 0 10px;
  display: flex;
  align-items: center;
  gap: 10px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ys-text);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.menu-item:hover,
.menu-item:focus-visible {
  background: var(--ys-bg-muted);
}

.menu-item.danger {
  color: var(--ys-err-fg);
}

.menu-enter-active,
.menu-leave-active {
  transition:
    opacity 0.12s,
    transform 0.12s;
}

.menu-enter-from,
.menu-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

@media (max-width: 1100px) {
  .search {
    width: 260px;
  }
  .user-text {
    display: none;
  }
}
</style>
