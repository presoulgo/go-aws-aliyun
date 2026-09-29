<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import AppIcon from '@/components/AppIcon.vue'
import SegmentedControl from '@/components/SegmentedControl.vue'
import { accountApi, errorMessage, type Account, type AccountInput, type Provider, type TestResult } from '@/api'
import { useAppStore } from '@/stores/app'
import { intervalText } from '@/utils/format'
import { formatUID } from '@/utils/resource'

const open = defineModel<boolean>({ required: true })
const props = defineProps<{ account: Account | null }>()
const emit = defineEmits<{ saved: [account: Account, created: boolean] }>()

const app = useAppStore()
const editing = computed(() => !!props.account)

const form = reactive({
  provider: 'aws' as Provider,
  partition: 'aws',
  name: '',
  remark: '',
  accessKeyId: '',
  accessKeySecret: '',
  roleArn: '',
})
const errors = reactive({ name: '', accessKeyId: '', accessKeySecret: '', roleArn: '' })
const testing = ref(false)
const saving = ref(false)
const testError = ref('')
const result = ref<TestResult | null>(null)
/** null 表示全部地域（包括以后新开通的） */
const picked = ref<string[] | null>(null)
// 最近一次测试通过时的凭证，凭证改动后需要重新测试。
const testedKey = ref('')

const credKey = computed(() =>
  [form.provider, form.partition, form.accessKeyId.trim(), form.accessKeySecret, form.roleArn.trim()].join('|'),
)
const tested = computed(() => !!result.value && testedKey.value === credKey.value)
// 编辑时只改名称、备注、地域不需要重新测试。
const credChanged = computed(() => {
  const a = props.account
  if (!a) return true
  return (
    form.accessKeyId.trim() !== '' ||
    form.accessKeySecret !== '' ||
    form.roleArn.trim() !== a.role_arn ||
    (a.provider === 'aws' && form.partition !== a.partition)
  )
})
const canSave = computed(() => (editing.value && !credChanged.value) || tested.value)

watch([open, () => props.account?.id], ([v]) => {
  if (!v) return
  const a = props.account
  form.provider = a?.provider ?? 'aws'
  form.partition = a?.partition || 'aws'
  form.name = a?.name ?? ''
  form.remark = a?.remark ?? ''
  form.accessKeyId = ''
  form.accessKeySecret = ''
  form.roleArn = a?.role_arn ?? ''
  Object.assign(errors, { name: '', accessKeyId: '', accessKeySecret: '', roleArn: '' })
  testError.value = ''
  result.value = null
  testedKey.value = ''
  picked.value = a && a.regions.length ? [...a.regions] : null
  // 编辑时自动用已保存的凭证测试一次，列出可选地域。
  if (a) void runTest(true)
})

watch(
  () => [form.provider, form.partition],
  () => {
    if (!editing.value) {
      result.value = null
      picked.value = null
    }
  },
)

const providers = [
  { value: 'aws' as const, label: 'AWS', short: 'AWS', desc: 'IAM 用户 AccessKey，支持中国区' },
  { value: 'aliyun' as const, label: '阿里云', short: '阿里', desc: 'RAM 用户 AccessKey，支持 RAM 角色' },
]
const partitions = [
  { value: 'aws', label: '全球区' },
  { value: 'aws-cn', label: '中国区' },
]

const steps = computed(() => {
  const s = [
    { label: '填写凭证', state: tested.value ? 'done' : 'active' },
    { label: '测试连接', state: tested.value ? 'done' : testing.value ? 'active' : 'todo' },
    { label: '选择地域', state: tested.value ? 'active' : 'todo' },
  ]
  return s
})

