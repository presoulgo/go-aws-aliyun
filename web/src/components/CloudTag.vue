<script setup lang="ts">defineProps<{provider:string}>()</script><template><span class="tag-cloud" :class="provider">{{provider==='aws'?'AWS':'阿里云'}}</span></template>
