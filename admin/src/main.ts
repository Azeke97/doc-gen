import 'element-plus/dist/index.css'
import './style.css'

import ElementPlus from 'element-plus'
import ru from 'element-plus/es/locale/lang/ru'
import { createApp } from 'vue'

import App from './App.vue'
import { router } from './router'

createApp(App).use(router).use(ElementPlus, { locale: ru }).mount('#app')
