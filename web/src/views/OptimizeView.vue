<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useRoute, useRouter, type LocationQueryRaw } from 'vue-router'
import axios from 'axios'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import ResourceDrawer from './ResourceDrawer.vue'
import {
  dashboardApi,
  resourceApi,
  type DashboardSummary,
  type ProviderFilter,
  type Resource,
  type ResourceQuery,
} from '@/api'
import { useAppStore } from '@/stores/app'
import { formatDate, formatDateTime, formatPercent, fromNow } from '@/utils/format'
import { cpuTone, expiryInfo, extraNum, extraStr, ips, shortAccountName, vmSize, zoneLabel } from '@/utils/resource'
import { typeLabel } from '@/utils/status'

type Kind = 'idle' | 'disk' | 'eip' | 'expiring'

const PAGE_SIZE = 20
const kinds: Kind[] = ['idle', 'disk', 'eip', 'expiring']

const route = useRoute()
const router = useRouter()
const app = useAppStore()

function str(v: unknown): string | undefined {
  return typeof v === 'string' && v !== '' ? v : undefined
}

const state = computed(() => {
  const q = route.query
  const kind = str(q.kind) as Kind | undefined
  const provider = str(q.provider)
  const page = Number(str(q.page) ?? 1)
  return {
    kind: kind && kinds.includes(kind) ? kind : ('idle' as Kind),
    provider: (provider === 'aws' || provider === 'aliyun' ? provider : 'all') as ProviderFilter,
    page: Number.isInteger(page) && page > 0 ? page : 1,
  }
})

function update(patch: Partial<typeof state.value>) {
  const next = { ...state.value, page: 1, ...patch }
  const query: LocationQueryRaw = {
    kind: next.kind === 'idle' ? undefined : next.kind,
    provider: next.provider === 'all' ? undefined : next.provider,
    page: next.page > 1 ? String(next.page) : undefined,
    detail: route.query.detail,
  }
  void router.replace({ query })
}

const detailId = computed(() => {
  const id = Number(str(route.query.detail) ?? 0)
  return id > 0 ? id : null
})

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
const summary = ref<DashboardSummary | null>(null)
const loading = ref(false)
const loaded = ref(false)
let controller: AbortController | null = null

const listQuery = computed<ResourceQuery>(() => {
  const s = state.value
  const base = { provider: s.provider, page: s.page, page_size: PAGE_SIZE }
  switch (s.kind) {
    case 'idle':
      return { ...base, type: 'vm', idle: true, sort: 'cpu_24h:asc' }
    case 'disk':
      return { ...base, type: 'disk', sort: 'created_at:asc' }
    case 'eip':
      return { ...base, type: 'eip', sort: 'created_at:asc' }
    default:
      return { ...base, expiring: true, sort: 'expire_at:asc' }
  }
})

async function load() {
  controller?.abort()
  const ctl = new AbortController()
  controller = ctl
  loading.value = true
  try {
    const list = await resourceApi.list(listQuery.value, ctl.signal)
    items.value = list.items
    total.value = list.total
    loaded.value = true
  } catch (err) {
    if (!axios.isCancel(err)) loaded.value = true
  } finally {
    if (controller === ctl) loading.value = false
  }
}

async function loadSummary() {
  try {
    summary.value = await dashboardApi.summary(state.value.provider)
  } catch {
    // 计数只影响标签上的数字，失败时不打断列表。
  }
}

watch(
  listQuery,
  (a, b) => {
    if (JSON.stringify(a) !== JSON.stringify(b)) void load()
  },
  { immediate: true },
)
watch(() => state.value.provider, () => void loadSummary(), { immediate: true })
watch(
  () => app.syncTick,
  () => {
    void load()
    void loadSummary()
  },
)
onBeforeUnmount(() => controller?.abort())

function refresh() {
  void load()
  void loadSummary()
}

// ---------- 选项 ----------
const cloudOptions: { value: ProviderFilter; label: string }[] = [
  { value: 'all', label: '全部云' },
  { value: 'aws', label: 'AWS' },
  { value: 'aliyun', label: '阿里云' },
]

const provider = computed({
  get: () => state.value.provider,
  set: (v: ProviderFilter) => update({ provider: v }),
})

