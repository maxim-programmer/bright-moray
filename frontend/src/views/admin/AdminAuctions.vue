<script setup>
import { ref, onMounted } from 'vue'
import Pagination from '../../components/Pagination.vue'
import { adminApi } from '../../api'
import { state, formatDate, formatMoney, getCategory } from '../../store'
import { adminStatusText, statusClass } from '../../status'
import { useLoader } from './useAdmin'

const STATUSES = [
  { id: 'active', name: 'Активные' },
  { id: 'finished', name: 'Завершённые' },
  { id: 'cancelled', name: 'Отменённые' },
  { id: 'all', name: 'Все' }
]

const status = ref('active')
const q = ref('')
const page = ref(1)
const auctions = ref([])
const total = ref(0)
const totalPages = ref(1)

const loader = useLoader(() => adminApi.auctions({ status: status.value, q: q.value.trim(), page: page.value }))

async function load() {
  const data = await loader.run()
  if (!data) return
  auctions.value = data.auctions
  total.value = data.total
  totalPages.value = data.totalPages
}

function reload() {
  page.value = 1
  load()
}

function pick(id) {
  status.value = id
  reload()
}

function goTo(p) {
  page.value = p
  load()
}

// Строка «аукцион идёт» может устареть, пока страница открыта: сверяем с серверными часами
function isEnded(a) {
  return a.status === 'active' && a.endsAt <= state.now
}

onMounted(load)
</script>

<template>
  <div class="sub-tabs">
    <button v-for="s in STATUSES" :key="s.id" type="button" :class="{ active: status === s.id }" @click="pick(s.id)">
      {{ s.name }}
    </button>
  </div>

  <form class="admin-bar" @submit.prevent="reload">
    <label class="field grow">
      Поиск по названию и описанию
      <input v-model="q" type="search" />
    </label>
    <button class="button" type="submit">Найти</button>
  </form>

  <div v-if="loader.error.value" class="error">{{ loader.error.value }}</div>
  <p class="muted small">Найдено: {{ total }}</p>

  <div class="admin-table-wrap">
    <div v-if="!auctions.length && !loader.loading.value" class="admin-empty">Аукционов не найдено.</div>
    <table v-else class="admin-table">
      <thead>
        <tr>
          <th>№</th><th>Лот</th><th>Автор</th><th>Категория</th><th class="num">Цена</th>
          <th class="num">Ставок</th><th>Статус</th><th>Окончание</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="a in auctions" :key="a.id">
          <td>{{ a.id }}</td>
          <td><router-link :to="'/admin/auctions/' + a.id" class="admin-link">{{ a.title }}</router-link></td>
          <td><router-link :to="'/admin/users/' + a.authorId" class="admin-link">{{ a.authorUsername }}</router-link></td>
          <td>{{ getCategory(a.category)?.name || a.category }}</td>
          <td class="num">{{ a.bidsCount ? formatMoney(a.currentPrice) : '—' }}</td>
          <td class="num">{{ a.bidsCount }}</td>
          <td><span class="pill" :class="statusClass(a, state.now)">{{ adminStatusText(a, state.now) }}</span></td>
          <td class="nowrap">{{ formatDate(a.endsAt) }}<span v-if="isEnded(a)" class="muted small"> (подводим итоги)</span></td>
        </tr>
      </tbody>
    </table>
  </div>
  <Pagination :page="page" :total-pages="totalPages" @change="goTo" />
</template>
