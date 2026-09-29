<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import axios from 'axios'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import MetricChart, { type ChartSeries } from '@/components/MetricChart.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import StatusTag from '@/components/StatusTag.vue'
import {
  changeApi,
  errorMessage,
  metricApi,
  resourceApi,
  type MetricsResult,
  type RangeKey,
  type ResourceChange,
  type ResourceDetail,
} from '@/api'
import { useAppStore } from '@/stores/app'
import { actionInfo, changeText } from '@/utils/change'
import { chartColors } from '@/utils/charts'
import { copyText } from '@/utils/clipboard'
import { dayjs, formatBytes, formatDateTime, formatMetric, formatNumber } from '@/utils/format'
import {
  bucketClass,
  bucketObjects,
  bucketSize,
  engineLabel,
  expiryInfo,
  extraNum,
  extraStr,
  formatUID,
  ips,
  lbKindLabel,
  networkLabel,
  rdsStorage,
  shortAccountName,
  vmSize,
  zoneLabel,
} from '@/utils/resource'
import { chargeLabel, providerLabel, resourceStatusInfo } from '@/utils/status'

const props = defineProps<{ id: number | null }>()
const emit = defineEmits<{ close: [] }>()

const router = useRouter()
const app = useAppStore()

type Tab = 'info' | 'tags' | 'monitor' | 'changes' | 'raw'

const detail = ref<ResourceDetail | null>(null)
const loadingDetail = ref(false)
const tab = ref<Tab>('monitor')
const range = ref<RangeKey>('6h')
const metrics = ref<MetricsResult | null>(null)
const metricsLoading = ref(false)
const metricsError = ref('')
let metricsCtl: AbortController | null = null

const open = computed(() => props.id !== null)

watch(
  () => props.id,
  async (id) => {
    metrics.value = null
    metricsError.value = ''
    if (id === null) return
    loadingDetail.value = true
    try {
      const d = await resourceApi.get(id)
      if (props.id !== id) return
      detail.value = d
      tab.value = d.supported_metrics.length ? 'monitor' : 'info'
    } catch {
      if (props.id === id) emit('close')
    } finally {
      loadingDetail.value = false
    }
  },
  { immediate: true },
)

async function loadMetrics() {
  const d = detail.value
  if (!d || !d.supported_metrics.length || tab.value !== 'monitor') return
  metricsCtl?.abort()
  const ctl = new AbortController()
  metricsCtl = ctl
  metricsLoading.value = true
  metricsError.value = ''
  try {
    const res = await metricApi.forResource(d.id, range.value, [], ctl.signal)
    if (metricsCtl === ctl) metrics.value = res
  } catch (err) {
    if (!axios.isCancel(err) && metricsCtl === ctl) metricsError.value = errorMessage(err)
  } finally {
    if (metricsCtl === ctl) metricsLoading.value = false
  }
}

watch([() => detail.value?.id, range, tab], () => void loadMetrics())

// ---------- 变更历史 ----------
const history = ref<ResourceChange[] | null>(null)
const historyTotal = ref(0)
let historyOf = 0 // 已加载历史的资源，切换资源或重新打开抽屉时重新加载

watch(
  () => props.id,
  () => (historyOf = 0),
)
watch([() => detail.value?.id, tab], async () => {
  const d = detail.value
  if (!d || tab.value !== 'changes' || historyOf === d.id) return
  historyOf = d.id
  history.value = null
  try {
    const res = await changeApi.list({ account_id: d.account_id, type: d.type, resource_id: d.resource_id, page_size: 20 })
    if (detail.value?.id === d.id) {
      history.value = res.items
      historyTotal.value = res.total
    }
  } catch {
    history.value = []
  }
})

const rangeOptions: { value: RangeKey; label: string }[] = [
  { value: '1h', label: '1 小时' },
  { value: '6h', label: '6 小时' },
  { value: '24h', label: '24 小时' },
  { value: '7d', label: '7 天' },
]
const rangeLabel: Record<RangeKey, string> = { '1h': '1 小时', '6h': '6 小时', '24h': '24 小时', '7d': '7 天' }

