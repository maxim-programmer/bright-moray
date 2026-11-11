<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LotCard from '../components/LotCard.vue'
import { api } from '../api'
import { realtime, subscribeLot } from '../realtime'
import {
  state,
  getStatusText,
  getCategory,
  getSubcategory,
  getDorm,
  formatMoney,
  formatDate,
  formatTimeLeft,
  handleApiError
} from '../store'

const REFRESH_MS = 15000 // резервный поллинг, пока сокет не подключён
const FINISH_WAIT_MS = 2500 // столько ждём lot_finished после нуля, потом спрашиваем REST
const NOTICE_MS = 6000

const TERMINAL = ['finished', 'cancelled']
// Поля публичного состояния лота, которые приходят в WebSocket-событиях
const LIVE_FIELDS = ['currentPrice', 'minBid', 'bidsCount', 'leaderId', 'endsAt', 'status']

// Тексты ошибок ставки по коду сервера; для остальных кодов — текст сервера
const BID_ERRORS = {
  bid_too_low: 'Ставка меньше минимальной — цена могла уже вырасти.',
  already_leading: 'Ваша ставка и так лидирует.',
  lot_closed: 'Аукцион уже завершён, ставки не принимаются.',
  own_lot: 'Нельзя ставить на собственный лот.',
  validation: 'Введите сумму ставки целым числом.',
  rate_limited: 'Слишком много запросов, попробуйте через пару секунд.',
  network: 'Нет связи с сервером, ставка не отправлена.'
}

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
const notice = ref(null) // { text, kind } — «Время продлено», «Вашу ставку перебили»
// Статус берём только с сервера: по нулю таймера лот локально не завершаем, ждём lot_finished
const status = computed(() => (lot.value ? lot.value.status : ''))
// Время вышло, а итог от сервера ещё не пришёл
const awaitingResult = computed(() => status.value === 'active' && lot.value.endsAt <= state.now)
const antiSnipeHint = computed(() => {
  const a = realtime.antiSnipe
  if (!a || !a.enabled || !a.windowMs || !a.extendMs) return ''
  return (
    `Anti-snipe: ставка в последние ${formatDuration(a.windowMs)} продлевает аукцион ` +
    `на ${formatDuration(a.extendMs)} от момента ставки.`
  )
})
const hasBids = computed(() => lot.value && lot.value.bidsCount > 0)
const isOwner = computed(() => state.user && lot.value && lot.value.authorId === state.user.id)
const isLeader = computed(() => state.user && lot.value && lot.value.leaderId === state.user.id)
// Публичных профилей нет: имя лидера берём из его ставки
const leaderName = computed(() => {
  if (!lot.value || !lot.value.leaderId) return ''
  const bid = bids.value.find((b) => b.userId === lot.value.leaderId)
  return bid ? bid.username : '—'
})

function formatDuration(ms) {
  const seconds = Math.round(ms / 1000)
  if (seconds % 60 === 0) return seconds / 60 + ' мин'
  return seconds + ' с'
}

/* --- Уведомления --- */

let noticeTimer = null
function showNotice(text, kind) {
  notice.value = { text, kind }
  clearTimeout(noticeTimer)
  noticeTimer = setTimeout(() => (notice.value = null), NOTICE_MS)
}

function notifyExtended(endsAt) {
  showNotice('Время продлено до ' + new Date(endsAt).toLocaleTimeString('ru-RU'), 'info')
}

/* --- Слияние состояния лота --- */

// Единые правила для REST-ответов и WebSocket-событий:
// * более старое состояние (bidsCount меньше локального) не применяем;
// * завершённый или отменённый лот обратно в «активный» не откатываем.
function applyLot(next, { full = false } = {}) {
  const current = lot.value
  if (!current) {
    if (full) lot.value = next
    return
  }
  if (next.bidsCount < current.bidsCount) {
    // устарело по ставкам, но поля, зависящие от зрителя (contact, myMaxBid), из полного ответа берём
    if (full) lot.value = { ...current, contact: next.contact, myMaxBid: Math.max(current.myMaxBid || 0, next.myMaxBid || 0) }
    return
  }
  const prevLeader = current.leaderId
  const merged = full ? { ...current, ...next } : { ...current }
  if (!full) for (const key of LIVE_FIELDS) if (key in next) merged[key] = next[key]
  if (TERMINAL.includes(current.status) && !TERMINAL.includes(merged.status)) {
    merged.status = current.status
  }
  lot.value = merged

  // «Вашу ставку перебили»
  const me = state.user?.id
  if (me && prevLeader === me && merged.leaderId !== me && merged.status === 'active') {
    showNotice('Вашу ставку перебили! Новая цена — ' + formatMoney(merged.currentPrice), 'bad')
  }
}

function addBid(bid) {
  if (!bid || bids.value.some((b) => b.id === bid.id)) return
  bids.value = sortBids([bid, ...bids.value])
}

function sortBids(list) {
  return list.sort((a, b) => b.createdAt - a.createdAt || b.id - a.id)
}

