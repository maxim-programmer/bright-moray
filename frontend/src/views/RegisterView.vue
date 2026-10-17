<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import { register, dorms } from '../store'

const router = useRouter()
const form = reactive({
  name: '',
  username: '',
  telegram: '',
  dormId: '',
  password: '',
  confirm: ''
})
const error = ref('')

function submit() {
  error.value = register({ ...form, username: form.username.trim() }) || ''
  if (!error.value) {
    router.push('/')
  }
}
</script>

<template>
  <form class="form narrow" @submit.prevent="submit">
    <h1>Регистрация</h1>
    <label class="field">
      Имя
      <input v-model="form.name" type="text" autocomplete="name" />
    </label>
    <label class="field">
      Никнейм
      <input v-model="form.username" type="text" autocomplete="username" />
    </label>
    <label class="field">
      Telegram
      <div class="input-prefix">
        <span>@</span>
        <input v-model="form.telegram" type="text" placeholder="username" />
      </div>
    </label>
    <label class="field">
      Общежитие
      <select v-model="form.dormId">
        <option value="" disabled>Выберите общежитие</option>
        <option v-for="dorm in dorms" :key="dorm.id" :value="dorm.id">
          {{ dorm.name }}
        </option>
      </select>
    </label>
    <label class="field">
      Пароль
      <input v-model="form.password" type="password" autocomplete="new-password" />
    </label>
    <label class="field">
      Подтверждение пароля
      <input v-model="form.confirm" type="password" autocomplete="new-password" />
    </label>
    <div v-if="error" class="error">{{ error }}</div>
    <button class="button" type="submit">Зарегистрироваться</button>
    <div class="muted">
      Уже есть аккаунт? <router-link to="/login"><b>Войти</b></router-link>
    </div>
  </form>
</template>
