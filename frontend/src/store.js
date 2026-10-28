import { reactive } from 'vue'
import { api, ApiError } from './api'

export const state = reactive({
  user: null, // профиль из /auth/me; null — не вошли
  categories: [], // справочники грузятся один раз при старте
  dorms: [],
  now: Date.now(), // серверное время, обновляется раз в секунду
  initError: ''
})

/* ---------- Серверное время ---------- */

// Часы клиента могут врать, поэтому таймеры считаем как Date.now() + поправка от /api/time
let timeOffset = 0

export function serverNow() {
  return Date.now() + timeOffset
}

async function syncTime() {
  const sent = Date.now()
  const { now } = await api.serverTime()
  const received = Date.now()
  // ответ пришёл примерно в середине между отправкой и получением
  timeOffset = now - Math.round((sent + received) / 2)
  state.now = serverNow()
}

setInterval(() => {
  state.now = serverNow()
}, 1000)

/* ---------- Инициализация сессии ---------- */

let initPromise = null

// Вызывается router guard'ом перед первой навигацией. Повторные вызовы ждут тот же запрос.
export function initSession() {
  if (!initPromise) {
    initPromise = bootstrap().then((ok) => {
      if (!ok) initPromise = null // при следующей навигации попробуем ещё раз
    })
  }
  return initPromise
}

async function bootstrap() {
  const [me, time, categories, dorms] = await Promise.allSettled([
    api.me(),
    syncTime(),
    state.categories.length ? state.categories : api.categories(),
    state.dorms.length ? state.dorms : api.dorms()
  ])
  const errors = []
  for (const [key, result] of [['categories', categories], ['dorms', dorms]]) {
    if (result.status === 'fulfilled') state[key] = result.value
    else errors.push(result.reason)
  }
  if (me.status === 'fulfilled') {
    state.user = me.value
  } else {
    state.user = null
    // 401 — просто не вошли, это не ошибка
    if (me.reason?.status !== 401) errors.push(me.reason)
  }
  if (time.status === 'rejected') errors.push(time.reason)
  state.initError = errors.length ? errors[0].message || 'Ошибка сервера' : ''
  return errors.length === 0
}

/* ---------- Ошибки API ---------- */

let unauthorizedHandler = null

// router.js регистрирует сюда переход на страницу входа
export function onUnauthorized(handler) {
  unauthorizedHandler = handler
}

// Возвращает текст ошибки для пользователя. Если сессия истекла (401 от защищённого метода) —
// забывает пользователя и отправляет на вход.
export function handleApiError(error) {
  if (error instanceof ApiError && error.status === 401 && error.code === 'unauthorized') {
    state.user = null
    if (unauthorizedHandler) unauthorizedHandler()
  }
  return error?.message || 'Ошибка сервера'
}

export const PAGE_SIZE = 20 // столько лотов сервер отдаёт на страницу каталога

export const sortOptions = [
  { id: 'ending', name: 'Скоро завершатся' },
  { id: 'new', name: 'Сначала новые' },
  { id: 'price_asc', name: 'Цена: по возрастанию' },
  { id: 'price_desc', name: 'Цена: по убыванию' },
  { id: 'title_asc', name: 'Название: А → Я' },
  { id: 'title_desc', name: 'Название: Я → А' },
  { id: 'bids', name: 'Больше ставок' }
]

export const statusOptions = [
  { id: 'active', name: 'Активные' },
  { id: 'finished', name: 'Завершённые' },
  { id: 'all', name: 'Все' }
]

/* ---------- Справочники ---------- */

export function getCategory(id) {
  return state.categories.find((c) => c.id === id)
}

export function getSubcategory(categoryId, subId) {
  const category = getCategory(categoryId)
  return category ? category.subs.find((s) => s.id === subId) : undefined
}

export function getDorm(id) {
  return state.dorms.find((d) => d.id === Number(id))
}

export function getDormLabel(id) {
  const dorm = getDorm(id)
  return dorm ? dorm.name : '—'
}

/* ---------- Лоты ---------- */

