<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { login } from '../store'

const router = useRouter()
const username = ref('')
const password = ref('')
const error = ref('')

function submit() {
  error.value = login(username.value, password.value) || ''
  if (!error.value) {
    router.push('/')
  }
}
</script>

<template>
  <form class="form narrow" @submit.prevent="submit">
    <h1>Вход</h1>
    <label class="field">
      Никнейм
      <input v-model="username" type="text" />
    </label>
    <label class="field">
      Пароль
      <input v-model="password" type="password" />
    </label>
    <div v-if="error" class="error">{{ error }}</div>
    <button class="button" type="submit">Войти</button>
    <div class="muted">
      Нет аккаунта? <router-link to="/register"><b>Зарегистрироваться</b></router-link>
    </div>
  </form>
</template>
