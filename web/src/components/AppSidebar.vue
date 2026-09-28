<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppIcon from './AppIcon.vue'
import type { IconName } from './icons'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

interface NavItem {
  to: string
  label: string
  icon: IconName
}

const main: NavItem[] = [
  { to: '/dashboard', label: '概览', icon: 'dashboard' },
  { to: '/resources', label: '资源中心', icon: 'resources' },
  { to: '/monitor', label: '监控中心', icon: 'monitor' },
  { to: '/accounts', label: '云账号', icon: 'key' },
]
const system: NavItem[] = [
  { to: '/system/users', label: '用户管理', icon: 'users' },
  { to: '/system/audit', label: '审计日志', icon: 'audit' },
]

const route = useRoute()
const app = useAppStore()
const auth = useAuthStore()

const version = computed(() => {
  const v = app.meta.version
  return /^\d/.test(v) ? `v${v}` : v
})

function active(item: NavItem) {
  return route.path === item.to || route.path.startsWith(item.to + '/')
}
</script>

<template>
  <aside class="sidebar">
    <RouterLink to="/dashboard" class="brand">
      <span class="brand-mark"><AppIcon name="logo" :size="20" /></span>
      <span class="brand-text">
        <strong>{{ app.meta.name }}</strong>
        <span>多云运维聚合平台</span>
      </span>
    </RouterLink>
    <nav aria-label="主导航" class="nav">
      <RouterLink
        v-for="item in main"
        :key="item.to"
        :to="item.to"
        class="nav-item"
        :class="{ active: active(item) }"
        :aria-current="active(item) ? 'page' : undefined"
      >
        <AppIcon :name="item.icon" />
        {{ item.label }}
      </RouterLink>
      <template v-if="auth.isAdmin">
        <div class="nav-group">系统管理</div>
        <RouterLink
          v-for="item in system"
          :key="item.to"
          :to="item.to"
          class="nav-item"
          :class="{ active: active(item) }"
          :aria-current="active(item) ? 'page' : undefined"
        >
          <AppIcon :name="item.icon" />
          {{ item.label }}
        </RouterLink>
      </template>
    </nav>
    <div class="foot">
      <span class="mono">{{ version }}</span>
      <span>只读接入</span>
    </div>
  </aside>
</template>

<style scoped>
.sidebar {
  position: sticky;
  top: 0;
  width: 232px;
  height: 100vh;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--ys-surface);
  border-right: 1px solid var(--ys-border);
}

.brand {
  height: 64px;
  flex-shrink: 0;
  padding: 0 20px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--ys-divider);
  color: var(--ys-text);
}

.brand:hover {
  color: var(--ys-text);
}

.brand-mark {
  width: 32px;
  height: 32px;
  border-radius: 9px;
  background: var(--ys-primary);
  color: #fff;
  display: flex;
  align-items: center;
  justify-content: center;
}

.brand-text {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}

.brand-text strong {
  font-size: 16px;
  font-weight: 600;
}

.brand-text span {
  font-size: 11px;
  color: var(--ys-text-muted);
}

.nav {
  padding: 16px 12px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  overflow-y: auto;
}

.nav-item {
  height: 40px;
  padding: 0 12px;
  display: flex;
  align-items: center;
  gap: 12px;
  border-radius: 8px;
  font-size: 14px;
  color: var(--ys-text-label);
  transition: background-color 0.15s;
}

.nav-item:hover {
  background: var(--ys-bg-subtle);
  color: var(--ys-text);
}

.nav-item.active {
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
  font-weight: 600;
}

.nav-group {
  margin: 16px 12px 6px;
  font-size: 12px;
  color: var(--ys-text-muted);
}

.foot {
  margin-top: auto;
  padding: 16px 20px;
  display: flex;
  justify-content: space-between;
  border-top: 1px solid var(--ys-divider);
  font-size: 12px;
  color: var(--ys-text-muted);
}
</style>
