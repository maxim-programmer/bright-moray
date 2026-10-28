<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LotCard from '../components/LotCard.vue'
import { api } from '../api'
import {
  state,
  getStatus,
  getStatusText,
  getCategory,
  getSubcategory,
  getDorm,
  formatMoney,
  formatDate,
  formatTimeLeft,
  handleApiError
} from '../store'

const REFRESH_MS = 15000

const route = useRoute()
const router = useRouter()
const lot = ref(null)
const bids = ref([])
const similar = ref([])
const loading = ref(true)
const notFound = ref(false)
const loadError = ref('')
const amount = ref('')
const error = ref('')
const busy = ref(false)

const category = computed(() => (lot.value ? getCategory(lot.value.category) : null))
const sub = computed(() => (lot.value ? getSubcategory(lot.value.category, lot.value.subcategory) : null))
const dorm = computed(() => (lot.value ? getDorm(lot.value.dormId) : null))
const status = computed(() => (lot.value ? getStatus(lot.value) : ''))
const hasBids = computed(() => lot.value && lot.value.bidsCount > 0)
const isOwner = computed(() => state.user && lot.value && lot.value.authorId === state.user.id)
const isLeader = computed(() => state.user && lot.value && lot.value.leaderId === state.user.id)
// Публичных профилей нет: имя лидера берём из его ставки
const leaderName = computed(() => {
  if (!lot.value || !lot.value.leaderId) return ''
  const bid = bids.value.find((b) => b.userId === lot.value.leaderId)
  return bid ? bid.username : '—'
})

/* --- Загрузка --- */

// Ответы могут прийти не по порядку (перезапрос по таймеру и ответ на ставку) —
// применяем только самый свежий
let version = 0

async function refresh() {
  const id = ++version
  const [lotData, bidsData] = await Promise.all([api.lot(route.params.id), api.bids(route.params.id)])
  if (id !== version) return
  lot.value = lotData
  bids.value = bidsData
}

async function load() {
  loading.value = true
  try {
    await refresh()
  } catch (e) {
    if (e.status === 404) notFound.value = true
    else loadError.value = handleApiError(e)
    return
  } finally {
    loading.value = false
  }
  // похожие лоты не критичны: если не загрузились — просто не показываем блок
  api.similarLots(route.params.id).then(
    (list) => (similar.value = list),
    () => {}
  )
}

// Пока лот активен, раз в 15 секунд подтягиваем цену и ставки
let timer = null
onMounted(() => {
  load()
  timer = setInterval(() => {
    if (lot.value && lot.value.status === 'active' && !document.hidden) refresh().catch(() => {})
  }, REFRESH_MS)
})
onUnmounted(() => {
  clearInterval(timer)
  version++ // запоздавшие ответы после ухода со страницы не нужны
})

// Срок вышел, пока страница открыта: локально уже показываем «завершён», а итог берём с сервера
watch(
  () => lot.value && lot.value.status === 'active' && status.value === 'finished',
  (expired) => {
    if (expired) refresh().catch(() => {})
  }
)

/* --- Действия --- */

async function submitBid() {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    // все проверки делает сервер; его текст ошибки показываем как есть
    const res = await api.placeBid(lot.value.id, Number(amount.value))
    version++
    lot.value = res.lot
    bids.value = [res.bid, ...bids.value.filter((b) => b.id !== res.bid.id)]
    amount.value = ''
  } catch (e) {
    error.value = handleApiError(e)
    // цена или статус могли измениться — подтягиваем актуальное состояние
    if (e.status !== 401) refresh().catch(() => {})
  } finally {
    busy.value = false
  }
}

function fillMin() {
  amount.value = lot.value.minBid
}

async function cancel() {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    const updated = await api.cancelLot(lot.value.id)
    version++
    lot.value = updated
  } catch (e) {
    error.value = handleApiError(e)
    if (e.status !== 401) refresh().catch(() => {})
  } finally {
    busy.value = false
  }
}

async function openRandom() {
  error.value = ''
  try {
    const { id } = await api.randomLot()
    if (id) router.push('/lots/' + id)
  } catch (e) {
    error.value = handleApiError(e)
  }
}
</script>

