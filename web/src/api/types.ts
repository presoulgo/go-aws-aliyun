// 与后端 /api/v1 的 JSON 结构一一对应。

export type Provider = 'aws' | 'aliyun'
export type ProviderFilter = Provider | 'all'
export type Role = 'admin' | 'viewer'
export type ResourceType = 'vm' | 'rds' | 'lb' | 'bucket'
export type ResourceStatus =
  | 'running'
  | 'stopped'
  | 'pending'
  | 'starting'
  | 'stopping'
  | 'changing'
  | 'terminating'
  | 'failed'
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
  category: 'login' | 'account' | 'sync' | 'user'
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
