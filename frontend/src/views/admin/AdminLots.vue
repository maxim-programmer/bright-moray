<script setup>
import { ref, watch, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import Pagination from '../../components/Pagination.vue'
import ReasonDialog from '../../components/admin/ReasonDialog.vue'
import ApproveDialog from '../../components/admin/ApproveDialog.vue'
import { vStack } from '../../components/admin/stackTable'
import { adminApi } from '../../api'
import { formatDate, formatMoney, getCategory } from '../../store'
import { adminStatusText, statusClass } from '../../status'
import { useLoader, useAction } from './useAdmin'

const STATUSES = [
  { id: 'pending', name: 'Ожидают решения' },
  { id: 'all', name: 'Все' },
  { id: 'approved', name: 'Одобренные' },
  { id: 'rejected', name: 'Отклонённые' },
  { id: 'active', name: 'Идут' },
  { id: 'finished', name: 'Завершённые' },
  { id: 'cancelled', name: 'Отменённые' }
]

const route = useRoute()
// Статус из адреса (?status=pending — ссылка со страницы статистики); неизвестное значение — очередь заявок
function statusFromQuery() {
  const s = route.query.status
  return typeof s === 'string' && STATUSES.some((x) => x.id === s) ? s : 'pending'
}
const status = ref(statusFromQuery())
const q = ref('')
const page = ref(1)
const lots = ref([])
const total = ref(0)
const totalPages = ref(1)

const loader = useLoader(() => adminApi.lots({ status: status.value, q: q.value.trim(), page: page.value }))

async function load() {
  const data = await loader.run()
  if (!data) return
  lots.value = data.lots
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

/* --- быстрые действия над заявками --- */

const dialog = ref(null) // { kind: 'approve' | 'reject', lot }
const action = useAction()

function open(kind, lot) {
  action.error.value = ''
  dialog.value = { kind, lot }
}

async function approve(startsAt) {
  const lot = dialog.value.lot
  if (await action.exec(() => adminApi.approveLot(lot.id, startsAt))) {
    dialog.value = null
    load()
  }
}

async function reject(reason) {
  const lot = dialog.value.lot
  if (await action.exec(() => adminApi.rejectLot(lot.id, reason))) {
    dialog.value = null
    load()
  }
}

onMounted(load)

// Страница не пересоздаётся при смене query (ключ router-view — path), поэтому следим за адресом сами
watch(
  () => route.query.status,
  () => {
    const next = statusFromQuery()
    if (next !== status.value) pick(next)
  }
)
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
    <div v-if="!lots.length && !loader.loading.value" class="admin-empty">
      {{ status === 'pending' ? 'Заявок на модерацию нет.' : 'Лотов не найдено.' }}
    </div>
    <table v-else v-stack class="admin-table">
      <thead>
        <tr>
          <th></th><th>№</th><th>Лот</th><th>Автор</th><th>Категория</th><th class="num">Старт. цена</th>
          <th>Статус</th><th>Создан</th><th></th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="l in lots" :key="l.id">
          <td><img class="thumb" :src="l.image" :alt="l.title" loading="lazy" /></td>
          <td>{{ l.id }}</td>
          <td><router-link :to="'/admin/lots/' + l.id" class="admin-link">{{ l.title }}</router-link></td>
          <td><router-link :to="'/admin/users/' + l.authorId" class="admin-link">{{ l.authorUsername }}</router-link></td>
          <td>{{ getCategory(l.category)?.name || l.category }}</td>
          <td class="num">{{ formatMoney(l.startPrice) }}</td>
          <td>
            <span class="pill" :class="statusClass(l)">{{ adminStatusText(l) }}</span>
          </td>
          <td class="nowrap">{{ formatDate(l.createdAt) }}</td>
          <td>
            <div v-if="l.status === 'pending'" class="row-actions">
              <button class="button small" type="button" @click="open('approve', l)">Одобрить</button>
              <button class="button small danger" type="button" @click="open('reject', l)">Отклонить</button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <Pagination :page="page" :total-pages="totalPages" @change="goTo" />

  <ApproveDialog
    v-if="dialog && dialog.kind === 'approve'"
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="approve"
    @cancel="dialog = null"
  />
  <ReasonDialog
    v-if="dialog && dialog.kind === 'reject'"
    :title="'Отклонить лот «' + dialog.lot.title + '»'"
    hint="Причину увидит автор лота."
    confirm-text="Отклонить"
    danger
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="reject"
    @cancel="dialog = null"
  />
</template>