const tabs = computed(() => {
  const d = detail.value
  const list: { value: Tab; label: string }[] = [
    { value: 'info', label: '基本信息' },
    { value: 'tags', label: `标签 · ${d ? Object.keys(d.tags ?? {}).length : 0}` },
  ]
  if (d?.supported_metrics.length) list.push({ value: 'monitor', label: '监控' })
  list.push({ value: 'changes', label: '变更历史' })
  list.push({ value: 'raw', label: '原始数据' })
  return list
})

const account = computed(() => (detail.value ? shortAccountName(detail.value.account_name, detail.value.provider) : ''))

const headline = computed(() => {
  const d = detail.value
  if (!d) return ''
  const where = [d.region, zoneLabel(d.zone, d.provider)].filter(Boolean).join(' ')
  return [account.value, where, d.type === 'bucket' ? '' : d.spec].filter(Boolean).join(' · ')
})

// ---------- 监控面板 ----------
interface Panel {
  title: string
  keys: string[]
  labels: string[]
  note?: string
  /** 没有数据时的提示，默认用 note。 */
  empty?: string
  colors: string[]
}

const panels = computed<Panel[]>(() => {
  const d = detail.value
  if (!d) return []
  const blue = chartColors.series[0]!
  const orange = chartColors.series[1]!
  const green = chartColors.series[2]!
  let defs: Panel[] = []
  if (d.type === 'vm') {
    defs = [
      { title: 'CPU 使用率', keys: ['cpu_util'], labels: ['当前'], colors: [blue] },
      {
        title: '内存使用率',
        keys: ['mem_util'],
        labels: ['当前'],
        colors: [green],
        note: d.provider === 'aws' ? '需安装 CloudWatch Agent' : '需安装云监控插件',
      },
      { title: '网络带宽', keys: ['net_in', 'net_out'], labels: ['入', '出'], colors: [blue, orange] },
      { title: '磁盘读写', keys: ['disk_read', 'disk_write'], labels: ['读', '写'], colors: [blue, orange] },
    ]
  } else if (d.type === 'rds') {
    defs = [
      { title: 'CPU 使用率', keys: ['cpu_util'], labels: ['当前'], colors: [blue] },
      { title: '磁盘使用率', keys: ['disk_util'], labels: ['当前'], colors: [green] },
      { title: '内存使用率', keys: ['mem_util'], labels: ['当前'], colors: [green] },
      { title: '可用内存', keys: ['free_mem'], labels: ['当前'], colors: [green] },
      { title: '连接数', keys: ['connections'], labels: ['当前'], colors: [blue] },
    ]
  } else if (d.type === 'lb') {
    defs = [
      { title: 'QPS', keys: ['qps'], labels: ['当前'], colors: [blue] },
      { title: '活跃连接数', keys: ['active_conn'], labels: ['当前'], colors: [blue] },
      { title: '新建连接数', keys: ['new_conn'], labels: ['当前'], colors: [blue] },
      { title: '流量', keys: ['traffic'], labels: ['当前'], colors: [blue] },
    ]
  } else if (d.type === 'bucket') {
    // 存储量和对象数是云厂商定时统计的：阿里云每小时一次，AWS 每天一次。
    const daily = d.provider === 'aws'
    defs = [
      {
        title: '存储量',
        keys: ['storage'],
        labels: ['当前'],
        colors: [blue],
        note: daily ? '每天统计一次' : '每小时统计一次',
        empty: daily ? 'AWS 每天统计一次，请切到 7 天查看' : '每小时统计一次，请切到 6 小时以上查看',
      },
      {
        title: '对象数',
        keys: ['objects'],
        labels: ['当前'],
        colors: [green],
        note: '每天统计一次',
        empty: 'AWS 每天统计一次，请切到 7 天查看',
      },
      { title: '请求数', keys: ['requests'], labels: ['当前'], colors: [blue] },
      { title: '公网流量', keys: ['net_in', 'net_out'], labels: ['流入', '流出'], colors: [blue, orange] },
    ]
  }
  const supported = new Set(d.supported_metrics)
  return defs.filter((p) => p.keys.some((k) => supported.has(k)))
})

