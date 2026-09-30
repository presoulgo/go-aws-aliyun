<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import PageHeader from '@/components/PageHeader.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import {
  alertApi,
  channelApi,
  errorMessage,
  type AlertEvent,
  type AlertRule,
  type AlertStatus,
  type ChannelType,
  type NotifyChannel,
} from '@/api'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { dayjs } from '@/utils/format'
import { shortAccountName } from '@/utils/resource'

type Tab = 'events' | 'rules' | 'channels'
const PAGE_SIZE = 20

const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()

const tabs: { value: Tab; label: string }[] = [
  { value: 'events', label: '告警事件' },
  { value: 'rules', label: '告警规则' },
  { value: 'channels', label: '通知渠道' },
]
const tab = ref<Tab>(tabs.find((t) => t.value === route.query.tab)?.value ?? 'events')
watch(tab, (t) => void router.replace({ query: { tab: t === 'events' ? undefined : t } }))

// ---------- 数据 ----------
const rules = ref<AlertRule[]>([])
const channels = ref<NotifyChannel[]>([])

async function loadRules() {
  rules.value = (await alertApi.rules()).items
}
async function loadChannels() {
  channels.value = (await channelApi.list()).items
}
void loadRules()
void loadChannels()

const firingTotal = computed(() => rules.value.reduce((n, r) => n + r.firing, 0))

// ---------- 告警事件 ----------
const statusOptions: { value: AlertStatus | 'all'; label: string }[] = [
  { value: 'firing', label: '告警中' },
  { value: 'resolved', label: '已恢复' },
  { value: 'all', label: '全部' },
]
const status = ref<AlertStatus | 'all'>('firing')
const ruleFilter = ref('')
const page = ref(1)
const events = ref<AlertEvent[]>([])
const eventTotal = ref(0)
const eventsLoading = ref(false)
const eventsLoaded = ref(false)

async function loadEvents() {
  eventsLoading.value = true
  try {
    const res = await alertApi.events({
      status: status.value === 'all' ? undefined : status.value,
      rule: ruleFilter.value || undefined,
      page: page.value,
      page_size: PAGE_SIZE,
    })
    events.value = res.items
    eventTotal.value = res.total
    eventsLoaded.value = true
  } finally {
    eventsLoading.value = false
  }
}
watch([status, ruleFilter], () => {
  page.value = 1
  void loadEvents()
})
watch(page, () => void loadEvents())
watch(
  () => app.syncTick,
  () => {
    void loadEvents()
    void loadRules()
  },
)
void loadEvents()

function time(t: string | null): string {
  if (!t) return '—'
  const d = dayjs(t)
  const today = dayjs(app.now).startOf('day')
  if (d.isAfter(today)) return `今天 ${d.format('HH:mm')}`
  if (d.isAfter(today.subtract(1, 'day'))) return `昨天 ${d.format('HH:mm')}`
  return d.format('MM-DD HH:mm')
}

function duration(e: AlertEvent): string {
  const end = e.resolved_at ? dayjs(e.resolved_at) : dayjs(app.now)
  const mins = Math.max(0, end.diff(dayjs(e.fired_at), 'minute'))
  if (mins < 60) return `${mins} 分钟`
  if (mins < 24 * 60) return `${Math.floor(mins / 60)} 小时`
  return `${Math.floor(mins / 1440)} 天`
}

function eventLink(e: AlertEvent) {
  if (e.rule === 'sync_failed') return { path: '/accounts' }
  if (e.resource_type === 'disk' || e.resource_type === 'eip') return { path: '/optimize', query: { kind: e.resource_type } }
  return { path: '/resources', query: { type: e.resource_type || undefined, q: e.resource_id } }
}

