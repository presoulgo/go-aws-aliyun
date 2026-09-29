import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import '@fontsource/ibm-plex-sans/latin-400.css'
import '@fontsource/ibm-plex-sans/latin-500.css'
import '@fontsource/ibm-plex-sans/latin-600.css'
import '@fontsource/ibm-plex-sans/latin-700.css'
import '@fontsource/ibm-plex-mono/latin-400.css'
import '@fontsource/ibm-plex-mono/latin-500.css'
import './styles/theme.css'

import App from './App.vue'
import { onUnauthorized } from './api/http'
import { router } from './router'
import { useAppStore } from './stores/app'
import { useAuthStore } from './stores/auth'

const app = createApp(App)
const pinia = createPinia()
app.use(pinia)
app.use(ElementPlus, { locale: zhCn })

// 任意接口返回 401：清理登录态，回到登录页并带上当前地址。
onUnauthorized(() => {
  const auth = useAuthStore(pinia)
  const hadSession = !!auth.user
  auth.reset()
  useAppStore(pinia).stopPolling()
  const current = router.currentRoute.value
  if (current.name !== 'login') {
    void router.replace({
      name: 'login',
      query: { redirect: current.fullPath, ...(hadSession ? { expired: '1' } : {}) },
    })
  }
})

void useAppStore(pinia).loadMeta()
app.use(router)
app.mount('#app')
