<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '@/components/AppIcon.vue'
import PageHeader from '@/components/PageHeader.vue'
import StatusTag from '@/components/StatusTag.vue'
import { errorMessage, userApi, type Role, type UserView } from '@/api'
import { useAppStore } from '@/stores/app'
import { copyText } from '@/utils/clipboard'
import { formatDate, formatShortTime } from '@/utils/format'
import { checkPassword, generatePassword } from '@/utils/password'
import { roleLabel } from '@/utils/status'

const app = useAppStore()
const users = ref<UserView[]>([])
const loaded = ref(false)

async function load() {
  const res = await userApi.list()
  users.value = res.items
  loaded.value = true
}
onMounted(load)

const summary = computed(() => {
  const list = users.value
  const admins = list.filter((u) => u.role === 'admin').length
  const disabled = list.filter((u) => u.disabled).length
  return `${list.length} 个用户 · ${admins} 个管理员 · ${list.length - admins} 个只读 · ${disabled} 个已禁用`
})

const roles: { value: Role; label: string; desc: string }[] = [
  { value: 'admin', label: '管理员', desc: '可纳管云账号、触发同步、管理用户' },
  { value: 'viewer', label: '只读', desc: '只能查看概览、资源和监控' },
]

function status(u: UserView) {
  if (u.disabled) return { tone: 'off' as const, label: '已禁用' }
  if (u.failed_today > 0) return { tone: 'warn' as const, label: `今天 ${u.failed_today} 次登录失败` }
  return { tone: 'ok' as const, label: '正常' }
}

// ---------- 新增 / 编辑 ----------
const dialog = reactive({
  open: false,
  mode: 'create' as 'create' | 'edit',
  id: 0,
  username: '',
  displayName: '',
  password: '',
  role: 'viewer' as Role,
  me: false,
  error: '',
  saving: false,
})

function openCreate() {
  Object.assign(dialog, {
    open: true,
    mode: 'create',
    id: 0,
    username: '',
    displayName: '',
    password: '',
    role: 'viewer',
    me: false,
    error: '',
  })
}

function openEdit(u: UserView) {
  Object.assign(dialog, {
    open: true,
    mode: 'edit',
    id: u.id,
    username: u.username,
    displayName: u.display_name,
    password: '',
    role: u.role,
    me: u.me,
    error: '',
  })
}

async function submit() {
  dialog.error = ''
  if (dialog.mode === 'create') {
    const name = dialog.username.trim().toLowerCase()
    if (!/^[a-z0-9][a-z0-9._-]{1,31}$/.test(name)) {
      dialog.error = '用户名为 2–32 位小写字母、数字、点、下划线或中划线，且以字母或数字开头'
      return
    }
    const pwErr = checkPassword(dialog.password)
    if (pwErr) {
      dialog.error = pwErr
      return
    }
  }
  dialog.saving = true
  try {
    if (dialog.mode === 'create') {
      await userApi.create({
        username: dialog.username.trim().toLowerCase(),
        display_name: dialog.displayName.trim(),
        password: dialog.password,
        role: dialog.role,
      })
      ElMessage.success(`已创建用户 ${dialog.username.trim().toLowerCase()}，请把初始密码私下发给对方`)
    } else {
      await userApi.update(dialog.id, {
        display_name: dialog.displayName.trim(),
        role: dialog.me ? undefined : dialog.role,
      })
      ElMessage.success('已保存')
    }
    dialog.open = false
    await load()
  } catch (err) {
    dialog.error = errorMessage(err)
  } finally {
    dialog.saving = false
  }
}

// ---------- 重置密码 / 启停 / 删除 ----------
const resetResult = reactive({ open: false, username: '', password: '' })

async function resetPassword(u: UserView) {
  try {
    await ElMessageBox.confirm(`将为 ${u.username} 生成一个新的随机密码，旧密码和已登录的会话立即失效。`, '重置密码', {
      confirmButtonText: '重置',
      cancelButtonText: '取消',
      type: 'warning',
    })
  } catch {
    return
  }
  const res = await userApi.resetPassword(u.id)
  Object.assign(resetResult, { open: true, username: u.username, password: res.password })
  await load()
}

async function toggleDisabled(u: UserView) {
  const disable = !u.disabled
  if (disable) {
    try {
      await ElMessageBox.confirm(`禁用后 ${u.username} 将立即退出登录，且无法再登录。`, '禁用用户', {
        confirmButtonText: '禁用',
        cancelButtonText: '取消',
        type: 'warning',
      })
    } catch {
      return
    }
  }
  try {
    await userApi.update(u.id, { disabled: disable })
  } catch (err) {
    ElMessage.error(errorMessage(err))
    return
  }
  ElMessage.success(disable ? `已禁用 ${u.username}` : `已启用 ${u.username}`)
  await load()
}

