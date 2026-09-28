<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import axios from 'axios'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import StatusTag from '@/components/StatusTag.vue'
import ResourceDrawer from './ResourceDrawer.vue'
import {
  resourceApi,
  type ProviderFilter,
  type Resource,
  type ResourceFilters,
  type ResourceQuery,
  type ResourceType,
} from '@/api'
import { useAppStore } from '@/stores/app'
import { formatDate, formatPercent, fromNow } from '@/utils/format'
import {
  bucketClass,
  bucketObjects,
  bucketSize,
  cpuTone,
  engineLabel,
  expiryInfo,
  extraStr,
  ips,
  lbKindLabel,
  networkLabel,
  rdsStorage,
  shortAccountName,
  vmSize,
  zoneLabel,
} from '@/utils/resource'
import { resourceStatusInfo, typeLabel } from '@/utils/status'

const PAGE_SIZE = 20
const types: ResourceType[] = ['vm', 'rds', 'lb', 'bucket']

const route = useRoute()
const router = useRouter()
const app = useAppStore()

interface State {
  type: ResourceType
  provider: ProviderFilter
  account_id?: number
  region?: string
  status?: string
  q?: string
  idle: boolean
  expiring: boolean
  sort?: string
  page: number
}

function str(v: unknown): string | undefined {
  return typeof v === 'string' && v !== '' ? v : undefined
}

const state = computed<State>(() => {
  const q = route.query
  const type = str(q.type) as ResourceType | undefined
  const provider = str(q.provider)
  const page = Number(str(q.page) ?? 1)
  const account = Number(str(q.account_id) ?? 0)
  return {
    type: type && types.includes(type) ? type : 'vm',
    provider: provider === 'aws' || provider === 'aliyun' ? provider : 'all',
    account_id: account > 0 ? account : undefined,
    region: str(q.region),
    status: str(q.status),
    q: str(q.q),
    idle: q.idle === '1',
    expiring: q.expiring === '1',
    sort: str(q.sort),
    page: Number.isInteger(page) && page > 0 ? page : 1,
  }
})

const detailId = computed(() => {
  const id = Number(str(route.query.detail) ?? 0)
  return id > 0 ? id : null
})

function update(patch: Partial<State>, keepPage = false) {
  const next: State = { ...state.value, ...patch }
  if (!keepPage && patch.page === undefined) next.page = 1
  const query: LocationQueryRaw = {
    type: next.type === 'vm' && !route.query.type && !patch.type ? undefined : next.type,
    provider: next.provider === 'all' ? undefined : next.provider,
    account_id: next.account_id ? String(next.account_id) : undefined,
    region: next.region,
    status: next.status,
    q: next.q,
    idle: next.idle ? '1' : undefined,
    expiring: next.expiring ? '1' : undefined,
    sort: next.sort,
    page: next.page > 1 ? String(next.page) : undefined,
    detail: route.query.detail,
  }
  void router.replace({ query })
}

function openDetail(r: Resource) {
  void router.push({ query: { ...route.query, detail: String(r.id) } })
}

function closeDetail() {
  const { detail: _omit, ...rest } = route.query
  void router.replace({ query: rest })
}

// ---------- 数据 ----------
const items = ref<Resource[]>([])
const total = ref(0)
const filters = ref<ResourceFilters | null>(null)
const loading = ref(false)
const loaded = ref(false)
let controller: AbortController | null = null

// 云主机默认按近 1 小时 CPU 从高到低排，最忙的实例排在最前。
const effectiveSort = computed(() => state.value.sort ?? (state.value.type === 'vm' ? 'cpu_1h:desc' : undefined))

const listQuery = computed<ResourceQuery>(() => {
  const s = state.value
  return {
    type: s.type,
    provider: s.provider,
    account_id: s.account_id,
    region: s.region,
    status: s.status,
    q: s.q,
    idle: s.idle,
    expiring: s.expiring,
    sort: effectiveSort.value,
    page: s.page,
    page_size: PAGE_SIZE,
  }
})

async function load() {
  controller?.abort()
  const ctl = new AbortController()
  controller = ctl
  loading.value = true
  try {
    const [list, f] = await Promise.all([
      resourceApi.list(listQuery.value, ctl.signal),
      resourceApi.filters(listQuery.value, ctl.signal),
    ])
    items.value = list.items
    total.value = list.total
    filters.value = f
    loaded.value = true
    // 没指定类型且当前类型为空时，跳到第一个有数据的类型（例如从“到期”卡片进来）。
    if (!route.query.type && f.counts[state.value.type] === 0) {
      const first = types.find((t) => (f.counts[t] ?? 0) > 0)
      if (first) update({ type: first }, true)
    }
  } catch (err) {
    if (!axios.isCancel(err)) loaded.value = true
  } finally {
    if (controller === ctl) loading.value = false
  }
}

