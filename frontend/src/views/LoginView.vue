<script setup>
import { ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { login, handleApiError } from '../store'

const route = useRoute()
const router = useRouter()
const username = ref('')
const password = ref('')
const error = ref('')
const busy = ref(false)

async function submit() {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    // при превышении лимита попыток сервер вернёт 429 с готовым текстом — показываем его как есть
    await login(username.value.trim(), password.value)
  } catch (e) {
    error.value = handleApiError(e)
    return
  } finally {
    busy.value = false
  }
  // возвращаем туда, куда пользователь шёл до входа (например, «Создать лот»)
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/'
  router.push(redirect.startsWith('/') && !redirect.startsWith('//') ? redirect : '/')
}
</script>

<template>
  <form class="form narrow" @submit.prevent="submit">
    <h1>Вход</h1>
    <p v-if="route.query.redirect" class="muted">Войдите, чтобы продолжить</p>
    <label class="field">
      Никнейм
      <input v-model="username" type="text" autocomplete="username" />
    </label>
    <label class="field">
      Пароль
      <input v-model="password" type="password" autocomplete="current-password" />
    </label>
    <div v-if="error" class="error">{{ error }}</div>
    <button class="button" type="submit" :disabled="busy">Войти</button>
    <div class="muted">
      Нет аккаунта? <router-link to="/register"><b>Зарегистрироваться</b></router-link>
    </div>
  </form>
</template>