function seriesOf(key: string) {
  return metrics.value?.series.find((s) => s.key === key)
}

function lastValue(key: string): number | null {
  const pts = seriesOf(key)?.points ?? []
  return pts.length ? pts[pts.length - 1]![1] : null
}

function panelSeries(p: Panel): ChartSeries[] {
  return p.keys.map((k, i) => ({
    name: p.keys.length > 1 ? p.labels[i]! : p.title,
    color: p.colors[i]!,
    area: p.keys.length === 1,
    points: seriesOf(k)?.points ?? [],
  }))
}

function panelUnit(p: Panel): string {
  return seriesOf(p.keys[0]!)?.unit ?? 'count'
}

const grain = computed(() => {
  const s = metrics.value?.period ?? 0
  if (!s) return ''
  if (s < 3600) return `${Math.round(s / 60)} 分钟`
  return `${Math.round(s / 3600)} 小时`
})

const timeWindow = computed(() => {
  const m = metrics.value
  return m ? { start: dayjs(m.start).valueOf(), end: dayjs(m.end).valueOf() } : { start: undefined, end: undefined }
})

const cpuStats = computed(() => {
  const pts = (seriesOf('cpu_util')?.points ?? []).map((p) => p[1])
  if (!pts.length) return null
  const avg = pts.reduce((a, b) => a + b, 0) / pts.length
  const max = Math.max(...pts)
  return { avg, max }
})

const bandwidthCap = computed(() => (detail.value ? extraNum(detail.value, 'bandwidth_out_mbps') : null))

// ---------- 基本信息 ----------
const idLabel: Record<string, string> = { bucket: '存储桶', disk: '云盘 ID', eip: 'EIP ID' }

