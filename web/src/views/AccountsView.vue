<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import EmptyState from '@/components/EmptyState.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import AccountDialog from './AccountDialog.vue'
import { accountApi, syncApi, type Account, type SyncJob, type SyncScope } from '@/api'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { dayjs, formatDuration, formatNumber, fromNow, intervalText } from '@/utils/format'
import { formatUID, roleName } from '@/utils/resource'
import { jobStatusInfo, partitionLabel, typeLabel, type Tone } from '@/utils/status'

const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()

const accounts = ref<Account[]>([])
const loaded = ref(false)
const selectedId = ref<number | null>(null)
const busy = ref(new Set<number>())

async function loadAccounts() {
  const res = await accountApi.list()
  accounts.value = res.items
  loaded.value = true
  if (!accounts.value.some((a) => a.id === selectedId.value)) {
    // 默认展示需要关注的账号的历史。
    const problem = accounts.value.find((a) => ['partial', 'failed', 'interrupted'].includes(a.last_sync_status))
    selectedId.value = (problem ?? accounts.value[0])?.id ?? null
  }
}

const selected = computed(() => accounts.value.find((a) => a.id === selectedId.value) ?? null)
const anyRunning = computed(() => accounts.value.some((a) => a.last_job?.status === 'running'))

const subtitle = computed(() => {
  const list = accounts.value
  if (!list.length) return '还没有纳管云账号'
  const ok = list.filter((a) => a.enabled && a.last_sync_status === 'success').length
  const attention = list.filter((a) => a.enabled && ['partial', 'failed', 'interrupted'].includes(a.last_sync_status)).length
  const parts = [`${list.length} 个账号`, `${ok} 个同步正常`]
  if (attention) parts.push(`${attention} 个需要关注`)
  const off = list.filter((a) => !a.enabled).length
  if (off) parts.push(`${off} 个已停用`)
  parts.push(`${intervalText(app.meta.sync_interval_minutes)}自动同步`)
  return parts.join(' · ')
})

function credNote(a: Account): string {
  if (!a.role_arn) return 'AccessKey'
  return a.provider === 'aws' ? `AssumeRole · ${roleName(a.role_arn)}` : `RAM 角色 · ${roleName(a.role_arn)}`
}

function regionsText(a: Account): string {
  return a.regions.length ? `${a.regions.length} 个地域` : `全部（${a.region_count} 个）`
}

function syncState(a: Account): { tone: Tone; label: string; sub: string } {
  const job = a.last_job
  if (job?.status === 'running') {
    return {
      tone: 'busy',
      label: '同步中',
      sub: job.tasks_total ? `已完成 ${job.tasks_done} / ${job.tasks_total} 个任务` : '正在准备任务',
    }
  }
  if (!a.enabled) return { tone: 'off', label: '已停用', sub: '不参与定时同步' }
  if (!a.last_sync_status) return { tone: 'off', label: '未同步', sub: '等待首次同步' }
  const info = jobStatusInfo(a.last_sync_status)
  const ago = fromNow(a.last_sync_at, app.now)
  const errs = job?.error_count ?? 0
  return { tone: info.tone, label: info.label, sub: errs && a.last_sync_status !== 'success' ? `${ago} · ${errs} 个错误` : ago }
}

// ---------- 操作 ----------
function mark(id: number, on: boolean) {
  const next = new Set(busy.value)
  if (on) next.add(id)
  else next.delete(id)
  busy.value = next
}

async function syncOrCancel(a: Account) {
  mark(a.id, true)
  try {
    if (a.last_job?.status === 'running') {
      await syncApi.cancel(a.last_job.id)
      ElMessage.success('已取消同步')
    } else {
      await syncApi.syncAccount(a.id)
      ElMessage.success(`已开始同步「${a.name}」`)
    }
    selectedId.value = a.id
    await Promise.all([loadAccounts(), loadJobs(true), app.refreshSync()])
  } finally {
    mark(a.id, false)
  }
}

