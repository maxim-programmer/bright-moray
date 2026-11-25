<script setup>
import { ref, watch, onMounted, onUnmounted } from 'vue'
import Pagination from '../../components/Pagination.vue'
import { vStack } from '../../components/admin/stackTable'
import ReasonDialog from '../../components/admin/ReasonDialog.vue'
import ConfirmDialog from '../../components/admin/ConfirmDialog.vue'
import { adminApi } from '../../api'
import { formatDate } from '../../store'
import { useLoader, useAction } from './useAdmin'

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
  clearTimeout(timer)
  page.value = 1
  load()
}

function goTo(p) {
  page.value = p
  load()
}

/* --- поиск по мере ввода --- */

let timer
watch(q, () => {
  clearTimeout(timer)
  timer = setTimeout(search, 350)
})
onUnmounted(() => clearTimeout(timer))

/* --- блокировка --- */

const dialog = ref(null) // { kind: 'block' | 'unblock', user }
const action = useAction()

function open(kind, user) {
  action.error.value = ''
  dialog.value = { kind, user }
}

async function run(fn) {
  if (await action.exec(fn)) {
    dialog.value = null
    load()
  }
}

const block = (reason) => run(() => adminApi.blockUser(dialog.value.user.id, reason))
const unblock = () => run(() => adminApi.unblockUser(dialog.value.user.id))

onMounted(load)
</script>

<template>
  <form class="admin-bar" @submit.prevent="search">
    <label class="field grow">
      Поиск по ID, имени, username или Telegram
      <input v-model="q" type="search" placeholder="Например: 15, Иван, ivan_k" />
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
          <th class="num">Лотов</th><th class="num">Ставок</th><th>Регистрация</th><th>Активность</th><th></th>
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
          <td class="nowrap">{{ u.lastActivity ? formatDate(u.lastActivity) : '—' }}</td>
          <td>
            <div v-if="u.role !== 'admin'" class="row-actions">
              <button v-if="u.blocked" class="button small light" type="button" @click="open('unblock', u)">Разблокировать</button>
              <button v-else class="button small danger" type="button" @click="open('block', u)">Заблокировать</button>
            </div>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <Pagination :page="page" :total-pages="totalPages" @change="goTo" />
  <ReasonDialog
    v-if="dialog?.kind === 'block'"
    :title="'Заблокировать ' + dialog.user.username + '?'"
    hint="Пользователь будет разлогинен и не сможет войти или делать ставки, пока вы его не разблокируете."
    confirm-text="Заблокировать"
    :required="false"
    danger
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="block"
    @cancel="dialog = null"
  />
  <ConfirmDialog
    v-if="dialog?.kind === 'unblock'"
    :title="'Разблокировать ' + dialog.user.username + '?'"
    hint="Пользователь снова сможет войти, создавать лоты и делать ставки."
    confirm-text="Разблокировать"
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="unblock"
    @cancel="dialog = null"
  />
</template>