watch(
  listQuery,
  (a, b) => {
    if (JSON.stringify(a) !== JSON.stringify(b)) void load()
  },
  { immediate: true },
)
watch(
  () => app.syncTick,
  () => void load(),
)
onBeforeUnmount(() => controller?.abort())

// ---------- 搜索框（输入停顿后生效，回车立即生效） ----------
const keyword = ref(state.value.q ?? '')
let timer: ReturnType<typeof setTimeout> | undefined
watch(
  () => state.value.q,
  (q) => {
    if ((q ?? '') !== keyword.value.trim()) keyword.value = q ?? ''
  },
)
function onKeyword() {
  clearTimeout(timer)
  timer = setTimeout(applyKeyword, 400)
}
function applyKeyword() {
  clearTimeout(timer)
  const q = keyword.value.trim()
  if (q !== (state.value.q ?? '')) update({ q: q || undefined })
}

function reset() {
  keyword.value = ''
  void router.replace({ query: { type: route.query.type, detail: route.query.detail } })
}

const hasFilters = computed(() => {
  const s = state.value
  return !!(s.provider !== 'all' || s.account_id || s.region || s.status || s.q || s.idle || s.expiring)
})

// ---------- 选项 ----------
const cloudOptions: { value: ProviderFilter; label: string }[] = [
  { value: 'all', label: '全部云' },
  { value: 'aws', label: 'AWS' },
  { value: 'aliyun', label: '阿里云' },
]

const provider = computed({
  get: () => state.value.provider,
  set: (v: ProviderFilter) => {
    // 切换云时，已选账号若不属于该云则清掉。
    const acc = filters.value?.accounts.find((a) => a.id === state.value.account_id)
    update({ provider: v, account_id: acc && v !== 'all' && acc.provider !== v ? undefined : state.value.account_id })
  },
})

const accountModel = computed({
  get: () => state.value.account_id ?? '',
  set: (v: number | '') => update({ account_id: v || undefined }),
})
const regionModel = computed({
  get: () => state.value.region ?? '',
  set: (v: string) => update({ region: v || undefined }),
})
const statusModel = computed({
  get: () => state.value.status ?? '',
  set: (v: string) => update({ status: v || undefined }),
})

const tabs = computed(() =>
  types.map((t) => ({ value: t, label: typeLabel[t], count: filters.value?.counts[t] ?? 0 })),
)

const allCount = computed(() => {
  const c = filters.value?.counts
  return c ? types.reduce((n, t) => n + (c[t] ?? 0), 0) : 0
})

const subtitle = computed(() => {
  const parts = [`共 ${allCount.value.toLocaleString('zh-CN')} 项`]
  const n = filters.value?.accounts.length ?? 0
  if (n) parts.push(`${n} 个云账号定时同步`)
  const t = app.syncStatus?.last_finished_at
  if (t) parts.push(`数据更新于 ${fromNow(t, app.now)}`)
  return parts.join(' · ')
})

function switchType(t: ResourceType) {
  if (t === state.value.type) return
  // 状态、地域、排序都跟类型相关，切换时清空。
  update({ type: t, region: undefined, status: undefined, sort: undefined, idle: t === 'vm' ? state.value.idle : false })
}

// ---------- 表格 ----------
const sortState = computed(() => {
  const [prop, dir] = (effectiveSort.value ?? '').split(':')
  return prop ? { prop, order: dir === 'asc' ? ('ascending' as const) : ('descending' as const) } : undefined
})

function onSort({ prop, order }: { prop: string; order: 'ascending' | 'descending' | null }) {
  const sort = order ? `${prop}:${order === 'ascending' ? 'asc' : 'desc'}` : undefined
  // 回到默认排序时不写进地址栏。
  update({ sort: sort === (state.value.type === 'vm' ? 'cpu_1h:desc' : undefined) ? undefined : sort })
}

function rowClass({ row }: { row: Resource }) {
  return row.id === detailId.value ? 'is-open' : ''
}

const expDays = 30
</script>

