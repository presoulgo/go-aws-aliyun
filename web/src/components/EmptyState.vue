<script setup lang="ts">
import AppIcon from './AppIcon.vue'
import type { IconName } from './icons'

withDefaults(defineProps<{ icon?: IconName; title: string; description?: string; compact?: boolean }>(), {
  icon: 'stack',
})
</script>

<template>
  <div class="empty" :class="{ compact }">
    <span class="icon"><AppIcon :name="icon" :size="compact ? 20 : 24" /></span>
    <p class="title">{{ title }}</p>
    <p v-if="description || $slots.description" class="desc">
      <slot name="description">{{ description }}</slot>
    </p>
    <div v-if="$slots.default" class="actions"><slot /></div>
  </div>
</template>

<style scoped>
.empty {
  padding: 48px 24px;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  text-align: center;
}

.empty.compact {
  padding: 24px 16px;
}

.icon {
  width: 48px;
  height: 48px;
  margin-bottom: 4px;
  border-radius: 12px;
  background: var(--ys-bg-muted);
  color: var(--ys-text-muted);
  display: flex;
  align-items: center;
  justify-content: center;
}

.compact .icon {
  width: 40px;
  height: 40px;
  border-radius: 10px;
}

.title {
  font-size: 14px;
  font-weight: 600;
  color: var(--ys-text);
}

.desc {
  max-width: 420px;
  font-size: 13px;
  line-height: 1.6;
  color: var(--ys-text-muted);
}

.actions {
  margin-top: 8px;
  display: flex;
  gap: 10px;
}
</style>
