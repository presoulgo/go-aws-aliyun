<script setup lang="ts">
import { computed } from 'vue'
import { jobStatusInfo, resourceStatusInfo, type Tone } from '@/utils/status'

// 状态一律“圆点 + 文字”，不只靠颜色表达。
const props = defineProps<{
  /** 资源状态（running/stopped…） */
  status?: string
  /** 同步任务状态（success/partial…） */
  job?: string
  /** 直接指定色调和文字 */
  tone?: Tone
  label?: string
  /** 只显示圆点和文字，不带底色 */
  plain?: boolean
}>()

const info = computed(() => {
  if (props.tone) return { tone: props.tone, label: props.label ?? '' }
  if (props.job !== undefined) return jobStatusInfo(props.job)
  return resourceStatusInfo(props.status ?? '')
})
</script>

<template>
  <span class="status-tag" :class="[info.tone, { plain }]">
    <span class="dot" aria-hidden="true" />
    <slot>{{ info.label }}</slot>
  </span>
</template>

<style scoped>
.status-tag {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 2px 8px;
  border-radius: 999px;
  font-size: 12px;
  line-height: 18px;
  white-space: nowrap;
}

.status-tag.plain {
  padding: 0;
  background: transparent !important;
}

.dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex-shrink: 0;
}

.plain .dot {
  width: 7px;
  height: 7px;
}

.ok {
  background: var(--ys-ok-bg);
  color: var(--ys-ok-fg);
}
.ok .dot {
  background: var(--ys-ok-dot);
}

.off {
  background: var(--ys-off-bg);
  color: var(--ys-off-fg);
}
.off .dot {
  background: var(--ys-off-dot);
}

.busy {
  background: var(--ys-busy-bg);
  color: var(--ys-busy-fg);
}
.busy .dot {
  background: var(--ys-busy-dot);
}

.err {
  background: var(--ys-err-bg);
  color: var(--ys-err-fg);
}
.err .dot {
  background: var(--ys-err-dot);
}

.warn {
  background: var(--ys-warn-bg);
  color: var(--ys-warn-fg);
}
.warn .dot {
  background: var(--ys-warn-dot);
}
</style>
