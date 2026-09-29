<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import axios from 'axios'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import PageHeader from '@/components/PageHeader.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import { changeApi, type ChangeAction, type ResourceChange, type ResourceType } from '@/api'
import { useAppStore } from '@/stores/app'
import { actionInfo, changeText } from '@/utils/change'
import { dayjs } from '@/utils/format'
import { shortAccountName } from '@/utils/resource'
import { typeLabel } from '@/utils/status'

type Range = 'today' | '7d' | '30d' | 'all'
type ActionFilter = 'all' | ChangeAction
const PAGE_SIZE = 20

const route = useRoute()
const router = useRouter()
const app = useAppStore()

const ranges: { value: Range; label: string }[] = [
  { value: 'today', label: '今天' },
  { value: '7d', label: '近 7 天' },
  { value: '30d', label: '近 30 天' },
  { value: 'all', label: '全部' },
]
const actions: { value: ActionFilter; label: string }[] = [
  { value: 'all', label: '全部动作' },
  { value: 'created', label: '新增' },
  { value: 'updated', label: '变更' },
  { value: 'deleted', label: '删除' },
]
const types = Object.keys(typeLabel) as ResourceType[]

function q(key: string) {
  const v = route.query[key]
  return typeof v === 'string' ? v : ''
}

const range = ref<Range>(ranges.find((r) => r.value === q('range'))?.value ?? '7d')
const action = ref<ActionFilter>(actions.find((a) => a.value === q('action'))?.value ?? 'all')
const type = ref<ResourceType | ''>(types.find((t) => t === q('type')) ?? '')
const keyword = ref(q('q'))
const page = ref(Number(q('page')) > 0 ? Number(q('page')) : 1)
const accountId = Number(q('account_id')) || undefined

const items = ref<ResourceChange[]>([])
const total = ref(0)
const loading = ref(false)
const loaded = ref(false)
let controller: AbortController | null = null

async function load() {
  controller?.abort()
  const ctl = new AbortController()
  controller = ctl
  loading.value = true
  try {
    const res = await changeApi.list(
      {
        range: range.value,
        action: action.value === 'all' ? undefined : action.value,
        type: type.value || undefined,
        account_id: accountId,
        q: keyword.value.trim() || undefined,
        page: page.value,
        page_size: PAGE_SIZE,
      },
      ctl.signal,
    )
    items.value = res.items
    total.value = res.total
    loaded.value = true
  } catch (err) {
    if (!axios.isCancel(err)) loaded.value = true
  } finally {
    if (controller === ctl) loading.value = false
  }
}

function syncUrl() {
  void router.replace({
    query: {
      range: range.value === '7d' ? undefined : range.value,
      action: action.value === 'all' ? undefined : action.value,
      type: type.value || undefined,
      account_id: accountId ? String(accountId) : undefined,
      q: keyword.value.trim() || undefined,
      page: page.value > 1 ? String(page.value) : undefined,
    },
  })
}

watch([range, action, type], () => {
  page.value = 1
  syncUrl()
  void load()
})
watch(page, () => {
  syncUrl()
  void load()
})
watch(
  () => app.syncTick,
  () => void load(),
)

let timer: ReturnType<typeof setTimeout> | undefined
watch(keyword, () => {
  clearTimeout(timer)
  timer = setTimeout(() => {
    page.value = 1
    syncUrl()
    void load()
  }, 400)
})

void load()
onBeforeUnmount(() => controller?.abort())

function time(t: string): string {
  const d = dayjs(t)
  const today = dayjs(app.now).startOf('day')
  if (d.isAfter(today)) return `今天 ${d.format('HH:mm:ss')}`
  if (d.isAfter(today.subtract(1, 'day'))) return `昨天 ${d.format('HH:mm:ss')}`
  return d.format('MM-DD HH:mm:ss')
}

// 资源中心只有四类核心资源，闲置云盘和 EIP 在成本优化页。
function resourceLink(c: ResourceChange) {
  if (c.action === 'deleted') return null
  if (c.type === 'disk' || c.type === 'eip') return { path: '/optimize', query: { kind: c.type } }
  return { path: '/resources', query: { type: c.type, q: c.resource_id } }
}

const emptyText = computed(() =>
  keyword.value.trim() || action.value !== 'all' || type.value ? '没有符合条件的变更' : '这个时间范围内资源没有变化',
)
</script>

