<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import axios from 'axios'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import MetricChart, { type ChartSeries } from '@/components/MetricChart.vue'
import PageHeader from '@/components/PageHeader.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import {
  errorMessage,
  metricApi,
  resourceApi,
  type CompareResult,
  type MetricDef,
  type Provider,
  type RangeKey,
  type Resource,
  type ResourceType,
} from '@/api'
import { chartColors } from '@/utils/charts'
import { dayjs, formatMetric, unitLabel } from '@/utils/format'
import { providerLabel } from '@/utils/status'

type MonitorType = Exclude<ResourceType, 'bucket'>
const MAX = 4
const monitorTypes: { value: MonitorType; label: string }[] = [
  { value: 'vm', label: '云主机' },
  { value: 'rds', label: '数据库' },
  { value: 'lb', label: '负载均衡' },
]
const rangeOptions: { value: RangeKey; label: string }[] = [
  { value: '1h', label: '1 小时' },
  { value: '6h', label: '6 小时' },
  { value: '24h', label: '24 小时' },
  { value: '7d', label: '7 天' },
]
const rangeMs: Record<RangeKey, number> = { '1h': 3600e3, '6h': 6 * 3600e3, '24h': 24 * 3600e3, '7d': 7 * 86400e3 }

interface Picked {
  id: number
  name: string
  provider: Provider
  region: string
  account_name: string
}

const route = useRoute()
const router = useRouter()

function queryStr(key: string): string {
  const v = route.query[key]
  return typeof v === 'string' ? v : ''
}

const type = ref<MonitorType>((['vm', 'rds', 'lb'] as const).find((t) => t === queryStr('type')) ?? 'vm')
const range = ref<RangeKey>(rangeOptions.find((r) => r.value === queryStr('range'))?.value ?? '6h')
const metric = ref(queryStr('metric'))
// 槽位决定颜色：移除某个资源后，其余资源的颜色保持不变。
const slots = ref<(number | null)[]>(
  queryStr('ids')
    .split(',')
    .slice(0, MAX)
    .map((s) => (/^\d+$/.test(s) ? Number(s) : null)),
)
const info = ref(new Map<number, Picked>())

const catalog = ref<MetricDef[]>([])
const result = ref<CompareResult | null>(null)
const loading = ref(false)
const error = ref('')
let ctl: AbortController | null = null

const metrics = computed(() => catalog.value.filter((m) => m.type === type.value))
const current = computed(() => metrics.value.find((m) => m.key === metric.value) ?? metrics.value[0])
const selectedIds = computed(() => slots.value.filter((id): id is number => id !== null))
const full = computed(() => selectedIds.value.length >= MAX)

function syncUrl() {
  const ids = slots.value.map((id) => (id === null ? '' : String(id)))
  while (ids.length && ids[ids.length - 1] === '') ids.pop()
  void router.replace({
    query: {
      type: type.value,
      metric: current.value?.key,
      range: range.value,
      ids: ids.length ? ids.join(',') : undefined,
    },
  })
}

function remember(r: Resource | Picked) {
  info.value.set(r.id, { id: r.id, name: r.name, provider: r.provider, region: r.region, account_name: r.account_name })
}

// 首次进入且没有指定资源时，每朵云各挑两台（云主机按 CPU 从高到低），直接呈现跨云对比。
async function pickDefaults() {
  const sort = type.value === 'vm' ? 'cpu_1h:desc' : undefined
  const status = 'running'
  const size = type.value === 'lb' ? 10 : 2
  const [a, b] = await Promise.all([
    resourceApi.list({ type: type.value, provider: 'aliyun', status, sort, page_size: size }),
    resourceApi.list({ type: type.value, provider: 'aws', status, sort, page_size: size }),
  ])
  // 负载均衡默认指标是 QPS，四层的 NLB 没有这个指标，优先挑七层的。
  const prefer = (list: Resource[]) =>
    type.value === 'lb' ? [...list].sort((x, y) => Number(y.extra?.lb_kind !== 'nlb') - Number(x.extra?.lb_kind !== 'nlb')) : list
  // 两朵云交替排列，相邻两条曲线来自不同的云，和原型一致。
  const [ali, aws] = [prefer(a.items), prefer(b.items)]
  const picked = [ali[0], aws[0], ali[1], aws[1]].filter((r): r is Resource => !!r).slice(0, MAX)
  picked.forEach(remember)
  slots.value = picked.map((r) => r.id)
}

