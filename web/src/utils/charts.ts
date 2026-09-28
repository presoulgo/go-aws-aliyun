// ECharts 按需注册 + 图表通用配置。
// 配色和标记规格遵循 dataviz 规范：细线 2px、实线网格、图例常在、提示框先数值后名称。
import { use } from 'echarts/core'
import { CanvasRenderer } from 'echarts/renderers'
import { BarChart, LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent } from 'echarts/components'
import type { Provider } from '@/api/types'

use([CanvasRenderer, LineChart, BarChart, GridComponent, TooltipComponent])

export const chartColors = {
  grid: '#EEF0F3',
  axis: '#D5D9E0',
  label: '#6B7280',
  text: '#151821',
  secondary: '#555C6B',
  surface: '#FFFFFF',
  // 图表里的云厂商颜色经校验（明度带、色度、色弱区分）后微调，标签仍用原型色。
  aws: '#2F56B8',
  aliyun: '#E8832A',
  // 多资源对比：按加入顺序占用固定槽位，移除后其余曲线不改色。
  series: ['#2443B5', '#E07A1F', '#13866F', '#8B3FB8'],
}

export const providerColor: Record<Provider, string> = { aws: chartColors.aws, aliyun: chartColors.aliyun }

export const monoFont = "'IBM Plex Mono', 'SFMono-Regular', Menlo, Consolas, monospace"
export const sansFont = "'IBM Plex Sans', 'PingFang SC', 'Hiragino Sans GB', 'Microsoft YaHei', 'Noto Sans SC', sans-serif"

export const axisLabel = {
  color: chartColors.label,
  fontFamily: monoFont,
  fontSize: 11,
}

export const tooltipBase = {
  backgroundColor: '#FFFFFF',
  borderColor: '#E4E7EC',
  borderWidth: 1,
  padding: [8, 10],
  textStyle: { color: chartColors.text, fontFamily: sansFont, fontSize: 12 },
  extraCssText: 'box-shadow: 0 8px 24px rgba(21, 24, 33, 0.12); border-radius: 8px;',
  confine: true,
}

export interface TooltipRow {
  color: string
  name: string
  value: string
  muted?: boolean
}

/**
 * 用 DOM 构造提示框内容：名称来自云厂商（用户可自定义），一律走 textContent，避免注入。
 */
export function tooltipBox(title: string, rows: TooltipRow[]): HTMLElement {
  const box = document.createElement('div')
  box.className = 'ys-tt'
  const head = document.createElement('div')
  head.className = 'ys-tt-title'
  head.textContent = title
  box.append(head)
  for (const r of rows) {
    const row = document.createElement('div')
    row.className = 'ys-tt-row'
    const key = document.createElement('span')
    key.className = 'ys-tt-key'
    key.style.background = r.color
    const value = document.createElement('strong')
    value.className = r.muted ? 'ys-tt-value muted' : 'ys-tt-value'
    value.textContent = r.value
    const name = document.createElement('span')
    name.className = 'ys-tt-name'
    name.textContent = r.name
    row.append(key, value, name)
    box.append(row)
  }
  return box
}

/** 取一个“好看”的数：1/2/2.5/5 × 10^n，不小于 v。 */
export function niceCeil(v: number): number {
  if (!(v > 0)) return 1
  const exp = Math.floor(Math.log10(v))
  const base = 10 ** exp
  for (const m of [1, 2, 2.5, 5, 10]) {
    if (v <= m * base) return m * base
  }
  return 10 * base
}

/**
 * 纵轴刻度：留 10% 余量后按约 ticks 段取整，保证最大值落在整刻度上。
 * cap 用于百分比这类有上限的指标。
 */
export function niceScale(peak: number, ticks = 4, cap?: number): { max: number; interval: number } {
  const top = Math.max(peak * 1.1, Number.EPSILON)
  let interval = niceCeil(top / ticks)
  let max = Math.ceil(top / interval) * interval
  if (cap !== undefined && max > cap) {
    max = cap
    interval = niceCeil(cap / ticks)
    if (cap % interval !== 0) interval = cap / ticks
  }
  if (!(max > 0)) return { max: 1, interval: 0.25 }
  return { max, interval }
}