const handling = reactive({ open: false, id: 0, acknowledged: false, note: '', silence: -1, saving: false })
const retrying = ref<number | null>(null)
function silenced(e: AlertEvent): boolean {
  return !!e.silenced_until && dayjs(e.silenced_until).isAfter(dayjs(app.now))
}
function openHandling(e: AlertEvent) {
  Object.assign(handling, { open: true, id: e.id, acknowledged: !!e.acknowledged_at, note: e.note, silence: -1 })
}
async function saveHandling() {
  handling.saving = true
  try {
    await alertApi.handle(handling.id, {
      acknowledged: handling.acknowledged, note: handling.note,
      silence_minutes: handling.silence < 0 ? undefined : handling.silence,
    })
    handling.open = false
    ElMessage.success('已保存告警处理记录')
    await loadEvents()
  } catch (e) { ElMessage.error(errorMessage(e)) }
  finally { handling.saving = false }
}
async function retryNotification(e: AlertEvent) {
  retrying.value = e.id
  try {
    const res = await alertApi.retry(e.id)
    ElMessage.success(`已成功重发 ${res.sent} 条通知`)
  } catch (err) { ElMessage.error(errorMessage(err)) }
  finally { retrying.value = null; await loadEvents() }
}

// ---------- 告警规则 ----------
const paramMeta: Record<string, { label: string; unit: string; min: number; max: number; step: number }> = {
  threshold: { label: '阈值', unit: '%', min: 0.1, max: 100, step: 1 },
  days: { label: '天数', unit: '天', min: 1, max: 365, step: 1 },
}
interface RuleDraft {
  enabled: boolean
  params: Record<string, number>
  channel_ids: number[]
  saving: boolean
}
const drafts = reactive<Record<string, RuleDraft>>({})
watch(
  rules,
  (list) => {
    for (const r of list) {
      if (drafts[r.key]?.saving) continue
      drafts[r.key] = { enabled: r.enabled, params: { ...r.params }, channel_ids: [...r.channel_ids], saving: false }
    }
  },
  { immediate: true },
)

function dirty(r: AlertRule): boolean {
  const d = drafts[r.key]
  if (!d) return false
  return (
    d.enabled !== r.enabled ||
    JSON.stringify(d.params) !== JSON.stringify(r.params) ||
    JSON.stringify([...d.channel_ids].sort()) !== JSON.stringify([...r.channel_ids].sort())
  )
}

async function saveRule(r: AlertRule) {
  const d = drafts[r.key]!
  d.saving = true
  try {
    await alertApi.updateRule(r.key, { enabled: d.enabled, params: d.params, channel_ids: d.channel_ids })
    ElMessage.success(`已保存「${r.name}」`)
    d.saving = false
    await Promise.all([loadRules(), loadEvents()])
  } catch (err) {
    ElMessage.error(errorMessage(err))
    d.saving = false
  }
}

// ---------- 通知渠道 ----------
const typeLabel: Record<ChannelType, string> = { feishu: '飞书机器人', webhook: '通用 Webhook' }
const dialog = reactive({
  open: false,
  id: 0,
  name: '',
  type: 'feishu' as ChannelType,
  url: '',
  secret: '',
  hasSecret: false,
  clearSecret: false,
  enabled: true,
  saving: false,
  error: '',
})

function openCreate() {
  Object.assign(dialog, { open: true, id: 0, name: '', type: 'feishu', url: '', secret: '', hasSecret: false, clearSecret: false, enabled: true, error: '' })
}
function openEdit(c: NotifyChannel) {
  Object.assign(dialog, {
    open: true,
    id: c.id,
    name: c.name,
    type: c.type,
    url: '',
    secret: '',
    hasSecret: c.has_secret,
    clearSecret: false,
    enabled: c.enabled,
    error: '',
  })
}

async function submitChannel() {
  dialog.error = ''
  if (!dialog.name.trim()) return void (dialog.error = '请填写渠道名称')
  if (!dialog.id && !dialog.url.trim()) return void (dialog.error = '请填写 Webhook 地址')
  dialog.saving = true
  try {
    const body = {
      name: dialog.name.trim(),
      type: dialog.type,
      url: dialog.url.trim() || undefined,
      secret: dialog.type === 'feishu' ? dialog.secret.trim() || undefined : undefined,
      clear_secret: dialog.clearSecret || undefined,
      enabled: dialog.enabled,
    }
    if (dialog.id) await channelApi.update(dialog.id, body)
    else await channelApi.create(body)
    ElMessage.success('已保存')
    dialog.open = false
    await loadChannels()
  } catch (err) {
    dialog.error = errorMessage(err)
  } finally {
    dialog.saving = false
  }
}

const testing = ref(0)
async function testChannel(c: NotifyChannel) {
  testing.value = c.id
  try {
    await channelApi.test(c.id)
    ElMessage.success(`测试消息已发送到「${c.name}」`)
  } catch (err) {
    ElMessage.error(errorMessage(err))
  } finally {
    testing.value = 0
  }
}

