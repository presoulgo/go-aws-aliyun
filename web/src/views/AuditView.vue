<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import PageHeader from '@/components/PageHeader.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import { auditApi, type AuditLog } from '@/api'
import { useAppStore } from '@/stores/app'
import { dayjs } from '@/utils/format'

type Range = 'today' | '7d' | '30d'
type Category = 'all' | 'login' | 'account' | 'sync' | 'user'
const PAGE_SIZE = 20

const route = useRoute()
const router = useRouter()
const app = useAppStore()

const ranges: { value: Range; label: string }[] = [
  { value: 'today', label: '今天' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
]
const categories: { value: Category; label: string }[] = [
  { value: 'all', label: '全部操作' },
  { value: 'login', label: '登录' },
  { value: 'account', label: '云账号' },
  { value: 'sync', label: '同步' },
  { value: 'user', label: '用户' },
]

function q(key: string) {
  const v = route.query[key]
  return typeof v === 'string' ? v : ''
}

const range = ref<Range>(ranges.find((r) => r.value === q('range'))?.value ?? '7d')
const category = ref<Category>(categories.find((c) => c.value === q('category'))?.value ?? 'all')
const keyword = ref(q('q'))
const page = ref(Number(q('page')) > 0 ? Number(q('page')) : 1)

const logs = ref<AuditLog[]>([])
const total = ref(0)
const loading = ref(false)
const loaded = ref(false)

async function load() {
  loading.value = true
  try {
    const res = await auditApi.list({
      range: range.value,
      category: category.value === 'all' ? undefined : category.value,
      q: keyword.value.trim() || undefined,
      page: page.value,
      page_size: PAGE_SIZE,
    })
    logs.value = res.items
    total.value = res.total
    loaded.value = true
  } finally {
    loading.value = false
  }
}

function syncUrl() {
  void router.replace({
    query: {
      range: range.value === '7d' ? undefined : range.value,
      category: category.value === 'all' ? undefined : category.value,
      q: keyword.value.trim() || undefined,
      page: page.value > 1 ? String(page.value) : undefined,
    },
  })
}

watch([range, category], () => {
  page.value = 1
  syncUrl()
  void load()
})
watch(page, () => {
  syncUrl()
  void load()
})

let timer: ReturnType<typeof setTimeout> | undefined
watch(keyword, () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    page.value = 1
    syncUrl()
    void load()
  }, 400)
})

onMounted(load)

const actionLabel: Record<string, string> = {
  login_success: '登录成功',
  login_failed: '登录失败',
  account_create: '新增云账号',
  account_update: '修改云账号',
  account_delete: '删除云账号',
  account_enable: '启用云账号',
  account_disable: '停用云账号',
  sync_manual: '手动同步',
  sync_all: '全部同步',
  sync_cancel: '取消同步',
  user_create: '新增用户',
  user_update: '修改用户',
  user_delete: '删除用户',
  user_enable: '启用用户',
  user_disable: '禁用用户',
  user_reset_password: '重置密码',
  password_change: '修改密码',
}

function tone(l: AuditLog): string {
  if (l.result === 'failed') return 'warn'
  if (l.action.endsWith('_delete')) return 'danger'
  return l.category
}

function time(t: string): string {
  const d = dayjs(t)
  const today = dayjs(app.now).startOf('day')
  if (d.isAfter(today)) return `今天 ${d.format('HH:mm:ss')}`
  if (d.isAfter(today.subtract(1, 'day'))) return `昨天 ${d.format('HH:mm:ss')}`
  return d.format('MM-DD HH:mm:ss')
}

const emptyText = computed(() => (keyword.value.trim() ? '没有匹配的记录' : '这个时间范围内没有此类操作'))
</script>

