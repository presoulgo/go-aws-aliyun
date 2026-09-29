<script setup lang="ts">
defineProps<{ title: string; subtitle?: string }>()
</script>

<template>
  <div class="page-header">
    <div class="titles">
      <h1>{{ title }}</h1>
      <span v-if="subtitle || $slots.subtitle" class="subtitle">
        <slot name="subtitle">{{ subtitle }}</slot>
      </span>
    </div>
    <div v-if="$slots.default" class="actions">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.page-header {
  min-height: 44px;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}

.titles {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

h1 {
  font-size: 22px;
  font-weight: 600;
  line-height: 1.3;
}

.subtitle {
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.actions {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-shrink: 0;
}
</style>
