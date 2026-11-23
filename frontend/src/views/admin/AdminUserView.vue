<script setup>
import { ref, computed, onMounted, watch } from 'vue'
import { useRoute } from 'vue-router'
import Pagination from '../../components/Pagination.vue'
import ReasonDialog from '../../components/admin/ReasonDialog.vue'
import { adminApi } from '../../api'
import { state, formatDate, formatMoney, getDormLabel } from '../../store'
import { adminStatusText, statusClass } from '../../status'
import { describeEvent, dayStart, dayEnd } from './format'
import { useLoader, useAction } from './useAdmin'

const route = useRoute()
const id = computed(() => Number(route.params.id))

const user = ref(null)
const stats = ref(null)
const notFound = ref(false)
const from = ref('')
const to = ref('')
const tab = ref('activity')

const loadUser = useLoader(() => adminApi.user(id.value))
const loadStats = useLoader(() => adminApi.userStats(id.value, { from: dayStart(from.value), to: dayEnd(to.value) }))

async function refreshUser() {
  const data = await loadUser.run()
  if (data) {
    user.value = data.user
    stats.value = data.stats
  } else if (loadUser.status.value === 404) {
    notFound.value = true
  }
}

async function refreshStats() {
  const data = await loadStats.run()
  if (data) stats.value = data
}

/* --- вкладки: активность, лоты, ставки --- */

const eventType = ref('')
const eventTypes = ref({ types: [], groups: [] })
const events = ref([])
const lots = ref([])
const bids = ref([])
const pages = ref({ activity: [1, 1], lots: [1, 1], bids: [1, 1] }) // [страница, всего страниц]

const loadTab = useLoader(async () => {
  const [page] = pages.value[tab.value]
  if (tab.value === 'activity') {
    return adminApi.userActivity(id.value, { type: eventType.value, from: dayStart(from.value), to: dayEnd(to.value), page })
  }
  if (tab.value === 'lots') return adminApi.userLots(id.value, { page })
  return adminApi.userBids(id.value, { page })
})

async function refreshTab() {
  const data = await loadTab.run()
  if (!data) return
  if (tab.value === 'activity') events.value = data.events
  else if (tab.value === 'lots') lots.value = data.lots
  else bids.value = data.bids
  pages.value[tab.value] = [data.page, data.totalPages]
}

function setTab(name) {
  tab.value = name
  refreshTab()
}

function goTo(p) {
  pages.value[tab.value][0] = p
  refreshTab()
}

function applyPeriod() {
  pages.value.activity[0] = 1
  refreshStats()
  if (tab.value === 'activity') refreshTab()
}

/* --- блокировка --- */

const dialog = ref('') // '' | 'block'
const action = useAction()

async function block(reason) {
  const updated = await action.exec(() => adminApi.blockUser(id.value, reason))
  if (updated) {
    dialog.value = ''
    await refreshUser()
    if (tab.value === 'activity') refreshTab()
  }
}

async function unblock() {
  const updated = await action.exec(() => adminApi.unblockUser(id.value))
  if (updated) {
    await refreshUser()
    if (tab.value === 'activity') refreshTab()
  }
}

const isSelf = computed(() => state.user && user.value && state.user.id === user.value.id)

onMounted(async () => {
  adminApi.eventTypes().then((t) => (eventTypes.value = t), () => {})
  await refreshUser()
  if (user.value) refreshTab()
})
watch(eventType, () => {
  pages.value.activity[0] = 1
  refreshTab()
})
</script>

