<script setup>
import { ref, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Pagination from '../../components/Pagination.vue'
import { vStack } from '../../components/admin/stackTable'
import UserPicker from '../../components/admin/UserPicker.vue'
import { adminApi } from '../../api'
import { formatDate } from '../../store'
import { describeEvent, dayStart, dayEnd } from './format'
import { useLoader } from './useAdmin'

const route = useRoute()
const router = useRouter()
const str = (v) => (typeof v === 'string' ? v : '')

const userId = ref(str(route.query.userId))
const lotId = ref(str(route.query.lotId))
const type = ref(str(route.query.type))
const byAdmin = ref(route.query.byAdmin === 'true')
const from = ref(str(route.query.from))
const to = ref(str(route.query.to))
const page = ref(1)

const events = ref([])
const total = ref(0)
const totalPages = ref(1)
const types = ref({ types: [], groups: [] })

const loader = useLoader(() =>
  adminApi.events({
    userId: userId.value,
    lotId: lotId.value,
    type: type.value,
    byAdmin: byAdmin.value ? 'true' : '',
    from: dayStart(from.value),
    to: dayEnd(to.value),
    page: page.value
  })
)

const periodError = ref('')

async function load() {
  const data = await loader.run()
  if (!data) return
  events.value = data.events
  total.value = data.total
  totalPages.value = data.totalPages
}

function reload() {
  periodError.value = from.value && to.value && from.value > to.value ? 'Дата «С» должна быть не позже даты «По»' : ''
  if (periodError.value) return
  page.value = 1
  // фильтры — в адресе: ссылкой на выборку можно поделиться, а обновление страницы её не сбросит
  const query = {}
  if (userId.value) query.userId = String(userId.value)
  if (lotId.value) query.lotId = String(lotId.value)
  if (type.value) query.type = type.value
  if (byAdmin.value) query.byAdmin = 'true'
  if (from.value) query.from = from.value
  if (to.value) query.to = to.value
  router.replace({ query })
  load()
}

watch(userId, reload)

function reset() {
  userId.value = ''
  lotId.value = ''
  type.value = ''
  byAdmin.value = false
  from.value = ''
  to.value = ''
  reload()
}

function goTo(p) {
  page.value = p
  load()
}

onMounted(() => {
  adminApi.eventTypes().then((t) => (types.value = t), () => {})
  load()
})
</script>

<template>
  <form class="admin-bar" @submit.prevent="reload">
    <UserPicker v-model="userId" />
    <label class="field">
      № лота
      <input v-model="lotId" type="number" min="1" placeholder="любой" />
    </label>
    <label class="field">
      Тип события
      <select v-model="type" @change="reload">
        <option value="">Все события</option>
        <optgroup label="Группы">
          <option v-for="g in types.groups" :key="g.type" :value="g.type">{{ g.label }}</option>
        </optgroup>
        <optgroup label="События">
          <option v-for="t in types.types" :key="t.type" :value="t.type">{{ t.label }}</option>
        </optgroup>
      </select>
    </label>
    <label class="field">С даты<input v-model="from" type="date" @change="reload" /></label>
    <label class="field">По дату<input v-model="to" type="date" @change="reload" /></label>
    <label class="check"><input v-model="byAdmin" type="checkbox" @change="reload" /> Только действия администраторов</label>
    <button class="button" type="submit">Применить</button>
    <button class="button light" type="button" @click="reset">Сбросить</button>
  </form>

  <div v-if="periodError || loader.error.value" class="error">{{ periodError || loader.error.value }}</div>
  <p class="muted small">Записей: {{ total }}</p>

  <div class="admin-table-wrap">
    <div v-if="!events.length && !loader.loading.value" class="admin-empty">Событий не найдено.</div>
    <table v-else v-stack class="admin-table">
      <thead>
        <tr><th>Время</th><th>Событие</th><th>Пользователь</th><th>Исполнитель</th><th>Лот</th><th>Подробности</th></tr>
      </thead>
      <tbody>
        <tr v-for="e in events" :key="e.id">
          <td class="nowrap">{{ formatDate(e.createdAt) }}</td>
          <td>{{ e.label }}</td>
          <td>
            <router-link v-if="e.userId" :to="'/admin/users/' + e.userId" class="admin-link">{{ e.username || '№' + e.userId }}</router-link>
            <span v-else class="muted">—</span>
          </td>
          <td>
            <template v-if="e.actorId">
              <router-link :to="'/admin/users/' + e.actorId" class="admin-link">{{ e.actorUsername || '№' + e.actorId }}</router-link>
              <span v-if="e.byAdmin" class="pill good">админ</span>
            </template>
            <span v-else class="muted">{{ e.byAdmin ? 'терминал' : 'система' }}</span>
          </td>
          <td><router-link v-if="e.lotId" :to="'/admin/lots/' + e.lotId" class="admin-link">№{{ e.lotId }}</router-link></td>
          <td class="details">{{ describeEvent(e) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
  <Pagination :page="page" :total-pages="totalPages" @change="goTo" />
</template>