const info = computed(() => {
  const d = detail.value
  if (!d) return []
  const st = resourceStatusInfo(d.status)
  const exp = expiryInfo(d, 30, app.now)
  const rows: { k: string; v: string; mono?: boolean }[] = [
    { k: idLabel[d.type] ?? '实例 ID', v: d.resource_id, mono: true },
    {
      k: '云账号',
      v: `${providerLabel[d.provider]} · ${account.value}${d.cloud_account_uid ? `（UID ${formatUID(d.cloud_account_uid, d.provider)}）` : ''}`,
    },
    { k: d.zone ? '地域 / 可用区' : '地域', v: [d.region, d.zone].filter(Boolean).join(' / ') },
    { k: '状态', v: d.raw_status && d.raw_status !== st.label ? `${st.label}（${d.raw_status}）` : st.label },
  ]
  const sgs = Array.isArray(d.extra?.security_groups) ? (d.extra.security_groups as string[]).join(', ') : ''
  if (d.type === 'vm') {
    const size = vmSize(d)
    rows.push(
      { k: '规格', v: [d.spec, size].filter(Boolean).join(' · ') },
      { k: '操作系统', v: extraStr(d, 'os') || '—' },
      { k: '私网 IP', v: ips(d.private_ip).join(', ') || '—', mono: true },
      { k: '公网 IP', v: ips(d.public_ip).join(', ') || '—', mono: true },
      {
        k: d.provider === 'aws' ? 'VPC / 子网' : 'VPC / 交换机',
        v: [d.vpc_id, extraStr(d, 'subnet_id') || extraStr(d, 'vswitch_id')].filter(Boolean).join(' / ') || '—',
        mono: true,
      },
      { k: '安全组', v: sgs || '—', mono: true },
      { k: '镜像', v: extraStr(d, 'image_id') || '—', mono: true },
    )
    const bw = extraNum(d, 'bandwidth_out_mbps')
    if (bw) rows.push({ k: '公网带宽上限', v: `${bw} Mbps` })
  } else if (d.type === 'rds') {
    rows.push(
      { k: '引擎', v: engineLabel(d) || '—' },
      { k: '规格', v: d.spec || '—', mono: true },
      { k: '存储', v: rdsStorage(d) },
      { k: '连接地址', v: extraStr(d, 'endpoint') || '—', mono: true },
      { k: 'VPC', v: d.vpc_id || '—', mono: true },
    )
    const maxConn = extraNum(d, 'max_connections')
    if (maxConn) rows.push({ k: '最大连接数', v: formatNumber(maxConn) })
  } else if (d.type === 'lb') {
    rows.push(
      { k: '类型', v: lbKindLabel(d) },
      { k: '服务地址', v: extraStr(d, 'address') || '—', mono: true },
      { k: '网络类型', v: networkLabel(d) },
      { k: 'VPC', v: d.vpc_id || '—', mono: true },
    )
    const bw = extraNum(d, 'bandwidth_mbps')
    if (bw) rows.push({ k: '带宽', v: `${bw} Mbps` })
  } else if (d.type === 'disk') {
    rows.push({ k: '规格', v: d.spec || '—' })
    const detached = extraStr(d, 'detached_at')
    if (detached) rows.push({ k: '卸载时间', v: formatDateTime(detached) })
  } else if (d.type === 'eip') {
    rows.push(
      { k: '公网 IP', v: ips(d.public_ip).join(', ') || '—', mono: true },
      { k: '带宽', v: d.spec || '—' },
    )
  } else {
    const size = extraNum(d, 'size_bytes')
    rows.push(
      { k: '存储类型', v: bucketClass(d) },
      { k: '容量', v: size === null ? '—' : `${bucketSize(d)}（${formatBytes(size, 0)}）` },
      { k: '对象数', v: bucketObjects(d) },
    )
  }
  if (d.type !== 'bucket') {
    rows.push({ k: '计费方式', v: chargeLabel[d.charge_type] ?? '—' })
    rows.push({
      k: '到期时间',
      v: d.expire_at ? `${formatDateTime(d.expire_at).slice(0, 16)}（${exp.sub || '包年包月'}）` : '—',
    })
  }
  rows.push(
    { k: '创建时间', v: formatDateTime(d.cloud_created_at) },
    { k: '首次发现', v: formatDateTime(d.created_at) },
    { k: '最后同步', v: formatDateTime(d.synced_at) },
  )
  return rows
})

const tags = computed(() =>
  Object.entries(detail.value?.tags ?? {}).sort(([a], [b]) => a.localeCompare(b)),
)

const rawJson = computed(() => {
  const d = detail.value
  if (!d) return ''
  const { supported_metrics: _s, idle: _i, ...rest } = d
  return JSON.stringify(rest, null, 2)
})

function compare() {
  const d = detail.value
  if (!d) return
  void router.push({ path: '/monitor', query: { type: d.type, ids: String(d.id), metric: d.supported_metrics[0] } })
}
</script>