async function toggleEnabled(a: Account, enabled: boolean) {
  mark(a.id, true)
  try {
    const res = await accountApi.setEnabled(a.id, enabled)
    Object.assign(a, res)
    ElMessage.success(enabled ? `已启用「${a.name}」` : `已停用「${a.name}」，不再参与定时同步`)
    void app.refreshSync()
  } finally {
    mark(a.id, false)
  }
}

const confirmTarget = ref<Account | null>(null)
const deleting = ref(false)

async function doDelete() {
  const a = confirmTarget.value
  if (!a) return
  deleting.value = true
  try {
    await accountApi.remove(a.id)
    ElMessage.success(`已删除「${a.name}」`)
    confirmTarget.value = null
    if (selectedId.value === a.id) selectedId.value = null
    await loadAccounts()
    await loadJobs(true)
    void app.refreshSync()
  } finally {
    deleting.value = false
  }
}

// ---------- 新增 / 编辑弹窗（地址栏 ?new=1 / ?edit=<id>） ----------
const dialogOpen = computed({
  // 编辑要等账号列表加载完，才能把当前配置带进表单。
  get: () => auth.isAdmin && (route.query.new === '1' || (!!route.query.edit && !!editing.value)),
  set: (v: boolean) => {
    if (!v) {
      const { new: _n, edit: _e, ...rest } = route.query
      void router.replace({ query: rest })
    }
  },
})
const editing = computed(() => {
  const id = Number(route.query.edit ?? 0)
  return id ? (accounts.value.find((a) => a.id === id) ?? null) : null
})

function openCreate() {
  void router.replace({ query: { ...route.query, new: '1', edit: undefined } })
}

function openEdit(a: Account) {
  void router.replace({ query: { ...route.query, edit: String(a.id), new: undefined } })
}

async function onSaved(acc: Account) {
  selectedId.value = acc.id
  await loadAccounts()
  await loadJobs(true)
  void app.refreshSync()
}

// ---------- 同步历史 ----------
const jobs = ref<SyncJob[]>([])
const scopes = ref<SyncScope[]>([])
const jobsTotal = ref(0)
const jobPage = ref(1)
const jobsLoading = ref(false)
const JOB_PAGE = 10

async function loadJobs(reset = false) {
  const id = selectedId.value
  if (!id) {
     scopes.value = []
    jobs.value = []
    jobsTotal.value = 0
    return
  }
  if (reset) jobPage.value = 1
  jobsLoading.value = true
  try {
    const [res, health] = await Promise.all([
      syncApi.jobs({ account_id: id, page: 1, page_size: JOB_PAGE * jobPage.value }),
      syncApi.health(id),
    ])
    if (selectedId.value === id) {
      scopes.value = health.items
      jobs.value = res.items
      jobsTotal.value = res.total
    }
  } finally {
    jobsLoading.value = false
  }
}

function more() {
  jobPage.value++
  void loadJobs()
}

watch(selectedId, () => void loadJobs(true))

function jobTime(j: SyncJob): string {
  const t = dayjs(j.started_at)
  return t.isSame(dayjs(), 'day') ? t.format('HH:mm:ss') : t.format('MM-DD HH:mm')
}

function jobItems(j: SyncJob): string {
  if (j.status === 'running') return '—'
  const n = Object.values(j.stats ?? {}).reduce((a, b) => a + b, 0)
  return `${formatNumber(n)} 项`
}

function jobCost(j: SyncJob): string {
  if (j.status === 'running') return `${formatDuration(j.started_at, new Date(app.now).toISOString())}…`
  return formatDuration(j.started_at, j.finished_at)
}

function jobTrigger(j: SyncJob): string {
  if (j.trigger === 'schedule') return '定时同步'
  return j.triggered_by ? `手动 · ${j.triggered_by}` : '手动'
}

function jobResult(j: SyncJob): { text: string; warn: boolean } {
  if (j.status === 'running') return { text: j.tasks_total ? `正在采集 · ${j.tasks_done}/${j.tasks_total}` : '正在准备…', warn: false }
  if (j.error_count) return { text: `${j.error_count} 个任务失败`, warn: true }
  if (j.status === 'cancelled' || j.status === 'interrupted') return { text: j.message || jobStatusInfo(j.status).label, warn: false }
  if (j.status === 'failed') return { text: j.message || '同步失败', warn: true }
  return { text: '无错误', warn: false }
}

