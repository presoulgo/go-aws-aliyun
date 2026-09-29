// 与后端 /api/v1 的 JSON 结构一一对应。

export type Provider = 'aws' | 'aliyun'
export type ProviderFilter = Provider | 'all'
export type Role = 'admin' | 'viewer'
export type CoreResourceType = 'vm' | 'rds' | 'lb' | 'bucket'
/** disk / eip 只采集闲置项（未挂载云盘、未绑定弹性 IP），用于成本优化。 */
export type ResourceType = CoreResourceType | 'disk' | 'eip'
export type ResourceStatus =
  | 'running'
  | 'stopped'
  | 'pending'
  | 'starting'
  | 'stopping'
  | 'changing'
  | 'terminating'
  | 'failed'
  | 'available'
  | 'unknown'
export type JobStatus = 'running' | 'success' | 'partial' | 'failed' | 'cancelled' | 'interrupted'
export type RangeKey = '1h' | '6h' | '24h' | '7d'
export type MetricUnit = 'percent' | 'bit/s' | 'byte/s' | 'count' | 'count/s' | 'byte'

export interface ListResult<T> {
  items: T[]
  total: number
}

export interface Meta {
  name: string
  demo: boolean
  version: string
  sync_interval_minutes: number
  audit_retention_days: number
  change_retention_days: number
}

export interface User {
  id: number
  username: string
  display_name: string
  role: Role
  disabled: boolean
  last_login_at: string | null
  last_login_ip: string
  created_at: string
  updated_at: string
}

export interface UserView extends User {
  failed_today: number
  me: boolean
}

export interface LoginResult {
  token: string
  expires_at: string
  user: User
}

export interface AuditLog {
  id: number
  user_id: number
  username: string
  category: 'login' | 'account' | 'sync' | 'user' | 'alert'
  action: string
  result: 'success' | 'failed'
  target: string
  detail: string
  ip: string
  created_at: string
}

export interface Region {
  id: string
  name: string
}

export interface JobSummary {
  id: number
  status: JobStatus
  trigger: 'schedule' | 'manual'
  tasks_total: number
  tasks_done: number
  error_count: number
  started_at: string
  finished_at: string | null
}

export interface Account {
  id: number
  name: string
  provider: Provider
  partition: string
  access_key_masked: string
  role_arn: string
  regions: string[]
  enabled: boolean
  remark: string
  cloud_account_uid: string
  last_sync_at: string | null
  last_sync_status: JobStatus | ''
  last_sync_error: string
  resource_count: number
  region_count: number
  last_job: JobSummary | null
  created_at: string
  updated_at: string
}

export interface AccountInput {
  name: string
  provider: Provider
  partition?: string
  access_key_id: string
  access_key_secret?: string
  role_arn?: string
  regions: string[]
  remark?: string
  enabled?: boolean
}

export interface TestResult {
  account_uid: string
  arn: string
  regions: Region[]
}

export interface TaskError {
  region: string
  type: string
  message: string
}

export interface SyncJob {
  id: number
  account_id: number
  account_name?: string
  provider?: Provider
  trigger: 'schedule' | 'manual'
  triggered_by: string
  status: JobStatus
  tasks_total: number
  tasks_done: number
  error_count: number
  stats: Record<string, number>
  errors: TaskError[]
  message: string
  started_at: string
  finished_at: string | null
  created_at: string
}

export interface SyncStatus {
  state: 'ok' | 'partial' | 'failed' | 'none'
  running: number
  accounts: number
  problem_accounts: number
  last_finished_at: string | null
}

export interface Resource {
  id: number
  account_id: number
  account_name: string
  provider: Provider
  type: ResourceType
  region: string
  resource_id: string
  zone: string
  name: string
  status: ResourceStatus
  raw_status: string
  spec: string
  private_ip: string
  public_ip: string
  vpc_id: string
  charge_type: 'prepaid' | 'postpaid' | ''
  expire_at: string | null
  cloud_created_at: string | null
  tags: Record<string, string>
  extra: Record<string, unknown>
  cpu_1h: number | null
  cpu_24h: number | null
  metrics_at: string | null
  synced_at: string
  created_at: string
  updated_at: string
  idle: boolean
}

