<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import { errorMessage } from '@/api'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()

const form = reactive({ username: '', password: '' })
const showPw = ref(false)
const loading = ref(false)
const error = ref('')
const userInput = ref<HTMLInputElement>()

const expired = computed(() => route.query.expired === '1' && !error.value)
const version = computed(() => (/^\d/.test(app.meta.version) ? `v${app.meta.version}` : app.meta.version))

function redirectTarget(): string {
  const r = route.query.redirect
  return typeof r === 'string' && r.startsWith('/') && !r.startsWith('//') ? r : '/dashboard'
}

async function submit() {
  if (loading.value) return
  if (!form.username.trim() || !form.password) {
    error.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  error.value = ''
  try {
    await auth.login(form.username.trim(), form.password)
    await router.replace(redirectTarget())
  } catch (err) {
    error.value = errorMessage(err)
    form.password = ''
  } finally {
    loading.value = false
  }
}

onMounted(() => userInput.value?.focus())
</script>

<template>
  <div class="login">
    <section class="brand">
      <svg class="pattern" width="520" height="520" viewBox="0 0 520 520" fill="none" aria-hidden="true">
        <path d="M260 20l207.8 120v240L260 500 52.2 380V140z" />
        <path d="M260 80l155.9 90v180L260 440 104.1 350V170z" />
        <path d="M260 140l103.9 60v120L260 380 156.1 320V200z" />
        <path d="M260 200l51.9 30v60L260 320l-51.9-30v-60z" />
        <path d="M260 20v180M467.8 140L311.9 230M467.8 380L311.9 290M260 500V320M52.2 380l155.9-90M52.2 140l155.9 90" />
      </svg>
      <div class="logo">
        <span class="logo-mark"><AppIcon name="logo" :size="24" /></span>
        <span class="logo-text">
          <strong>{{ app.meta.name }}</strong>
          <span>多云运维聚合平台</span>
        </span>
      </div>
      <div class="slogan">
        <h1>AWS 与阿里云，<br />一个视图看全</h1>
        <p>把多个账号、多个地域的资源和监控收拢到一起，排查问题不用再在两个控制台之间来回切换。</p>
      </div>
      <ul class="features">
        <li>
          <span class="feat-icon"><AppIcon name="stack" :size="17" /></span>
          统一资源清单：云主机、数据库、负载均衡、对象存储
        </li>
        <li>
          <span class="feat-icon"><AppIcon name="monitor" :size="17" /></span>
          监控聚合：CloudWatch 与云监控曲线同屏对比
        </li>
        <li>
          <span class="feat-icon"><AppIcon name="lock" :size="17" /></span>
          安全接入：只读 AK 加密存储，关键操作全程审计
        </li>
      </ul>
      <span class="version mono">{{ version }}</span>
    </section>

    <section class="panel">
      <form class="form" novalidate @submit.prevent="submit">
        <div class="heading">
          <h2>登录{{ app.meta.name }}</h2>
          <p>使用平台账号登录，账号由管理员创建</p>
        </div>
        <label class="field">
          用户名
          <input
            ref="userInput"
            v-model="form.username"
            type="text"
            name="username"
            autocomplete="username"
            autocapitalize="off"
            spellcheck="false"
            placeholder="请输入用户名"
            class="input"
          />
        </label>
        <div class="field">
          <label for="login-password">密码</label>
          <div class="input pw">
            <input
              id="login-password"
              v-model="form.password"
              :type="showPw ? 'text' : 'password'"
              name="password"
              autocomplete="current-password"
              placeholder="请输入密码"
            />
            <button
              type="button"
              class="eye"
              :aria-label="showPw ? '隐藏密码' : '显示密码'"
              :aria-pressed="showPw"
              @click="showPw = !showPw"
            >
              <AppIcon :name="showPw ? 'eyeOff' : 'eye'" />
            </button>
          </div>
        </div>
        <div v-if="error" class="alert error" role="alert">
          <AppIcon name="errorCircle" :size="18" />
          <span>{{ error }}</span>
        </div>
        <div v-else-if="expired" class="alert info" role="status">
          <AppIcon name="info" :size="18" />
          <span>登录已过期或已在其他地方修改密码，请重新登录。</span>
        </div>
        <button type="submit" class="submit" :disabled="loading">
          <span v-if="loading" class="spinner" aria-hidden="true" />
          {{ loading ? '正在登录…' : '登录' }}
        </button>
        <div class="tip">
          <AppIcon name="info" :size="18" class="tip-icon" />
          <span>首次部署时，初始管理员 admin 的密码会打印在服务启动日志里，登录后请尽快修改。</span>
        </div>
        <p class="note">连续 5 次密码错误，账号将锁定 5 分钟。忘记密码请联系管理员重置。</p>
      </form>
    </section>
  </div>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: flex;
  background: #fff;
}