async function remove(u: UserView) {
  try {
    await ElMessageBox.confirm(`删除后 ${u.username} 无法再登录，审计日志中的历史记录会保留。`, `删除用户 ${u.username}？`, {
      confirmButtonText: '确认删除',
      cancelButtonText: '取消',
      type: 'error',
      confirmButtonClass: 'el-button--danger',
    })
  } catch {
    return
  }
  await userApi.remove(u.id)
  ElMessage.success(`已删除 ${u.username}`)
  await load()
}
</script>

<template>
  <PageHeader title="用户管理" :subtitle="loaded ? summary : '正在加载…'">
    <el-button type="primary" class="add-btn" @click="openCreate">
      <AppIcon name="plus" :size="15" />
      <span class="gap">新增用户</span>
    </el-button>
  </PageHeader>

  <div class="roles">
    <div v-for="r in roles" :key="r.value" class="ys-card role-card">
      <span class="role-tag" :class="r.value">{{ r.label }}</span>
      <span>{{
        r.value === 'admin' ? '纳管和编辑云账号、触发同步、管理用户、查看审计日志' : '查看概览、资源清单和监控；不能修改任何配置'
      }}</span>
    </div>
  </div>

  <section class="ys-card table-card">
    <el-table v-loading="!loaded" :data="users" row-key="id" class="user-table" :row-class-name="({ row }: { row: UserView }) => (row.disabled ? 'is-off' : '')">
      <el-table-column label="用户" min-width="220">
        <template #default="{ row }">
          <div class="user-cell">
            <span class="avatar" :class="row.role">{{ row.username.slice(0, 1).toUpperCase() }}</span>
            <span class="stack">
              <span class="name-line">
                <span class="mono strong">{{ row.username }}</span>
                <span v-if="row.me" class="me-tag">当前登录</span>
              </span>
              <span class="sub-muted">{{ row.display_name || '—' }}</span>
            </span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="角色" width="110">
        <template #default="{ row }">
          <span class="role-tag" :class="row.role">{{ roleLabel[row.role] }}</span>
        </template>
      </el-table-column>
      <el-table-column label="状态" width="160">
        <template #default="{ row }"><StatusTag :tone="status(row).tone" :label="status(row).label" /></template>
      </el-table-column>
      <el-table-column label="最近登录" width="190">
        <template #default="{ row }">
          <div class="stack">
            <span>{{ row.last_login_at ? formatShortTime(row.last_login_at, app.now) : '从未登录' }}</span>
            <span class="sub-mono">{{ row.last_login_ip }}</span>
          </div>
        </template>
      </el-table-column>
      <el-table-column label="创建时间" width="120">
        <template #default="{ row }"><span class="sub">{{ formatDate(row.created_at) }}</span></template>
      </el-table-column>
      <el-table-column label="操作" width="250">
        <template #default="{ row }">
          <div class="ops">
            <button type="button" class="ys-link" @click="openEdit(row)">编辑</button>
            <button type="button" class="ys-link" @click="resetPassword(row)">重置密码</button>
            <el-tooltip :disabled="!row.me" content="不能禁用自己" placement="top">
              <button type="button" class="ys-link" :disabled="row.me" @click="toggleDisabled(row)">
                {{ row.disabled ? '启用' : '禁用' }}
              </button>
            </el-tooltip>
            <el-tooltip :disabled="!row.me" content="不能删除自己" placement="top">
              <button type="button" class="ys-link danger" :disabled="row.me" @click="remove(row)">删除</button>
            </el-tooltip>
          </div>
        </template>
      </el-table-column>
    </el-table>
  </section>

  <el-dialog
    v-model="dialog.open"
    :title="dialog.mode === 'create' ? '新增用户' : `编辑用户 ${dialog.username}`"
    width="520px"
    :close-on-click-modal="false"
    append-to-body
  >
    <form class="form" autocomplete="off" @submit.prevent="submit">
      <div class="grid2">
        <label class="field">
          <span class="label">用户名</span>
          <el-input
            v-model="dialog.username"
            class="mono-input"
            placeholder="字母、数字、点，例如 li.ming"
            :disabled="dialog.mode === 'edit'"
            maxlength="32"
          />
        </label>
        <label class="field">
          <span class="label">显示名称</span>
          <el-input v-model="dialog.displayName" placeholder="例如：数据组 李明" maxlength="64" />
        </label>
      </div>
      <div v-if="dialog.mode === 'create'" class="field">
        <label class="label" for="new-user-pw">初始密码</label>
        <div class="pw-row">
          <el-input
            id="new-user-pw"
            v-model="dialog.password"
            class="mono-input"
            placeholder="输入初始密码，或点右侧随机生成"
            autocomplete="new-password"
          />
          <el-button @click="dialog.password = generatePassword()">随机生成</el-button>
          <el-button :disabled="!dialog.password" @click="copyText(dialog.password, '已复制密码')">复制</el-button>
        </div>
        <span class="hint">至少 10 位，包含字母和数字；把密码私下发给对方，首次登录后提醒修改</span>
      </div>
      <div class="field">
        <span class="label">角色</span>
        <div class="role-pick" role="radiogroup" aria-label="角色">
          <button
            v-for="r in roles"
            :key="r.value"
            type="button"
            role="radio"
            class="role-opt"
            :class="{ on: dialog.role === r.value }"
            :aria-checked="dialog.role === r.value"
            :disabled="dialog.me"
            @click="dialog.role = r.value"
          >
            <span class="role-name">{{ r.label }}</span>
            <span class="role-desc">{{ r.desc }}</span>
          </button>
        </div>
        <span v-if="dialog.me" class="hint">不能修改自己的角色</span>
      </div>
      <el-alert v-if="dialog.error" :title="dialog.error" type="error" :closable="false" show-icon />
      <button type="submit" hidden />
    </form>
    <template #footer>
      <el-button @click="dialog.open = false">取消</el-button>
      <el-button type="primary" :loading="dialog.saving" @click="submit">
        {{ dialog.mode === 'create' ? '创建用户' : '保存' }}
      </el-button>
    </template>
  </el-dialog>

  <el-dialog v-model="resetResult.open" title="密码已重置" width="440px" append-to-body>
    <p class="reset-tip">{{ resetResult.username }} 的新密码如下，只显示这一次，请私下发给对方：</p>
    <div class="reset-pw">
      <span class="mono">{{ resetResult.password }}</span>
      <el-button size="small" @click="copyText(resetResult.password, '已复制密码')">复制</el-button>
    </div>
    <template #footer>
      <el-button type="primary" @click="resetResult.open = false">我已记下</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.add-btn {
  height: 38px;
}

