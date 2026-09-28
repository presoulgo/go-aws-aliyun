import dayjs from 'dayjs'
import relativeTime from 'dayjs/plugin/relativeTime'
import 'dayjs/locale/zh-cn'
import type { MetricUnit } from '@/api/types'

dayjs.extend(relativeTime)
dayjs.locale('zh-cn')

export { dayjs }

const DASH = '—'

type TimeInput = string | number | Date | null | undefined

function valid(t: TimeInput): t is string | number | Date {
  return t !== null && t !== undefined && t !== '' && dayjs(t).isValid()
}

/** 2026-09-28 14:03:21 */
export function formatDateTime(t: TimeInput): string {
  return valid(t) ? dayjs(t).format('YYYY-MM-DD HH:mm:ss') : DASH
}

/** 2026-09-28 */
export function formatDate(t: TimeInput): string {
  return valid(t) ? dayjs(t).format('YYYY-MM-DD') : DASH
}

/** 今天 09:12 / 昨天 21:04 / 09-26 18:30 / 2025-12-01 */
export function formatShortTime(t: TimeInput, now = Date.now()): string {
  if (!valid(t)) return DASH
  const d = dayjs(t)
  const today = dayjs(now).startOf('day')
  if (d.isAfter(today)) return '今天 ' + d.format('HH:mm')
  if (d.isAfter(today.subtract(1, 'day'))) return '昨天 ' + d.format('HH:mm')
  if (d.year() === today.year()) return d.format('MM-DD HH:mm')
  return d.format('YYYY-MM-DD')
}

/** 刚刚 / 2 分钟前 / 3 小时前 */
export function fromNow(t: TimeInput, now = Date.now()): string {
  if (!valid(t)) return DASH
  const diff = now - dayjs(t).valueOf()
  if (diff < 60_000 && diff > -60_000) return '刚刚'
  return dayjs(t).from(now)
}

/** 1 分 12 秒 */
export function formatDuration(start: TimeInput, end: TimeInput): string {
  if (!valid(start) || !valid(end)) return DASH
  let s = Math.max(0, Math.round((dayjs(end).valueOf() - dayjs(start).valueOf()) / 1000))
  if (s < 60) return `${s} 秒`
  const h = Math.floor(s / 3600)
  s -= h * 3600
  const m = Math.floor(s / 60)
  s -= m * 60
  if (h > 0) return `${h} 小时 ${m} 分`
  return s > 0 ? `${m} 分 ${s} 秒` : `${m} 分钟`
}

/** 距今还有几天（向上取整，已过期为负数）。 */
export function daysUntil(t: TimeInput, now = Date.now()): number | null {
  if (!valid(t)) return null
  return Math.ceil((dayjs(t).valueOf() - now) / 86_400_000)
}

export function formatNumber(v: number | null | undefined, digits = 0): string {
  if (v === null || v === undefined || Number.isNaN(v)) return DASH
  return v.toLocaleString('zh-CN', { minimumFractionDigits: digits, maximumFractionDigits: digits })
}

export function formatPercent(v: number | null | undefined, digits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return DASH
  return `${v.toFixed(digits)}%`
}

function scaled(v: number, base: number, units: string[], digits: number): string {
  let i = 0
  let n = Math.abs(v)
  while (n >= base && i < units.length - 1) {
    n /= base
    i++
  }
  const sign = v < 0 ? '-' : ''
  const d = i === 0 ? 0 : n >= 100 ? 0 : n >= 10 ? Math.min(1, digits) : digits
  return `${sign}${n.toFixed(d)} ${units[i]}`
}

/** 网络带宽：12.4 Mbps */
export function formatBits(v: number | null | undefined, digits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return DASH
  return scaled(v, 1000, ['bps', 'Kbps', 'Mbps', 'Gbps', 'Tbps'], digits)
}

/** 容量：3.2 GB */
export function formatBytes(v: number | null | undefined, digits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return DASH
  return scaled(v, 1024, ['B', 'KB', 'MB', 'GB', 'TB', 'PB'], digits)
}

/** 吞吐：8.1 MB/s */
export function formatBytesRate(v: number | null | undefined, digits = 1): string {
  if (v === null || v === undefined || Number.isNaN(v)) return DASH
  return formatBytes(v, digits) + '/s'
}

function formatCount(v: number, perSecond: boolean): string {
  const abs = Math.abs(v)
  let s: string
  if (abs >= 1e8) s = (v / 1e8).toFixed(1) + ' 亿'
  else if (abs >= 1e4) s = (v / 1e4).toFixed(1) + ' 万'
  else if (abs >= 100 || Number.isInteger(v)) s = Math.round(v).toLocaleString('zh-CN')
  else s = v.toFixed(1)
  return perSecond ? `${s}/s` : s
}

/** 按指标单位格式化数值。 */
export function formatMetric(v: number | null | undefined, unit: MetricUnit | string): string {
  if (v === null || v === undefined || Number.isNaN(v)) return DASH
  switch (unit) {
    case 'percent':
      return formatPercent(v)
    case 'bit/s':
      return formatBits(v)
    case 'byte/s':
      return formatBytesRate(v)
    case 'byte':
      return formatBytes(v)
    case 'count/s':
      return formatCount(v, true)
    default:
      return formatCount(v, false)
  }
}

/** 坐标轴刻度用的紧凑格式。 */
export function formatAxis(v: number, unit: MetricUnit | string): string {
  switch (unit) {
    case 'percent':
      return `${Math.round(v)}%`
    case 'bit/s':
      return formatBits(v, 0).replace(' ', '')
    case 'byte/s':
      return formatBytesRate(v, 0).replace(' ', '')
    case 'byte':
      return formatBytes(v, 0).replace(' ', '')
    default: {
      const abs = Math.abs(v)
      if (abs >= 1e8) return `${+(v / 1e8).toFixed(1)}亿`
      if (abs >= 1e4) return `${+(v / 1e4).toFixed(1)}万`
      return `${+v.toFixed(abs < 10 ? 1 : 0)}`
    }
  }
}

/** 每 30 分钟 / 每 2 小时 */
export function intervalText(minutes: number): string {
  if (minutes >= 60 && minutes % 60 === 0) return `每 ${minutes / 60} 小时`
  return `每 ${minutes} 分钟`
}

/** 单位的中文说明，用于图表标题旁。 */
export function unitLabel(unit: MetricUnit | string): string {
  return (
    {
      percent: '%',
      'bit/s': 'bit/s',
      'byte/s': 'Byte/s',
      byte: 'Byte',
      count: '个',
      'count/s': '次/秒',
    } as Record<string, string>
  )[unit] ?? unit
}
