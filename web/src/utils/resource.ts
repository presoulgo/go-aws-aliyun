// 资源字段的展示规则（两家云的字段先在后端归一化，这里只负责文案）。
import type { Provider, Resource } from '@/api/types'
import { daysUntil, formatBytes, formatDate, formatNumber } from './format'

function num(v: unknown): number | null {
  const n = typeof v === 'number' ? v : typeof v === 'string' && v !== '' ? Number(v) : NaN
  return Number.isFinite(n) ? n : null
}

function str(v: unknown): string {
  return typeof v === 'string' ? v : v === null || v === undefined ? '' : String(v)
}

export function extraNum(r: Pick<Resource, 'extra'>, key: string): number | null {
  return num(r.extra?.[key])
}

export function extraStr(r: Pick<Resource, 'extra'>, key: string): string {
  return str(r.extra?.[key])
}

/** “阿里云 电商业务” → “电商业务”：表格里已有云标签，账号名去掉云厂商前缀。 */
export function shortAccountName(name: string, provider: Provider | string): string {
  const prefixes = provider === 'aws' ? ['AWS ', 'AWS-', 'AWS'] : ['阿里云 ', '阿里云-', '阿里云']
  for (const p of prefixes) {
    if (name.startsWith(p) && name.length > p.length) return name.slice(p.length).trim()
  }
  return name
}

/** 8 vCPU · 16 GiB */
export function vmSize(r: Resource): string {
  const cpu = extraNum(r, 'cpu')
  const mem = extraNum(r, 'memory_mib')
  const parts: string[] = []
  if (cpu) parts.push(`${cpu} vCPU`)
  if (mem) {
    const gib = mem / 1024
    parts.push(`${Number.isInteger(gib) ? gib : gib.toFixed(1)} GiB`)
  }
  return parts.join(' · ')
}

const engineNames: Record<string, string> = {
  mysql: 'MySQL',
  postgres: 'PostgreSQL',
  postgresql: 'PostgreSQL',
  mariadb: 'MariaDB',
  sqlserver: 'SQL Server',
  'aurora-mysql': 'Aurora MySQL',
  'aurora-postgresql': 'Aurora PostgreSQL',
  aurora: 'Aurora',
  ppas: 'PPAS',
  polardb: 'PolarDB',
}

/** MySQL 8.0 / SQL Server 2019 */
export function engineLabel(r: Resource): string {
  const raw = extraStr(r, 'engine')
  const key = raw.toLowerCase()
  let name = engineNames[key]
  if (!name) {
    if (key.startsWith('sqlserver')) name = 'SQL Server'
    else if (key.startsWith('oracle')) name = 'Oracle'
    else if (key.startsWith('db2')) name = 'Db2'
    else name = raw
  }
  const version = extraStr(r, 'engine_version').split('_')[0]
  return [name, version].filter(Boolean).join(' ')
}

/** 数据库存储容量（AWS 为 GiB，阿里云为 GB）。 */
export function rdsStorage(r: Resource): string {
  const gib = extraNum(r, 'allocated_storage_gib')
  if (gib) return `${gib} GiB`
  const gb = extraNum(r, 'storage_gb')
  if (gb) return `${gb} GB`
  return '—'
}

const lbKinds: Record<string, string> = { alb: 'ALB', nlb: 'NLB', clb: 'CLB', gwlb: 'GWLB', elb: 'CLB' }

/** ALB / CLB · slb.s2.medium / ALB · 标准版 */
export function lbKindLabel(r: Resource): string {
  const kind = lbKinds[extraStr(r, 'lb_kind')] ?? r.spec ?? '—'
  const spec = extraStr(r, 'lb_spec')
  return spec ? `${kind} · ${spec}` : kind
}

export function networkLabel(r: Resource): string {
  const n = extraStr(r, 'network')
  if (n === 'internet') return '公网'
  if (n === 'internal' || n === 'intranet') return '内网'
  return '—'
}

const bucketClasses: Record<string, string> = {
  Standard: '标准存储',
  IA: '低频访问',
  Archive: '归档存储',
  ColdArchive: '冷归档存储',
  DeepColdArchive: '深度冷归档',
}

export function bucketClass(r: Resource): string {
  const label = extraStr(r, 'storage_class_label')
  if (label) return label
  const cls = extraStr(r, 'storage_class')
  if (cls) return bucketClasses[cls] ?? cls
  return r.provider === 'aws' ? 'S3（按对象）' : '—'
}

export function bucketSize(r: Resource): string {
  const v = extraNum(r, 'size_bytes')
  return v === null ? '—' : formatBytes(v, v >= 1024 ** 4 ? 2 : 1)
}

export function bucketObjects(r: Resource): string {
  const v = extraNum(r, 'object_count')
  return v === null ? '—' : formatNumber(v)
}

export function ips(list: string): string[] {
  return list ? list.split(',').map((s) => s.trim()).filter(Boolean) : []
}

export interface ExpiryInfo {
  text: string
  sub: string
  /** 30 天内到期 */
  soon: boolean
  expired: boolean
}

export function expiryInfo(r: Pick<Resource, 'charge_type' | 'expire_at'>, soonDays = 30, now = Date.now()): ExpiryInfo {
  if (!r.expire_at) {
    return { text: r.charge_type === 'prepaid' ? '包年包月' : '按量付费', sub: '', soon: false, expired: false }
  }
  const days = daysUntil(r.expire_at, now) ?? 0
  if (days < 0) return { text: formatDate(r.expire_at), sub: `已过期 ${-days} 天`, soon: true, expired: true }
  if (days <= soonDays) return { text: formatDate(r.expire_at), sub: days === 0 ? '今天到期' : `${days} 天后到期`, soon: true, expired: false }
  return { text: formatDate(r.expire_at), sub: '包年包月', soon: false, expired: false }
}

/** CPU 数值颜色：≥85% 红、≥70% 琥珀，其余正文色。 */
export function cpuTone(v: number | null | undefined): { text: string; bar: string } {
  if (v === null || v === undefined) return { text: 'var(--ys-text-muted)', bar: 'var(--ys-divider)' }
  if (v >= 85) return { text: 'var(--ys-err-fg)', bar: '#E5484D' }
  if (v >= 70) return { text: 'var(--ys-warn-fg)', bar: '#F0A020' }
  return { text: 'var(--ys-text)', bar: 'var(--ys-primary)' }
}

/** 阿里云 cn-hangzhou-h → 可用区 H；AWS 保持 us-east-1a。 */
export function zoneLabel(zone: string, provider: Provider | string): string {
  if (!zone) return ''
  const m = provider === 'aliyun' ? /-([a-z])$/.exec(zone) : null
  return m ? `可用区 ${m[1]!.toUpperCase()}` : zone
}

/** 云账号 UID：AWS 按 4 位分组，阿里云中间打码。 */
export function formatUID(uid: string, provider: Provider | string): string {
  if (!uid) return '—'
  if (provider === 'aws' && /^\d{12}$/.test(uid)) return `${uid.slice(0, 4)}-${uid.slice(4, 8)}-${uid.slice(8)}`
  if (provider === 'aliyun' && uid.length > 8) return `${uid.slice(0, 4)}••••${uid.slice(-4)}`
  return uid
}

/** arn:aws:iam::123:role/OpsReadOnly → OpsReadOnly */
export function roleName(arn: string): string {
  const i = arn.lastIndexOf('/')
  return i >= 0 ? arn.slice(i + 1) : arn
}