<template>
  <div v-if="loading" class="muted">Загрузка…</div>

  <div v-else-if="loadError" class="error">{{ loadError }}</div>

  <div v-else-if="notFound || !lot">
    <h1>Лот не найден</h1>
    <router-link to="/lots" class="button">К списку лотов</router-link>
  </div>

  <div v-else class="lot-page">
    <div class="crumbs">
      <router-link to="/lots" class="link">Лоты</router-link>
      <template v-if="category">
        <span>›</span>
        <router-link :to="{ path: '/lots', query: { cat: category.id } }" class="link">
          {{ category.name }}
        </router-link>
      </template>
      <template v-if="sub">
        <span>›</span>
        <router-link :to="{ path: '/lots', query: { cat: category.id, sub: sub.id } }" class="link">
          {{ sub.name }}
        </router-link>
      </template>
    </div>

    <div class="lot-layout">
      <div class="lot-left">
        <img class="lot-image" :src="lot.image" :alt="lot.title" />
        <h2 class="lot-section-title">Описание</h2>
        <p class="description">{{ lot.description }}</p>
      </div>

      <div class="lot-right">
        <h1 class="lot-title">{{ lot.title }}</h1>
        <div class="params">
          <div class="param">
            <span class="muted">Категория</span>
            <b>
              <router-link v-if="category" :to="{ path: '/lots', query: { cat: category.id } }" class="link">{{ category.name }}</router-link>
              <template v-if="sub">
                ·
                <router-link :to="{ path: '/lots', query: { cat: category.id, sub: sub.id } }" class="link">{{ sub.name }}</router-link>
              </template>
            </b>
          </div>
          <div class="param">
            <span class="muted">Продавец</span>
            <b>{{ lot.authorUsername }}</b>
          </div>
          <div v-if="dorm" class="param">
            <span class="muted">Общежитие</span>
            <b>{{ dorm.name }}</b>
          </div>
          <div class="param">
            <span class="muted">Начальная цена</span>
            <b>{{ formatMoney(lot.startPrice) }}</b>
          </div>
          <div class="param">
            <span class="muted">Шаг ставки</span>
            <b>{{ formatMoney(lot.step) }}</b>
          </div>
        </div>

        <!-- Активный аукцион -->
        <div v-if="status === 'active'" class="panel">
          <div class="muted">Текущая цена</div>
          <div class="price-big">{{ formatMoney(lot.currentPrice) }}</div>
          <p class="leader">
            Лидер:
            <b>{{ lot.leaderId ? leaderName : 'ставок пока нет' }}</b>
            <span v-if="isLeader" class="pill good">это вы</span>
          </p>
          <div class="timer"><span class="muted">До конца:</span> {{ formatTimeLeft(lot.endsAt) }}</div>

          <div v-if="!state.user" class="muted">
            Чтобы сделать ставку,
            <router-link :to="{ path: '/login', query: { redirect: route.fullPath } }"><b>войдите</b></router-link>.
          </div>
          <div v-else-if="isOwner" class="muted">
            Это ваш лот, делать ставки на него нельзя.
          </div>
          <div v-else-if="isLeader" class="muted">Ваша ставка лидирует — ждём соперников.</div>
          <template v-else>
            <form class="bid-row" @submit.prevent="submitBid">
              <input v-model="amount" type="number" :placeholder="'От ' + lot.minBid + ' ₽'" />
              <button class="button" type="submit" :disabled="busy">Сделать ставку</button>
            </form>
            <button class="link small bid-min" @click="fillMin">
              Подставить минимальную: {{ formatMoney(lot.minBid) }}
            </button>
          </template>
          <div v-if="error" class="error bid-error">{{ error }}</div>

          <div v-if="isOwner && !hasBids" class="cancel-row">
            <button class="button light small" :disabled="busy" @click="cancel">Отменить аукцион</button>
          </div>
        </div>

        <!-- Отменён -->
        <div v-else-if="status === 'cancelled'" class="panel">
          <span class="pill cancelled">{{ getStatusText(lot) }}</span>
          <p>Продавец отменил аукцион. Ставки больше не принимаются.</p>
        </div>

        <!-- Завершён -->
        <div v-else class="panel">
          <span class="pill" :class="hasBids ? 'good' : 'neutral'">
            {{ hasBids ? 'Продан' : 'Не продан' }}
          </span>
          <div class="params">
            <div class="param">
              <span class="muted">Итоговая цена</span>
              <b>{{ hasBids ? formatMoney(lot.currentPrice) : '—' }}</b>
            </div>
            <div v-if="hasBids" class="param">
              <span class="muted">Победитель</span>
              <b>{{ leaderName }}</b>
            </div>
            <div class="param">
              <span class="muted">Завершён</span>
              <b>{{ formatDate(lot.endsAt) }}</b>
            </div>
          </div>
          <!-- Контакты сервер присылает только продавцу и победителю -->
          <div v-if="lot.contact" class="contact-box">
            {{ lot.contact.role }}: <b>{{ lot.contact.name }}</b> — Telegram
            <b>@{{ lot.contact.telegram }}</b>
          </div>
          <div v-if="error" class="error">{{ error }}</div>
        </div>

        <button class="link small" @click="openRandom">Открыть случайный лот</button>
      </div>
    </div>

    <h2 class="history-title">История ставок</h2>
    <div v-if="bids.length === 0" class="muted">Ставок пока нет.</div>
    <table v-else class="table">
      <thead>
        <tr>
          <th>Пользователь</th>
          <th>Сумма</th>
          <th>Дата и время</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(bid, i) in bids" :key="bid.id" :class="{ top: i === 0 }">
          <td>{{ bid.username }}</td>
          <td><b>{{ formatMoney(bid.amount) }}</b></td>
          <td>{{ formatDate(bid.createdAt) }}</td>
        </tr>
      </tbody>
    </table>

    <section v-if="similar.length" class="similar">
      <div class="home-block-head">
        <h2>Похожие лоты</h2>
        <router-link
          :to="{ path: '/lots', query: sub ? { cat: category.id, sub: sub.id } : { cat: lot.category } }"
          class="link"
        >
          Все в разделе «{{ sub ? sub.name : category?.name }}» →
        </router-link>
      </div>
      <div class="grid grid-4">
        <LotCard v-for="item in similar" :key="item.id" :lot="item" />
      </div>
    </section>
  </div>
</template>
