import type { JobStatus, Provider, ResourceStatus, ResourceType } from '@/api/types'

/** 状态色调：ok 绿 / off 灰 / busy 主色 / err 红 / warn 琥珀。 */
export type Tone = 'ok' | 'off' | 'busy' | 'err' | 'warn'

export interface StatusInfo {
  label: string
  tone: Tone
}

const resourceStatus: Record<ResourceStatus, StatusInfo> = {
  running: { label: '运行中', tone: 'ok' },
  stopped: { label: '已停止', tone: 'off' },
  pending: { label: '创建中', tone: 'busy' },
  starting: { label: '启动中', tone: 'busy' },
  stopping: { label: '停止中', tone: 'busy' },
  changing: { label: '变更中', tone: 'busy' },
  terminating: { label: '释放中', tone: 'busy' },
  failed: { label: '异常', tone: 'err' },
  available: { label: '闲置', tone: 'warn' },
  unknown: { label: '未知', tone: 'off' },
}

export function resourceStatusInfo(s: string): StatusInfo {
  return resourceStatus[s as ResourceStatus] ?? { label: s || '未知', tone: 'off' }
}

const jobStatus: Record<JobStatus, StatusInfo> = {
  running: { label: '同步中', tone: 'busy' },
  success: { label: '成功', tone: 'ok' },
  partial: { label: '部分成功', tone: 'warn' },
  failed: { label: '失败', tone: 'err' },
  cancelled: { label: '已取消', tone: 'off' },
  interrupted: { label: '已中断', tone: 'warn' },
}

export function jobStatusInfo(s: string): StatusInfo {
  return jobStatus[s as JobStatus] ?? { label: '未同步', tone: 'off' }
}

export const providerLabel: Record<Provider, string> = { aws: 'AWS', aliyun: '阿里云' }

export const typeLabel: Record<ResourceType, string> = {
  vm: '云主机',
  rds: '数据库',
  lb: '负载均衡',
  bucket: '对象存储',
  disk: '未挂载云盘',
  eip: '未绑定弹性 IP',
}

export const typeProducts: Record<ResourceType, string> = {
  vm: 'EC2 / ECS',
  rds: 'RDS',
  lb: 'ELB / SLB·ALB',
  bucket: 'S3 / OSS',
  disk: 'EBS / 云盘',
  eip: 'Elastic IP / EIP',
}

export const roleLabel: Record<string, string> = { admin: '管理员', viewer: '只读' }

export const chargeLabel: Record<string, string> = { prepaid: '包年包月', postpaid: '按量付费' }

export const partitionLabel: Record<string, string> = { aws: '全球区', 'aws-cn': '中国区' }
