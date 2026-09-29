<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import VChart from 'vue-echarts'
import type { EChartsOption } from 'echarts'
import { ElMessage } from 'element-plus'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import KpiCard from '@/components/KpiCard.vue'
import MetricChart, { type ChartSeries } from '@/components/MetricChart.vue'
import PageHeader from '@/components/PageHeader.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import OnboardingPanel from './OnboardingPanel.vue'
import { dashboardApi, syncApi, type DashboardSummary, type Provider, type ProviderFilter } from '@/api'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { axisLabel, chartColors, niceScale, providerColor, sansFont, tooltipBase, tooltipBox } from '@/utils/charts'
import { dayjs, formatDuration, formatNumber, formatPercent, fromNow, intervalText } from '@/utils/format'
import { cpuTone, shortAccountName } from '@/utils/resource'
import { jobStatusInfo, providerLabel, typeLabel } from '@/utils/status'

const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()

const providerOptions: { value: ProviderFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'aws', label: 'AWS' },
  { value: 'aliyun', label: '阿里云' },
]

function queryProvider(): ProviderFilter {
  const p = route.query.provider
  return p === 'aws' || p === 'aliyun' ? p : 'all'
}

const provider = ref<ProviderFilter>(queryProvider())
const summary = ref<DashboardSummary | null>(null)
const loading = ref(false)
const empty = ref(false)
const cpuTable = ref(false)
const syncing = ref(false)
const triggered = ref(false)

const showAws = computed(() => provider.value !== 'aliyun')
const showAliyun = computed(() => provider.value !== 'aws')

async function load() {
  loading.value = true
  try {
    const s = await dashboardApi.summary(provider.value)
    // 过滤到某朵云时没有账号，不代表系统里没有账号。
    empty.value = s.accounts === 0 && (provider.value === 'all' || (await dashboardApi.summary('all')).accounts === 0)
    summary.value = s
  } finally {
    loading.value = false
  }
}

watch(provider, (p) => {
  void router.replace({ query: { ...route.query, provider: p === 'all' ? undefined : p } })
  void load()
})
watch(
  () => app.syncTick,
  () => void load(),
)
onMounted(load)

const running = computed(() => (app.syncStatus?.running ?? 0) > 0)

async function syncAll() {
  syncing.value = true
  try {
    const res = await syncApi.syncAll()
    triggered.value = true
    ElMessage.success(res.total > 0 ? `已触发 ${res.total} 个账号的同步` : '所有账号都已在同步中')
    await app.refreshSync()
    setTimeout(() => (triggered.value = false), 8000)
  } finally {
    syncing.value = false
  }
}

const updatedText = computed(() => {
  const t = app.syncStatus?.last_finished_at
  return t ? `数据更新于 ${fromNow(t, app.now)}` : '尚未完成同步'
})

const scopeText = computed(() => {
  const s = summary.value
  if (!s) return ''
  const base = `${s.accounts} 个${provider.value === 'all' ? '云' : ''}账号 · ${s.regions} 个地域`
  return provider.value === 'all' ? base : `${providerLabel[provider.value]} · ${base}`
})

const kpi = computed(() => {
  const s = summary.value
  if (!s) return null
  const typesWithData = s.distribution.filter((d) => d.aws + d.aliyun > 0).length
  const names = s.account_names.map((n) => shortAccountName(n, provider.value))
  return {
    accountsSub:
      provider.value === 'all'
        ? `AWS ${s.accounts_by_cloud.aws ?? 0} · 阿里云 ${s.accounts_by_cloud.aliyun ?? 0}`
        : names.join(' · ') || '暂无账号',
    totalSub: `${typesWithData} 类资源 · ${s.regions} 个地域`,
    runSub: s.vm_total ? `运行率 ${formatPercent((s.vm_running / s.vm_total) * 100)}` : '暂无云主机',
    expSub:
      s.next_expire_in_days !== null
        ? s.next_expire_in_days === 0
          ? '最近一项今天到期'
          : `最近一项 ${s.next_expire_in_days} 天后到期`
        : provider.value === 'aws'
          ? '按需实例没有到期时间'
          : `${s.expiring_days} 天内没有到期资源`,
  }
})