.brand {
  position: relative;
  width: 640px;
  flex-shrink: 0;
  padding: 56px 64px;
  display: flex;
  flex-direction: column;
  background: var(--ys-primary);
  color: #fff;
  overflow: hidden;
}

.pattern {
  position: absolute;
  right: -150px;
  bottom: -130px;
  stroke: #fff;
  stroke-opacity: 0.09;
  stroke-width: 1.5;
}

.logo {
  display: flex;
  align-items: center;
  gap: 12px;
}

.logo-mark {
  width: 40px;
  height: 40px;
  border-radius: 11px;
  background: #fff;
  color: var(--ys-primary);
  display: flex;
  align-items: center;
  justify-content: center;
}

.logo-text {
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}

.logo-text strong {
  font-size: 20px;
  font-weight: 600;
}

.logo-text span {
  font-size: 12px;
  color: #c9d3f5;
}

.slogan {
  position: relative;
  margin-top: clamp(48px, 16vh, 150px);
  display: flex;
  flex-direction: column;
  gap: 18px;
}

.slogan h1 {
  font-size: 38px;
  line-height: 1.3;
  font-weight: 600;
}

.slogan p {
  font-size: 16px;
  line-height: 1.7;
  color: #dce3fa;
  max-width: 440px;
}

.features {
  position: relative;
  margin: 44px 0 0;
  padding: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.features li {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 15px;
}

.feat-icon {
  width: 32px;
  height: 32px;
  flex-shrink: 0;
  border-radius: 8px;
  background: #3553c4;
  display: flex;
  align-items: center;
  justify-content: center;
}

.version {
  position: relative;
  margin-top: auto;
  padding-top: 32px;
  font-size: 12px;
  color: #c9d3f5;
}

.panel {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 48px 24px;
}

.form {
  width: 380px;
  max-width: 100%;
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.heading {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.heading h2 {
  font-size: 26px;
  font-weight: 600;
}

.heading p {
  font-size: 14px;
  color: var(--ys-text-secondary);
}

.field {
  display: flex;
  flex-direction: column;
  gap: 8px;
  font-size: 13px;
  font-weight: 500;
  color: var(--ys-text-label);
}

.input {
  height: 44px;
  padding: 0 14px;
  border: 1px solid var(--ys-border-input);
  border-radius: 8px;
  background: #fff;
  color: var(--ys-text);
  font-family: inherit;
  font-size: 14px;
  font-weight: 400;
  outline: none;
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}

.input:focus,
.input:focus-within {
  border-color: var(--ys-primary);
  box-shadow: 0 0 0 3px rgba(36, 67, 181, 0.12);
}

.input::placeholder,
.pw input::placeholder {
  color: var(--ys-text-placeholder);
}

.pw {
  display: flex;
  align-items: center;
  padding: 0 6px 0 14px;
}

.pw input {
  flex: 1;
  min-width: 0;
  height: 40px;
  border: 0;
  outline: none;
  background: transparent;
  color: var(--ys-text);
  font-family: inherit;
  font-size: 14px;
}

.eye {
  width: 36px;
  height: 36px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ys-text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
  cursor: pointer;
}

.eye:hover {
  background: var(--ys-bg-muted);
  color: var(--ys-text);
}

.submit {
  height: 46px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border: 0;
  border-radius: 8px;
  background: var(--ys-primary);
  color: #fff;
  font-size: 15px;
  font-weight: 500;
  cursor: pointer;
  transition: background-color 0.15s;
}

.submit:hover {
  background: var(--ys-primary-hover);
}

.submit:disabled {
  opacity: 0.75;
  cursor: progress;
}

.spinner {
  width: 16px;
  height: 16px;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.alert {
  display: flex;
  gap: 10px;
  padding: 10px 14px;
  border-radius: 8px;
  font-size: 13px;
  line-height: 1.6;
}

.alert :deep(svg) {
  margin-top: 2px;
}

.alert.error {
  background: var(--ys-err-bg);
  border: 1px solid var(--ys-err-border);
  color: var(--ys-err-fg);
}

.alert.info {
  background: #f4f6fb;
  border: 1px solid #e1e6f4;
  color: var(--ys-text-label);
}

.alert.info :deep(svg) {
  color: var(--ys-primary);
}

.tip {
  display: flex;
  gap: 10px;
  padding: 12px 14px;
  border-radius: 8px;
  background: #f4f6fb;
  border: 1px solid #e1e6f4;
  font-size: 13px;
  line-height: 1.6;
  color: var(--ys-text-label);
}

.tip-icon {
  margin-top: 2px;
  color: var(--ys-primary);
}

.note {
  font-size: 12px;
  color: var(--ys-text-muted);
}

@media (max-width: 1080px) {
  .brand {
    display: none;
  }
}
</style>
