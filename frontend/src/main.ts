import { createApp } from 'vue'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import 'element-plus/dist/index.css'
import './styles/index.css'
import App from './App.vue'
import router from './router'
import { useAuthStore } from './stores/auth'
import { formatMentions } from './utils/mentions'

async function bootstrap() {
  const app = createApp(App)
  // 模板里显示用户文字统一走 $at()：<@openid> 换成 @昵称
  app.config.globalProperties.$at = formatMentions
  const pinia = createPinia()
  app.use(pinia)
  await useAuthStore(pinia).restore()
  app.use(router).use(ElementPlus, { locale: zhCn }).mount('#app')
}

void bootstrap()