async function ensureInfo() {
  const missing = selectedIds.value.filter((id) => !info.value.has(id))
  const found = await Promise.all(missing.map((id) => resourceApi.get(id).catch(() => null)))
  found.forEach((r) => r && remember(r))
  // 已被删除的资源从对比里拿掉。
  const gone = missing.filter((_, i) => !found[i])
  if (gone.length) slots.value = slots.value.map((id) => (id !== null && gone.includes(id) ? null : id))
}

async function query() {
  ctl?.abort()
  const ids = selectedIds.value
  if (!current.value || !ids.length) {
    result.value = null
    return
  }
  const c = new AbortController()
  ctl = c
  loading.value = true
  error.value = ''
  try {
    const res = await metricApi.compare({ resource_ids: ids, key: current.value.key, range: range.value }, c.signal)
    if (ctl === c) {
      result.value = res
      res.items.forEach((it) =>
        remember({ id: it.resource_id, name: it.name, provider: it.provider, region: it.region, account_name: it.account_name }),
      )
    }
  } catch (err) {
    if (!axios.isCancel(err) && ctl === c) error.value = errorMessage(err)
  } finally {
    if (ctl === c) loading.value = false
  }
}

async function init() {
  const res = await metricApi.catalog()
  catalog.value = res.items
  if (!metrics.value.some((m) => m.key === metric.value)) metric.value = metrics.value[0]?.key ?? ''
  if (!selectedIds.value.length) await pickDefaults()
  else await ensureInfo()
  syncUrl()
  await query()
}

onMounted(() => void init())
onBeforeUnmount(() => ctl?.abort())

watch(type, async () => {
  metric.value = metrics.value[0]?.key ?? ''
  slots.value = []
  result.value = null
  pickerOpen.value = false
  await pickDefaults()
  syncUrl()
  await query()
})
watch([metric, range], () => {
  syncUrl()
  void query()
})

function selectMetric(key: string) {
  metric.value = key
}

function remove(id: number) {
  slots.value = slots.value.map((s) => (s === id ? null : s))
  syncUrl()
  void query()
}

function add(r: Resource) {
  if (full.value || selectedIds.value.includes(r.id)) return
  remember(r)
  const next = [...slots.value]
  const free = next.findIndex((s) => s === null)
  if (free >= 0) next[free] = r.id
  else next.push(r.id)
  slots.value = next
  pickerOpen.value = false
  syncUrl()
  void query()
}

// ---------- 添加资源的弹层 ----------
const pickerOpen = ref(false)
const pickerRight = ref(false)
const pickerRoot = ref<HTMLElement>()
const pickerInput = ref<HTMLInputElement>()
const keyword = ref('')
const candidates = ref<Resource[]>([])
const searching = ref(false)
let searchTimer: ReturnType<typeof setTimeout> | undefined

async function search() {
  searching.value = true
  try {
    const res = await resourceApi.list({ type: type.value, q: keyword.value.trim() || undefined, page_size: 8 })
    candidates.value = res.items
  } finally {
    searching.value = false
  }
}

watch(keyword, () => {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void search(), 300)
})

function togglePicker() {
  pickerOpen.value = !pickerOpen.value
  if (pickerOpen.value) {
    // 按钮靠右时弹层向左展开，避免超出窗口。
    const rect = pickerRoot.value?.getBoundingClientRect()
    pickerRight.value = !!rect && rect.left + 376 > document.documentElement.clientWidth
    keyword.value = ''
    void search()
    setTimeout(() => pickerInput.value?.focus(), 0)
  }
}

function onDocClick(e: MouseEvent) {
  if (pickerOpen.value && pickerRoot.value && !pickerRoot.value.contains(e.target as Node)) pickerOpen.value = false
}
onMounted(() => document.addEventListener('click', onDocClick))
onBeforeUnmount(() => document.removeEventListener('click', onDocClick))

// ---------- 图表与图例 ----------
const chips = computed(() =>
  slots.value
    .map((id, slot) => (id === null ? null : { id, slot, color: chartColors.series[slot]!, info: info.value.get(id) }))
    .filter((c): c is NonNullable<typeof c> => c !== null),
)

