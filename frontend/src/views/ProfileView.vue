<script setup>
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import {
  getCurrentUser,
  getMyLots,
  getMyBidLots,
  getPriceText,
  getStatusText,
  changeUsername,
  changePassword,
  logout
} from '../store'

const router = useRouter()
const newUsername = ref('')
const newPassword = ref('')
const confirm = ref('')
const usernameError = ref('')
const usernameDone = ref('')
const passwordError = ref('')
const passwordDone = ref('')

function submitUsername() {
  usernameDone.value = ''
  usernameError.value = changeUsername(newUsername.value) || ''
  if (!usernameError.value) {
    usernameDone.value = 'Никнейм изменён'
    newUsername.value = ''
  }
}

function submitPassword() {
  passwordDone.value = ''
  passwordError.value = changePassword(newPassword.value, confirm.value) || ''
  if (!passwordError.value) {
    passwordDone.value = 'Пароль изменён'
    newPassword.value = ''
    confirm.value = ''
  }
}

function exit() {
  logout()
  router.push('/')
}
</script>

<template>
  <h1>{{ getCurrentUser().username }}</h1>

  <div class="block">
    <h2>Мои лоты</h2>
    <div v-if="getMyLots().length === 0" class="muted">
      У вас пока нет лотов. <router-link to="/create"><b>Создать лот</b></router-link>
    </div>
    <table v-else class="table">
      <thead>
        <tr>
          <th>Название</th>
          <th>Цена</th>
          <th>Статус</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="lot in getMyLots()"
          :key="lot.id"
          class="clickable"
          @click="router.push('/lots/' + lot.id)"
        >
          <td>{{ lot.title }}</td>
          <td>{{ getPriceText(lot) }}</td>
          <td>{{ getStatusText(lot) }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <div class="block">
    <h2>Мои ставки</h2>
    <div v-if="getMyBidLots().length === 0" class="muted">
      Вы пока не делали ставок. <router-link to="/"><b>Смотреть аукционы</b></router-link>
    </div>
    <table v-else class="table">
      <thead>
        <tr>
          <th>Название</th>
          <th>Цена</th>
          <th>Статус</th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="lot in getMyBidLots()"
          :key="lot.id"
          class="clickable"
          @click="router.push('/lots/' + lot.id)"
        >
          <td>{{ lot.title }}</td>
          <td>{{ getPriceText(lot) }}</td>
          <td>{{ getStatusText(lot) }}</td>
        </tr>
      </tbody>
    </table>
  </div>

  <div class="block">
    <h2>Смена никнейма</h2>
    <form class="form" @submit.prevent="submitUsername">
      <label class="field">
        Новый никнейм
        <input v-model="newUsername" type="text" />
      </label>
      <div v-if="usernameError" class="error">{{ usernameError }}</div>
      <div v-if="usernameDone" class="success">{{ usernameDone }}</div>
      <button class="button" type="submit">Сохранить</button>
    </form>
  </div>

  <div class="block">
    <h2>Смена пароля</h2>
    <form class="form" @submit.prevent="submitPassword">
      <label class="field">
        Новый пароль
        <input v-model="newPassword" type="password" />
      </label>
      <label class="field">
        Подтверждение пароля
        <input v-model="confirm" type="password" />
      </label>
      <div v-if="passwordError" class="error">{{ passwordError }}</div>
      <div v-if="passwordDone" class="success">{{ passwordDone }}</div>
      <button class="button" type="submit">Сохранить</button>
    </form>
  </div>

  <div class="block">
    <button class="button danger" @click="exit">Выйти</button>
  </div>
</template>
