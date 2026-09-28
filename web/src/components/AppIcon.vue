<script setup lang="ts">
import { computed } from 'vue'
import { icons, type IconDef, type IconName } from './icons'

const props = withDefaults(defineProps<{ name: IconName; size?: number; stroke?: number }>(), { size: 18 })
const def = computed<IconDef>(() => icons[props.name])
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="stroke ?? def.sw"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    class="app-icon"
  >
    <template v-for="(el, i) in def.els" :key="i">
      <path v-if="el[0] === 'p'" :d="el[1]" />
      <circle v-else-if="el[0] === 'c'" :cx="el[1]" :cy="el[2]" :r="el[3]" />
      <rect v-else :x="el[1]" :y="el[2]" :width="el[3]" :height="el[4]" :rx="el[5]" />
    </template>
  </svg>
</template>

<style scoped>
.app-icon {
  flex-shrink: 0;
  display: block;
}
</style>