function validate(forTest: boolean): boolean {
  errors.name = !forTest && !form.name.trim() ? '请填写账号名称' : ''
  const needKey = !editing.value
  errors.accessKeyId = needKey && !form.accessKeyId.trim() ? '请填写 AccessKey ID' : ''
  errors.accessKeySecret = needKey && !form.accessKeySecret ? '请填写 AccessKey Secret' : ''
  const role = form.roleArn.trim()
  if (role && form.provider === 'aws' && !role.startsWith('arn:aws')) errors.roleArn = '应以 arn:aws 开头'
  else if (role && form.provider === 'aliyun' && !role.startsWith('acs:ram::')) errors.roleArn = '应以 acs:ram:: 开头'
  else errors.roleArn = ''
  return !errors.name && !errors.accessKeyId && !errors.accessKeySecret && !errors.roleArn
}

function payload(): AccountInput {
  const regions = picked.value ?? []
  return {
    name: form.name.trim(),
    provider: form.provider,
    partition: form.provider === 'aws' ? form.partition : '',
    access_key_id: form.accessKeyId.trim(),
    access_key_secret: form.accessKeySecret || undefined,
    role_arn: form.roleArn.trim(),
    regions,
    remark: form.remark.trim(),
  }
}

async function runTest(silent = false) {
  if (!validate(true)) return
  testing.value = true
  testError.value = ''
  const key = credKey.value
  try {
    const res = props.account
      ? await accountApi.testExisting(props.account.id, payload())
      : await accountApi.test({ ...payload(), name: form.name.trim() || 'test' })
    result.value = res
    testedKey.value = key
    // 之前选过的地域里，去掉账号未开通的。
    if (picked.value) {
      const ok = new Set(res.regions.map((r) => r.id))
      picked.value = picked.value.filter((r) => ok.has(r))
      if (picked.value.length === res.regions.length) picked.value = null
    }
  } catch (err) {
    result.value = null
    if (!silent || !props.account) testError.value = errorMessage(err)
    else testError.value = `无法用已保存的凭证获取地域列表：${errorMessage(err)}`
  } finally {
    testing.value = false
  }
}

const regions = computed(() => result.value?.regions ?? [])
const regionSummary = computed(() =>
  picked.value === null
    ? `· 全部 ${regions.value.length} 个（新开通的地域也会自动纳入）`
    : `· 已选 ${picked.value.length} / ${regions.value.length} 个`,
)

function isOn(id: string) {
  return picked.value === null || picked.value.includes(id)
}

function toggleRegion(id: string) {
  const all = regions.value.map((r) => r.id)
  const cur = picked.value === null ? [...all] : [...picked.value]
  const i = cur.indexOf(id)
  if (i >= 0) cur.splice(i, 1)
  else cur.push(id)
  picked.value = cur.length === all.length ? null : all.filter((r) => cur.includes(r))
}