function errWhere(e: { region: string; type: string }): string {
  const type = typeLabel[e.type as keyof typeof typeLabel] ?? (e.type === 'metrics' ? '监控数据' : e.type)
  return [e.region, type].filter(Boolean).join(' · ')
}

// ---------- 轮询：有任务在跑时每 3 秒刷新进度 ----------
let timer: ReturnType<typeof setInterval> | undefined
watch(anyRunning, (running) => {
  clearInterval(timer)
  if (running) {
    timer = setInterval(() => {
      void loadAccounts()
      void loadJobs()
    }, 3000)
  }
})
watch(
  () => app.syncTick,
  () => {
    void loadAccounts()
    void loadJobs()
  },
)

onMounted(async () => {
  await loadAccounts()
  await loadJobs(true)
})
onBeforeUnmount(() => clearInterval(timer))
</script>

<template>
  <PageHeader title="云账号" :subtitle="subtitle">
    <el-button v-if="auth.isAdmin" type="primary" class="add-btn" @click="openCreate">
      <AppIcon name="plus" :size="15" />
      <span class="gap">新增云账号</span>
    </el-button>
  </PageHeader>

  <section class="ys-card table-card">
    <el-table
      v-loading="!loaded"
      :data="accounts"
      row-key="id"
      class="acc-table"
      :row-class-name="({ row }: { row: Account }) => (row.id === selectedId ? 'is-selected' : '')"
    >
      <el-table-column label="账号名称 / 云账号 UID" min-width="200">
        <template #default="{ row }">
          <div class="stack">
            <span class="acc-name">{{ row.name }}</span>
            <span class="sub-mono">{{ formatUID(row.cloud_account_uid, row.provider) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="云厂商" width="110">
        <template #default="{ row }">
          <div class="stack start">
            <CloudTag :provider="row.provider" />
            <span v-if="row.provider === 'aws'" class="sub">{{ partitionLabel[row.partition] ?? '全球区' }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="凭证" width="176">
        <template #default="{ row }">
          <div class="stack">
            <span class="mono small">{{ row.access_key_masked }}</span>
            <span class="sub-muted">{{ credNote(row) }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="同步地域" width="108">
        <template #default="{ row }"><span class="small label-color">{{ regionsText(row) }}</span></template>
      </el-table-column>
      <el-table-column label="资源" width="70" align="right">
        <template #default="{ row }"><span class="strong">{{ formatNumber(row.resource_count) }}</span></template>
      </el-table-column>
      <el-table-column label="最近同步" width="176">
        <template #default="{ row }">
          <el-tooltip :disabled="!row.last_sync_error" :content="row.last_sync_error" placement="top">
            <div class="stack small">
              <StatusTag :tone="syncState(row).tone" :label="syncState(row).label" plain />
              <span class="sub-muted">{{ syncState(row).sub }}</span>
            </div>
          </el-tooltip>
        </template>
      </el-table-column>
      <el-table-column v-if="auth.isAdmin" label="启用" width="64">
        <template #default="{ row }">
          <el-switch
            :model-value="row.enabled"
            :loading="busy.has(row.id)"
            size="small"
            :aria-label="`启用 ${row.name}`"
            @change="(v: string | number | boolean) => toggleEnabled(row, !!v)"
          />
        </template>
      </el-table-column>
      <el-table-column label="操作" :width="auth.isAdmin ? 188 : 80">
        <template #default="{ row }">
          <div class="ops">
            <button
              v-if="auth.isAdmin"
              type="button"
              class="ys-link"
              :disabled="busy.has(row.id) || (!row.enabled && row.last_job?.status !== 'running')"
              @click="syncOrCancel(row)"
            >
              {{ row.last_job?.status === 'running' ? '取消' : '同步' }}
            </button>
            <button type="button" class="ys-link" @click="selectedId = row.id">历史</button>
            <button v-if="auth.isAdmin" type="button" class="ys-link" @click="openEdit(row)">编辑</button>
            <button v-if="auth.isAdmin" type="button" class="ys-link danger" @click="confirmTarget = row">删除</button>
          </div>
        </template>
      </el-table-column>
      <template #empty>
        <EmptyState
          v-if="loaded"
          icon="key"
          title="还没有纳管云账号"
          :description="auth.isAdmin ? '新增一个只读 AccessKey，平台会自动发现地域并同步资源。' : '请联系管理员接入云账号。'"
        >
          <el-button v-if="auth.isAdmin" type="primary" @click="openCreate">新增云账号</el-button>
        </EmptyState>
      </template>
    </el-table>
  </section>

  <section v-if="selected" class="ys-card history">
    <div class="hist-head">
      <h2 class="ys-card-title">同步历史 · {{ selected.name }}</h2>
      <span class="note">共 {{ jobsTotal }} 次，保留最近 50 次</span>
    </div>
  <details v-if="scopes.length" class="scope-health">
    <summary>各地域 / 类型最近采集结果（含空清单）</summary>
    <el-table :data="scopes" size="small" max-height="340" style="margin-top: 12px">
      <el-table-column label="类型" width="110"><template #default="{ row }">{{ row.type === 'metrics' ? 'CPU 指标' : typeLabel[row.type as keyof typeof typeLabel] }}</template></el-table-column>
      <el-table-column prop="region" label="地域" width="170"><template #default="{ row }">{{ row.region || '全局' }}</template></el-table-column>
      <el-table-column label="结果" width="100"><template #default="{ row }">{{ row.status === 'skipped' ? '不支持，跳过' : row.status === 'failed' ? '失败' : '成功' }}</template></el-table-column>
      <el-table-column label="最近成功" width="165"><template #default="{ row }">{{ row.last_success_at ? dayjs(row.last_success_at).format('MM-DD HH:mm:ss') : '尚无成功记录' }}</template></el-table-column>
      <el-table-column label="最近尝试" width="165"><template #default="{ row }">{{ dayjs(row.last_attempt_at).format('MM-DD HH:mm:ss') }}</template></el-table-column>
      <el-table-column prop="error" label="错误" min-width="230" />
    </el-table>
  </details>
    <div v-if="jobs.length" class="jobs">
      <div v-for="j in jobs" :key="j.id" class="job">
        <div class="job-line">
          <span class="job-time mono">{{ jobTime(j) }}</span>
          <span class="job-status"><StatusTag :job="j.status" /></span>
          <span class="job-col">{{ jobTrigger(j) }}</span>
          <span class="job-col">用时 {{ jobCost(j) }}</span>
          <span class="job-col">{{ jobItems(j) }}</span>
          <span :class="jobResult(j).warn ? 'warn-text' : 'muted'">{{ jobResult(j).text }}</span>
        </div>
        <div v-if="j.errors?.length" class="job-err">
          <AppIcon name="warning" :size="16" class="err-icon" />
          <div class="err-list">
            <div v-for="(e, i) in j.errors" :key="i" class="err-item">
              <span class="mono">{{ errWhere(e) }}</span>：{{ e.message }}
            </div>
            <div v-if="j.status === 'partial'" class="err-foot">
              失败的地域保留上次同步的数据，其余 {{ Math.max(0, j.tasks_total - j.error_count) }} 个任务正常完成。
            </div>
          </div>
        </div>
      </div>
      <button v-if="jobs.length < jobsTotal" type="button" class="ys-link more" :disabled="jobsLoading" @click="more">
        {{ jobsLoading ? '加载中…' : '加载更多' }}
      </button>
    </div>
    <p v-else class="muted empty">{{ jobsLoading ? '加载中…' : '该账号还没有同步记录' }}</p>
  </section>

  <AccountDialog v-model="dialogOpen" :account="editing" @saved="onSaved" />

  <el-dialog
    :model-value="!!confirmTarget"
    width="440px"
    :show-close="false"
    append-to-body
    class="confirm-dialog"
    @close="confirmTarget = null"
  >
    <div class="confirm">
      <div class="confirm-head">
        <span class="confirm-icon"><AppIcon name="trash" :size="18" /></span>
        <h2>删除云账号「{{ confirmTarget?.name }}」？</h2>
      </div>
      <p>
        将同时删除该账号下的 {{ formatNumber(confirmTarget?.resource_count ?? 0) }}
        项资源和全部同步记录，操作不可恢复。云上的资源本身不受影响。
      </p>
      <div class="confirm-actions">
        <el-button @click="confirmTarget = null">取消</el-button>
        <el-button type="danger" class="danger-solid" :loading="deleting" @click="doDelete">确认删除</el-button>
      </div>
    </div>
  </el-dialog>
</template>

<style scoped>
.scope-health { padding: 12px 20px; color: var(--ys-text-label); }
.scope-health summary { cursor: pointer; }
.add-btn {
  height: 38px;
}

.gap {
  margin-left: 8px;
}

.table-card {
  overflow: hidden;
}

.acc-table :deep(.el-table__cell) {
  height: 60px;
}

.acc-table :deep(th.el-table__cell) {
  height: 44px;
}

.acc-table :deep(tr > :first-child .cell) {
  padding-left: 20px;
}

.acc-table :deep(.el-table__row.is-selected > td) {
  background: #f5f7fe;
}

.stack {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.stack.start {
  align-items: flex-start;
  gap: 3px;
}

.acc-name {
  font-weight: 500;
}

.sub-mono {
  font-family: var(--ys-font-mono);
  font-size: 11px;
  color: var(--ys-text-muted);
}

.sub {
  font-size: 12px;
  color: var(--ys-text-secondary);
}

.sub-muted {
  font-size: 11px;
  color: var(--ys-text-muted);
}

.small {
  font-size: 12px;
}

.label-color {
  color: var(--ys-text-label);
}

.strong {
  font-weight: 500;
}

.ops {
  display: flex;
  align-items: center;
  gap: 12px;
}

.history {
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.hist-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.note {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.jobs {
  display: flex;
  flex-direction: column;
}

.job {
  padding: 9px 0;
  display: flex;
  flex-direction: column;
  gap: 8px;
  border-bottom: 1px solid var(--ys-divider);
}

.job:last-of-type {
  border-bottom-color: transparent;
}

.job-line {
  display: flex;
  align-items: center;
  gap: 16px;
  font-size: 13px;
}

.job-time {
  width: 84px;
  font-size: 12px;
  color: var(--ys-text-label);
}

.job-status {
  width: 88px;
}

.job-col {
  width: 110px;
  color: var(--ys-text-secondary);
}

.warn-text {
  color: var(--ys-warn-fg);
}

.muted {
  color: var(--ys-text-muted);
}

.job-err {
  margin-left: 100px;
  padding: 8px 12px;
  display: flex;
  gap: 10px;
  border-radius: 8px;
  background: #fff8ec;
  border: 1px solid var(--ys-warn-soft-border);
  font-size: 12px;
  line-height: 1.6;
  color: #6b3d00;
}

.err-icon {
  margin-top: 2px;
  color: #b26a12;
}

.err-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
  word-break: break-all;
}

.err-foot {
  color: #8a5a1c;
}

.more {
  margin-top: 8px;
  align-self: flex-start;
}

.empty {
  font-size: 13px;
  padding: 12px 0;
}

.confirm {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.confirm-head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.confirm-head h2 {
  font-size: 17px;
  font-weight: 600;
}

.confirm-icon {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
  display: flex;
  align-items: center;
  justify-content: center;
}

.confirm p {
  font-size: 14px;
  line-height: 1.7;
  color: var(--ys-text-label);
}

.confirm-actions {
  margin-top: 6px;
  display: flex;
  justify-content: flex-end;
  gap: 10px;
}

.confirm-actions .el-button + .el-button {
  margin-left: 0;
}

.danger-solid {
  --el-button-bg-color: var(--ys-err-fg);
  --el-button-border-color: var(--ys-err-fg);
  --el-button-hover-bg-color: #93190f;
  --el-button-hover-border-color: #93190f;
}
</style>

<style>
.confirm-dialog .el-dialog__header {
  display: none;
}
</style>