// ---------- 资源分布（分组柱状图） ----------
const cloudTotals = computed(() => {
  const d = summary.value?.distribution ?? []
  return { aws: d.reduce((n, x) => n + x.aws, 0), aliyun: d.reduce((n, x) => n + x.aliyun, 0) }
})

const distOption = computed<EChartsOption>(() => {
  const d = summary.value?.distribution ?? []
  const clouds = (['aws', 'aliyun'] as Provider[]).filter((p) => (p === 'aws' ? showAws.value : showAliyun.value))
  const peak = Math.max(1, ...d.map((x) => Math.max(showAws.value ? x.aws : 0, showAliyun.value ? x.aliyun : 0)))
  const scale = niceScale(peak * 1.05, 4)
  return {
    grid: { left: 4, right: 8, top: 22, bottom: 4, outerBoundsMode: 'same', outerBoundsContain: 'axisLabel' },
    xAxis: {
      type: 'category',
      data: d.map((x) => typeLabel[x.type]),
      axisLine: { lineStyle: { color: chartColors.axis } },
      axisTick: { show: false },
      axisLabel: { color: '#3F4654', fontFamily: sansFont, fontSize: 13, margin: 10 },
    },
    yAxis: {
      type: 'value',
      min: 0,
      max: scale.max,
      interval: scale.interval,
      axisLabel: { ...axisLabel },
      splitLine: { lineStyle: { color: chartColors.grid, width: 1, type: 'solid' } },
    },
    tooltip: {
      ...tooltipBase,
      trigger: 'item',
      formatter: (p) => {
        const item = Array.isArray(p) ? p[0]! : p
        return tooltipBox(String(item.name), [
          { color: String(item.color), name: item.seriesName ?? '', value: `${formatNumber(Number(item.value))} 项` },
        ])
      },
    },
    series: clouds.map((c) => ({
      type: 'bar',
      name: providerLabel[c],
      id: c,
      data: d.map((x) => ({ value: x[c], type: x.type })),
      barWidth: 24,
      barGap: '10%',
      barCategoryGap: '40%',
      itemStyle: { color: providerColor[c], borderRadius: [4, 4, 0, 0] },
      emphasis: { itemStyle: { opacity: 0.82 } },
      label: {
        show: true,
        position: 'top',
        distance: 4,
        color: '#3F4654',
        fontFamily: axisLabel.fontFamily,
        fontSize: 11,
      },
      cursor: 'pointer',
    })),
  }
})

function onBarClick(p: { seriesId?: string; data?: unknown }) {
  const type = (p.data as { type?: string } | undefined)?.type
  if (!type) return
  void router.push({ path: '/resources', query: { type, provider: p.seriesId } })
}

// ---------- CPU 均值曲线 ----------
const cpuSeries = computed<ChartSeries[]>(() => {
  const trend = summary.value?.cpu_trend ?? []
  const single = provider.value !== 'all'
  const out: ChartSeries[] = []
  if (showAws.value) out.push({ name: 'AWS', color: chartColors.aws, area: single, points: trend.map((p) => [p.t, p.aws]) })
  if (showAliyun.value)
    out.push({ name: '阿里云', color: chartColors.aliyun, area: single, points: trend.map((p) => [p.t, p.aliyun]) })
  return out
})

const cpuWindow = computed(() => {
  const trend = summary.value?.cpu_trend ?? []
  return trend.length ? { start: trend[0]!.t, end: trend[trend.length - 1]!.t } : { start: undefined, end: undefined }
})

// ---------- 列表 ----------
function resourceLink(id: number, type = 'vm') {
  return { path: '/resources', query: { type, detail: String(id) } }
}

