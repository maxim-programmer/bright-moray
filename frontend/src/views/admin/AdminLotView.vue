<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import Pagination from '../../components/Pagination.vue'
import ReasonDialog from '../../components/admin/ReasonDialog.vue'
import ApproveDialog from '../../components/admin/ApproveDialog.vue'
import { adminApi } from '../../api'
import { state, formatDate, formatMoney, getCategory, getSubcategory } from '../../store'
import { adminStatusText, statusClass } from '../../status'
import { describeEvent } from './format'
import { useLoader, useAction } from './useAdmin'

const route = useRoute()
const router = useRouter()
const id = computed(() => Number(route.params.id))

const lot = ref(null)
const author = ref(null)
const notFound = ref(false)
const events = ref([])
const hist = ref({ page: 1, totalPages: 1 })

const loadLot = useLoader(() => adminApi.lot(id.value))
const loadHistory = useLoader(() => adminApi.lotHistory(id.value, { page: hist.value.page }))

async function refreshLot() {
  const data = await loadLot.run()
  if (data) {
    lot.value = data.lot
    author.value = data.author
  } else if (loadLot.status.value === 404) {
    notFound.value = true
  }
}

async function refreshHistory() {
  const data = await loadHistory.run()
  if (!data) return
  events.value = data.events
  hist.value = { page: data.page, totalPages: data.totalPages }
}

function historyPage(p) {
  hist.value.page = p
  refreshHistory()
}

async function reloadAll() {
  await refreshLot()
  await refreshHistory()
}

const notStarted = computed(() => lot.value && ['pending', 'approved', 'rejected'].includes(lot.value.status))
const canUnpublish = computed(() => lot.value && !lot.value.hidden && !['pending', 'rejected'].includes(lot.value.status))
const category = computed(() => (lot.value ? getCategory(lot.value.category) : null))
const sub = computed(() => (lot.value ? getSubcategory(lot.value.category, lot.value.subcategory) : null))

/* --- действия --- */

const dialog = ref('') // approve | reject | unpublish | delete
const action = useAction()

function open(kind) {
  action.error.value = ''
  dialog.value = kind
}

async function run(fn) {
  if (await action.exec(fn)) {
    dialog.value = ''
    await reloadAll()
  }
}

const approve = (startsAt) => run(() => adminApi.approveLot(id.value, startsAt))
const reject = (reason) => run(() => adminApi.rejectLot(id.value, reason))
const unpublish = (reason) => run(() => adminApi.unpublishLot(id.value, reason))

async function remove(reason) {
  if (await action.exec(() => adminApi.deleteLot(id.value, reason))) {
    dialog.value = ''
    router.push('/admin/lots')
  }
}

/* --- редактирование --- */

const editing = ref(false)
const form = ref({})
const editAction = useAction()

function startEdit() {
  const l = lot.value
  form.value = {
    title: l.title,
    description: l.description,
    category: l.category,
    subcategory: l.subcategory,
    startPrice: l.startPrice,
    step: l.step,
    hours: Math.round((l.durationSec / 3600) * 100) / 100
  }
  editAction.error.value = ''
  editing.value = true
}

const formSubs = computed(() => getCategory(form.value.category)?.subs || [])
function onCategoryChange() {
  form.value.subcategory = ''
}

async function saveEdit() {
  const l = lot.value
  const f = form.value
  const body = {}
  if (f.title !== l.title) body.title = f.title
  if (f.description !== l.description) body.description = f.description
  if (f.category !== l.category || f.subcategory !== l.subcategory) {
    body.category = f.category
    body.subcategory = f.subcategory
  }
  if (notStarted.value) {
    if (Number(f.startPrice) !== l.startPrice) body.startPrice = Number(f.startPrice)
    if (Number(f.step) !== l.step) body.step = Number(f.step)
    if (Math.round(Number(f.hours) * 3600) !== l.durationSec) body.hours = Number(f.hours)
  }
  if (!Object.keys(body).length) {
    editing.value = false
    return
  }
  const result = await editAction.exec(() => adminApi.editLot(id.value, body))
  if (result) {
    editing.value = false
    await reloadAll()
  }
}

