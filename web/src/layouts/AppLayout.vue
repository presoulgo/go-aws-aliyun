<script setup lang="ts">
import { onBeforeUnmount, onMounted } from 'vue'
import AppSidebar from '@/components/AppSidebar.vue'
import AppTopbar from '@/components/AppTopbar.vue'
import { useAppStore } from '@/stores/app'

const app = useAppStore()

onMounted(() => app.startPolling())
onBeforeUnmount(() => app.stopPolling())
</script>

<template>
  <div class="layout">
    <AppSidebar />
    <div class="layout-main">
      <AppTopbar />
      <main class="layout-content">
        <RouterView />
      </main>
    </div>
  </div>
</template>

<style scoped>
.layout {
  display: flex;
  min-height: 100vh;
  min-width: 1180px;
}

.layout-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.layout-content {
  flex: 1;
  padding: 24px 28px 32px;
  display: flex;
  flex-direction: column;
  gap: 16px;
}
</style>