<template>
  <PageHeader title="资源中心" :subtitle="subtitle">
    <el-button :loading="loading && loaded" @click="load">
      <AppIcon v-if="!(loading && loaded)" name="refresh" :size="15" />
      <span class="btn-gap">刷新</span>
    </el-button>
  </PageHeader>

  <section class="ys-card panel">
    <div class="tabs" role="tablist" aria-label="资源类型">
      <button
        v-for="t in tabs"
        :key="t.value"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: state.type === t.value }"
        :aria-selected="state.type === t.value"
        @click="switchType(t.value)"
      >
        {{ t.label }}
        <span class="count">{{ t.count.toLocaleString('zh-CN') }}</span>
      </button>
    </div>

    <div class="filters">
      <SegmentedControl v-model="provider" :options="cloudOptions" label="云厂商" />
      <el-select v-model="accountModel" class="filter-select" placeholder="全部" :teleported="true">
        <template #prefix>账号：</template>
        <el-option label="全部" value="" />
        <el-option v-for="a in filters?.accounts ?? []" :key="a.id" :label="a.name" :value="a.id" />
      </el-select>
      <el-select v-model="regionModel" class="filter-select" placeholder="全部" filterable>
        <template #prefix>地域：</template>
        <el-option label="全部" value="" />
        <el-option v-for="r in filters?.regions ?? []" :key="r.value" :label="r.value" :value="r.value">
          <span class="opt">
            <span class="mono">{{ r.value }}</span>
            <span class="opt-label">{{ r.label }}</span>
            <span class="opt-count">{{ r.count }}</span>
          </span>
        </el-option>
      </el-select>
      <el-select v-model="statusModel" class="filter-select" placeholder="全部">
        <template #prefix>状态：</template>
        <el-option label="全部" value="" />
        <el-option
          v-for="s in filters?.statuses ?? []"
          :key="s.value"
          :label="resourceStatusInfo(s.value).label"
          :value="s.value"
        >
          <span class="opt">
            <span>{{ resourceStatusInfo(s.value).label }}</span>
            <span class="opt-count">{{ s.count }}</span>
          </span>
        </el-option>
      </el-select>
      <label class="search">
        <AppIcon name="search" :size="15" />
        <input
          v-model="keyword"
          type="search"
          aria-label="筛选资源"
          placeholder="名称、ID、IP 或 标签 env:prod"
          @input="onKeyword"
          @keydown.enter="applyKeyword"
        />
      </label>
      <button v-if="state.idle" type="button" class="chip" @click="update({ idle: false })">
        仅闲置主机 <AppIcon name="close" :size="12" />
      </button>
      <button v-if="state.expiring" type="button" class="chip" @click="update({ expiring: false })">
        仅 {{ expDays }} 天内到期 <AppIcon name="close" :size="12" />
      </button>
      <button v-if="hasFilters" type="button" class="ys-link reset" @click="reset">重置</button>
      <span class="total">共 {{ total.toLocaleString('zh-CN') }} 条</span>
    </div>

    <div class="table-wrap" :class="{ dim: loading && loaded }">
      <el-table
        :key="state.type"
        v-loading="loading && !loaded"
        :data="items"
        row-key="id"
        :default-sort="sortState"
        :row-class-name="rowClass"
        class="res-table"
        @sort-change="onSort"
        @row-click="openDetail"
      >
        <!-- 名称 -->
        <el-table-column :label="state.type === 'bucket' ? '存储桶名称' : state.type === 'lb' ? '名称 / ID' : '名称 / 实例 ID'" min-width="220">
          <template #default="{ row }">
            <div class="cell-stack">
              <a href="#" class="res-name" @click.prevent.stop="openDetail(row)">{{ row.name || row.resource_id }}</a>
              <span v-if="state.type !== 'bucket'" class="sub-mono ellipsis">{{ row.resource_id }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="云 · 账号" width="132">
          <template #default="{ row }">
            <div class="cell-stack start">
              <CloudTag :provider="row.provider" />
              <span class="sub ellipsis">{{ shortAccountName(row.account_name, row.provider) }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column :label="state.type === 'vm' ? '地域 · 可用区' : '地域'" width="148">
          <template #default="{ row }">
            <div class="cell-stack">
              <span>{{ row.region }}</span>
              <span v-if="state.type === 'vm'" class="sub-muted">{{ zoneLabel(row.zone, row.provider) }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="state.type !== 'bucket'" label="状态" width="100">
          <template #default="{ row }"><StatusTag :status="row.status" /></template>
        </el-table-column>

        <!-- 云主机 -->
        <template v-if="state.type === 'vm'">
          <el-table-column label="规格" width="150">
            <template #default="{ row }">
              <div class="cell-stack">
                <span class="mono small">{{ row.spec }}</span>
                <span class="sub-muted small">{{ vmSize(row) }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="私网 / 公网 IP" width="140">
            <template #default="{ row }">
              <div class="cell-stack mono small">
                <span>{{ ips(row.private_ip)[0] ?? '—' }}</span>
                <span class="muted-text">{{ ips(row.public_ip)[0] ?? '—' }}</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="CPU · 1h" prop="cpu_1h" sortable="custom" width="118" :sort-orders="['descending', 'ascending']">
            <template #default="{ row }">
              <div class="cpu-cell">
                <span class="cpu-line">
                  <span class="cpu-val" :style="{ color: cpuTone(row.cpu_1h).text }">{{ formatPercent(row.cpu_1h) }}</span>
                  <span v-if="row.idle" class="idle-tag">闲置</span>
                </span>
                <span class="bar"><span :style="{ width: `${Math.min(100, row.cpu_1h ?? 0)}%`, background: cpuTone(row.cpu_1h).bar }" /></span>
              </div>
            </template>
          </el-table-column>
        </template>

        <!-- 数据库 -->
        <template v-if="state.type === 'rds'">
          <el-table-column label="引擎" width="150">
            <template #default="{ row }">{{ engineLabel(row) || '—' }}</template>
          </el-table-column>
          <el-table-column label="规格" min-width="170">
            <template #default="{ row }"><span class="mono small">{{ row.spec }}</span></template>
          </el-table-column>
          <el-table-column label="存储" width="90">
            <template #default="{ row }">{{ rdsStorage(row) }}</template>
          </el-table-column>
        </template>

        <!-- 负载均衡 -->
        <template v-if="state.type === 'lb'">
          <el-table-column label="类型" width="150">
            <template #default="{ row }">{{ lbKindLabel(row) }}</template>
          </el-table-column>
          <el-table-column label="服务地址" min-width="240">
            <template #default="{ row }">
              <span class="mono small ellipsis block" :title="extraStr(row, 'address')">{{ extraStr(row, 'address') || '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="网络" width="72">
            <template #default="{ row }">{{ networkLabel(row) }}</template>
          </el-table-column>
        </template>

        <!-- 对象存储 -->
        <template v-if="state.type === 'bucket'">
          <el-table-column label="存储类型" width="130">
            <template #default="{ row }">{{ bucketClass(row) }}</template>
          </el-table-column>
          <el-table-column label="容量" width="110">
            <template #default="{ row }"><span class="strong">{{ bucketSize(row) }}</span></template>
          </el-table-column>
          <el-table-column label="对象数" width="140">
            <template #default="{ row }"><span class="mono small">{{ bucketObjects(row) }}</span></template>
          </el-table-column>
          <el-table-column label="创建时间" width="112">
            <template #default="{ row }"><span class="sub">{{ formatDate(row.cloud_created_at) }}</span></template>
          </el-table-column>
        </template>

        <!-- 到期 -->
        <el-table-column
          v-if="state.type === 'vm' || state.type === 'rds'"
          label="到期"
          prop="expire_at"
          sortable="custom"
          :sort-orders="['ascending', 'descending', null]"
          width="118"
        >
          <template #default="{ row }">
            <div class="cell-stack small">
              <span :class="{ 'warn-text': expiryInfo(row, expDays, app.now).soon }">{{ expiryInfo(row, expDays, app.now).text }}</span>
              <span class="sub-muted">{{ expiryInfo(row, expDays, app.now).sub }}</span>
            </div>
          </template>
        </el-table-column>

        <template #empty>
          <EmptyState
            v-if="loaded"
            compact
            icon="search"
            :title="hasFilters ? '没有符合条件的资源' : `还没有${typeLabel[state.type]}`"
            :description="
              hasFilters
                ? '换个关键字试试，或清空筛选条件。标签筛选的写法是 key:value，例如 env:prod。'
                : '接入云账号并完成同步后，资源会出现在这里。'
            "
          >
            <el-button v-if="hasFilters" @click="reset">清空筛选</el-button>
            <RouterLink v-else to="/accounts"><el-button>前往云账号</el-button></RouterLink>
          </EmptyState>
        </template>
      </el-table>
    </div>

    <div class="pager">
      <span>共 {{ total.toLocaleString('zh-CN') }} 条，每页 {{ PAGE_SIZE }} 条</span>
      <el-pagination
        v-if="total > PAGE_SIZE"
        background
        layout="prev, pager, next"
        :total="total"
        :page-size="PAGE_SIZE"
        :current-page="state.page"
        :pager-count="7"
        @current-change="(p: number) => update({ page: p }, true)"
      />
    </div>
  </section>

  <ResourceDrawer :id="detailId" @close="closeDetail" />
</template>

<style scoped>
.btn-gap {
  margin-left: 8px;
}

.panel {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.tabs {
  height: 48px;
  flex-shrink: 0;
  padding: 0 20px;
  display: flex;
  align-items: stretch;
  gap: 28px;
  border-bottom: 1px solid var(--ys-divider);
}

.tab {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 2px;
  border: 0;
  background: transparent;
  color: var(--ys-text-label);
  font-size: 14px;
  cursor: pointer;
}

.tab:hover {
  color: var(--ys-text);
}

.tab.on {
  color: var(--ys-primary);
  font-weight: 600;
  box-shadow: inset 0 -2px 0 var(--ys-primary);
}

.count {
  padding: 0 7px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 400;
  line-height: 20px;
  background: var(--ys-bg-muted);
  color: var(--ys-text-secondary);
}

.tab.on .count {
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
}

.filters {
  min-height: 60px;
  flex-shrink: 0;
  padding: 12px 20px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--ys-divider);
}

.filter-select {
  width: 150px;
}

.filter-select :deep(.el-select__prefix) {
  color: var(--ys-text-label);
  font-size: 13px;
}

.filter-select :deep(.el-select__selected-item) {
  font-size: 13px;
}

.opt {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
}

.opt-label {
  color: var(--ys-text-muted);
  font-size: 12px;
}

.opt-count {
  margin-left: auto;
  color: var(--ys-text-muted);
  font-size: 12px;
}

.search {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 300px;
  height: 36px;
  padding: 0 12px;
  border: 1px solid var(--ys-border-input);
  border-radius: 8px;
  background: #fff;
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

.search input::placeholder {
  color: var(--ys-text-muted);
}

.chip {
  height: 30px;
  padding: 0 8px 0 10px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid var(--ys-primary-soft-border);
  border-radius: 999px;
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
  font-size: 12px;
  cursor: pointer;
}

.reset {
  padding: 0 8px;
}

.total {
  margin-left: auto;
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.table-wrap {
  transition: opacity 0.2s;
}

.table-wrap.dim {
  opacity: 0.55;
}

.res-table :deep(.el-table__row) {
  cursor: pointer;
}

.res-table :deep(.el-table__cell) {
  height: 56px;
}

.res-table :deep(th.el-table__cell) {
  height: 40px;
}

.res-table :deep(.el-table__row.is-open > td) {
  background: var(--ys-primary-soft);
}

.res-table :deep(.cell) {
  padding: 0 10px;
}

.res-table :deep(tr > :first-child .cell) {
  padding-left: 20px;
}

.cell-stack {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.cell-stack.start {
  align-items: flex-start;
  gap: 3px;
}

.res-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.sub-mono {
  font-family: var(--ys-font-mono);
  font-size: 11px;
  color: var(--ys-text-muted);
}

.sub {
  font-size: 12px;
  color: var(--ys-text-secondary);
  max-width: 100%;
}

.sub-muted {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.small {
  font-size: 12px;
}

.muted-text {
  color: var(--ys-text-muted);
}

.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.block {
  display: block;
}

.strong {
  font-weight: 500;
}

.warn-text {
  color: var(--ys-warn-fg);
}

.cpu-cell {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.cpu-line {
  display: flex;
  align-items: center;
  gap: 6px;
}

.cpu-val {
  font-weight: 600;
}

.idle-tag {
  padding: 0 5px;
  border-radius: 4px;
  font-size: 11px;
  line-height: 16px;
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
}

.bar {
  display: block;
  width: 72px;
  height: 4px;
  border-radius: 2px;
  background: var(--ys-divider);
}

.bar span {
  display: block;
  height: 4px;
  border-radius: 2px;
}

.pager {
  min-height: 56px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  border-top: 1px solid var(--ys-divider);
  font-size: 13px;
  color: var(--ys-text-secondary);
}
</style>