<template>
  <router-link to="/admin/users" class="admin-link">← К списку пользователей</router-link>

  <div v-if="notFound" class="admin-card"><h2>Пользователь не найден</h2></div>
  <div v-else-if="loadUser.error.value" class="error">{{ loadUser.error.value }}</div>
  <div v-else-if="!user" class="muted">Загрузка…</div>

  <template v-else>
    <div class="admin-card">
      <h2>
        {{ user.username }}
        <span class="pill" :class="user.role === 'admin' ? 'good' : 'neutral'">{{ user.role === 'admin' ? 'Администратор' : 'Пользователь' }}</span>
        <span v-if="user.blocked" class="pill cancelled">Заблокирован</span>
      </h2>
      <dl class="kv">
        <dt>ID</dt><dd>{{ user.id }}</dd>
        <dt>Имя</dt><dd>{{ user.name }}</dd>
        <dt>Почта</dt><dd>{{ user.email || '—' }}</dd>
        <dt>Telegram</dt><dd>{{ user.telegram ? '@' + user.telegram : '—' }}</dd>
        <dt>Общежитие</dt><dd>{{ user.dormId ? getDormLabel(user.dormId) : '—' }}</dd>
        <dt>Зарегистрирован</dt><dd>{{ formatDate(user.createdAt) }}</dd>
        <dt>Последняя активность</dt><dd>{{ user.lastActivity ? formatDate(user.lastActivity) : '—' }}</dd>
        <template v-if="user.blocked">
          <dt>Заблокирован</dt><dd>{{ formatDate(user.blockedAt) }}</dd>
          <dt>Причина блокировки</dt><dd>{{ user.blockedReason || 'не указана' }}</dd>
        </template>
      </dl>
      <div v-if="action.error.value && !dialog" class="error">{{ action.error.value }}</div>
      <div class="admin-actions">
        <template v-if="user.role !== 'admin' && !isSelf">
          <button v-if="!user.blocked" class="button danger" type="button" @click="dialog = 'block'">Заблокировать</button>
          <button v-else class="button" type="button" :disabled="action.busy.value" @click="unblock">Разблокировать</button>
        </template>
        <span v-else class="muted small">Администратора заблокировать нельзя.</span>
      </div>
    </div>

    <div class="admin-card">
      <h2>Статистика</h2>
      <div class="admin-bar">
        <label class="field">С даты<input v-model="from" type="date" @change="applyPeriod" /></label>
        <label class="field">По дату<input v-model="to" type="date" @change="applyPeriod" /></label>
      </div>
      <div v-if="stats" class="stat-grid">
        <div class="stat-card"><div class="stat-value">{{ stats.lotsCreated }}</div><div class="stat-label">Создано лотов</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.bidsPlaced }}</div><div class="stat-label">Сделано ставок</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.participated }}</div><div class="stat-label">Аукционов с участием</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.won }}</div><div class="stat-label">Выиграно</div></div>
        <div class="stat-card"><div class="stat-value">{{ formatMoney(stats.spentTotal) }}</div><div class="stat-label">Потрачено</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.sold }}</div><div class="stat-label">Продано</div></div>
        <div class="stat-card"><div class="stat-value">{{ formatMoney(stats.earnedTotal) }}</div><div class="stat-label">Выручка</div></div>
        <div class="stat-card"><div class="stat-value">{{ stats.events }}</div><div class="stat-label">Событий в журнале</div></div>
      </div>
      <div v-if="loadStats.error.value" class="error">{{ loadStats.error.value }}</div>
    </div>

    <div class="sub-tabs">
      <button type="button" :class="{ active: tab === 'activity' }" @click="setTab('activity')">История активности</button>
      <button type="button" :class="{ active: tab === 'lots' }" @click="setTab('lots')">Лоты</button>
      <button type="button" :class="{ active: tab === 'bids' }" @click="setTab('bids')">Ставки</button>
    </div>
    <div v-if="loadTab.error.value" class="error">{{ loadTab.error.value }}</div>

    <template v-if="tab === 'activity'">
      <div class="admin-bar">
        <label class="field">
          Тип события
          <select v-model="eventType">
            <option value="">Все события</option>
            <optgroup label="Группы">
              <option v-for="g in eventTypes.groups" :key="g.type" :value="g.type">{{ g.label }}</option>
            </optgroup>
            <optgroup label="События">
              <option v-for="t in eventTypes.types" :key="t.type" :value="t.type">{{ t.label }}</option>
            </optgroup>
          </select>
        </label>
        <span class="muted small">Период — тот же, что у статистики выше.</span>
      </div>
      <div class="admin-table-wrap">
        <div v-if="!events.length && !loadTab.loading.value" class="admin-empty">Событий нет.</div>
        <table v-else class="admin-table">
          <thead><tr><th>Время</th><th>Событие</th><th>Лот</th><th>Подробности</th></tr></thead>
          <tbody>
            <tr v-for="e in events" :key="e.id">
              <td class="nowrap">{{ formatDate(e.createdAt) }}</td>
              <td>{{ e.label }}<span v-if="e.byAdmin" class="muted small"> (админ)</span></td>
              <td><router-link v-if="e.lotId" :to="'/admin/lots/' + e.lotId" class="admin-link">№{{ e.lotId }}</router-link></td>
              <td class="details">{{ describeEvent(e) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </template>

    <div v-else-if="tab === 'lots'" class="admin-table-wrap">
      <div v-if="!lots.length && !loadTab.loading.value" class="admin-empty">Лотов нет.</div>
      <table v-else class="admin-table">
        <thead><tr><th>№</th><th>Название</th><th>Статус</th><th class="num">Цена</th><th class="num">Ставок</th><th>Создан</th></tr></thead>
        <tbody>
          <tr v-for="l in lots" :key="l.id">
            <td>{{ l.id }}</td>
            <td><router-link :to="'/admin/lots/' + l.id" class="admin-link">{{ l.title }}</router-link></td>
            <td><span class="pill" :class="statusClass(l)">{{ adminStatusText(l) }}</span></td>
            <td class="num">{{ formatMoney(l.currentPrice) }}</td>
            <td class="num">{{ l.bidsCount }}</td>
            <td class="nowrap">{{ formatDate(l.createdAt) }}</td>
          </tr>
        </tbody>
      </table>
    </div>

    <div v-else class="admin-table-wrap">
      <div v-if="!bids.length && !loadTab.loading.value" class="admin-empty">Ставок нет.</div>
      <table v-else class="admin-table">
        <thead><tr><th>Время</th><th>Лот</th><th class="num">Ставка</th><th>Состояние</th></tr></thead>
        <tbody>
          <tr v-for="b in bids" :key="b.id">
            <td class="nowrap">{{ formatDate(b.createdAt) }}</td>
            <td><router-link :to="'/admin/auctions/' + b.lotId" class="admin-link">{{ b.lotTitle }}</router-link></td>
            <td class="num">{{ formatMoney(b.amount) }}</td>
            <td>
              <span v-if="b.leading" class="pill good">{{ b.lotStatus === 'finished' ? 'Выиграла' : 'Лидирует' }}</span>
              <span v-else class="pill neutral">Перебита</span>
              <span class="muted small"> · {{ b.lotStatus === 'active' ? 'идёт' : b.lotStatus === 'finished' ? 'завершён' : 'отменён' }}</span>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination :page="pages[tab][0]" :total-pages="pages[tab][1]" @change="goTo" />
  </template>

  <ReasonDialog
    v-if="dialog === 'block'"
    title="Заблокировать пользователя"
    hint="Пользователь будет разлогинен и не сможет войти или делать ставки, пока вы его не разблокируете."
    confirm-text="Заблокировать"
    :required="false"
    danger
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="block"
    @cancel="dialog = ''"
  />
</template>
