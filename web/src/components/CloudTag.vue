<script setup lang="ts">
import type { Provider } from '@/api/types'
import { providerLabel } from '@/utils/status'

withDefaults(defineProps<{ provider: Provider | string; small?: boolean }>(), { small: false })
</script>

<template>
  <span class="cloud-tag" :class="[provider, { small }]">{{ providerLabel[provider as Provider] ?? provider }}</span>
</template>

<style scoped>
.cloud-tag {
  display: inline-block;
  flex-shrink: 0;
  padding: 0 6px;
  border-radius: var(--ys-radius-tag);
  font-size: 12px;
  line-height: 18px;
  white-space: nowrap;
  background: var(--ys-bg-muted);
  color: var(--ys-text-label);
}

.cloud-tag.small {
  padding: 0 5px;
  border-radius: 4px;
  font-size: 11px;
  line-height: 16px;
}

.cloud-tag.aws {
  background: var(--ys-aws-tag-bg);
  color: var(--ys-aws-tag-fg);
}

.cloud-tag.aliyun {
  background: var(--ys-aliyun-tag-bg);
  color: var(--ys-aliyun-tag-fg);
}
</style>
