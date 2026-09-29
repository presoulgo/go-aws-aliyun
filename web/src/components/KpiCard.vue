<script setup lang="ts">
import type { RouteLocationRaw } from 'vue-router'
import AppIcon from './AppIcon.vue'
import type { IconName } from './icons'

defineProps<{
  label: string
  value: string | number
  /** 数值后的小号补充，例如 “ / 486” */
  suffix?: string
  sub?: string
  icon: IconName
  to?: RouteLocationRaw
  warn?: boolean
}>()
</script>

<template>
  <component :is="to ? 'RouterLink' : 'div'" :to="to" class="kpi" :class="{ warn, link: !!to }">
    <span class="head">
      <span class="label">{{ label }}</span>
      <AppIcon :name="icon" class="icon" />
    </span>
    <span class="value">
      {{ value }}<span v-if="suffix" class="suffix">{{ suffix }}</span>
    </span>
    <span class="sub">{{ sub }}</span>
  </component>
</template>

<style scoped>
.kpi {
  min-height: 108px;
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  justify-content: space-between;
  gap: 8px;
  border: 1px solid var(--ys-border);
  border-radius: var(--ys-radius-card);
  background: var(--ys-surface);
  color: var(--ys-text);
  transition:
    border-color 0.15s,
    box-shadow 0.15s;
}

.kpi.link:hover {
  color: var(--ys-text);
  border-color: #c9d1ee;
  box-shadow: 0 4px 12px rgba(21, 24, 33, 0.06);
}

.head {
  display: flex;
  align-items: center;
  justify-content: space-between;
}

.label {
  font-size: 13px;
  color: var(--ys-text-secondary);
}

.icon {
  color: var(--ys-icon-muted);
}

.value {
  font-size: 30px;
  font-weight: 600;
  line-height: 1;
}

.suffix {
  font-size: 16px;
  font-weight: 500;
  color: var(--ys-text-muted);
}

.sub {
  font-size: 12px;
  color: var(--ys-text-muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.kpi.warn {
  border-color: var(--ys-warn-soft-border);
  background: var(--ys-warn-soft-bg);
}

.kpi.warn .label,
.kpi.warn .sub {
  color: var(--ys-warn-label);
}

.kpi.warn .icon {
  color: #b26a12;
}

.kpi.warn .value {
  color: var(--ys-warn-fg);
}
</style>