/* --- Загрузка --- */

// Ответы REST могут прийти не по порядку (поллинг, ответ на ставку) — применяем только самый свежий
let version = 0
let alive = true

async function refresh() {
  const id = ++version
  const [lotData, bidsData] = await Promise.all([api.lot(route.params.id), api.bids(route.params.id)])
  if (id !== version || !alive) return
  applyLot(lotData, { full: true })
  // объединяем с тем, что уже пришло по сокету, без дублей
  const known = new Map(bidsData.map((b) => [b.id, b]))
  for (const b of bids.value) if (!known.has(b.id)) known.set(b.id, b)
  bids.value = sortBids([...known.values()])
}

async function refreshBids() {
  const list = await api.bids(route.params.id)
  if (!alive) return
  const known = new Map(list.map((b) => [b.id, b]))
  for (const b of bids.value) if (!known.has(b.id)) known.set(b.id, b)
  bids.value = sortBids([...known.values()])
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
  // подписываемся после первой загрузки: snapshot придёт поверх REST-данных и закроет промежуток
  if (alive) unsubscribe = subscribeLot(lot.value.id, onLiveMessage)
  // похожие лоты не критичны: если не загрузились — просто не показываем блок
  api.similarLots(route.params.id).then(
    (list) => (similar.value = list),
    () => {}
  )
}

/* --- Реалтайм --- */

let unsubscribe = null

function onLiveMessage(message) {
  if (!lot.value) return
  switch (message.type) {
    case 'snapshot':
      applyLot(message.lot)
      // пока сокет лежал, могли пройти ставки — события не повторяются, историю берём из REST
      if (bids.value.length < lot.value.bidsCount) refreshBids().catch(() => {})
      break
    case 'bid':
    case 'bid_ok':
      if (message.lot) applyLot(message.lot)
      addBid(message.bid)
      if (message.extended && message.lot) notifyExtended(message.lot.endsAt)
      break
    case 'lot_finished':
      applyLot(message.lot)
      // контакты победителя и продавца в событие не попадают — берём полный лот
      refresh().catch(() => {})
      break
    case 'lot_cancelled':
      applyLot(message.lot)
      break
    case 'error':
      if (message.code === 'not_found') notFound.value = true
      break
  }
}

// Резервный режим: пока сокет не подключён, раз в 15 секунд подтягиваем цену и ставки
let pollTimer = null
onMounted(() => {
  load()
  pollTimer = setInterval(() => {
    if (realtime.connected || document.hidden) return
    if (lot.value && lot.value.status === 'active') refresh().catch(() => {})
  }, REFRESH_MS)
})
onUnmounted(() => {
  alive = false
  clearInterval(pollTimer)
  clearTimeout(finishTimer)
  clearTimeout(noticeTimer)
  if (unsubscribe) unsubscribe()
  version++ // запоздавшие ответы после ухода со страницы не нужны
})

// Таймер дошёл до нуля: лот локально не завершаем, ждём lot_finished.
// Если его нет FINISH_WAIT_MS — спрашиваем сервер сами (и повторяем, пока итог не придёт).
let finishTimer = null
function scheduleFinishCheck() {
  clearTimeout(finishTimer)
  finishTimer = setTimeout(async () => {
    if (!alive || !awaitingResult.value) return
    await refresh().catch(() => {})
    if (awaitingResult.value) scheduleFinishCheck()
  }, FINISH_WAIT_MS)
}
watch(awaitingResult, (waiting) => {
  if (waiting) scheduleFinishCheck()
  else clearTimeout(finishTimer)
})

/* --- Действия --- */

function bidErrorText(e) {
  if (e?.code === 'unauthorized') return handleApiError(e)
  return BID_ERRORS[e?.code] || handleApiError(e)
}

async function submitBid() {
  if (busy.value) return
  error.value = ''
  busy.value = true
  try {
    // ставим через REST: все проверки делает сервер, событие bid разойдётся по сокету всем
    const res = await api.placeBid(lot.value.id, Number(amount.value))
    version++
    applyLot(res.lot, { full: true }) // полный Lot: обновится и myMaxBid
    addBid(res.bid)
    if (res.extended) notifyExtended(res.lot.endsAt)
    amount.value = ''
  } catch (e) {
    error.value = bidErrorText(e)
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
          <div v-if="awaitingResult" class="timer">Время вышло — подводим итоги…</div>
          <div v-else class="timer"><span class="muted">До конца:</span> {{ formatTimeLeft(lot.endsAt) }}</div>

          <div v-if="notice" class="live-notice" :class="notice.kind" role="status">{{ notice.text }}</div>

          <div v-if="awaitingResult" class="muted">Ставки больше не принимаются.</div>
          <div v-else-if="!state.user" class="muted">
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
          <p v-if="antiSnipeHint && !awaitingResult" class="muted small anti-snipe">{{ antiSnipeHint }}</p>

          <div v-if="isOwner && !hasBids && !awaitingResult" class="cancel-row">
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