const items = computed(() => new Map((result.value?.items ?? []).map((it) => [it.resource_id, it])))

const series = computed<ChartSeries[]>(() =>
  chips.value.flatMap((c) => {
    const it = items.value.get(c.id)
    if (!it || !it.supported || it.error) return []
    return [{ name: c.info?.name ?? String(c.id), color: c.color, points: it.points }]
  }),
)

const unit = computed(() => current.value?.unit ?? 'count')

const legend = computed(() =>
  chips.value.map((c) => {
    const it = items.value.get(c.id)
    const vals = (it?.points ?? []).map((p) => p[1])
    let reason = ''
    if (it && !it.supported) {
      reason = current.value && c.info && !current.value.providers.includes(c.info.provider) ? '该云不提供此指标' : '该资源不提供此指标'
    } else if (it?.error) {
      reason = `查询失败：${it.error}`
    } else if (it && !vals.length) {
      reason = '所选时间范围内没有数据'
    }
    return {
      ...c,
      reason,
      ok: !!it && !reason,
      cur: vals.length ? vals[vals.length - 1]! : null,
      avg: vals.length ? vals.reduce((a, b) => a + b, 0) / vals.length : null,
      max: vals.length ? Math.max(...vals) : null,
    }
  }),
)

const grain = computed(() => {
  const s = result.value?.period ?? 0
  if (!s) return ''
  return s < 3600 ? `${Math.round(s / 60)} 分钟` : `${Math.round(s / 3600)} 小时`
})

const now = ref(Date.now())
watch(result, () => (now.value = Date.now()))
const chartWindow = computed(() => ({ start: now.value - rangeMs[range.value], end: now.value }))

function onlyLabel(m: MetricDef): string {
  if (m.providers.length !== 1) return ''
  return `仅 ${providerLabel[m.providers[0]!]}`
}

function openDetail(id: number) {
  void router.push({ path: '/resources', query: { type: type.value, detail: String(id) } })
}
</script>

