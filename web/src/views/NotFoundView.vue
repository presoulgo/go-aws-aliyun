<script setup lang="ts">
import EmptyState from '@/components/EmptyState.vue'
</script>

<template>
  <div class="ys-card wrap">
    <EmptyState icon="warning" title="页面不存在" description="地址可能输错了，或者该页面已被移除。">
      <RouterLink to="/dashboard"><el-button type="primary">返回概览</el-button></RouterLink>
    </EmptyState>
  </div>
</template>

<style scoped>
.wrap {
  padding: 48px 0;
}
</style>
