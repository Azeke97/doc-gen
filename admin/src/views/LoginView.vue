<script setup lang="ts">
import { reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'

import { api, ApiError } from '../api'
import { loadAdmin } from '../session'

const route = useRoute()
const router = useRouter()

const form = reactive({ email: '', password: '' })
const error = ref('')
const loading = ref(false)

async function submit() {
  error.value = ''
  loading.value = true
  try {
    await api.login(form.email, form.password)
    await loadAdmin()
    const next = typeof route.query.next === 'string' && route.query.next.startsWith('/') ? route.query.next : '/templates'
    await router.replace(next)
  } catch (e) {
    error.value = e instanceof ApiError && e.status === 401 ? 'Неверный email или пароль' : String((e as Error).message)
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login">
    <el-card class="card" shadow="never">
      <h1>DocGen</h1>
      <p class="muted">Вход в админку</p>
      <el-form label-position="top" @submit.prevent="submit">
        <el-form-item label="Email">
          <el-input v-model="form.email" type="email" autocomplete="username" autofocus />
        </el-form-item>
        <el-form-item label="Пароль">
          <el-input v-model="form.password" type="password" autocomplete="current-password" show-password />
        </el-form-item>
        <el-alert v-if="error" :title="error" type="error" :closable="false" show-icon class="error" />
        <el-button type="primary" native-type="submit" :loading="loading" class="submit">Войти</el-button>
      </el-form>
    </el-card>
  </div>
</template>

<style scoped>
.login {
  min-height: 100vh;
  display: grid;
  place-items: center;
  padding: 16px;
  box-sizing: border-box;
}

.card {
  width: 100%;
  max-width: 380px;
}

h1 {
  margin: 0;
  font-size: 24px;
}

.error {
  margin-bottom: 16px;
}

.submit {
  width: 100%;
}
</style>