<template>
  <PageHeader title="监控中心" :subtitle="`跨云、跨账号对比同一指标 · 最多同时对比 ${MAX} 个资源`" />

  <section class="ys-card controls">
    <div class="ctl-row">
      <span class="ctl-label">资源类型</span>
      <SegmentedControl v-model="type" :options="monitorTypes" label="资源类型" />
      <span class="ctl-label right">时间范围</span>
      <SegmentedControl v-model="range" :options="rangeOptions" label="时间范围" />
    </div>
    <div class="ctl-row">
      <span class="ctl-label">指标</span>
      <div class="pills" role="radiogroup" aria-label="指标">
        <el-tooltip v-for="m in metrics" :key="m.key" :content="m.note" :disabled="!m.note" placement="top">
          <button
            type="button"
            role="radio"
            class="pill"
            :class="{ on: current?.key === m.key }"
            :aria-checked="current?.key === m.key"
            @click="selectMetric(m.key)"
          >
            {{ m.name }}<span v-if="onlyLabel(m)" class="only">{{ onlyLabel(m) }}</span>
          </button>
        </el-tooltip>
      </div>
    </div>
    <div class="ctl-row top">
      <span class="ctl-label">对比资源</span>
      <div class="chips">
        <span v-for="c in chips" :key="c.id" class="chip">
          <span class="swatch" :style="{ background: c.color }" />
          <span class="chip-name">{{ c.info?.name ?? `#${c.id}` }}</span>
          <CloudTag v-if="c.info" :provider="c.info.provider" small />
          <button type="button" class="chip-x" :aria-label="`移除 ${c.info?.name ?? c.id}`" @click="remove(c.id)">
            <AppIcon name="close" :size="12" :stroke="2.4" />
          </button>
        </span>
        <span ref="pickerRoot" class="picker-wrap">
          <button
            type="button"
            class="add"
            :class="{ open: pickerOpen }"
            :aria-expanded="pickerOpen"
            @click="togglePicker"
          >
            <AppIcon name="plus" :size="14" />添加资源
          </button>
          <div v-if="pickerOpen" class="picker" :class="{ right: pickerRight }" role="dialog" aria-label="添加对比资源">
            <label class="picker-search">
              <AppIcon name="search" :size="14" />
              <input
                ref="pickerInput"
                v-model="keyword"
                type="search"
                aria-label="搜索要添加的资源"
                placeholder="搜索名称、ID 或 IP"
              />
            </label>
            <div class="cands">
              <button
                v-for="r in candidates"
                :key="r.id"
                type="button"
                class="cand"
                :disabled="full || selectedIds.includes(r.id)"
                @click="add(r)"
              >
                <span class="cand-main">
                  <span class="cand-name">{{ r.name }}</span>
                  <span class="cand-sub">{{ providerLabel[r.provider] }} · {{ r.region }} · {{ r.account_name }}</span>
                </span>
                <span class="cand-action">{{
                  selectedIds.includes(r.id) ? '已加入' : full ? `已满 ${MAX} 个` : '加入对比'
                }}</span>
              </button>
              <span v-if="!searching && !candidates.length" class="cand-empty">没有找到匹配的资源</span>
            </div>
          </div>
        </span>
      </div>
    </div>
  </section>

  <section class="ys-card chart-card">
    <div class="card-head">
      <h2 class="ys-card-title">{{ current?.name ?? '指标' }}</h2>
      <span class="note">单位 {{ unitLabel(unit) }}<template v-if="grain"> · 粒度 {{ grain }}</template></span>
      <span class="note right">AWS 数据来自 CloudWatch，阿里云数据来自云监控</span>
    </div>
    <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon />
    <MetricChart
      :series="series"
      :unit="unit"
      :height="300"
      :start="chartWindow.start"
      :end="chartWindow.end"
      :dim="loading && !!result"
      :label="`${current?.name ?? ''}对比曲线`"
      :empty-text="chips.length ? (loading ? '正在查询…' : '所选资源没有该指标的数据') : '点击上方“添加资源”开始对比'"
    />
    <div class="legend-table" role="table" aria-label="对比数值">
      <div class="lt-row lt-head" role="row">
        <span role="columnheader">资源</span>
        <span role="columnheader">云 · 地域</span>
        <span role="columnheader" class="num-col">当前</span>
        <span role="columnheader" class="num-col">平均</span>
        <span role="columnheader" class="num-col">最大</span>
      </div>
      <div v-for="l in legend" :key="l.id" class="lt-row" role="row">
        <span role="cell" class="lt-name">
          <span class="line-key" :style="{ background: l.ok ? l.color : '#D5D9E0' }" />
          <a href="#" @click.prevent="openDetail(l.id)">{{ l.info?.name ?? `#${l.id}` }}</a>
        </span>
        <span role="cell" class="lt-cloud">
          <CloudTag v-if="l.info" :provider="l.info.provider" small />{{ l.info?.region }}
        </span>
        <template v-if="l.ok">
          <span role="cell" class="num-col strong">{{ formatMetric(l.cur, unit) }}</span>
          <span role="cell" class="num-col">{{ formatMetric(l.avg, unit) }}</span>
          <span role="cell" class="num-col">{{ formatMetric(l.max, unit) }}</span>
        </template>
        <template v-else>
          <span role="cell" class="reason">{{ l.reason || '查询中…' }}</span>
        </template>
      </div>
      <div v-if="!legend.length" class="lt-empty">还没有选择资源</div>
    </div>
    <p v-if="result" class="updated">数据截至 {{ dayjs(now).format('HH:mm:ss') }}，结果缓存 1 分钟</p>
  </section>
</template>

