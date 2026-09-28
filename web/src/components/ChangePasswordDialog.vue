<script setup lang="ts">
import { reactive, ref, watch } from 'vue'
import { ElMessage, type FormInstance, type FormRules } from 'element-plus'
import { errorMessage } from '@/api'
import { useAuthStore } from '@/stores/auth'
import { passwordRule } from '@/utils/password'

const open = defineModel<boolean>({ required: true })

const auth = useAuthStore()
const formRef = ref<FormInstance>()
const form = reactive({ old: '', next: '', confirm: '' })
const saving = ref(false)
const error = ref('')

const rules: FormRules<typeof form> = {
  old: [{ required: true, message: '请输入当前密码', trigger: 'blur' }],
  next: [
    { required: true, message: '请输入新密码', trigger: 'blur' },
    passwordRule,
    {
      validator: (_r, v: string, cb) => (v && v === form.old ? cb(new Error('新密码不能与当前密码相同')) : cb()),
      trigger: 'blur',
    },
  ],
  confirm: [
    { required: true, message: '请再次输入新密码', trigger: 'blur' },
    {
      validator: (_r, v: string, cb) => (v !== form.next ? cb(new Error('两次输入的密码不一致')) : cb()),
      trigger: 'blur',
    },
  ],
}

watch(open, (v) => {
  if (v) {
    form.old = form.next = form.confirm = ''
    error.value = ''
    formRef.value?.clearValidate()
  }
})

async function submit() {
  if (!(await formRef.value?.validate().catch(() => false))) return
  saving.value = true
  error.value = ''
  try {
    await auth.changePassword(form.old, form.next)
    open.value = false
    ElMessage.success('密码已修改，其他设备上的登录已失效')
  } catch (err) {
    error.value = errorMessage(err)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-dialog v-model="open" title="修改密码" width="440px" :close-on-click-modal="false" append-to-body>
    <el-form ref="formRef" :model="form" :rules="rules" label-position="top" @submit.prevent="submit">
      <el-form-item label="当前密码" prop="old">
        <el-input v-model="form.old" type="password" show-password autocomplete="current-password" />
      </el-form-item>
      <el-form-item label="新密码" prop="next">
        <el-input
          v-model="form.next"
          type="password"
          show-password
          autocomplete="new-password"
          placeholder="至少 10 位，需同时包含字母和数字"
        />
      </el-form-item>
      <el-form-item label="确认新密码" prop="confirm">
        <el-input v-model="form.confirm" type="password" show-password autocomplete="new-password" />
      </el-form-item>
      <p class="hint">修改后，其他设备上的登录会失效，需要重新登录。</p>
      <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="err" />
      <button type="submit" hidden />
    </el-form>
    <template #footer>
      <el-button @click="open = false">取消</el-button>
      <el-button type="primary" :loading="saving" @click="submit">确认修改</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.hint {
  font-size: 12px;
  color: var(--ys-text-muted);
}

.err {
  margin-top: 12px;
}
</style>