<template>
  <PageHeader title="审计日志" subtitle="记录登录、云账号变更、手动同步和用户管理操作 · 只增不改" />

  <section class="ys-card panel">
    <div class="filters">
      <SegmentedControl v-model="range" :options="ranges" label="时间范围" />
      <div class="cats" role="radiogroup" aria-label="操作分类">
        <button
          v-for="c in categories"
          :key="c.value"
          type="button"
          role="radio"
          class="cat"
          :class="{ on: category === c.value }"
          :aria-checked="category === c.value"
          @click="category = c.value"
        >
          {{ c.label }}
        </button>
      </div>
      <label class="search">
        <AppIcon name="search" :size="15" />
        <input v-model="keyword" type="search" aria-label="搜索审计日志" placeholder="搜索操作人、对象或 IP" />
      </label>
    </div>

    <el-table v-loading="loading && !loaded" :data="logs" row-key="id" class="audit-table" :class="{ dim: loading && loaded }">
      <el-table-column label="时间" width="160">
        <template #default="{ row }"><span class="time">{{ time(row.created_at) }}</span></template>
      </el-table-column>
      <el-table-column label="操作人" width="140">
        <template #default="{ row }"><span class="mono small">{{ row.username || '—' }}</span></template>
      </el-table-column>
      <el-table-column label="操作" width="130">
        <template #default="{ row }">
          <span class="act" :class="tone(row)">{{ actionLabel[row.action] ?? row.action }}</span>
        </template>
      </el-table-column>
      <el-table-column label="对象" width="200" show-overflow-tooltip>
        <template #default="{ row }">{{ row.target || '—' }}</template>
      </el-table-column>
      <el-table-column label="详情" min-width="240" show-overflow-tooltip>
        <template #default="{ row }"><span class="detail">{{ row.detail || '—' }}</span></template>
      </el-table-column>
      <el-table-column label="来源 IP" width="130">
        <template #default="{ row }"><span class="mono small detail">{{ row.ip || '—' }}</span></template>
      </el-table-column>
      <template #empty>
        <span v-if="loaded" class="empty">{{ emptyText }}</span>
      </template>
    </el-table>

    <div class="foot">
      <span>共 {{ total.toLocaleString('zh-CN') }} 条</span>
      <el-pagination
        v-if="total > PAGE_SIZE"
        v-model:current-page="page"
        background
        layout="prev, pager, next"
        :total="total"
        :page-size="PAGE_SIZE"
      />
      <span class="keep">日志保留 {{ app.meta.audit_retention_days }} 天</span>
    </div>
  </section>
</template>

<style scoped>
.panel {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.filters {
  min-height: 60px;
  padding: 12px 20px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid var(--ys-divider);
}

.cats {
  display: flex;
  gap: 6px;
}

.cat {
  height: 32px;
  padding: 0 12px;
  border: 1px solid #dde1e7;
  border-radius: 999px;
  background: #fff;
  color: var(--ys-text-label);
  font-size: 13px;
  cursor: pointer;
}

.cat:hover {
  border-color: #c9d1ee;
}

.cat.on {
  border-color: var(--ys-primary);
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
}

.search {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  width: 280px;
  height: 36px;
  padding: 0 12px;
  border: 1px solid var(--ys-border-input);
  border-radius: 8px;
  color: var(--ys-text-muted);
}

.search:focus-within {
  border-color: var(--ys-primary);
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

.audit-table {
  transition: opacity 0.2s;
}

.audit-table.dim {
  opacity: 0.55;
}

.audit-table :deep(.el-table__cell) {
  height: 46px;
}

.audit-table :deep(tr > :first-child .cell) {
  padding-left: 20px;
}

.time {
  font-size: 12px;
  color: var(--ys-text-label);
}

.small {
  font-size: 12px;
}

.detail {
  color: var(--ys-text-secondary);
}

.act {
  display: inline-block;
  padding: 2px 8px;
  border-radius: var(--ys-radius-tag);
  font-size: 12px;
  white-space: nowrap;
}

.act.login {
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
}
.act.account {
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
}
.act.sync {
  background: var(--ys-ok-bg);
  color: var(--ys-ok-fg);
}
.act.user {
  background: #f3eafb;
  color: #6b2fa0;
}
.act.warn {
  background: var(--ys-warn-bg);
  color: var(--ys-warn-fg);
}
.act.danger {
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
}

.empty {
  font-size: 13px;
  color: var(--ys-text-muted);
}

.foot {
  min-height: 52px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  gap: 16px;
  border-top: 1px solid var(--ys-divider);
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.foot .el-pagination {
  margin-left: auto;
}

.keep {
  margin-left: auto;
  font-size: 12px;
  color: var(--ys-text-muted);
}

.foot .el-pagination + .keep {
  margin-left: 8px;
}
</style>