<template>
  <el-drawer
    :model-value="open"
    size="680px"
    :with-header="false"
    class="res-drawer"
    :aria-label="detail ? `资源详情：${detail.name}` : '资源详情'"
    @close="emit('close')"
  >
    <div v-loading="loadingDetail && !detail" class="drawer">
      <template v-if="detail">
        <div class="head">
          <div class="head-row">
            <div class="head-main">
              <div class="title-line">
                <h2 class="ellipsis">{{ detail.name || detail.resource_id }}</h2>
                <CloudTag :provider="detail.provider" />
                <StatusTag v-if="detail.type !== 'bucket'" :status="detail.status" />
              </div>
              <div class="id-line">
                <span class="mono id">{{ detail.resource_id }}</span>
                <button type="button" class="icon-btn" aria-label="复制实例 ID" @click="copyText(detail.resource_id, '已复制 ID')">
                  <AppIcon name="copy" :size="15" />
                </button>
                <span class="ellipsis">{{ headline }}</span>
              </div>
            </div>
            <button type="button" class="close" aria-label="关闭详情" @click="emit('close')">
              <AppIcon name="close" :size="18" />
            </button>
          </div>
          <div class="tabs" role="tablist" aria-label="详情分类">
            <button
              v-for="t in tabs"
              :key="t.value"
              type="button"
              role="tab"
              class="tab"
              :class="{ on: tab === t.value }"
              :aria-selected="tab === t.value"
              @click="tab = t.value"
            >
              {{ t.label }}
            </button>
          </div>
        </div>

        <div class="body">
          <!-- 监控 -->
          <template v-if="tab === 'monitor'">
            <div class="mon-bar">
              <SegmentedControl v-model="range" :options="rangeOptions" label="时间范围" />
              <span class="source">
                数据来源：{{ detail.provider === 'aws' ? 'AWS CloudWatch' : '阿里云云监控' }}<template v-if="grain">
                  · 粒度 {{ grain }}</template
                >
              </span>
            </div>
            <el-alert v-if="metricsError" :title="metricsError" type="warning" :closable="false" show-icon />
            <div class="grid">
              <section v-for="p in panels" :key="p.title" class="panel">
                <div class="panel-head">
                  <h3>{{ p.title }}</h3>
                  <span class="panel-note">{{ p.note ?? '' }}</span>
                </div>
                <div class="panel-values">
                  <span v-for="(k, i) in p.keys" :key="k" class="pv">
                    <span class="line-key" :style="{ background: p.colors[i] }" />{{ p.labels[i] }}
                    <strong>{{ formatMetric(lastValue(k), seriesOf(k)?.unit ?? '') }}</strong>
                  </span>
                </div>
                <MetricChart
                  :series="panelSeries(p)"
                  :unit="panelUnit(p)"
                  :height="132"
                  :start="timeWindow.start"
                  :end="timeWindow.end"
                  compact
                  :dim="metricsLoading && !!metrics"
                  :label="`${p.title}趋势`"
                  :empty-text="metricsLoading ? '加载中…' : (p.empty ?? (p.note ? `暂无数据（${p.note}）` : '暂无数据'))"
                />
              </section>
            </div>
            <div class="chips">
              <span v-if="cpuStats" class="stat">
                CPU {{ rangeLabel[range] }}均值 <strong>{{ formatMetric(cpuStats.avg, 'percent') }}</strong>
              </span>
              <span v-if="cpuStats" class="stat">
                CPU {{ rangeLabel[range] }}峰值
                <strong :class="{ hot: cpuStats.max >= 85 }">{{ formatMetric(cpuStats.max, 'percent') }}</strong>
              </span>
              <span v-if="bandwidthCap" class="stat">
                带宽上限 <strong>{{ bandwidthCap }} Mbps</strong>
              </span>
            </div>
          </template>

          <!-- 基本信息 -->
          <dl v-else-if="tab === 'info'" class="info">
            <div v-for="row in info" :key="row.k" class="info-item">
              <dt>{{ row.k }}</dt>
              <dd :class="{ mono: row.mono }">{{ row.v }}</dd>
            </div>
          </dl>

          <!-- 标签 -->
          <template v-else-if="tab === 'tags'">
            <p class="hint">
              标签来自云厂商，随同步更新；可在资源中心用 <span class="mono strong">env:prod</span> 这样的写法筛选。
            </p>
            <div v-if="tags.length" class="tag-list">
              <span v-for="[k, v] in tags" :key="k" class="kv">
                <span class="k">{{ k }}</span>
                <span class="v">{{ v || '（空）' }}</span>
              </span>
            </div>
            <p v-else class="hint muted">该资源没有标签。</p>
          </template>

          <!-- 变更历史 -->
          <template v-else-if="tab === 'changes'">
            <p class="hint">
              每次同步与上一次对比记录的变化，只保留最近 {{ app.meta.change_retention_days }} 天<template v-if="historyTotal > 20">
                ，这里显示最近 20 条（共 {{ historyTotal }} 条）</template
              >。
            </p>
            <div v-if="history === null" v-loading="true" class="history-loading" />
            <ol v-else-if="history.length" class="history">
              <li v-for="c in history" :key="c.id" class="history-item">
                <span class="history-time">{{ dayjs(c.created_at).format('MM-DD HH:mm') }}</span>
                <span class="history-act" :class="actionInfo[c.action].tone">{{ actionInfo[c.action].label }}</span>
                <span class="history-body">
                  <template v-if="c.changes.length">
                    <span v-for="f in c.changes" :key="f.field" class="history-line">{{ changeText(f) }}</span>
                  </template>
                  <span v-else class="history-line muted">{{ c.action === 'created' ? '同步时首次发现' : '同步时已不存在' }}</span>
                </span>
              </li>
            </ol>
            <p v-else class="hint muted">暂无变更记录。账号首次同步不记录新增，之后的变化会显示在这里。</p>
          </template>

          <!-- 原始数据 -->
          <template v-else>
            <div class="raw-bar">
              <span>归一化后的资源数据（云厂商的原始字段保存在 extra 中）</span>
              <el-button size="small" @click="copyText(rawJson, '已复制 JSON')">复制 JSON</el-button>
            </div>
            <pre class="raw">{{ rawJson }}</pre>
          </template>
        </div>

        <div class="foot">
          <span class="ellipsis">
            最后同步 {{ dayjs(detail.synced_at).format('HH:mm:ss') }} · 来自账号「{{ detail.account_name }}」
          </span>
          <el-button
            v-if="detail.supported_metrics.length && detail.type !== 'bucket'"
            class="compare"
            @click="compare"
          >
            <AppIcon name="monitor" :size="15" />
            <span class="btn-gap">在监控中心对比</span>
          </el-button>
        </div>
      </template>
    </div>
  </el-drawer>