// Сервер отдаёт статус с учётом срока, но пока страница открыта, лот может истечь —
// тогда показываем «завершён» сразу, не дожидаясь перезапроса.
export function getStatus(lot) {
  if (lot.status === 'active' && lot.endsAt <= state.now) {
    return 'finished'
  }
  return lot.status
}

export function getStatusText(lot) {
  const status = getStatus(lot)
  if (status === 'active') return 'Активен'
  if (status === 'cancelled') return 'Отменён'
  return lot.bidsCount > 0 ? 'Завершён — продан' : 'Завершён — не продан'
}

// Цену и число ставок не считаем — берём из полей лота
export function getPriceText(lot) {
  if (getStatus(lot) === 'finished' && lot.bidsCount === 0) {
    return '—'
  }
  return formatMoney(lot.currentPrice)
}

/* ---------- Форматирование ---------- */

export function formatMoney(value) {
  return value.toLocaleString('ru-RU') + ' ₽'
}

export function formatDate(timestamp) {
  return new Date(timestamp).toLocaleString('ru-RU', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit'
  })
}

export function formatTimeLeft(endsAt) {
  const diff = endsAt - state.now
  if (diff <= 0) return 'Завершён'
  const totalSeconds = Math.floor(diff / 1000)
  const days = Math.floor(totalSeconds / 86400)
  const hours = Math.floor((totalSeconds % 86400) / 3600)
  const minutes = Math.floor((totalSeconds % 3600) / 60)
  const seconds = totalSeconds % 60
  const parts = []
  if (days > 0) parts.push(days + ' д')
  parts.push(hours + ' ч')
  parts.push(minutes + ' мин')
  parts.push(seconds + ' с')
  return parts.join(' ')
}

// Короткий вариант для карточек: «2 д 5 ч», «3 ч 12 мин», «45 с»
export function formatTimeShort(endsAt) {
  const diff = endsAt - state.now
  if (diff <= 0) return 'Завершён'
  const totalMinutes = Math.floor(diff / 60000)
  const days = Math.floor(totalMinutes / 1440)
  const hours = Math.floor((totalMinutes % 1440) / 60)
  const minutes = totalMinutes % 60
  if (days > 0) return days + ' д ' + hours + ' ч'
  if (hours > 0) return hours + ' ч ' + minutes + ' мин'
  if (minutes > 0) return minutes + ' мин'
  return Math.floor(diff / 1000) + ' с'
}

export function isEndingSoon(lot) {
  return getStatus(lot) === 'active' && lot.endsAt - state.now < 60 * 60 * 1000
}

/* ---------- Авторизация ---------- */

// Сервер ставит httpOnly-cookie сессии, в JS храним только профиль
export async function register(data) {
  state.user = await api.register({ ...data, dormId: Number(data.dormId) })
}

export async function login(username, password) {
  state.user = await api.login(username, password)
}

export async function logout() {
  await api.logout()
  state.user = null
}

/* ---------- Профиль и настройки ---------- */

// Профиль меняет сервер; в стор кладём то, что он вернул
export async function updateProfile(data) {
  state.user = await api.updateProfile({
    name: data.name,
    telegram: data.telegram,
    dormId: Number(data.dormId)
  })
}

export async function changeUsername(newUsername) {
  state.user = await api.changeUsername(newUsername)
}

export async function changePassword(current, newPassword, confirm) {
  await api.changePassword({ current, newPassword, confirm })
}

// Положение пользователя в торгах по лоту — для «Моих ставок»
export function getMyBidState(lot) {
  const leading = Boolean(state.user) && lot.leaderId === state.user.id
  const status = getStatus(lot)
  if (status === 'cancelled') return { text: 'Аукцион отменён', kind: 'neutral' }
  if (status === 'finished') {
    return leading ? { text: 'Вы выиграли', kind: 'good' } : { text: 'Не выиграли', kind: 'neutral' }
  }
  return leading ? { text: 'Вы лидируете', kind: 'good' } : { text: 'Ставку перебили', kind: 'bad' }
}
