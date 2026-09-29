import { http } from './http'
import type {
  Account,
  AccountInput,
  AlertEvent,
  AlertRule,
  AlertStatus,
  AuditLog,
  ChannelInput,
  NotifyChannel,
  ChangeQuery,
  CompareResult,
  DashboardSummary,
  ListResult,
  LoginResult,
  Meta,
  MetricDef,
  MetricsResult,
  ProviderFilter,
  RangeKey,
  ResourceDetail,
  ResourceFilters,
  Resource,
  ResourceQuery,
  ResourceChange,
  Role,
  SyncJob,
  SyncStatus,
  TestResult,
  User,
  UserView,
} from './types'

export * from './types'
export { errorMessage, tokenStore } from './http'

// 去掉空值，布尔值转成 1，避免把 undefined 拼进查询串。
function params(q: object): Record<string, string | number> {
  const out: Record<string, string | number> = {}
  for (const [k, v] of Object.entries(q)) {
    if (v === undefined || v === null || v === '' || v === false) continue
    out[k] = v === true ? 1 : (v as string | number)
  }
  return out
}

export const metaApi = {
  get: () => http.get<Meta>('/meta', { silent: true }).then((r) => r.data),
}

export const authApi = {
  login: (username: string, password: string) =>
    http.post<LoginResult>('/auth/login', { username, password }, { silent: true }).then((r) => r.data),
  me: () => http.get<User>('/auth/me', { silent: true }).then((r) => r.data),
  changePassword: (oldPassword: string, newPassword: string) =>
    http
      .put<LoginResult>('/auth/password', { old_password: oldPassword, new_password: newPassword }, { silent: true })
      .then((r) => r.data),
}

export const userApi = {
  list: () => http.get<ListResult<UserView>>('/users').then((r) => r.data),
  create: (body: { username: string; display_name: string; password: string; role: Role }) =>
    http.post<User>('/users', body, { silent: true }).then((r) => r.data),
  update: (id: number, body: { display_name?: string; role?: Role; disabled?: boolean }) =>
    http.put<User>(`/users/${id}`, body, { silent: true }).then((r) => r.data),
  remove: (id: number) => http.delete(`/users/${id}`),
  resetPassword: (id: number, password?: string) =>
    http
      .post<{ password: string }>(`/users/${id}/reset-password`, password ? { password } : undefined)
      .then((r) => r.data),
}

export const auditApi = {
  list: (q: { range?: string; category?: string; q?: string; page?: number; page_size?: number }) =>
    http.get<ListResult<AuditLog>>('/audit-logs', { params: params(q) }).then((r) => r.data),
}

export const accountApi = {
  list: () => http.get<ListResult<Account>>('/accounts').then((r) => r.data),
  get: (id: number) => http.get<Account>(`/accounts/${id}`).then((r) => r.data),
  test: (body: AccountInput) => http.post<TestResult>('/accounts/test', body, { silent: true }).then((r) => r.data),
  testExisting: (id: number, body: Partial<AccountInput>) =>
    http.post<TestResult>(`/accounts/${id}/test`, body, { silent: true }).then((r) => r.data),
  create: (body: AccountInput) => http.post<Account>('/accounts', body, { silent: true }).then((r) => r.data),
  update: (id: number, body: AccountInput) =>
    http.put<Account>(`/accounts/${id}`, body, { silent: true }).then((r) => r.data),
  setEnabled: (id: number, enabled: boolean) =>
    http.patch<Account>(`/accounts/${id}`, { enabled }).then((r) => r.data),
  remove: (id: number) => http.delete(`/accounts/${id}`),
}

export const syncApi = {
  syncAccount: (id: number) => http.post<SyncJob>(`/accounts/${id}/sync`).then((r) => r.data),
  syncAll: () => http.post<ListResult<SyncJob>>('/sync/all').then((r) => r.data),
  cancel: (jobId: number) => http.post<SyncJob>(`/sync-jobs/${jobId}/cancel`).then((r) => r.data),
  jobs: (q: { account_id?: number; page?: number; page_size?: number }) =>
    http.get<ListResult<SyncJob>>('/sync-jobs', { params: params(q) }).then((r) => r.data),
  job: (id: number) => http.get<SyncJob>(`/sync-jobs/${id}`).then((r) => r.data),
  status: () => http.get<SyncStatus>('/sync/status', { silent: true }).then((r) => r.data),
}

export const resourceApi = {
  list: (q: ResourceQuery, signal?: AbortSignal) =>
    http.get<ListResult<Resource>>('/resources', { params: params(q), signal }).then((r) => r.data),
  filters: (q: ResourceQuery, signal?: AbortSignal) =>
    http.get<ResourceFilters>('/resources/filters', { params: params(q), signal }).then((r) => r.data),
  get: (id: number) => http.get<ResourceDetail>(`/resources/${id}`).then((r) => r.data),
}

export const alertApi = {
  rules: () => http.get<ListResult<AlertRule>>('/alert-rules').then((r) => r.data),
  updateRule: (key: string, body: { enabled: boolean; params: Record<string, number>; channel_ids: number[] }) =>
    http.put(`/alert-rules/${key}`, body, { silent: true }),
  events: (q: { status?: AlertStatus; rule?: string; account_id?: number; page?: number; page_size?: number }) =>
    http.get<ListResult<AlertEvent>>('/alert-events', { params: params(q) }).then((r) => r.data),
}

export const channelApi = {
  list: () => http.get<ListResult<NotifyChannel>>('/notify-channels').then((r) => r.data),
  create: (body: ChannelInput) => http.post<NotifyChannel>('/notify-channels', body, { silent: true }).then((r) => r.data),
  update: (id: number, body: ChannelInput) =>
    http.put<NotifyChannel>(`/notify-channels/${id}`, body, { silent: true }).then((r) => r.data),
  remove: (id: number) => http.delete(`/notify-channels/${id}`),
  test: (id: number) => http.post(`/notify-channels/${id}/test`, undefined, { silent: true }),
}

export const changeApi = {
  list: (q: ChangeQuery, signal?: AbortSignal) =>
    http.get<ListResult<ResourceChange>>('/changes', { params: params(q), signal }).then((r) => r.data),
}

export const metricApi = {
  catalog: (type?: string) =>
    http.get<ListResult<MetricDef>>('/metrics/catalog', { params: params({ type }) }).then((r) => r.data),
  forResource: (id: number, range: RangeKey, keys: string[] = [], signal?: AbortSignal) =>
    http
      .get<MetricsResult>(`/resources/${id}/metrics`, {
        params: params({ range, keys: keys.join(',') }),
        signal,
        silent: true,
      })
      .then((r) => r.data),
  compare: (body: { resource_ids: number[]; key: string; range: RangeKey }, signal?: AbortSignal) =>
    http.post<CompareResult>('/metrics/query', body, { signal, silent: true }).then((r) => r.data),
}

export const dashboardApi = {
  summary: (provider: ProviderFilter) =>
    http
      .get<DashboardSummary>('/dashboard/summary', { params: params({ provider: provider === 'all' ? '' : provider }) })
      .then((r) => r.data),
}