</template>

<style scoped>
.drawer {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.head {
  padding: 22px 24px 0;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.head-row {
  display: flex;
  align-items: flex-start;
  gap: 12px;
}

.head-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.title-line {
  display: flex;
  align-items: center;
  gap: 10px;
  min-width: 0;
}

.title-line h2 {
  font-size: 20px;
  font-weight: 600;
  min-width: 0;
}

.id-line {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  color: var(--ys-text-secondary);
  min-width: 0;
}

.id {
  font-size: 12px;
  color: var(--ys-text);
  white-space: nowrap;
}

.icon-btn {
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ys-text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.icon-btn:hover {
  background: var(--ys-bg-muted);
  color: var(--ys-text);
}

.close {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  border: 0;
  border-radius: 8px;
  background: var(--ys-bg-page);
  color: var(--ys-text-label);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.close:hover {
  background: var(--ys-segment-bg);
}

.tabs {
  height: 42px;
  display: flex;
  align-items: stretch;
  gap: 26px;
  border-bottom: 1px solid var(--ys-divider);
}

.tab {
  padding: 0 2px;
  border: 0;
  background: transparent;
  color: var(--ys-text-label);
  font-size: 14px;
  cursor: pointer;
}

.tab.on {
  color: var(--ys-primary);
  font-weight: 600;
  box-shadow: inset 0 -2px 0 var(--ys-primary);
}

.body {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 18px 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.mon-bar {
  display: flex;
  align-items: center;
  gap: 12px;
}

.source {
  margin-left: auto;
  font-size: 12px;
  color: var(--ys-text-muted);
}

.grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.panel {
  padding: 14px 12px 10px;
  display: flex;
  flex-direction: column;
  gap: 6px;
  border: 1px solid var(--ys-border);
  border-radius: 10px;
  min-width: 0;
}

.panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 0 2px;
}

.panel-head h3 {
  font-size: 13px;
  font-weight: 600;
}

.panel-note {
  font-size: 11px;
  color: var(--ys-text-muted);
}

.panel-values {
  display: flex;
  align-items: baseline;
  flex-wrap: wrap;
  gap: 4px 12px;
  padding: 0 2px;
  font-size: 12px;
  color: var(--ys-text-secondary);
}

.pv {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.pv strong {
  font-size: 17px;
  font-weight: 600;
  color: var(--ys-text);
}

.line-key {
  width: 10px;
  height: 3px;
  border-radius: 2px;
}

.chips {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
}

.stat {
  padding: 6px 10px;
  border-radius: 8px;
  background: var(--ys-bg-subtle);
  border: 1px solid var(--ys-divider);
  font-size: 12px;
  color: var(--ys-text-label);
}

.stat strong {
  color: var(--ys-text);
  font-weight: 600;
}

.stat strong.hot {
  color: var(--ys-err-fg);
}

.info {
  margin: 0;
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  column-gap: 24px;
}

.info-item {
  padding: 12px 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
  border-bottom: 1px solid var(--ys-divider);
  min-width: 0;
}

.info-item dt {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.info-item dd {
  margin: 0;
  font-size: 13px;
  color: var(--ys-text);
  word-break: break-all;
}

.info-item dd.mono {
  font-size: 12px;
}

.hint {
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.strong {
  color: var(--ys-text);
}

.tag-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
}

.kv {
  display: flex;
  align-items: stretch;
  border: 1px solid #dde1e7;
  border-radius: 6px;
  overflow: hidden;
  font-family: var(--ys-font-mono);
  font-size: 12px;
}

.kv .k {
  padding: 5px 8px;
  background: var(--ys-bg-page);
  color: var(--ys-text-secondary);
}

.kv .v {
  padding: 5px 8px;
  background: #fff;
  color: var(--ys-text);
}

.hint.muted,
.history-line.muted {
  color: var(--ys-text-muted);
}

.history-loading {
  height: 80px;
}

.history {
  margin: 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
}

.history-item {
  padding: 10px 0;
  display: flex;
  align-items: flex-start;
  gap: 12px;
  border-bottom: 1px solid var(--ys-divider);
  font-size: 13px;
}

.history-time {
  flex-shrink: 0;
  width: 84px;
  font-size: 12px;
  line-height: 20px;
  color: var(--ys-text-label);
}

.history-act {
  flex-shrink: 0;
  padding: 0 8px;
  border-radius: var(--ys-radius-tag);
  font-size: 12px;
  line-height: 20px;
}

.history-act.ok {
  background: var(--ys-ok-bg);
  color: var(--ys-ok-fg);
}

.history-act.busy {
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
}

.history-act.err {
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
}

.history-body {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
  line-height: 20px;
  word-break: break-all;
}

.raw-bar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  font-size: 12px;
  color: var(--ys-text-muted);
}

.raw {
  margin: 0;
  padding: 14px 16px;
  border-radius: 8px;
  background: #151821;
  color: #e4e7ec;
  font-family: var(--ys-font-mono);
  font-size: 12px;
  line-height: 1.65;
  white-space: pre;
  overflow: auto;
}

.foot {
  height: 64px;
  flex-shrink: 0;
  padding: 0 24px;
  display: flex;
  align-items: center;
  gap: 12px;
  border-top: 1px solid var(--ys-divider);
  font-size: 12px;
  color: var(--ys-text-muted);
}

.compare {
  margin-left: auto;
}

.btn-gap {
  margin-left: 8px;
}

.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>

<style>
.res-drawer .el-drawer__body {
  padding: 0;
}

.res-drawer {
  box-shadow: -16px 0 40px rgba(21, 24, 33, 0.18);
}
</style>
