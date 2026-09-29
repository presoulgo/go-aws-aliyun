<script setup lang="ts" generic="T extends string">
defineProps<{ options: { value: T; label: string }[]; label?: string }>()
const model = defineModel<T>({ required: true })

function onKey(e: KeyboardEvent, options: { value: T }[], index: number) {
  const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0
  if (!step) return
  e.preventDefault()
  const next = options[(index + step + options.length) % options.length]!
  model.value = next.value
  const group = (e.currentTarget as HTMLElement).parentElement
  ;(group?.children[(index + step + options.length) % options.length] as HTMLElement | undefined)?.focus()
}
</script>

<template>
  <div class="segmented" role="radiogroup" :aria-label="label">
    <button
      v-for="(o, i) in options"
      :key="o.value"
      type="button"
      role="radio"
      :aria-checked="model === o.value"
      :tabindex="model === o.value ? 0 : -1"
      class="seg-item"
      :class="{ on: model === o.value }"
      @click="model = o.value"
      @keydown="onKey($event, options, i)"
    >
      {{ o.label }}
    </button>
  </div>
</template>

<style scoped>
.segmented {
  display: inline-flex;
  flex-shrink: 0;
  gap: 2px;
  padding: 3px;
  border-radius: 9px;
  background: var(--ys-segment-bg);
}

.seg-item {
  height: 30px;
  padding: 0 14px;
  border: 0;
  border-radius: 6px;
  background: transparent;
  color: var(--ys-text-secondary);
  font-size: 13px;
  white-space: nowrap;
  cursor: pointer;
  transition:
    background-color 0.15s,
    color 0.15s;
}

.seg-item:hover {
  color: var(--ys-text);
}

.seg-item.on {
  background: #fff;
  color: var(--ys-text);
  font-weight: 500;
  box-shadow: 0 1px 2px rgba(21, 24, 33, 0.08);
}
</style>
