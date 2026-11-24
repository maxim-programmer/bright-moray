<script setup>
import { ref, onMounted } from 'vue'
import Pagination from '../../components/Pagination.vue'
import { vStack } from '../../components/admin/stackTable'
import { adminApi } from '../../api'
import { formatDate } from '../../store'
import { useLoader } from './useAdmin'

const q = ref('')
const role = ref('')
const blocked = ref('')
const page = ref(1)
const users = ref([])
const total = ref(0)
const totalPages = ref(1)

const loader = useLoader(() =>
  adminApi.users({ q: q.value.trim(), role: role.value, blocked: blocked.value, page: page.value })
)

async function load() {
  const data = await loader.run()
  if (!data) return
  users.value = data.users
  total.value = data.total
  totalPages.value = data.totalPages
}

function search() {
  page.value = 1
  load()
}

function goTo(p) {
  page.value = p
  load()
}

onMounted(load)
</script>

<template>
  <form class="admin-bar" @submit.prevent="search">
    <label class="field grow">
      Поиск по ID, имени, username, почте или Telegram
      <input v-model="q" type="search" placeholder="Например: 15, Иван, ivan_k, ivan@mail.ru" />
    </label>
    <label class="field">
      Роль
      <select v-model="role" @change="search">
        <option value="">Все</option>
        <option value="user">Пользователи</option>
        <option value="admin">Администраторы</option>
      </select>
    </label>
    <label class="field">
      Состояние
      <select v-model="blocked" @change="search">
        <option value="">Все</option>
        <option value="false">Активные</option>
        <option value="true">Заблокированные</option>
      </select>
    </label>
    <button class="button" type="submit">Найти</button>
  </form>

  <div v-if="loader.error.value" class="error">{{ loader.error.value }}</div>
  <p class="muted small">Найдено: {{ total }}</p>

  <div class="admin-table-wrap">
    <div v-if="!users.length && !loader.loading.value" class="admin-empty">Никого не найдено.</div>
    <table v-else v-stack class="admin-table">
      <thead>
        <tr>
          <th>ID</th><th>Никнейм</th><th>Имя</th><th>Telegram</th><th>Роль</th><th>Состояние</th>
          <th class="num">Лотов</th><th class="num">Ставок</th><th>Регистрация</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="u in users" :key="u.id">
          <td>{{ u.id }}</td>
          <td><router-link :to="'/admin/users/' + u.id" class="admin-link">{{ u.username }}</router-link></td>
          <td>{{ u.name }}</td>
          <td>{{ u.telegram ? '@' + u.telegram : '—' }}</td>
          <td><span class="pill" :class="u.role === 'admin' ? 'good' : 'neutral'">{{ u.role === 'admin' ? 'Админ' : 'Пользователь' }}</span></td>
          <td><span class="pill" :class="u.blocked ? 'cancelled' : 'good'">{{ u.blocked ? 'Заблокирован' : 'Активен' }}</span></td>
          <td class="num">{{ u.lotsCount }}</td>
          <td class="num">{{ u.bidsCount }}</td>
          <td class="nowrap">{{ formatDate(u.createdAt) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
  <Pagination :page="page" :total-pages="totalPages" @change="goTo" />
</template>