const tabs = computed(() => {
  const s = summary.value
  return [
    { value: 'idle' as Kind, label: '闲置主机', count: s?.idle ?? 0 },
    { value: 'disk' as Kind, label: typeLabel.disk, count: s?.waste_disks ?? 0 },
    { value: 'eip' as Kind, label: typeLabel.eip, count: s?.waste_eips ?? 0 },
    { value: 'expiring' as Kind, label: '即将到期', count: s?.expiring ?? 0 },
  ]
})

const hint = computed(() => {
  const s = summary.value
  switch (state.value.kind) {
    case 'idle':
      return `运行中且 24 小时 CPU 均值低于 ${s?.idle_threshold ?? 5}% 的云主机，可考虑降配或释放。`
    case 'disk':
      return '未挂载到任何实例的云盘仍按容量计费，确认数据无用后可创建快照再释放。'
    case 'eip':
      return '未绑定实例的弹性公网 IP 会持续收取保有费用，不再使用时建议释放。'
    default:
      return `${s?.expiring_days ?? 30} 天内到期的包年包月资源，请确认是否续费。`
  }
})

const expDays = computed(() => summary.value?.expiring_days ?? 30)

function diskSize(r: Resource): string {
  const n = extraNum(r, 'size_gib')
  return n === null ? '—' : `${n} GiB`
}

// 规格形如“ESSD 云盘 · 100 GiB”，容量单独成列，这里只取类型。
function diskKind(r: Resource): string {
  return r.spec.split(' · ')[0] || extraStr(r, 'category') || '—'
}

const eipChargeLabel: Record<string, string> = { PayByTraffic: '按流量', PayByBandwidth: '按固定带宽' }

function eipCharge(r: Resource): string {
  if (r.provider === 'aws') return '按小时计费'
  const t = extraStr(r, 'internet_charge_type')
  return eipChargeLabel[t] ?? (t || '—')
}

function rowClass({ row }: { row: Resource }) {
  return row.id === detailId.value ? 'is-open' : ''
}

const emptyText: Record<Kind, string> = {
  idle: '没有闲置主机',
  disk: '没有未挂载的云盘',
  eip: '没有未绑定的弹性 IP',
  expiring: '近期没有到期的资源',
}
</script>