onMounted(async () => {
  await refreshLot()
  if (lot.value) refreshHistory()
})
</script>

<template>
  <router-link to="/admin/lots" class="admin-link">← К модерации лотов</router-link>

  <div v-if="notFound" class="admin-card">
    <h2>Лот не найден</h2>
    <p class="muted">Возможно, он удалён. История удалённого лота остаётся в журнале событий.</p>
    <router-link :to="{ path: '/admin/events', query: { lotId: id } }" class="admin-link">Открыть журнал по лоту №{{ id }} →</router-link>
  </div>
  <div v-else-if="loadLot.error.value" class="error">{{ loadLot.error.value }}</div>
  <div v-else-if="!lot" class="muted">Загрузка…</div>

  <template v-else>
    <div class="admin-card">
      <div class="lot-admin-layout">
        <img class="lot-admin-image" :src="lot.image" :alt="lot.title" />
        <div>
          <h2>
            {{ lot.title }}
            <span class="pill" :class="statusClass(lot, state.now)">{{ adminStatusText(lot, state.now) }}</span>
          </h2>
          <dl class="kv">
            <dt>Номер</dt><dd>{{ lot.id }}</dd>
            <dt>Автор</dt>
            <dd>
              <router-link :to="'/admin/users/' + lot.authorId" class="admin-link">{{ lot.authorUsername }}</router-link>
              <span v-if="author && author.telegram" class="muted"> · @{{ author.telegram }}</span>
            </dd>
            <dt>Категория</dt><dd>{{ category?.name || lot.category }}<template v-if="sub"> · {{ sub.name }}</template></dd>
            <dt>Стартовая цена</dt><dd>{{ formatMoney(lot.startPrice) }}</dd>
            <dt>Шаг ставки</dt><dd>{{ formatMoney(lot.step) }}</dd>
            <dt>Длительность</dt><dd>{{ Math.round((lot.durationSec / 3600) * 100) / 100 }} ч</dd>
            <dt>Создан</dt><dd>{{ formatDate(lot.createdAt) }}</dd>
            <template v-if="lot.status === 'approved' && lot.startsAt">
              <dt>Начало аукциона</dt><dd>{{ formatDate(lot.startsAt) }}</dd>
            </template>
            <template v-if="lot.status === 'active' || lot.status === 'finished'">
              <dt>Текущая цена</dt><dd>{{ formatMoney(lot.currentPrice) }} ({{ lot.bidsCount }} ставок)</dd>
              <dt>Окончание</dt><dd>{{ formatDate(lot.endsAt) }}</dd>
            </template>
            <template v-if="lot.moderationReason">
              <dt>Решение модератора</dt><dd>{{ lot.moderationReason }}</dd>
            </template>
            <template v-if="lot.moderatedAt">
              <dt>Решение принято</dt>
              <dd>{{ formatDate(lot.moderatedAt) }}<template v-if="lot.moderatedBy"> · администратор №{{ lot.moderatedBy }}</template></dd>
            </template>
          </dl>
          <h3>Описание</h3>
          <p class="description">{{ lot.description }}</p>

          <div v-if="action.error.value && !dialog" class="error">{{ action.error.value }}</div>
          <div class="admin-actions">
            <template v-if="lot.status === 'pending'">
              <button class="button" type="button" @click="open('approve')">Одобрить</button>
              <button class="button danger" type="button" @click="open('reject')">Отклонить</button>
            </template>
            <button class="button light" type="button" @click="startEdit">Редактировать</button>
            <button v-if="canUnpublish" class="button light" type="button" @click="open('unpublish')">Снять с публикации</button>
            <button class="button danger" type="button" @click="open('delete')">Удалить</button>
            <router-link
              v-if="['active', 'finished', 'cancelled'].includes(lot.status)"
              :to="'/admin/auctions/' + lot.id"
              class="button light"
            >
              Страница аукциона
            </router-link>
            <router-link v-if="!lot.hidden && ['active', 'finished', 'cancelled'].includes(lot.status)" :to="'/lots/' + lot.id" class="button ghost">
              Открыть на сайте
            </router-link>
          </div>
        </div>
      </div>

      <form v-if="editing" class="edit-form" @submit.prevent="saveEdit">
        <h3>Редактирование лота</h3>
        <label class="field">Название<input v-model="form.title" type="text" maxlength="300" /></label>
        <label class="field">Описание<textarea v-model="form.description" rows="5"></textarea></label>
        <div class="field-row">
          <label class="field">
            Категория
            <select v-model="form.category" @change="onCategoryChange">
              <option v-for="c in state.categories" :key="c.id" :value="c.id">{{ c.name }}</option>
            </select>
          </label>
          <label class="field">
            Подкатегория
            <select v-model="form.subcategory">
              <option value="" disabled>Выберите</option>
              <option v-for="s in formSubs" :key="s.id" :value="s.id">{{ s.name }}</option>
            </select>
          </label>
        </div>
        <template v-if="notStarted">
          <div class="field-row">
            <label class="field">Стартовая цена, ₽<input v-model="form.startPrice" type="number" min="1" /></label>
            <label class="field">Шаг ставки, ₽<input v-model="form.step" type="number" min="1" /></label>
          </div>
          <label class="field">Длительность аукциона, часов<input v-model="form.hours" type="number" min="0" step="any" /></label>
        </template>
        <p v-else class="muted small">Цену, шаг и длительность нельзя менять после начала аукциона.</p>
        <div v-if="editAction.error.value" class="error">{{ editAction.error.value }}</div>
        <div class="admin-actions">
          <button class="button" type="submit" :disabled="editAction.busy.value">Сохранить</button>
          <button class="button light" type="button" @click="editing = false">Отмена</button>
        </div>
      </form>
    </div>

    <section class="admin-section">
      <h2>История изменений</h2>
      <div v-if="loadHistory.error.value" class="error">{{ loadHistory.error.value }}</div>
      <div class="admin-table-wrap">
        <div v-if="!events.length" class="admin-empty">Записей нет.</div>
        <table v-else class="admin-table">
          <thead><tr><th>Время</th><th>Событие</th><th>Кто</th><th>Подробности</th></tr></thead>
          <tbody>
            <tr v-for="e in events" :key="e.id">
              <td class="nowrap">{{ formatDate(e.createdAt) }}</td>
              <td>{{ e.label }}</td>
              <td>{{ e.byAdmin ? 'Администратор' : e.actorUsername || 'Система' }}</td>
              <td class="details">{{ describeEvent(e) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
      <Pagination :page="hist.page" :total-pages="hist.totalPages" @change="historyPage" />
    </section>
  </template>

  <ApproveDialog v-if="dialog === 'approve'" :busy="action.busy.value" :error="action.error.value" @confirm="approve" @cancel="dialog = ''" />
  <ReasonDialog
    v-if="dialog === 'reject'"
    title="Отклонить лот"
    hint="Причину увидит автор лота."
    confirm-text="Отклонить"
    danger
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="reject"
    @cancel="dialog = ''"
  />
  <ReasonDialog
    v-if="dialog === 'unpublish'"
    title="Снять лот с публикации"
    hint="Лот исчезнет из каталога. Идущий или ожидающий старта аукцион будет отменён. Причину увидит автор."
    confirm-text="Снять с публикации"
    danger
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="unpublish"
    @cancel="dialog = ''"
  />
  <ReasonDialog
    v-if="dialog === 'delete'"
    title="Удалить лот"
    hint="Лот и все его ставки будут удалены безвозвратно. Запись об этом останется в журнале событий."
    confirm-text="Удалить"
    danger
    :busy="action.busy.value"
    :error="action.error.value"
    @confirm="remove"
    @cancel="dialog = ''"
  />
</template>
