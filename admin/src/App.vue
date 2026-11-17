<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { authRequired, currentAdmin, logout } from './session'

const route = useRoute()
const router = useRouter()

const section = computed(() => '/' + (route.path.split('/')[1] ?? ''))

// Сначала уходим на страницу входа: иначе текущая страница успеет запросить данные без сессии.
async function onLogout() {
  await router.push('/login')
  await logout()
}
</script>

<template>
  <el-container v-if="currentAdmin && !route.meta.public" direction="vertical">
    <div v-if="authRequired === false" class="demo-banner">
      Демо-режим: вход отключён, журнал открыт всем, у кого есть адрес. Только для тестовых данных.
    </div>
    <header class="top">
      <div class="top-inner">
        <router-link to="/" class="brand">DocGen</router-link>
        <el-menu mode="horizontal" :default-active="section" router :ellipsis="false" class="nav">
          <el-menu-item index="/journal">Журнал</el-menu-item>
          <el-menu-item index="/reports">Отчёты</el-menu-item>
          <el-menu-item index="/projects">Проекты</el-menu-item>
        </el-menu>
        <div v-if="authRequired !== false" class="user">
          <span class="muted">{{ currentAdmin.name }}</span>
          <el-button link @click="onLogout">Выйти</el-button>
        </div>
      </div>
    </header>
    <main><router-view :key="route.path" /></main>
  </el-container>
  <router-view v-else />
</template>

<style scoped>
.demo-banner {
  background: var(--el-color-warning-light-9);
  color: var(--el-color-warning-dark-2);
  border-bottom: 1px solid var(--el-color-warning-light-7);
  text-align: center;
  font-size: 13px;
  padding: 6px 16px;
}

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
