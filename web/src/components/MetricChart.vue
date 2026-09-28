<script setup lang="ts">
import { computed } from 'vue'
import VChart from 'vue-echarts'
import type { EChartsOption, LineSeriesOption } from 'echarts'
import { axisLabel, chartColors, niceScale, tooltipBase, tooltipBox } from '@/utils/charts'
import { dayjs, formatAxis, formatMetric } from '@/utils/format'

export type ChartPoint = [number, number | null]

export interface ChartSeries {
  name: string
  color: string
  points: ChartPoint[]
  /** 单条曲线时加 8% 的面积填充 */
  area?: boolean
}

const props = withDefaults(
  defineProps<{
    series: ChartSeries[]
    unit: string
    height?: number
    /** 时间窗口（毫秒），让横轴始终覆盖完整范围 */
    start?: number
    end?: number
    compact?: boolean
    /** 在每条曲线末端画一个带白边的圆点 */
    endMarker?: boolean
    /** 重新加载时保持上一版画面并变淡 */
    dim?: boolean
    label?: string
    emptyText?: string
  }>(),
  { height: 240, emptyText: '暂无数据' },
)

const hasData = computed(() => props.series.some((s) => s.points.some((p) => p[1] !== null)))

// 采集间隔明显变大的地方插入空值，让曲线断开而不是把缺口连起来。
function withGaps(points: ChartPoint[]): ChartPoint[] {
  if (points.length < 3) return points
  const deltas = points.slice(1).map((p, i) => p[0] - points[i]![0]).sort((a, b) => a - b)
  const step = deltas[Math.floor(deltas.length / 2)]!
  const out: ChartPoint[] = [points[0]!]
  for (let i = 1; i < points.length; i++) {
    const prev = points[i - 1]!
    const cur = points[i]!
    if (step > 0 && cur[0] - prev[0] > step * 2.5) out.push([prev[0] + step, null])
    out.push(cur)
  }
  return out
}

const span = computed(() => {
  let min = props.start ?? Infinity
  let max = props.end ?? -Infinity
  if (props.start === undefined || props.end === undefined) {
    for (const s of props.series) {
      for (const p of s.points) {
        min = Math.min(min, p[0])
        max = Math.max(max, p[0])
      }
    }
  }
  return Number.isFinite(max - min) ? max - min : 0
})

function timeLabel(v: number) {
  return span.value > 36 * 3600_000 ? dayjs(v).format('MM-DD') : dayjs(v).format('HH:mm')
}