async function removeChannel(c: NotifyChannel) {
  try {
    await ElMessageBox.confirm('删除后引用它的告警规则不再向这里发送通知。', `删除通知渠道「${c.name}」？`, {
      confirmButtonText: '确认删除',
      cancelButtonText: '取消',
      type: 'error',
      confirmButtonClass: 'el-button--danger',
    })
  } catch {
    return
  }
  try {
    await channelApi.remove(c.id)
  } catch (err) {
    ElMessage.error(errorMessage(err))
    return
  }
  ElMessage.success('已删除')
  await Promise.all([loadChannels(), loadRules()])
}
</script>

<template>
  <PageHeader title="告警中心" subtitle="每次同步后按内置规则检查本地数据，新告警和恢复会按规则聚合后发送到通知渠道" />

  <section class="ys-card panel">
    <div class="tabs" role="tablist" aria-label="告警中心">
      <button
        v-for="t in tabs"
        :key="t.value"
        type="button"
        role="tab"
        class="tab"
        :class="{ on: tab === t.value }"
        :aria-selected="tab === t.value"
        @click="tab = t.value"
      >
        {{ t.label }}
        <span v-if="t.value === 'events'" class="count" :class="{ err: firingTotal > 0 }">{{ firingTotal }}</span>
        <span v-if="t.value === 'channels'" class="count">{{ channels.length }}</span>
      </button>
    </div>

    <!-- 告警事件 -->
    <template v-if="tab === 'events'">
      <div class="filters">
        <SegmentedControl v-model="status" :options="statusOptions" label="告警状态" />
        <el-select v-model="ruleFilter" class="rule-select" placeholder="全部">
          <template #prefix>规则：</template>
          <el-option label="全部" value="" />
          <el-option v-for="r in rules" :key="r.key" :label="r.name" :value="r.key" />
        </el-select>
        <span class="total">共 {{ eventTotal.toLocaleString('zh-CN') }} 条</span>
      </div>
      <el-table v-loading="eventsLoading && !eventsLoaded" :data="events" row-key="id" class="ev-table" :class="{ dim: eventsLoading && eventsLoaded }">
        <el-table-column label="状态" width="92">
          <template #default="{ row }">
            <span class="st" :class="row.status">{{ row.status === 'firing' ? '告警中' : '已恢复' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="规则" width="190">
          <template #default="{ row }">{{ row.rule_name || row.rule }}</template>
        </el-table-column>
        <el-table-column label="对象" min-width="220">
          <template #default="{ row }">
            <div class="cell-stack">
              <RouterLink :to="eventLink(row)" class="res-name ellipsis">{{ row.name }}</RouterLink>
              <span v-if="row.resource_id && row.resource_id !== row.name" class="sub-mono ellipsis">{{ row.resource_id }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="账号 · 地域" width="170">
          <template #default="{ row }">
            <div class="cell-stack start">
              <span class="acc-line">
                <CloudTag v-if="row.provider" :provider="row.provider" />
                <span class="sub ellipsis">{{ shortAccountName(row.account_name, row.provider) }}</span>
              </span>
              <span class="sub-muted">{{ row.region || '—' }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="详情" min-width="220">
          <template #default="{ row }">
            <div class="cell-stack">
              <span class="detail">{{ row.detail || '—' }}</span>
              <span v-if="row.notify_error" class="notify-err" :title="row.notify_error">通知失败：{{ row.notify_error }}</span>
        <span v-if="row.notify_error && !row.notify_retryable" class="sub-muted">历史通知未保存投递内容，无法重发</span>
        <span v-if="row.acknowledged_at" class="sub-muted">{{ row.acknowledged_by }} 已确认 · {{ time(row.acknowledged_at) }}</span>
        <span v-if="row.note" class="detail">备注：{{ row.note }}</span>
        <span v-if="silenced(row)" class="sub-muted">静默至 {{ dayjs(row.silenced_until).format('MM-DD HH:mm') }}</span>
            </div>
          </template>
        </el-table-column>
        <el-table-column label="触发 / 恢复" width="150">
          <template #default="{ row }">
            <div class="cell-stack small">
              <span>{{ time(row.fired_at) }}</span>
              <span class="sub-muted">{{ row.resolved_at ? `${time(row.resolved_at)} 恢复` : `已持续 ${duration(row)}` }}</span>
            </div>
          </template>
        </el-table-column>
    <el-table-column v-if="auth.isAdmin" label="处理" width="140" fixed="right">
      <template #default="{ row }">
        <div class="cell-stack">
          <el-button link type="primary" @click="openHandling(row)">处理 / 备注</el-button>
          <el-button v-if="row.notify_retryable" link type="primary" :loading="retrying === row.id" :disabled="retrying !== null || silenced(row)" @click="retryNotification(row)">重发通知</el-button>
        </div>
      </template>
    </el-table-column>
        <template #empty>
          <span v-if="eventsLoaded" class="empty">{{ status === 'firing' ? '当前没有告警' : '没有记录' }}</span>
        </template>
      </el-table>
      <div class="foot">
        <span>共 {{ eventTotal.toLocaleString('zh-CN') }} 条</span>
        <el-pagination
          v-if="eventTotal > PAGE_SIZE"
          v-model:current-page="page"
          background
          layout="prev, pager, next"
          :total="eventTotal"
          :page-size="PAGE_SIZE"
        />
      </div>
    </template>

    <!-- 告警规则 -->
    <div v-else-if="tab === 'rules'" class="rules">
      <p class="note">
        规则检查本地数据。超过 {{ Math.max(90, app.meta.sync_interval_minutes * 2) }} 分钟的数据不产生新告警（CPU 从采样小时结束起计算）；采集失败或数据过期时保留已有告警，采集恢复后重新判断。部分失败会显示受影响的地域和类型。
        <template v-if="!auth.isAdmin">只读用户只能查看规则。</template>
      </p>
      <div v-for="r in rules" :key="r.key" class="rule" :class="{ off: !drafts[r.key]?.enabled }">
        <template v-if="drafts[r.key]">
          <el-switch v-model="drafts[r.key]!.enabled" :disabled="!auth.isAdmin" :aria-label="`启用${r.name}`" />
          <div class="rule-main">
            <div class="rule-title">
              <strong>{{ r.name }}</strong>
              <span v-if="r.firing" class="firing">{{ r.firing }} 项告警中</span>
            </div>
            <span class="rule-desc">{{ r.description }}</span>
          </div>
          <label v-for="k in Object.keys(r.params)" :key="k" class="param">
            <span>{{ paramMeta[k]?.label ?? k }}</span>
            <el-input-number
              v-model="drafts[r.key]!.params[k]"
              :min="paramMeta[k]?.min"
              :max="paramMeta[k]?.max"
              :step="paramMeta[k]?.step"
              :disabled="!auth.isAdmin"
              controls-position="right"
              size="small"
              class="num"
            />
            <span class="unit">{{ paramMeta[k]?.unit }}</span>
          </label>
          <el-select
            v-model="drafts[r.key]!.channel_ids"
            multiple
            collapse-tags
            collapse-tags-tooltip
            :disabled="!auth.isAdmin"
            placeholder="不发送通知"
            class="ch-select"
            size="default"
          >
            <el-option v-for="c in channels" :key="c.id" :label="c.name" :value="c.id" :disabled="!c.enabled" />
          </el-select>
          <el-button
            v-if="auth.isAdmin"
            type="primary"
            :disabled="!dirty(r)"
            :loading="drafts[r.key]!.saving"
            @click="saveRule(r)"
            >保存</el-button
          >
        </template>
      </div>
      <p v-if="auth.isAdmin && !channels.length" class="note">
        还没有通知渠道，告警只会记录在页面上。<a href="#" class="ys-link" @click.prevent="tab = 'channels'">去添加通知渠道</a>
      </p>
    </div>

    <!-- 通知渠道 -->
    <template v-else>
      <div class="filters">
        <span class="note inline">Webhook 地址等同于凭证，加密保存，页面只显示脱敏后的地址。</span>
        <el-button v-if="auth.isAdmin" type="primary" class="add" @click="openCreate">
          <AppIcon name="plus" :size="14" /><span class="gap">新增渠道</span>
        </el-button>
      </div>
      <el-table :data="channels" row-key="id" class="ev-table">
        <el-table-column label="名称" min-width="160">
          <template #default="{ row }"><strong class="ch-name">{{ row.name }}</strong></template>
        </el-table-column>
        <el-table-column label="类型" width="130">
          <template #default="{ row }">{{ typeLabel[row.type as ChannelType] }}</template>
        </el-table-column>
        <el-table-column label="地址" min-width="260">
          <template #default="{ row }">
            <span class="mono small">{{ row.url_masked }}</span>
            <span v-if="row.has_secret" class="tag-sm">已签名</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <span class="st" :class="row.enabled ? 'resolved' : 'off'">{{ row.enabled ? '启用' : '停用' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="引用规则" width="200">
          <template #default="{ row }">
            <span class="sub">{{ rules.filter((r) => r.channel_ids.includes(row.id)).map((r) => r.name).join('、') || '—' }}</span>
          </template>
        </el-table-column>
        <el-table-column v-if="auth.isAdmin" label="操作" width="200">
          <template #default="{ row }">
            <el-button link type="primary" :loading="testing === row.id" @click="testChannel(row)">发送测试</el-button>
            <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
            <el-button link type="danger" @click="removeChannel(row)">删除</el-button>
          </template>
        </el-table-column>
        <template #empty>
          <span class="empty">还没有通知渠道。支持飞书群机器人和通用 Webhook（POST JSON）。</span>
        </template>
      </el-table>
    </template>
  </section>

  <el-dialog v-model="handling.open" title="处理告警" width="500px" :close-on-click-modal="false" append-to-body>
    <div class="form">
      <el-checkbox v-model="handling.acknowledged">已确认，正在处理</el-checkbox>
      <label class="field">
        <span class="label">处理备注</span>
        <el-input v-model="handling.note" type="textarea" :rows="4" maxlength="2000" show-word-limit />
      </label>
      <label class="field">
        <span class="label">静默通知</span>
        <el-select v-model="handling.silence">
          <el-option label="保持当前设置" :value="-1" />
          <el-option label="取消静默" :value="0" />
          <el-option label="静默 1 小时" :value="60" />
          <el-option label="静默 4 小时" :value="240" />
          <el-option label="静默 24 小时" :value="1440" />
        </el-select>
        <span class="hint">静默期间继续记录状态，暂停该对象的触发、恢复及重发通知。到期后恢复后续状态变更通知，期间通知不补发。</span>
      </label>
    </div>
    <template #footer>
      <el-button @click="handling.open = false">取消</el-button>
      <el-button type="primary" :loading="handling.saving" @click="saveHandling">保存</el-button>
    </template>
  </el-dialog>

  <el-dialog
    v-model="dialog.open"
    :title="dialog.id ? `编辑通知渠道 ${dialog.name}` : '新增通知渠道'"
    width="520px"
    :close-on-click-modal="false"
    append-to-body
  >
    <form class="form" autocomplete="off" @submit.prevent="submitChannel">
      <div class="field">
        <span class="label">类型</span>
        <el-radio-group v-model="dialog.type" :disabled="!!dialog.id">
          <el-radio-button value="feishu">飞书机器人</el-radio-button>
          <el-radio-button value="webhook">通用 Webhook</el-radio-button>
        </el-radio-group>
      </div>
      <label class="field">
        <span class="label">名称</span>
        <el-input v-model="dialog.name" maxlength="64" placeholder="例如：运维值班群" />
      </label>
      <label class="field">
        <span class="label">Webhook 地址</span>
        <el-input
          v-model="dialog.url"
          class="mono-input"
          :placeholder="dialog.id ? '留空表示不修改' : dialog.type === 'feishu' ? 'https://open.feishu.cn/open-apis/bot/v2/hook/…' : 'https://…'"
        />
        <span v-if="dialog.type === 'webhook'" class="hint">
          以 POST 发送 JSON：{event, rule, title, account, items: [{name, resource_id, region, value}], total, at}
        </span>
      </label>
      <label v-if="dialog.type === 'feishu'" class="field">
        <span class="label">签名密钥（可选）</span>
        <el-input
          v-model="dialog.secret"
          type="password"
          show-password
          class="mono-input"
          :disabled="dialog.clearSecret"
          :placeholder="dialog.hasSecret ? '已设置，留空表示不修改' : '机器人安全设置里开启“签名校验”后填写'"
        />
        <el-checkbox v-if="dialog.hasSecret" v-model="dialog.clearSecret">清除已保存的签名密钥</el-checkbox>
      </label>
      <el-checkbox v-model="dialog.enabled">启用</el-checkbox>
      <el-alert v-if="dialog.error" :title="dialog.error" type="error" :closable="false" show-icon />
      <button type="submit" hidden />
    </form>
    <template #footer>
      <el-button @click="dialog.open = false">取消</el-button>
      <el-button type="primary" :loading="dialog.saving" @click="submitChannel">保存</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.panel {
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.tabs {
  height: 48px;
  padding: 0 20px;
  display: flex;
  align-items: stretch;
  gap: 28px;
  border-bottom: 1px solid var(--ys-divider);
}

.tab {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 2px;
  border: 0;
  background: transparent;
  color: var(--ys-text-label);
  font-size: 14px;
  cursor: pointer;
}

.tab.on {
  color: var(--ys-primary);
  font-weight: 600;
  box-shadow: inset 0 -2px 0 var(--ys-primary);
}

.count {
  padding: 0 7px;
  border-radius: 999px;
  font-size: 12px;
  font-weight: 400;
  line-height: 20px;
  background: var(--ys-bg-muted);
  color: var(--ys-text-secondary);
}

.count.err {
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
}

.filters {
  min-height: 60px;
  padding: 12px 20px;
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
  border-bottom: 1px solid var(--ys-divider);
}

.rule-select {
  width: 220px;
}

.rule-select :deep(.el-select__prefix) {
  color: var(--ys-text-label);
  font-size: 13px;
}

.total,
.add {
  margin-left: auto;
}

.total {
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.gap {
  margin-left: 6px;
}

.ev-table.dim {
  opacity: 0.55;
}

.ev-table :deep(.el-table__cell) {
  height: 56px;
}

.ev-table :deep(tr > :first-child .cell) {
  padding-left: 20px;
}

.st {
  display: inline-block;
  padding: 2px 8px;
  border-radius: var(--ys-radius-tag);
  font-size: 12px;
  white-space: nowrap;
}

.st.firing {
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
}

.st.resolved {
  background: var(--ys-ok-bg);
  color: var(--ys-ok-fg);
}

.st.off {
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
}

.cell-stack {
  display: flex;
  flex-direction: column;
  gap: 3px;
  min-width: 0;
}

.cell-stack.start {
  align-items: flex-start;
}

.acc-line {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  max-width: 100%;
}

.res-name {
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
  font-size: 12px;
  color: var(--ys-text-muted);
}

.small {
  font-size: 12px;
}

.detail {
  font-size: 13px;
}

.notify-err {
  font-size: 12px;
  color: var(--ys-err-fg);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.empty {
  font-size: 13px;
  color: var(--ys-text-muted);
}

.foot {
  min-height: 52px;
  padding: 0 20px;
  display: flex;
  align-items: center;
  gap: 16px;
  border-top: 1px solid var(--ys-divider);
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.foot .el-pagination {
  margin-left: auto;
}

.rules {
  padding: 8px 20px 16px;
  display: flex;
  flex-direction: column;
}

.note {
  margin: 8px 0;
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.note.inline {
  margin: 0;
}

.rule {
  min-height: 72px;
  padding: 12px 0;
  display: flex;
  align-items: center;
  gap: 16px;
  border-bottom: 1px solid var(--ys-divider);
}

.rule-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.rule.off .rule-main strong {
  color: var(--ys-text-secondary);
}

.rule-title {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 14px;
}

.firing {
  padding: 0 7px;
  border-radius: 999px;
  font-size: 12px;
  line-height: 20px;
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
}

.rule-desc {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.param {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  color: var(--ys-text-label);
}

.num {
  width: 100px;
}

.unit {
  width: 16px;
}

.ch-select {
  width: 220px;
}

.ch-name {
  font-weight: 500;
}

.tag-sm {
  margin-left: 8px;
  padding: 0 6px;
  border-radius: 4px;
  font-size: 11px;
  line-height: 18px;
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
}

.mono {
  font-family: var(--ys-font-mono);
}

.form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.label {
  font-size: 13px;
  font-weight: 500;
  color: var(--ys-text-label);
}

.hint {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.mono-input :deep(input) {
  font-family: var(--ys-font-mono);
}
</style>
