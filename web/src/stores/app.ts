import { defineStore } from 'pinia'
import { ref } from 'vue'
import { metaApi, syncApi, type Meta, type SyncStatus } from '@/api'

const IDLE_POLL = 60_000
const BUSY_POLL = 5_000

export const useAppStore = defineStore('app', () => {
  const meta = ref<Meta>({ name: '云枢', demo: false, version: '' })
  const syncStatus = ref<SyncStatus | null>(null)
  /** 有同步任务开始或结束时加一，页面据此重新拉取数据。 */
  const syncTick = ref(0)
  const now = ref(Date.now())

  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let clockTimer: ReturnType<typeof setInterval> | undefined
  let polling = false

  async function loadMeta() {
    try {
      meta.value = await metaApi.get()
    } catch {
      /* 使用默认值 */
    }
  }

  async function refreshSync() {
    const prev = syncStatus.value
    try {
      const next = await syncApi.status()
      syncStatus.value = next
      if (prev && (prev.running !== next.running || prev.last_finished_at !== next.last_finished_at)) {
        syncTick.value++
      }
    } catch {
      /* 保留上一次的状态 */
    }
    schedule()
  }

  // 有同步任务在跑时 5 秒刷新一次，否则 60 秒。
  function schedule() {
    clearTimeout(pollTimer)
    if (!polling) return
    const delay = (syncStatus.value?.running ?? 0) > 0 ? BUSY_POLL : IDLE_POLL
    pollTimer = setTimeout(refreshSync, delay)
  }

  function startPolling() {
    if (polling) return
    polling = true
    clockTimer = setInterval(() => (now.value = Date.now()), 30_000)
    void refreshSync()
  }

  function stopPolling() {
    polling = false
    clearTimeout(pollTimer)
    clearInterval(clockTimer)
    syncStatus.value = null
  }

  return { meta, syncStatus, syncTick, now, loadMeta, refreshSync, startPolling, stopPolling }
})