async function save() {
  if (!validate(false)) return
  if (!canSave.value) {
    testError.value = '请先测试连接'
    return
  }
  if (picked.value !== null && picked.value.length === 0) {
    testError.value = '请至少选择一个地域'
    return
  }
  saving.value = true
  testError.value = ''
  try {
    const body = payload()
    const acc = props.account ? await accountApi.update(props.account.id, body) : await accountApi.create(body)
    open.value = false
    ElMessage.success(props.account ? '已保存' : '已保存，正在进行首次同步')
    emit('saved', acc, !props.account)
  } catch (err) {
    testError.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}

const placeholders = computed(() =>
  form.provider === 'aws'
    ? { name: '例如：AWS 海外电商', ak: 'AKIA 开头，20 位', role: 'arn:aws:iam::123456789012:role/OpsReadOnly' }
    : { name: '例如：阿里云 视频业务', ak: 'LTAI 开头', role: 'acs:ram::1234567890123456:role/ops-readonly' },
)
</script>

<template>
  <el-dialog
    v-model="open"
    width="840px"
    align-center
    :show-close="false"
    :close-on-click-modal="false"
    class="account-dialog"
    append-to-body
    :aria-label="editing ? '编辑云账号' : '新增云账号'"
  >
    <template #header>
      <div class="dlg-head">
        <h2>{{ editing ? '编辑云账号' : '新增云账号' }}</h2>
        <ol class="steps" aria-label="步骤">
          <li v-for="(s, i) in steps" :key="s.label" :class="s.state">
            <span class="dot">
              <AppIcon v-if="s.state === 'done'" name="check" :size="12" />
              <template v-else>{{ i + 1 }}</template>
            </span>
            {{ s.label }}
            <span v-if="i < steps.length - 1" class="line" />
          </li>
        </ol>
        <button type="button" class="close" aria-label="关闭" @click="open = false">
          <AppIcon name="close" :size="18" />
        </button>
      </div>
    </template>

    <form class="form" autocomplete="off" @submit.prevent="save">
      <div v-if="!editing" class="field">
        <span class="label">云厂商</span>
        <div class="providers" role="radiogroup" aria-label="云厂商">
          <button
            v-for="p in providers"
            :key="p.value"
            type="button"
            role="radio"
            class="provider"
            :class="[p.value, { on: form.provider === p.value }]"
            :aria-checked="form.provider === p.value"
            @click="form.provider = p.value"
          >
            <span class="logo">{{ p.short }}</span>
            <span class="p-text">
              <span class="p-name">{{ p.label }}</span>
              <span class="p-desc">{{ p.desc }}</span>
            </span>
            <span class="ring" />
          </button>
        </div>
      </div>

      <div class="grid2">
        <label class="field">
          <span class="label">账号名称</span>
          <el-input v-model="form.name" :placeholder="placeholders.name" maxlength="64" />
          <span v-if="errors.name" class="err">{{ errors.name }}</span>
        </label>
        <div v-if="form.provider === 'aws'" class="field">
          <span class="label">分区</span>
          <SegmentedControl v-model="form.partition" :options="partitions" label="分区" />
        </div>
        <label v-else class="field">
          <span class="label">备注（可选）</span>
          <el-input v-model="form.remark" placeholder="例如：视频业务线，负责人 ops" maxlength="200" />
        </label>
      </div>

      <div class="grid2">
        <label class="field">
          <span class="label">AccessKey ID</span>
          <el-input
            v-model="form.accessKeyId"
            class="mono-input"
            :placeholder="editing ? `${account?.access_key_masked}（留空表示不修改）` : placeholders.ak"
            autocomplete="off"
            spellcheck="false"
          />
          <span v-if="errors.accessKeyId" class="err">{{ errors.accessKeyId }}</span>
        </label>
        <label class="field">
          <span class="label">
            AccessKey Secret <span class="label-note">· AES-256-GCM 加密存储，保存后不再显示</span>
          </span>
          <el-input
            v-model="form.accessKeySecret"
            class="mono-input"
            type="password"
            show-password
            :placeholder="editing ? '留空表示不修改' : '输入后加密保存'"
            autocomplete="new-password"
          />
          <span v-if="errors.accessKeySecret" class="err">{{ errors.accessKeySecret }}</span>
        </label>
      </div>

      <div :class="form.provider === 'aws' ? 'grid-role' : ''">
        <label class="field">
          <span class="label">
            角色 ARN <span class="label-note">（可选：用上面的 AK 扮演该角色访问成员账号）</span>
          </span>
          <el-input v-model="form.roleArn" class="mono-input" :placeholder="placeholders.role" spellcheck="false" />
          <span v-if="errors.roleArn" class="err">{{ errors.roleArn }}</span>
        </label>
        <label v-if="form.provider === 'aws'" class="field">
          <span class="label">备注（可选）</span>
          <el-input v-model="form.remark" placeholder="例如：海外电商，负责人 ops" maxlength="200" />
        </label>
      </div>

      <div v-if="testError" class="alert error" role="alert">
        <AppIcon name="errorCircle" :size="18" />
        <span>{{ testError }}</span>
      </div>

      <template v-if="result">
        <div class="alert ok">
          <AppIcon name="checkCircle" :size="18" />
          <span>
            连接成功 · 云账号 UID <span class="mono">{{ formatUID(result.account_uid, form.provider) }}</span> · 发现
            {{ result.regions.length }} 个已开通地域
          </span>
        </div>
        <div class="region-head">
          <span class="label">同步地域 <span class="label-note">{{ regionSummary }}</span></span>
          <button type="button" class="ys-link small" :disabled="picked === null" @click="picked = null">
            {{ picked === null ? '全部已选' : '全选' }}
          </button>
        </div>
        <div class="regions">
          <button
            v-for="r in regions"
            :key="r.id"
            type="button"
            role="checkbox"
            class="region"
            :class="{ on: isOn(r.id) }"
            :aria-checked="isOn(r.id)"
            @click="toggleRegion(r.id)"
          >
            <span class="box"><AppIcon v-if="isOn(r.id)" name="check" :size="10" :stroke="3.5" /></span>
            <span class="mono">{{ r.id }}</span>
            <span class="r-name">{{ r.name }}</span>
          </button>
        </div>
      </template>
      <div v-else class="placeholder">
        {{
          testing
            ? '正在通过 STS 校验凭证并获取地域列表…'
            : '测试连接成功后，这里会列出账号已开通的地域，默认全部同步'
        }}
      </div>
      <button type="submit" hidden />
    </form>

    <template #footer>
      <div class="dlg-foot">
        <span class="foot-note">
          {{
            editing
              ? credChanged
                ? '凭证有改动，保存前需要重新测试连接'
                : '只修改名称、备注或地域时无需重新测试'
              : `保存后立即同步一次，之后${intervalText(app.meta.sync_interval_minutes)}自动同步`
          }}
        </span>
        <el-button @click="open = false">取消</el-button>
        <el-button class="test-btn" :loading="testing" @click="runTest()">
          <AppIcon v-if="!testing" name="pulse" :size="15" />
          <span class="gap">{{ testing ? '测试中…' : tested ? '重新测试' : '测试连接' }}</span>
        </el-button>
        <el-tooltip :disabled="canSave" content="请先测试连接" placement="top">
          <span>
            <el-button
              type="primary"
              :disabled="!canSave || (picked !== null && picked.length === 0)"
              :loading="saving"
              @click="save"
            >
              {{ editing ? '保存' : '保存并同步' }}
            </el-button>
          </span>
        </el-tooltip>
      </div>
    </template>
  </el-dialog>
</template>

<style scoped>
.dlg-head {
  height: 64px;
  padding: 0 24px;
  display: flex;
  align-items: center;
  gap: 16px;
  border-bottom: 1px solid var(--ys-divider);
}

.dlg-head h2 {
  font-size: 18px;
  font-weight: 600;
}

.steps {
  margin: 0 0 0 auto;
  padding: 0;
  list-style: none;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}

.steps li {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--ys-text);
}

