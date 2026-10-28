<script setup>
import { ref, reactive, computed } from 'vue'
import { useRouter } from 'vue-router'
import {
  state,
  updateProfile,
  changeUsername,
  changePassword,
  logout,
  handleApiError
} from '../store'

const router = useRouter()
const current = computed(() => state.user)
const user = state.user || {}

// Личные данные
const profile = reactive({ name: user.name || '', telegram: user.telegram || '', dormId: user.dormId || '' })
const profileMsg = reactive({ error: '', done: '', busy: false })
async function saveProfile() {
  if (profileMsg.busy) return
  profileMsg.done = profileMsg.error = ''
  profileMsg.busy = true
  try {
    await updateProfile(profile)
    profileMsg.done = 'Данные сохранены'
  } catch (e) {
    profileMsg.error = handleApiError(e)
  } finally {
    profileMsg.busy = false
  }
}

// Никнейм
const newUsername = ref('')
const usernameMsg = reactive({ error: '', done: '', busy: false })
async function saveUsername() {
  if (usernameMsg.busy) return
  usernameMsg.done = usernameMsg.error = ''
  usernameMsg.busy = true
  try {
    await changeUsername(newUsername.value.trim())
    usernameMsg.done = 'Никнейм изменён'
    newUsername.value = ''
  } catch (e) {
    usernameMsg.error = handleApiError(e)
  } finally {
    usernameMsg.busy = false
  }
}

// Пароль
const password = reactive({ current: '', next: '', confirm: '' })
const passwordMsg = reactive({ error: '', done: '', busy: false })
async function savePassword() {
  if (passwordMsg.busy) return
  passwordMsg.done = passwordMsg.error = ''
  passwordMsg.busy = true
  try {
    await changePassword(password.current, password.next, password.confirm)
    passwordMsg.done = 'Пароль изменён'
    password.current = password.next = password.confirm = ''
  } catch (e) {
    passwordMsg.error = handleApiError(e)
  } finally {
    passwordMsg.busy = false
  }
}

const exitError = ref('')
async function exit() {
  exitError.value = ''
  try {
    await logout()
    router.push('/')
  } catch (e) {
    exitError.value = handleApiError(e)
  }
}
</script>

<template>
  <div v-if="current" class="settings">
    <div class="settings-head">
      <router-link to="/profile" class="link">← Назад в профиль</router-link>
      <h1>Настройки</h1>
    </div>

    <div class="settings-grid">
      <form class="form settings-card wide-card" @submit.prevent="saveProfile">
        <h2>Личные данные</h2>
        <label class="field">
          Имя
          <input v-model="profile.name" type="text" autocomplete="name" />
        </label>
        <label class="field">
          Telegram
          <div class="input-prefix">
            <span>@</span>
            <input v-model="profile.telegram" type="text" placeholder="username" />
          </div>
          <span class="hint">Его увидит победитель или продавец после окончания торгов</span>
        </label>
        <label class="field">
          Общежитие
          <select v-model="profile.dormId">
            <option v-for="dorm in state.dorms" :key="dorm.id" :value="dorm.id">
              {{ dorm.name }}
            </option>
          </select>
        </label>
        <div v-if="profileMsg.error" class="error">{{ profileMsg.error }}</div>
        <div v-if="profileMsg.done" class="success">{{ profileMsg.done }}</div>
        <button class="button" type="submit" :disabled="profileMsg.busy">Сохранить</button>
      </form>

      <form class="form settings-card" @submit.prevent="saveUsername">
        <h2>Никнейм</h2>
        <div class="muted">Сейчас: <b>@{{ current.username }}</b></div>
        <label class="field">
          Новый никнейм
          <input v-model="newUsername" type="text" autocomplete="username" />
        </label>
        <div v-if="usernameMsg.error" class="error">{{ usernameMsg.error }}</div>
        <div v-if="usernameMsg.done" class="success">{{ usernameMsg.done }}</div>
        <button class="button" type="submit" :disabled="usernameMsg.busy">Сменить никнейм</button>
      </form>

      <form class="form settings-card" @submit.prevent="savePassword">
        <h2>Пароль</h2>
        <label class="field">
          Текущий пароль
          <input v-model="password.current" type="password" autocomplete="current-password" />
        </label>
        <label class="field">
          Новый пароль
          <input v-model="password.next" type="password" autocomplete="new-password" />
        </label>
        <label class="field">
          Повторите новый пароль
          <input v-model="password.confirm" type="password" autocomplete="new-password" />
        </label>
        <div v-if="passwordMsg.error" class="error">{{ passwordMsg.error }}</div>
        <div v-if="passwordMsg.done" class="success">{{ passwordMsg.done }}</div>
        <button class="button" type="submit" :disabled="passwordMsg.busy">Сменить пароль</button>
      </form>

      <div class="settings-card danger-zone wide-card">
        <div>
          <h2>Выход</h2>
          <p class="muted">Завершить сессию на этом устройстве.</p>
        </div>
        <button class="button danger" @click="exit">Выйти из аккаунта</button>
        <div v-if="exitError" class="error">{{ exitError }}</div>
      </div>
    </div>
  </div>
</template>
