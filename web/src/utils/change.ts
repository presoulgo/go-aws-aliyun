// 资源变更记录的展示文案。
import type { ChangeAction, FieldChange } from '@/api/types'
import { formatDate } from './format'
import { chargeLabel, resourceStatusInfo, type Tone } from './status'

export const actionInfo: Record<ChangeAction, { label: string; tone: Tone }> = {
  created: { label: '新增', tone: 'ok' },
  updated: { label: '变更', tone: 'busy' },
  deleted: { label: '删除', tone: 'err' },
}

const fieldLabels: Record<string, string> = {
  name: '名称',
  status: '状态',
  spec: '规格',
  private_ip: '私网 IP',
  public_ip: '公网 IP',
  charge_type: '计费方式',
  expire_at: '到期时间',
}

export function fieldLabel(field: string): string {
  if (field.startsWith('tags.')) return `标签 ${field.slice(5)}`
  return fieldLabels[field] ?? field
}

export function fieldValue(field: string, v: string): string {
  if (!v) return '（空）'
  if (field === 'status') return resourceStatusInfo(v).label
  if (field === 'charge_type') return chargeLabel[v] ?? v
  if (field === 'expire_at') return formatDate(v)
  return v
}

export function changeText(c: FieldChange): string {
  return `${fieldLabel(c.field)}：${fieldValue(c.field, c.old)} → ${fieldValue(c.field, c.new)}`
}