<template>
  <PageHeader title="变更记录" subtitle="每次同步与上一次对比，记录资源的新增、删除，以及名称、状态、规格、IP、计费、到期时间和标签的变化" />

  <section class="ys-card panel">
    <div class="filters">
      <SegmentedControl v-model="range" :options="ranges" label="时间范围" />
      <div class="cats" role="radiogroup" aria-label="变更动作">
        <button
          v-for="a in actions"
          :key="a.value"
          type="button"
          role="radio"
          class="cat"
          :class="{ on: action === a.value }"
          :aria-checked="action === a.value"
          @click="action = a.value"
        >
          {{ a.label }}
        </button>
      </div>
      <el-select v-model="type" class="type-select" placeholder="全部">
        <template #prefix>类型：</template>
        <el-option label="全部" value="" />
        <el-option v-for="t in types" :key="t" :label="typeLabel[t]" :value="t" />
      </el-select>
      <label class="search">
        <AppIcon name="search" :size="15" />
        <input v-model="keyword" type="search" aria-label="搜索变更记录" placeholder="资源名称或 ID" />
      </label>
    </div>

    <el-table v-loading="loading && !loaded" :data="items" row-key="id" class="change-table" :class="{ dim: loading && loaded }">
      <el-table-column label="时间" width="150">
        <template #default="{ row }"><span class="time">{{ time(row.created_at) }}</span></template>
      </el-table-column>
      <el-table-column label="动作" width="80">
        <template #default="{ row }">
          <span class="act" :class="actionInfo[row.action as ChangeAction].tone">{{ actionInfo[row.action as ChangeAction].label }}</span>
        </template>
      </el-table-column>
      <el-table-column label="资源" min-width="240">
        <template #default="{ row }">
          <div class="cell-stack">
            <span class="res-line">
              <RouterLink v-if="resourceLink(row)" :to="resourceLink(row)!" class="res-name ellipsis">{{ row.name || row.resource_id }}</RouterLink>
              <span v-else class="res-name gone ellipsis">{{ row.name || row.resource_id }}</span>
              <span class="type">{{ typeLabel[row.type as ResourceType] ?? row.type }}</span>
            </span>
            <span v-if="row.name && row.name !== row.resource_id" class="sub-mono ellipsis">{{ row.resource_id }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="账号 · 地域" width="170">
        <template #default="{ row }">
          <div class="cell-stack start">
            <span class="acc-line">
              <CloudTag :provider="row.provider" />
              <span class="sub ellipsis">{{ shortAccountName(row.account_name, row.provider) }}</span>
            </span>
            <span class="sub-muted">{{ row.region }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="变化明细" min-width="320">
        <template #default="{ row }">
          <ul v-if="row.changes.length" class="diff">
            <li v-for="c in row.changes" :key="c.field">{{ changeText(c) }}</li>
          </ul>
          <span v-else class="sub-muted">{{ row.action === 'created' ? '同步时首次发现' : row.action === 'deleted' ? '同步时已不存在' : '—' }}</span>
        </template>
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
      <span class="keep">记录保留 {{ app.meta.change_retention_days }} 天 · 账号首次同步不记录新增</span>
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

.type-select {
  width: 170px;
}

.type-select :deep(.el-select__prefix) {
  color: var(--ys-text-label);
  font-size: 13px;
}

.search {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 8px;
  width: 260px;
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

.change-table {
  transition: opacity 0.2s;
}

.change-table.dim {
  opacity: 0.55;
}

.change-table :deep(.el-table__cell) {
  height: 56px;
  vertical-align: top;
  padding-top: 10px;
  padding-bottom: 10px;
}

.change-table :deep(tr > :first-child .cell) {
  padding-left: 20px;
}

.time {
  font-size: 12px;
  color: var(--ys-text-label);
}

.act {
  display: inline-block;
  padding: 2px 8px;
  border-radius: var(--ys-radius-tag);
  font-size: 12px;
  white-space: nowrap;
}

.act.ok {
  background: var(--ys-ok-bg);
  color: var(--ys-ok-fg);
}

.act.busy {
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
}

.act.err {
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
}

.cell-stack {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.cell-stack.start {
  align-items: flex-start;
}

.res-line,
.acc-line {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  max-width: 100%;
}

.res-name {
  font-weight: 500;
  min-width: 0;
}

.res-name.gone {
  color: var(--ys-text-secondary);
  text-decoration: line-through;
}

.type {
  flex-shrink: 0;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 11px;
  line-height: 18px;
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
}

.sub-mono {
  font-family: var(--ys-font-mono);
  font-size: 11px;
  color: var(--ys-text-muted);
}

.sub {
  font-size: 12px;
  color: var(--ys-text-secondary);
}

.sub-muted {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.diff {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 2px;
  font-size: 12px;
  color: var(--ys-text);
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