const option = computed<EChartsOption>(() => {
  const compact = props.compact
  let peak = 0
  for (const s of props.series) for (const p of s.points) if (p[1] !== null) peak = Math.max(peak, p[1])
  const ticks = compact ? 2 : 4
  let scale: { max: number; interval: number }
  if (props.unit === 'percent') {
    scale = niceScale(Math.max(peak, 1), ticks, 100)
  } else if (props.unit === 'byte' || props.unit === 'byte/s') {
    // 字节按 1024 进位显示，刻度也要在显示单位里取整，否则会出现 19MB/s 这样的刻度。
    const factor = peak > 0 ? 1024 ** Math.max(0, Math.floor(Math.log(peak * 1.1) / Math.log(1024))) : 1
    const s = niceScale(peak / factor, ticks)
    scale = { max: s.max * factor, interval: s.interval * factor }
  } else {
    scale = niceScale(peak, ticks)
  }

  const lines: LineSeriesOption[] = props.series.map((s) => ({
    type: 'line',
    name: s.name,
    data: withGaps(s.points),
    // 点很少时（比如存储量每小时或每天才统计一次）画出数据点，只有一个点也看得见。
    showSymbol: s.points.filter((p) => p[1] !== null).length <= 12,
    symbol: 'circle',
    symbolSize: 8,
    connectNulls: false,
    sampling: 'lttb',
    lineStyle: { width: 2, color: s.color, cap: 'round', join: 'round' },
    itemStyle: { color: s.color, borderColor: chartColors.surface, borderWidth: 2 },
    areaStyle: s.area ? { color: s.color, opacity: 0.08 } : undefined,
    emphasis: { disabled: true },
    animationDuration: 300,
  }))
  if (props.endMarker) {
    for (const s of props.series) {
      const last = [...s.points].reverse().find((p) => p[1] !== null)
      if (!last) continue
      lines.push({
        type: 'line',
        id: `end:${s.name}`,
        data: [last],
        symbol: 'circle',
        symbolSize: 9,
        itemStyle: { color: s.color, borderColor: chartColors.surface, borderWidth: 2 },
        tooltip: { show: false },
        emphasis: { disabled: true },
        silent: true,
        z: 3,
      })
    }
  }

  return {
    animation: true,
    grid: {
      left: compact ? 4 : 8,
      right: compact ? 8 : 16,
      top: compact ? 10 : 12,
      bottom: compact ? 4 : 8,
      outerBoundsMode: 'same',
      outerBoundsContain: 'axisLabel',
    },
    xAxis: {
      type: 'time',
      min: props.start,
      max: props.end,
      axisLine: { lineStyle: { color: chartColors.axis } },
      axisTick: { show: false },
      splitLine: { show: false },
      axisLabel: { ...axisLabel, fontSize: compact ? 10 : 11, hideOverlap: true, formatter: (v: number) => timeLabel(v) },
      splitNumber: compact ? 3 : 6,
    },
    yAxis: {
      type: 'value',
      min: 0,
      max: scale.max,
      interval: scale.interval,
      axisLine: { show: false },
      axisTick: { show: false },
      splitLine: { lineStyle: { color: chartColors.grid, width: 1, type: 'solid' } },
      // 没有数据时刻度没有意义，只留网格和提示文字。
      axisLabel: {
        ...axisLabel,
        show: hasData.value,
        fontSize: compact ? 10 : 11,
        formatter: (v: number) => formatAxis(v, props.unit),
      },
    },
    tooltip: {
      ...tooltipBase,
      trigger: 'axis',
      axisPointer: { type: 'line', lineStyle: { color: '#9AA1AD', width: 1 }, label: { show: false } },
      formatter: (params) => {
        const list = (Array.isArray(params) ? params : [params]).filter((p) => !String(p.seriesId ?? '').startsWith('end:'))
        const first = list[0]
        const t = first ? (first.value as ChartPoint)[0] : 0
        return tooltipBox(
          dayjs(t).format('MM-DD HH:mm'),
          list.map((p) => {
            const v = (p.value as ChartPoint)[1]
            return {
              color: String(p.color),
              name: p.seriesName ?? '',
              value: v === null || v === undefined ? '无数据' : formatMetric(v, props.unit),
              muted: v === null || v === undefined,
            }
          }),
        )
      },
    },
    series: lines,
  }
})
</script>

<template>
  <figure class="metric-chart" :class="{ dim }" :style="{ height: `${height}px` }" role="img" :aria-label="label">
    <VChart :option="option" autoresize class="chart" />
    <div v-if="!hasData" class="nodata">
      <span>{{ emptyText }}</span>
    </div>
  </figure>
</template>

<style scoped>
.metric-chart {
  position: relative;
  margin: 0;
  width: 100%;
  transition: opacity 0.2s;
}

.metric-chart.dim {
  opacity: 0.5;
}

.chart {
  width: 100%;
  height: 100%;
}

/* 没有数据时纵轴不显示刻度，提示居中；底色挡住穿过的网格线。 */
.nodata {
  position: absolute;
  inset: 0 0 24px;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 0 8px;
  font-size: 12px;
  color: var(--ys-text-muted);
  text-align: center;
  pointer-events: none;
}
.nodata span {
  padding: 2px 8px;
  background: var(--ys-surface);
}
</style>