.gap {
  margin-left: 8px;
}

.roles {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.role-card {
  padding: 14px 18px;
  display: flex;
  align-items: center;
  gap: 14px;
  font-size: 13px;
  color: var(--ys-text-label);
}

.role-tag {
  display: inline-block;
  padding: 2px 8px;
  border-radius: var(--ys-radius-tag);
  font-size: 12px;
  white-space: nowrap;
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
}

.role-tag.admin {
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
}

.table-card {
  overflow: hidden;
}

.user-table :deep(.el-table__cell) {
  height: 64px;
}

.user-table :deep(th.el-table__cell) {
  height: 44px;
}

.user-table :deep(tr > :first-child .cell) {
  padding-left: 20px;
}

.user-table :deep(.el-table__row.is-off > td .cell) {
  opacity: 0.72;
}

.user-cell {
  display: flex;
  align-items: center;
  gap: 12px;
}

.avatar {
  width: 34px;
  height: 34px;
  flex-shrink: 0;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 600;
  background: var(--ys-divider);
  color: var(--ys-text-label);
}

.avatar.admin {
  background: #dde4f8;
  color: var(--ys-primary);
}

.stack {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.name-line {
  display: flex;
  align-items: center;
  gap: 8px;
}

.strong {
  font-weight: 500;
}

.me-tag {
  padding: 0 6px;
  border-radius: 4px;
  font-size: 11px;
  line-height: 18px;
  background: var(--ys-ok-bg);
  color: var(--ys-ok-fg);
}

.sub-muted {
  font-size: 12px;
  color: var(--ys-text-muted);
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

.ops {
  display: flex;
  align-items: center;
  gap: 12px;
}

.form {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.grid2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 14px;
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

.mono-input :deep(input) {
  font-family: var(--ys-font-mono);
}

.pw-row {
  display: flex;
  gap: 8px;
}

.pw-row .el-button + .el-button {
  margin-left: 0;
}

.hint {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.role-pick {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
}

.role-opt {
  padding: 12px 14px;
  display: flex;
  flex-direction: column;
  align-items: flex-start;
  gap: 4px;
  border: 1px solid var(--ys-border-input);
  border-radius: 10px;
  background: #fff;
  text-align: left;
  cursor: pointer;
}

.role-opt.on {
  border: 1.5px solid var(--ys-primary);
  background: #f5f7fe;
}

.role-opt:disabled {
  cursor: not-allowed;
  opacity: 0.6;
}

.role-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--ys-text);
}

.role-desc {
  font-size: 12px;
  line-height: 1.5;
  color: var(--ys-text-secondary);
}

.reset-tip {
  font-size: 14px;
  line-height: 1.7;
  color: var(--ys-text-label);
}

.reset-pw {
  margin-top: 12px;
  padding: 12px 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  border-radius: 8px;
  background: var(--ys-bg-subtle);
  border: 1px solid var(--ys-divider);
  font-size: 16px;
  letter-spacing: 0.04em;
}
</style>