<template>
  <PageHeader
    title="成本优化"
    :subtitle="app.syncStatus?.last_finished_at ? `数据更新于 ${fromNow(app.syncStatus.last_finished_at, app.now)}` : '找出闲置和即将到期的资源'"
  >
    <el-button :loading="loading && loaded" @click="refresh">
      <AppIcon v-if="!(loading && loaded)" name="refresh" :size="15" />
      <span class="btn-gap">刷新</span>
    </el-button>
  </PageHeader>

  <section class="ys-card panel">
    <div class="tabs" role="tablist" aria-label="优化类别">
      <button
        v-for="t in tabs"
        :key="t.value"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: state.kind === t.value }"
        :aria-selected="state.kind === t.value"
        @click="update({ kind: t.value })"
      >
        {{ t.label }}
        <span class="count" :class="{ warn: t.count > 0 }">{{ t.count.toLocaleString('zh-CN') }}</span>
      </button>
    </div>

    <div class="filters">
      <SegmentedControl v-model="provider" :options="cloudOptions" label="云厂商" />
      <span class="hint">{{ hint }}</span>
      <span class="total">共 {{ total.toLocaleString('zh-CN') }} 条</span>
    </div>

    <div class="table-wrap" :class="{ dim: loading && loaded }">
      <el-table
        :key="state.kind"
        v-loading="loading && !loaded"
        :data="items"
        row-key="id"
        :row-class-name="rowClass"
        class="res-table"
        @row-click="openDetail"
      >
        <el-table-column :label="state.kind === 'eip' ? 'IP 地址 / ID' : '名称 / ID'" min-width="220">
          <template #default="{ row }">
            <div class="cell-stack">
              <a href="#" class="res-name" :class="{ mono: state.kind === 'eip' }" @click.prevent.stop="openDetail(row)">
                {{ row.name || row.resource_id }}
              </a>
              <span v-if="row.name && row.name !== row.resource_id" class="sub-mono ellipsis">{{ row.resource_id }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column v-if="state.kind === 'expiring'" label="类型" width="100">
          <template #default="{ row }">{{ typeLabel[row.type as keyof typeof typeLabel] ?? row.type }}</template>
        </el-table-column>
        <el-table-column label="云 · 账号" width="132">
          <template #default="{ row }">
            <div class="cell-stack start">
              <CloudTag :provider="row.provider" />
              <span class="sub ellipsis">{{ shortAccountName(row.account_name, row.provider) }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="地域 · 可用区" width="148">
          <template #default="{ row }">
            <div class="cell-stack">
              <span>{{ row.region }}</span>
              <span v-if="row.zone" class="sub-muted">{{ zoneLabel(row.zone, row.provider) }}</span>
              <span v-if="row.data_stale" class="sub-muted" :title="`最后采集：${formatDateTime(row.synced_at)}`">采集失败或已过期</span>
            </div>
          </template>
        </el-table-column>

        <!-- 闲置主机 -->
        <template v-if="state.kind === 'idle'">
          <el-table-column label="规格" width="160">
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
          <el-table-column label="CPU · 24h 均值" width="130">
            <template #default="{ row }">
              <span class="strong" :style="{ color: cpuTone(row.cpu_24h).text }">{{ formatPercent(row.cpu_24h) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="CPU · 1h" width="100">
            <template #default="{ row }">{{ formatPercent(row.cpu_1h) }}</template>
          </el-table-column>
        </template>

        <!-- 未挂载云盘 -->
        <template v-if="state.kind === 'disk'">
          <el-table-column label="类型" min-width="140">
            <template #default="{ row }"><span class="small">{{ diskKind(row) }}</span></template>
          </el-table-column>
          <el-table-column label="容量" width="100">
            <template #default="{ row }"><span class="strong">{{ diskSize(row) }}</span></template>
          </el-table-column>
          <el-table-column label="卸载时间" width="120">
            <template #default="{ row }">
              <span class="sub">{{ extraStr(row, 'detached_at') ? fromNow(extraStr(row, 'detached_at'), app.now) : '—' }}</span>
            </template>
          </el-table-column>
          <el-table-column label="创建时间" width="112">
            <template #default="{ row }"><span class="sub">{{ formatDate(row.cloud_created_at) }}</span></template>
          </el-table-column>
        </template>

        <!-- 未绑定 EIP -->
        <template v-if="state.kind === 'eip'">
          <el-table-column label="带宽" width="110">
            <template #default="{ row }">{{ row.spec || '—' }}</template>
          </el-table-column>
          <el-table-column label="计费" min-width="140">
            <template #default="{ row }">
              <span class="small">{{ eipCharge(row) }}</span>
            </template>
          </el-table-column>
          <el-table-column label="创建时间" width="112">
            <template #default="{ row }"><span class="sub">{{ formatDate(row.cloud_created_at) }}</span></template>
          </el-table-column>
        </template>

        <!-- 即将到期 -->
        <template v-if="state.kind === 'expiring'">
          <el-table-column label="规格" min-width="160">
            <template #default="{ row }"><span class="mono small">{{ row.spec || '—' }}</span></template>
          </el-table-column>
          <el-table-column label="到期" width="160">
            <template #default="{ row }">
              <div class="cell-stack small">
                <span :class="{ 'warn-text': expiryInfo(row, expDays, app.now).soon }">{{ expiryInfo(row, expDays, app.now).text }}</span>
                <span class="sub-muted">{{ formatDateTime(row.expire_at).slice(0, 16) }}</span>
              </div>
            </template>
          </el-table-column>
        </template>

        <template #empty>
          <EmptyState
            v-if="loaded"
            compact
            icon="saving"
            :title="emptyText[state.kind]"
            description="数据来自最近一次同步，同步后会自动刷新。"
          />
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
        @current-change="(p: number) => update({ page: p })"
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

.count.warn {
  background: var(--ys-warn-soft-bg);
  color: var(--ys-warn-fg);
}

.filters {
  min-height: 60px;
  flex-shrink: 0;
  padding: 12px 20px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 16px;
  border-bottom: 1px solid var(--ys-divider);
}

.hint {
  font-size: 13px;
  color: var(--ys-text-secondary);
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

.strong {
  font-weight: 500;
}

.warn-text {
  color: var(--ys-warn-fg);
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