<style scoped>
.controls {
  position: relative;
  z-index: 5;
  padding: 16px 20px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.ctl-row {
  display: flex;
  align-items: center;
  gap: 16px;
  min-height: 36px;
}

.ctl-label {
  width: 64px;
  flex-shrink: 0;
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.ctl-label.right {
  width: auto;
  margin-left: auto;
}

.ctl-row.top {
  align-items: flex-start;
}

.ctl-row.top .ctl-label {
  line-height: 32px;
}

.pills {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.pill {
  height: 32px;
  padding: 0 12px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid #dde1e7;
  border-radius: 999px;
  background: #fff;
  color: var(--ys-text-label);
  font-size: 13px;
  cursor: pointer;
}

.pill:hover {
  border-color: #c9d1ee;
}

.pill.on {
  border-color: var(--ys-primary);
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
}

.only {
  font-size: 11px;
  color: var(--ys-text-muted);
}

.chips {
  flex: 1;
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.chip {
  height: 32px;
  padding: 0 4px 0 10px;
  display: inline-flex;
  align-items: center;
  gap: 8px;
  border: 1px solid #dde1e7;
  border-radius: 8px;
  background: #fff;
  font-size: 13px;
}

.swatch {
  width: 10px;
  height: 10px;
  border-radius: 3px;
}

.chip-name {
  max-width: 180px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.chip-x {
  width: 24px;
  height: 24px;
  border: 0;
  border-radius: 5px;
  background: transparent;
  color: var(--ys-text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.chip-x:hover {
  background: var(--ys-bg-muted);
  color: var(--ys-text);
}

.picker-wrap {
  position: relative;
}

.add {
  height: 32px;
  padding: 0 12px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px dashed #aeb7c6;
  border-radius: 8px;
  background: #fff;
  color: var(--ys-primary);
  font-size: 13px;
  cursor: pointer;
}

.add.open,
.add:hover {
  background: var(--ys-primary-soft);
}

.picker {
  position: absolute;
  top: 40px;
  left: 0;
  z-index: 30;
  width: 360px;
  padding: 8px;
  display: flex;
  flex-direction: column;
  gap: 4px;
  border: 1px solid var(--ys-border);
  border-radius: 10px;
  background: #fff;
  box-shadow: var(--ys-shadow-pop);
}

.picker.right {
  left: auto;
  right: 0;
}

.picker-search {
  display: flex;
  align-items: center;
  gap: 8px;
  height: 34px;
  padding: 0 10px;
  margin-bottom: 4px;
  border: 1px solid var(--ys-border-input);
  border-radius: 7px;
  color: var(--ys-text-muted);
}

.picker-search:focus-within {
  border-color: var(--ys-primary);
}

.picker-search input {
  flex: 1;
  min-width: 0;
  border: 0;
  outline: none;
  background: transparent;
  color: var(--ys-text);
  font-family: inherit;
  font-size: 13px;
}

.cands {
  max-height: 320px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
}

.cand {
  min-height: 44px;
  padding: 4px 10px;
  display: flex;
  align-items: center;
  gap: 8px;
  border: 0;
  border-radius: 7px;
  background: transparent;
  color: var(--ys-text);
  font-size: 13px;
  text-align: left;
  cursor: pointer;
}

.cand:hover:not(:disabled) {
  background: var(--ys-bg-subtle);
}

.cand:disabled {
  cursor: default;
}

.cand:disabled .cand-action {
  color: var(--ys-text-muted);
}

.cand-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.cand-name {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cand-sub {
  font-size: 11px;
  color: var(--ys-text-muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cand-action {
  font-size: 12px;
  color: var(--ys-primary);
  white-space: nowrap;
}

.cand-empty {
  padding: 10px;
  font-size: 12px;
  color: var(--ys-text-muted);
}

.chart-card {
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.card-head {
  min-height: 22px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.note {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.note.right {
  margin-left: auto;
}

.legend-table {
  display: flex;
  flex-direction: column;
}

.lt-row {
  min-height: 40px;
  padding: 0 12px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 220px 130px 130px 130px;
  column-gap: 12px;
  align-items: center;
  border-bottom: 1px solid var(--ys-divider);
  font-size: 13px;
}

.lt-head {
  min-height: 32px;
  border-bottom: 0;
  border-radius: 6px;
  background: var(--ys-bg-subtle);
  font-size: 12px;
  color: var(--ys-text-secondary);
}

.lt-name {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.lt-name a {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.line-key {
  width: 14px;
  height: 3px;
  flex-shrink: 0;
  border-radius: 2px;
}

.lt-cloud {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: var(--ys-text-secondary);
}

.num-col {
  text-align: right;
  font-variant-numeric: tabular-nums;
}

.strong {
  font-weight: 600;
}

.reason {
  grid-column: 3 / span 3;
  text-align: right;
  font-size: 12px;
  color: var(--ys-text-muted);
}

.lt-empty {
  padding: 16px 12px;
  font-size: 13px;
  color: var(--ys-text-muted);
}

.updated {
  font-size: 12px;
  color: var(--ys-text-muted);
}
</style>