.steps li.todo {
  color: var(--ys-text-muted);
}

.steps .dot {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 600;
  background: var(--ys-primary);
  color: #fff;
}

.steps li.todo .dot {
  background: var(--ys-divider);
  color: var(--ys-text-muted);
}

.steps li.done .dot {
  background: var(--ys-ok-bg);
  color: var(--ys-ok-fg);
}

.steps .line {
  width: 24px;
  height: 1px;
  background: var(--ys-border-input);
}

.close {
  margin-left: 8px;
  width: 36px;
  height: 36px;
  border: 0;
  border-radius: 8px;
  background: transparent;
  color: var(--ys-text-label);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.close:hover {
  background: var(--ys-bg-muted);
}

.form {
  max-height: calc(100vh - 180px);
  overflow-y: auto;
  padding: 20px 24px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
  min-width: 0;
}

.label {
  font-size: 13px;
  font-weight: 500;
  color: var(--ys-text-label);
}

.label-note {
  font-weight: 400;
  color: var(--ys-text-muted);
}

.err {
  font-size: 12px;
  color: var(--ys-err-fg);
}

.grid2 {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.grid-role {
  display: grid;
  grid-template-columns: minmax(0, 3fr) minmax(0, 2fr);
  gap: 16px;
}

.field :deep(.el-input__wrapper) {
  min-height: 38px;
}

.field > .segmented {
  align-self: flex-start;
}

.mono-input :deep(input) {
  font-family: var(--ys-font-mono);
  font-size: 13px;
}

.providers {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 12px;
}

.provider {
  height: 60px;
  padding: 0 16px;
  display: flex;
  align-items: center;
  gap: 12px;
  border: 1px solid var(--ys-border-input);
  border-radius: 10px;
  background: #fff;
  text-align: left;
  cursor: pointer;
}

.provider.on {
  border: 1.5px solid var(--ys-primary);
  background: #f5f7fe;
}

.logo {
  width: 36px;
  height: 36px;
  flex-shrink: 0;
  border-radius: 8px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 700;
}

.provider.aws .logo {
  background: var(--ys-aws-tag-bg);
  color: var(--ys-aws-tag-fg);
}

.provider.aliyun .logo {
  background: var(--ys-aliyun-tag-bg);
  color: var(--ys-aliyun-tag-fg);
}

.p-text {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.p-name {
  font-size: 14px;
  font-weight: 600;
  color: var(--ys-text);
}

.p-desc {
  font-size: 12px;
  color: var(--ys-text-secondary);
}

.ring {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  border: 1.5px solid #aeb7c6;
}

.provider.on .ring {
  border: 5px solid var(--ys-primary);
}

.alert {
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 14px;
  border-radius: 8px;
  font-size: 13px;
  line-height: 1.6;
}

.alert :deep(svg) {
  margin-top: 1px;
}

.alert.ok {
  background: var(--ys-ok-bg);
  border: 1px solid #bfe3ce;
  color: #11643a;
}

.alert.error {
  background: var(--ys-err-bg);
  border: 1px solid var(--ys-err-border);
  color: var(--ys-err-fg);
}

.region-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.small {
  font-size: 12px;
}

.regions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  max-height: 172px;
  overflow-y: auto;
}

.region {
  height: 32px;
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  gap: 6px;
  border: 1px solid #dde1e7;
  border-radius: 7px;
  background: #fff;
  color: var(--ys-text-secondary);
  font-size: 12px;
  cursor: pointer;
}

.region.on {
  border-color: #afc0f0;
  background: #f5f7fe;
  color: var(--ys-text);
}

.box {
  width: 14px;
  height: 14px;
  border-radius: 4px;
  border: 1.5px solid #aeb7c6;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #fff;
}

.region.on .box {
  border-color: var(--ys-primary);
  background: var(--ys-primary);
}

.r-name {
  color: var(--ys-text-muted);
}

.placeholder {
  height: 56px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px dashed #c9ced6;
  border-radius: 8px;
  font-size: 13px;
  color: var(--ys-text-muted);
}

.dlg-foot {
  height: 68px;
  padding: 0 24px;
  display: flex;
  align-items: center;
  gap: 10px;
  border-top: 1px solid var(--ys-divider);
}

.foot-note {
  margin-right: auto;
  font-size: 12px;
  color: var(--ys-text-muted);
}

.dlg-foot .el-button + .el-button,
.dlg-foot span + .el-button {
  margin-left: 0;
}

.test-btn {
  --el-button-text-color: var(--ys-primary);
  --el-button-border-color: var(--ys-primary);
  --el-button-hover-text-color: var(--ys-primary);
  --el-button-hover-border-color: var(--ys-primary);
  --el-button-hover-bg-color: var(--ys-primary-soft);
}

.gap {
  margin-left: 8px;
}
</style>

<style>
.account-dialog.el-dialog {
  padding: 0;
  --el-dialog-padding-primary: 0;
}

.account-dialog .el-dialog__header {
  padding: 0;
  margin: 0;
}

.account-dialog .el-dialog__footer {
  padding: 0;
}
</style>
