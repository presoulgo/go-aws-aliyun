<script setup lang="ts">
import { useRouter } from 'vue-router'
import AppIcon from '@/components/AppIcon.vue'
import CloudTag from '@/components/CloudTag.vue'
import { useAppStore } from '@/stores/app'
import { useAuthStore } from '@/stores/auth'
import { intervalText } from '@/utils/format'

const router = useRouter()
const app = useAppStore()
const auth = useAuthStore()

const awsActions = [
  'sts:GetCallerIdentity',
  'ec2:DescribeRegions, ec2:DescribeInstances, ec2:DescribeInstanceTypes',
  'rds:DescribeDBInstances',
  'elasticloadbalancing:Describe*',
  's3:ListAllMyBuckets',
  'cloudwatch:GetMetricData, cloudwatch:ListMetrics',
]
const aliyunPolicies = [
  'AliyunECSReadOnlyAccess',
  'AliyunRDSReadOnlyAccess',
  'AliyunSLBReadOnlyAccess',
  'AliyunALBReadOnlyAccess',
  'AliyunOSSReadOnlyAccess',
  'AliyunCloudMonitorReadOnlyAccess',
]
</script>

<template>
  <section class="hero ys-card">
    <svg class="art" width="300" height="220" viewBox="0 0 300 220" fill="none" aria-hidden="true">
      <path d="M70 60 C 110 60, 120 110, 150 110" stroke="#C9D1EE" stroke-width="2" stroke-dasharray="5 5" />
      <path d="M230 60 C 190 60, 180 110, 150 110" stroke="#C9D1EE" stroke-width="2" stroke-dasharray="5 5" />
      <path d="M150 110 V 170" stroke="#C9D1EE" stroke-width="2" />
      <rect x="16" y="32" width="96" height="56" rx="12" fill="#E6ECF5" />
      <text x="64" y="66" text-anchor="middle" font-size="15" font-weight="600" fill="#1B2F52">AWS</text>
      <rect x="188" y="32" width="96" height="56" rx="12" fill="#FDEEE2" />
      <text x="236" y="66" text-anchor="middle" font-size="15" font-weight="600" fill="#A94600">阿里云</text>
      <path d="M150 84l22.5 13v26L150 136l-22.5-13V97z" fill="#2443B5" />
      <path d="M150 110V84M150 110l22.5 13M150 110l-22.5 13" stroke="#FFFFFF" stroke-width="1.6" />
      <rect x="86" y="170" width="128" height="36" rx="8" fill="#FFFFFF" stroke="#E4E7EC" />
      <rect x="98" y="182" width="12" height="12" rx="3" fill="#E9F6EF" />
      <rect x="118" y="184" width="80" height="8" rx="4" fill="#EEF0F3" />
    </svg>
    <div class="hero-text">
      <h2>接入第一个云账号</h2>
      <p>
        平台只需要只读权限。填写 AccessKey 后会自动校验凭证、发现可用地域并同步资源，同步完成就能在资源中心按名称、ID、IP
        检索。
      </p>
      <div v-if="auth.isAdmin" class="hero-actions">
        <el-button type="primary" size="large" @click="router.push({ path: '/accounts', query: { new: '1' } })">
          <AppIcon name="plus" :size="16" />
          <span class="btn-text">新增云账号</span>
        </el-button>
        <el-button size="large" @click="router.push('/accounts')">前往云账号页</el-button>
      </div>
      <p v-else class="viewer-note">
        <AppIcon name="info" :size="16" />
        你的角色是只读，请联系管理员接入云账号。
      </p>
    </div>
  </section>

  <div class="steps">
    <div class="step ys-card">
      <span class="num">1</span>
      <div>
        <strong>创建只读 AccessKey</strong>
        <span>在 AWS IAM 或阿里云 RAM 创建专用子账号，只授予下方的只读策略</span>
      </div>
    </div>
    <div class="step ys-card">
      <span class="num">2</span>
      <div>
        <strong>测试连接并选择地域</strong>
        <span>平台通过 STS 校验凭证，列出已开通地域，默认全部同步</span>
      </div>
    </div>
    <div class="step ys-card">
      <span class="num">3</span>
      <div>
        <strong>自动同步资源</strong>
        <span
          >保存后立即同步一次，之后{{ intervalText(app.meta.sync_interval_minutes) }}自动更新，也可随时手动同步</span
        >
      </div>
    </div>
  </div>

  <div class="policies">
    <section class="policy ys-card">
      <div class="policy-head">
        <CloudTag provider="aws" />
        <h3>所需权限（IAM 策略）</h3>
      </div>
      <p>直接使用托管策略 ReadOnlyAccess，或只授予以下动作：</p>
      <div class="code">
        <span v-for="a in awsActions" :key="a">{{ a }}</span>
        <span class="muted">使用角色 ARN 时，另需 sts:AssumeRole</span>
      </div>
    </section>
    <section class="policy ys-card">
      <div class="policy-head">
        <CloudTag provider="aliyun" />
        <h3>所需权限（RAM 系统策略）</h3>
      </div>
      <p>给 RAM 用户授予以下只读系统策略：</p>
      <div class="code">
        <span v-for="p in aliyunPolicies" :key="p">{{ p }}</span>
        <span class="muted">使用 RAM 角色时，另需 AliyunSTSAssumeRoleAccess</span>
      </div>
    </section>
  </div>
</template>

<style scoped>
.hero {
  min-height: 300px;
  padding: 36px 48px;
  display: flex;
  align-items: center;
  gap: 56px;
}

.art {
  flex-shrink: 0;
}

.art text {
  font-family: var(--ys-font-sans);
}

.hero-text {
  display: flex;
  flex-direction: column;
  gap: 14px;
  max-width: 560px;
}

.hero-text h2 {
  font-size: 24px;
  font-weight: 600;
}

.hero-text p {
  font-size: 15px;
  line-height: 1.75;
  color: var(--ys-text-label);
}

.hero-actions {
  margin-top: 6px;
  display: flex;
  gap: 12px;
}

.hero-actions .el-button {
  height: 40px;
  padding: 0 18px;
  font-size: 14px;
}

.btn-text {
  margin-left: 8px;
}

.viewer-note {
  margin-top: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px !important;
  color: var(--ys-text-secondary) !important;
}

.steps {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 16px;
}

.step {
  padding: 18px 20px;
  display: flex;
  gap: 14px;
}

.step .num {
  width: 28px;
  height: 28px;
  flex-shrink: 0;
  border-radius: 50%;
  background: var(--ys-primary-soft);
  color: var(--ys-primary);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 13px;
  font-weight: 600;
}

.step div {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.step strong {
  font-size: 14px;
  font-weight: 600;
}

.step span:not(.num) {
  font-size: 13px;
  line-height: 1.6;
  color: var(--ys-text-secondary);
}

.policies {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 16px;
}

.policy {
  padding: 18px 20px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.policy-head {
  display: flex;
  align-items: center;
  gap: 10px;
}

.policy-head h3 {
  font-size: 15px;
  font-weight: 600;
}

.policy p {
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.code {
  flex: 1;
  padding: 12px 14px;
  border-radius: 8px;
  background: var(--ys-bg-subtle);
  border: 1px solid var(--ys-divider);
  font-family: var(--ys-font-mono);
  font-size: 12px;
  line-height: 1.8;
  display: flex;
  flex-direction: column;
}

.code .muted {
  margin-top: 4px;
  font-family: var(--ys-font-sans);
}
</style>
