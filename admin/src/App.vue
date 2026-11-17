<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { currentAdmin, logout } from './session'

const route = useRoute()
const router = useRouter()

const section = computed(() => '/' + (route.path.split('/')[1] ?? ''))

async function onLogout() {
  await logout()
  await router.push('/login')
}
</script>

<template>
  <el-container v-if="currentAdmin && !route.meta.public" direction="vertical">
    <header class="top">
      <div class="top-inner">
        <router-link to="/" class="brand">DocGen</router-link>
        <el-menu mode="horizontal" :default-active="section" router :ellipsis="false" class="nav">
          <el-menu-item index="/projects">Проекты</el-menu-item>
        </el-menu>
        <div class="user">
          <span class="muted">{{ currentAdmin.name }}</span>
          <el-button link @click="onLogout">Выйти</el-button>
        </div>
      </div>
    </header>
    <!-- key: при смене только параметра (другой шаблон) страница создаётся заново, а не показывает старые данные -->
    <main><router-view :key="route.fullPath" /></main>
  </el-container>
  <router-view v-else />
</template>

<style scoped>
.top {
  background: #fff;
  border-bottom: 1px solid var(--el-border-color-light);
}

.top-inner {
  max-width: 1200px;
  margin: 0 auto;
  padding: 0 16px;
  display: flex;
  align-items: center;
  gap: 24px;
}

.brand {
  font-weight: 700;
  font-size: 18px;
  color: var(--el-text-color-primary);
  text-decoration: none;
}

.nav {
  flex: 1;
  border-bottom: none;
}

.user {
  display: flex;
  align-items: center;
  gap: 12px;
}
</style>