export interface ResourceDetail extends Resource {
  cloud_account_uid: string
  supported_metrics: string[]
}

export interface ResourceQuery {
  type?: ResourceType
  provider?: ProviderFilter
  account_id?: number
  region?: string
  status?: string
  q?: string
  idle?: boolean
  expiring?: boolean
  sort?: string
  page?: number
  page_size?: number
}

export interface Option {
  value: string
  label?: string
  count: number
}

export interface ResourceFilters {
  counts: Record<string, number>
  regions: Option[]
  statuses: Option[]
  accounts: { id: number; name: string; provider: Provider }[]
}

export interface MetricDef {
  type: ResourceType
  key: string
  name: string
  unit: MetricUnit
  providers: Provider[]
  note?: string
}

export type Point = [number, number]

export interface Series {
  key: string
  unit: MetricUnit
  points: Point[]
}

export interface MetricsResult {
  resource_id: number
  range: RangeKey
  period: number
  start: string
  end: string
  supported: string[]
  series: Series[]
}

export interface CompareSeries {
  resource_id: number
  name: string
  provider: Provider
  region: string
  account_name: string
  supported: boolean
  unit: MetricUnit
  points: Point[]
  error?: string
}

export interface CompareResult {
  key: string
  range: RangeKey
  period: number
  items: CompareSeries[]
}

export interface DashboardSummary {
  provider: ProviderFilter
  accounts: number
  accounts_by_cloud: Record<string, number>
  account_names: string[]
  resources: number
  regions: number
  vm_total: number
  vm_running: number
  idle: number
  idle_threshold: number
  expiring: number
  expiring_days: number
  next_expire_in_days: number | null
  waste_disks: number
  waste_eips: number
  distribution: { type: ResourceType; aws: number; aliyun: number }[]
  cpu_trend: { t: number; aws: number | null; aliyun: number | null }[]
  top_cpu: { id: number; name: string; provider: Provider; region: string; account_name: string; cpu: number }[]
  expiring_items: {
    id: number
    name: string
    type: ResourceType
    provider: Provider
    region: string
    expire_at: string
    days: number
  }[]
  recent_sync: {
    account_id: number
    account_name: string
    provider: Provider
    job_id: number
    status: JobStatus
    started_at: string
    finished_at: string | null
    items: number
    regions: number
    error_count: number
    message: string
    tasks_total: number
    tasks_done: number
    first_error?: TaskError
  }[]
}

export type ChangeAction = 'created' | 'updated' | 'deleted'

export interface FieldChange {
  field: string
  old: string
  new: string
}

export interface ResourceChange {
  id: number
  account_id: number
  account_name: string
  provider: Provider
  type: ResourceType
  region: string
  resource_id: string
  name: string
  action: ChangeAction
  changes: FieldChange[]
  job_id: number
  created_at: string
}

export interface ChangeQuery {
  provider?: ProviderFilter
  account_id?: number
  type?: ResourceType
  action?: ChangeAction
  region?: string
  resource_id?: string
  range?: 'today' | '7d' | '30d' | 'all'
  q?: string
  page?: number
  page_size?: number
}

export type AlertRuleKey = 'cpu_high' | 'idle_host' | 'expiring' | 'waste' | 'sync_failed'
export type AlertStatus = 'firing' | 'resolved'
export type ChannelType = 'feishu' | 'webhook'

export interface AlertRule {
  key: AlertRuleKey
  name: string
  description: string
  enabled: boolean
  params: Record<string, number>
  channel_ids: number[]
  firing: number
}

export interface AlertEvent {
  id: number
  rule: AlertRuleKey
  rule_name: string
  account_id: number
  account_name: string
  provider: Provider
  status: AlertStatus
  target_key: string
  resource_type: ResourceType | ''
  region: string
  resource_id: string
  name: string
  detail: string
  fired_at: string
  resolved_at: string | null
  notify_error: string
}

export interface NotifyChannel {
  id: number
  name: string
  type: ChannelType
  url_masked: string
  has_secret: boolean
  enabled: boolean
  created_at: string
  updated_at: string
}

export interface ChannelInput {
  name: string
  type: ChannelType
  url?: string
  secret?: string
  clear_secret?: boolean
  enabled?: boolean
}
