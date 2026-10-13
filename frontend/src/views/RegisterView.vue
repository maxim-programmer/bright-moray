<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { register } from '../store'

const router = useRouter()
const username = ref('')
const password = ref('')
const confirm = ref('')
const error = ref('')

function submit() {
  error.value = register(username.value, password.value, confirm.value) || ''
  if (!error.value) {
    router.push('/')
  }
}
</script>

<template>
  <form class="form narrow" @submit.prevent="submit">
    <h1>Регистрация</h1>
    <label class="field">
      Никнейм
      <input v-model="username" type="text" />
    </label>
    <label class="field">
      Пароль
      <input v-model="password" type="password" />
    </label>
    <label class="field">
      Подтверждение пароля
      <input v-model="confirm" type="password" />
    </label>
    <div v-if="error" class="error">{{ error }}</div>
    <button class="button" type="submit">Зарегистрироваться</button>
    <div class="muted">
      Уже есть аккаунт? <router-link to="/login"><b>Войти</b></router-link>
    </div>
  </form>
</template>