const syncRows = computed(() =>
  (summary.value?.recent_sync ?? []).map((s) => {
    const info = jobStatusInfo(s.status)
    let detail: string
    if (s.status === 'running') {
      detail = s.tasks_total ? `正在采集 · 已完成 ${s.tasks_done} / ${s.tasks_total} 个任务` : '正在准备同步任务'
    } else if (s.first_error) {
      const e = s.first_error
      const where = [e.region, typeLabel[e.type as keyof typeof typeLabel] ?? (e.type === 'metrics' ? '监控数据' : e.type)]
        .filter(Boolean)
        .join(' ')
      detail = `${where}：${e.message}`
    } else if (s.message && s.status !== 'success') {
      detail = s.message
    } else {
      detail = `${s.regions} 个地域 · ${formatNumber(s.items)} 项 · 用时 ${formatDuration(s.started_at, s.finished_at)}`
    }
    return {
      ...s,
      info,
      detail,
      time: s.status === 'running' ? '同步中…' : fromNow(s.finished_at ?? s.started_at, app.now),
    }
  }),
)
</script>

<template>
  <template v-if="empty">
    <PageHeader title="概览" subtitle="还没有纳管任何云账号" />
    <OnboardingPanel />
  </template>
  <template v-else>
    <PageHeader title="概览">
      <template #subtitle>
        <template v-if="summary">{{ scopeText }} · {{ updatedText }}</template>
        <template v-else>正在加载…</template>
      </template>
      <SegmentedControl v-model="provider" :options="providerOptions" label="云厂商" />
      <el-button
        v-if="auth.isAdmin"
        :type="triggered || running ? 'default' : 'primary'"
        class="sync-btn"
        :class="{ soft: triggered || running }"
        :loading="syncing"
        :disabled="running && !triggered"
        @click="syncAll"
      >
        <AppIcon v-if="!syncing" name="sync" :size="15" />
        <span>{{ running ? '同步进行中' : triggered ? '同步已触发' : '立即同步' }}</span>
      </el-button>
    </PageHeader>

    <section v-if="summary && kpi" class="kpis" aria-label="关键指标">
      <KpiCard label="纳管账号" icon="key" :value="summary.accounts" :sub="kpi.accountsSub" to="/accounts" />
      <KpiCard
        label="资源总数"
        icon="stack"
        :value="formatNumber(summary.resources)"
        :sub="kpi.totalSub"
        :to="{ path: '/resources', query: { provider: provider === 'all' ? undefined : provider } }"
      />
      <KpiCard
        label="运行中主机"
        icon="monitor"
        :value="formatNumber(summary.vm_running)"
        :suffix="` / ${formatNumber(summary.vm_total)}`"
        :sub="kpi.runSub"
        :to="{
          path: '/resources',
          query: { type: 'vm', status: 'running', provider: provider === 'all' ? undefined : provider },
        }"
      />
      <KpiCard
        label="闲置主机"
        icon="idle"
        :value="formatNumber(summary.idle)"
        :sub="`24 小时 CPU 均值低于 ${summary.idle_threshold}%`"
        :to="{ path: '/resources', query: { type: 'vm', idle: '1', provider: provider === 'all' ? undefined : provider } }"
      />
      <KpiCard
        :label="`${summary.expiring_days} 天内到期`"
        icon="clock"
        :value="formatNumber(summary.expiring)"
        :sub="kpi.expSub"
        :warn="summary.expiring > 0"
        :to="{ path: '/resources', query: { expiring: '1', provider: provider === 'all' ? undefined : provider } }"
      />
      <KpiCard
        label="可优化资源"
        icon="saving"
        :value="formatNumber(summary.waste_disks + summary.waste_eips)"
        :sub="`未挂载云盘 ${summary.waste_disks} · 未绑定 EIP ${summary.waste_eips}`"
        :warn="summary.waste_disks + summary.waste_eips > 0"
        :to="{
          path: '/optimize',
          query: {
            kind: summary.waste_disks ? 'disk' : summary.waste_eips ? 'eip' : undefined,
            provider: provider === 'all' ? undefined : provider,
          },
        }"
      />
    </section>

    <div v-if="summary" class="charts">
      <section class="ys-card chart-card">
        <div class="card-head">
          <h2 class="ys-card-title">资源分布</h2>
          <div class="legend" aria-label="图例">
            <span class="legend-item" :class="{ off: !showAws }">
              <span class="swatch" :style="{ background: chartColors.aws }" />AWS
              <span class="legend-num">{{ formatNumber(cloudTotals.aws) }}</span>
            </span>
            <span class="legend-item" :class="{ off: !showAliyun }">
              <span class="swatch" :style="{ background: chartColors.aliyun }" />阿里云
              <span class="legend-num">{{ formatNumber(cloudTotals.aliyun) }}</span>
            </span>
          </div>
        </div>
        <div class="chart-box" :class="{ dim: loading }" role="img" aria-label="各类资源在 AWS 与阿里云的数量">
          <VChart :option="distOption" autoresize class="chart" @click="onBarClick" />
        </div>
      </section>

      <section class="ys-card chart-card">
        <div class="card-head">
          <h2 class="ys-card-title">主机 CPU 均值 · 近 24 小时</h2>
          <div class="legend" aria-label="图例">
            <span class="legend-item" :class="{ off: !showAws }">
              <span class="line-key" :style="{ background: chartColors.aws }" />AWS
            </span>
            <span class="legend-item" :class="{ off: !showAliyun }">
              <span class="line-key" :style="{ background: chartColors.aliyun }" />阿里云
            </span>
            <el-tooltip :content="cpuTable ? '切换为曲线' : '切换为表格'" placement="top">
              <button
                type="button"
                class="icon-btn"
                :aria-label="cpuTable ? '切换为曲线' : '切换为表格'"
                :aria-pressed="cpuTable"
                @click="cpuTable = !cpuTable"
              >
                <AppIcon :name="cpuTable ? 'chart' : 'table'" :size="16" />
              </button>
            </el-tooltip>
          </div>
        </div>
        <div v-if="cpuTable" class="cpu-table">
          <table>
            <thead>
              <tr>
                <th>时间</th>
                <th v-if="showAws">AWS</th>
                <th v-if="showAliyun">阿里云</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in [...(summary.cpu_trend ?? [])].reverse()" :key="p.t">
                <td class="mono">{{ dayjs(p.t).format('MM-DD HH:00') }}</td>
                <td v-if="showAws" class="num">{{ formatPercent(p.aws) }}</td>
                <td v-if="showAliyun" class="num">{{ formatPercent(p.aliyun) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <MetricChart
          v-else
          :series="cpuSeries"
          unit="percent"
          :height="222"
          :start="cpuWindow.start"
          :end="cpuWindow.end"
          end-marker
          :dim="loading"
          label="近 24 小时 AWS 与阿里云主机 CPU 均值曲线"
          empty-text="暂无 CPU 数据，完成一次同步后生成"
        />
      </section>
    </div>

    <div v-if="summary" class="lists">
      <section class="ys-card list-card">
        <div class="card-head">
          <h2 class="ys-card-title">CPU 使用率 Top 5</h2>
          <span class="card-note">近 1 小时</span>
          <RouterLink class="card-link" :to="{ path: '/resources', query: { type: 'vm', sort: 'cpu_1h:desc' } }"
            >查看全部</RouterLink
          >
        </div>
        <div v-if="summary.top_cpu.length" class="rows">
          <div v-for="(r, i) in summary.top_cpu" :key="r.id" class="row top-row">
            <span class="rank">{{ i + 1 }}</span>
            <span class="grow">
              <RouterLink :to="resourceLink(r.id)" class="name">{{ r.name }}</RouterLink>
              <span class="meta"><CloudTag :provider="r.provider" small />{{ r.region }}</span>
            </span>
            <span class="cpu">
              <span class="cpu-val" :style="{ color: cpuTone(r.cpu).text }">{{ formatPercent(r.cpu) }}</span>
              <span class="bar"><span :style="{ width: `${Math.min(100, r.cpu)}%`, background: cpuTone(r.cpu).bar }" /></span>
            </span>
          </div>
        </div>
        <div v-else class="list-empty">
          <AppIcon name="monitor" :size="28" />
          <span>暂无运行中主机的 CPU 数据</span>
        </div>
      </section>

      <section class="ys-card list-card">
        <div class="card-head">
          <h2 class="ys-card-title">即将到期</h2>
          <span class="card-note">包年包月 · {{ summary.expiring_days }} 天内</span>
          <RouterLink
            v-if="summary.expiring"
            class="card-link"
            :to="{ path: '/resources', query: { expiring: '1', provider: provider === 'all' ? undefined : provider } }"
            >全部 {{ summary.expiring }} 项</RouterLink
          >
        </div>
        <div v-if="summary.expiring_items.length" class="rows">
          <div v-for="e in summary.expiring_items" :key="e.id" class="row exp-row">
            <span class="grow">
              <RouterLink :to="resourceLink(e.id, e.type)" class="name">{{ e.name }}</RouterLink>
              <span class="meta-text">
                {{ typeLabel[e.type] }} · {{ providerLabel[e.provider] }} {{ e.region }} ·
                {{ dayjs(e.expire_at).format('MM-DD') }} 到期
              </span>
            </span>
            <span class="days" :class="{ urgent: e.days <= 10 }">{{ e.days === 0 ? '今天' : `${e.days} 天` }}</span>
          </div>
        </div>
        <div v-else class="list-empty">
          <AppIcon name="clock" :size="28" />
          <span>{{ provider === 'aws' ? 'AWS 账号下没有包年包月资源' : `${summary.expiring_days} 天内没有到期的资源` }}</span>
          <span class="list-empty-sub">{{ provider === 'aws' ? '按需实例没有到期时间' : '包年包月资源临近到期时会出现在这里' }}</span>
        </div>
      </section>

      <section class="ys-card list-card">
        <div class="card-head">
          <h2 class="ys-card-title">最近同步</h2>
          <span class="card-note">{{ intervalText(app.meta.sync_interval_minutes) }}自动同步</span>
          <RouterLink class="card-link" to="/accounts">同步历史</RouterLink>
        </div>
        <div v-if="syncRows.length" class="rows">
          <div v-for="s in syncRows" :key="s.job_id" class="row sync-row">
            <span class="sync-dot" :class="s.info.tone" aria-hidden="true" />
            <span class="grow">
              <span class="sync-title">
                <span class="sync-name">{{ s.account_name }}</span>
                <span class="sync-status" :class="s.info.tone">{{ s.info.label }}</span>
              </span>
              <el-tooltip :content="s.detail" placement="top" :show-after="400">
                <span class="meta-text ellipsis">{{ s.detail }}</span>
              </el-tooltip>
            </span>
            <span class="sync-time">{{ s.time }}</span>
          </div>
        </div>
        <div v-else class="list-empty">
          <AppIcon name="sync" :size="28" />
          <span>还没有同步记录</span>
        </div>
      </section>
    </div>
  </template>
</template>

<style scoped>
.sync-btn {
  height: 38px;
  padding: 0 16px;
}

.sync-btn :deep(span) {
  display: inline-flex;
  align-items: center;
  gap: 8px;
}

.sync-btn.soft {
  --el-button-bg-color: var(--ys-primary-soft);
  --el-button-border-color: var(--ys-primary-soft);
  --el-button-text-color: var(--ys-primary);
  --el-button-hover-bg-color: #e3e9fb;
  --el-button-hover-border-color: #e3e9fb;
  --el-button-hover-text-color: var(--ys-primary);
  --el-button-disabled-bg-color: var(--ys-primary-soft);
  --el-button-disabled-border-color: var(--ys-primary-soft);
  --el-button-disabled-text-color: var(--ys-primary);
}

.kpis {
  display: grid;
  grid-template-columns: repeat(6, minmax(0, 1fr));
  gap: 16px;
}

.charts {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.chart-card {
  padding: 20px;
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

.card-head {
  min-height: 22px;
  display: flex;
  align-items: center;
  gap: 10px;
}

.chart-card .card-head {
  justify-content: space-between;
}

.legend {
  display: flex;
  align-items: center;
  gap: 16px;
  font-size: 12px;
  color: var(--ys-text-secondary);
}

.legend-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  transition: opacity 0.2s;
}

.legend-item.off {
  opacity: 0.35;
}

.legend-num {
  color: var(--ys-text);
  font-weight: 500;
}

.swatch {
  width: 10px;
  height: 10px;
  border-radius: 3px;
}

.line-key {
  width: 14px;
  height: 3px;
  border-radius: 2px;
}

.icon-btn {
  width: 28px;
  height: 28px;
  margin-left: -4px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ys-text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.icon-btn:hover,
.icon-btn[aria-pressed='true'] {
  background: var(--ys-bg-muted);
  color: var(--ys-text);
}

.chart-box {
  height: 222px;
  transition: opacity 0.2s;
}

.chart-box.dim {
  opacity: 0.5;
}

.chart {
  width: 100%;
  height: 100%;
}

.cpu-table {
  height: 222px;
  overflow-y: auto;
  border: 1px solid var(--ys-divider);
  border-radius: 8px;
}

.cpu-table table {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}

.cpu-table th {
  position: sticky;
  top: 0;
  padding: 6px 12px;
  background: var(--ys-bg-subtle);
  color: var(--ys-text-secondary);
  font-weight: 500;
  text-align: right;
}

.cpu-table th:first-child,
.cpu-table td:first-child {
  text-align: left;
}

.cpu-table td {
  padding: 5px 12px;
  border-top: 1px solid var(--ys-divider);
  text-align: right;
}

.lists {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.list-card {
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
  min-width: 0;
  min-height: 272px;
}

.card-note {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.card-link {
  margin-left: auto;
  font-size: 12px;
}

.rows {
  display: flex;
  flex-direction: column;
}

.row {
  display: flex;
  align-items: center;
  gap: 10px;
  border-bottom: 1px solid var(--ys-divider);
}

.row:last-child {
  border-bottom-color: transparent;
}

.top-row {
  height: 42px;
}

.exp-row,
.sync-row {
  height: 52px;
  gap: 12px;
}

.grow {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.rank {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  border-radius: 5px;
  background: var(--ys-bg-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--ys-font-mono);
  font-size: 11px;
  color: var(--ys-text-label);
}

.name {
  font-size: 13px;
  font-weight: 500;
  color: var(--ys-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.name:hover {
  color: var(--ys-primary);
}

.meta {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--ys-text-muted);
}

.meta-text {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cpu {
  width: 78px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
}

.cpu-val {
  font-size: 13px;
  font-weight: 600;
}

.bar {
  display: block;
  width: 78px;
  height: 4px;
  border-radius: 2px;
  background: var(--ys-divider);
}

.bar span {
  display: block;
  height: 4px;
  border-radius: 2px;
}

.days {
  padding: 3px 9px;
  border-radius: 6px;
  font-size: 12px;
  font-weight: 600;
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
  white-space: nowrap;
}

.days.urgent {
  background: var(--ys-warn-bg);
  color: var(--ys-warn-fg);
}

.sync-dot {
  width: 8px;
  height: 8px;
  flex-shrink: 0;
  border-radius: 50%;
}

.sync-dot.ok {
  background: var(--ys-ok-dot);
}
.sync-dot.warn {
  background: var(--ys-warn-dot);
}
.sync-dot.err {
  background: var(--ys-err-dot);
}
.sync-dot.busy {
  background: var(--ys-busy-dot);
}
.sync-dot.off {
  background: var(--ys-off-dot);
}

.sync-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
  min-width: 0;
}

.sync-name {
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.sync-status {
  font-size: 12px;
  white-space: nowrap;
}

.sync-status.ok {
  color: var(--ys-ok-fg);
}
.sync-status.warn {
  color: var(--ys-warn-fg);
}
.sync-status.err {
  color: var(--ys-err-fg);
}
.sync-status.busy {
  color: var(--ys-busy-fg);
}
.sync-status.off {
  color: var(--ys-off-fg);
}

.sync-time {
  font-size: 12px;
  color: var(--ys-text-muted);
  white-space: nowrap;
}

.list-empty {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  color: var(--ys-text-muted);
  font-size: 13px;
  text-align: center;
}

.list-empty :deep(svg) {
  color: #b5bbc6;
}

.list-empty-sub {
  font-size: 12px;
}

@media (max-width: 1360px) {
  .kpis {
    grid-template-columns: repeat(3, minmax(0, 1fr));
  }
}
</style>
