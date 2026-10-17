import { reactive, watch } from 'vue'
import { users, lots, bids } from './mock/data'
import { categories, dorms } from './mock/categories'

function loadSession() {
  try {
    const id = Number(localStorage.getItem('currentUserId'))
    return users.some((u) => u.id === id) ? id : null
  } catch {
    return null
  }
}

export const state = reactive({
  currentUserId: loadSession(),
  users: users,
  lots: lots,
  bids: bids,
  now: Date.now()
})

watch(
  () => state.currentUserId,
  (id) => {
    try {
      if (id) localStorage.setItem('currentUserId', String(id))
      else localStorage.removeItem('currentUserId')
    } catch {
      // localStorage недоступен — просто не запоминаем сессию
    }
  }
)

setInterval(() => {
  state.now = Date.now()
}, 1000)

export const PAGE_SIZE = 20
export { categories, dorms }

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
  return categories.find((c) => c.id === id)
}

export function getSubcategory(categoryId, subId) {
  const category = getCategory(categoryId)
  return category ? category.subs.find((s) => s.id === subId) : undefined
}

export function getCategoryLabel(lot) {
  const category = getCategory(lot.category)
  const sub = getSubcategory(lot.category, lot.subcategory)
  if (!category) return ''
  return sub ? category.name + ' · ' + sub.name : category.name
}

export function getDorm(id) {
  return dorms.find((d) => d.id === Number(id))
}

export function getDormLabel(id) {
  const dorm = getDorm(id)
  return dorm ? dorm.name : '—'
}

/* ---------- Пользователи ---------- */

export function getUser(id) {
  return state.users.find((u) => u.id === id)
}

export function getUsername(id) {
  const user = getUser(id)
  return user ? user.username : '—'
}

export function getCurrentUser() {
  return getUser(state.currentUserId)
}

export function getLotDorm(lot) {
  const author = getUser(lot.authorId)
  return author ? getDorm(author.dormId) : undefined
}

/* ---------- Лоты и ставки ---------- */

export function getLot(id) {
  return state.lots.find((l) => l.id === Number(id))
}

export function getLotBids(lotId) {
  return state.bids
    .filter((b) => b.lotId === lotId)
    .sort((a, b) => b.createdAt - a.createdAt)
}

export function getBidsCount(lot) {
  return state.bids.filter((b) => b.lotId === lot.id).length
}

export function getStatus(lot) {
  if (lot.status === 'active' && lot.endsAt <= state.now) {
    return 'finished'
  }
  return lot.status
}

export function hasBids(lot) {
  return getBidsCount(lot) > 0
}

export function getCurrentPrice(lot) {
  const lotBids = getLotBids(lot.id)
  return lotBids.length > 0 ? lotBids[0].amount : lot.startPrice
}

export function getLeaderId(lot) {
  const lotBids = getLotBids(lot.id)
  return lotBids.length > 0 ? lotBids[0].userId : null
}

export function getMinBid(lot) {
  return getCurrentPrice(lot) + lot.step
}

export function getStatusText(lot) {
  const status = getStatus(lot)
  if (status === 'active') return 'Активен'
  if (status === 'cancelled') return 'Отменён'
  return hasBids(lot) ? 'Завершён — продан' : 'Завершён — не продан'
}

export function getPriceText(lot) {
  if (getStatus(lot) === 'finished' && !hasBids(lot)) {
    return '—'
  }
  return formatMoney(getCurrentPrice(lot))
}

/* ---------- Каталог: фильтры, сортировка, страницы ---------- */

// filters: { category, sub, q, status, priceMin, priceMax, dormId }
export function filterLots(filters) {
  const q = (filters.q || '').trim().toLowerCase()
  const min = Number(filters.priceMin) || 0
  const max = Number(filters.priceMax) || Infinity
  const status = filters.status || 'active'

  return state.lots.filter((lot) => {
    const lotStatus = getStatus(lot)
    if (status === 'active' && lotStatus !== 'active') return false
    if (status === 'finished' && lotStatus !== 'finished') return false

    if (filters.category && lot.category !== filters.category) return false
    if (filters.sub && lot.subcategory !== filters.sub) return false

    if (q && !lot.title.toLowerCase().includes(q) && !lot.description.toLowerCase().includes(q)) {
      return false
    }

    const price = getCurrentPrice(lot)
    if (price < min || price > max) return false

    if (filters.dormId) {
      const dorm = getLotDorm(lot)
      if (!dorm || dorm.id !== Number(filters.dormId)) return false
    }
    return true
  })
}

export function sortLots(list, sort) {
  const result = [...list]
  const byTitle = (a, b) => a.title.localeCompare(b.title, 'ru')
  switch (sort) {
    case 'new':
      return result.sort((a, b) => b.createdAt - a.createdAt)
    case 'price_asc':
      return result.sort((a, b) => getCurrentPrice(a) - getCurrentPrice(b))
    case 'price_desc':
      return result.sort((a, b) => getCurrentPrice(b) - getCurrentPrice(a))
    case 'title_asc':
      return result.sort(byTitle)
    case 'title_desc':
      return result.sort((a, b) => byTitle(b, a))
    case 'bids':
      return result.sort((a, b) => getBidsCount(b) - getBidsCount(a))
    case 'ending':
    default: {
      // активные — по близости окончания, завершённые и отменённые — в конце
      const rank = (lot) => (getStatus(lot) === 'active' ? 0 : 1)
      return result.sort((a, b) => rank(a) - rank(b) || a.endsAt - b.endsAt)
    }
  }
}

export function paginate(list, page) {
  const totalPages = Math.max(1, Math.ceil(list.length / PAGE_SIZE))
  const current = Math.min(Math.max(1, page), totalPages)
  const start = (current - 1) * PAGE_SIZE
  return { items: list.slice(start, start + PAGE_SIZE), page: current, totalPages }
}

// Похожие лоты: сначала активные из той же подкатегории, затем из той же категории
export function getSimilarLots(lot, limit = 4) {
  const others = getActiveLots().filter((l) => l.id !== lot.id)
  const sameSub = others.filter((l) => l.category === lot.category && l.subcategory === lot.subcategory)
  const sameCat = others.filter((l) => l.category === lot.category && l.subcategory !== lot.subcategory)
  return [...sortLots(sameSub, 'ending'), ...sortLots(sameCat, 'ending')].slice(0, limit)
}

export function getActiveLots() {
  return state.lots.filter((l) => getStatus(l) === 'active')
}

// Случайный активный лот: сначала из переданного списка, иначе из всех активных
export function getRandomLotId(list) {
  const fromList = (list || []).filter((l) => getStatus(l) === 'active')
  const pool = fromList.length > 0 ? fromList : getActiveLots()
  if (pool.length === 0) return null
  return pool[Math.floor(Math.random() * pool.length)].id
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

export function register(data) {
  const { name, username, telegram, dormId, password, confirm } = data
  if (!name || !username || !telegram || !dormId || !password || !confirm) return 'Заполните все поля'
  if (password.length < 6) return 'Пароль должен быть не короче 6 символов'
  if (password !== confirm) return 'Пароли не совпадают'
  if (state.users.some((u) => u.username === username)) return 'Такой никнейм уже занят'
  const user = {
    id: Date.now(),
    username,
    password,
    name,
    telegram: telegram.replace(/^@/, ''),
    dormId: Number(dormId)
  }
  state.users.push(user)
  state.currentUserId = user.id
  return null
}

export function login(username, password) {
  const user = state.users.find((u) => u.username === username && u.password === password)
  if (!user) return 'Неверный никнейм или пароль'
  state.currentUserId = user.id
  return null
}

export function logout() {
  state.currentUserId = null
}

/* ---------- Действия с лотами ---------- */

export function createLot(data) {
  const lot = {
    id: Date.now(),
    title: data.title,
    description: data.description,
    image: data.image,
    category: data.category,
    subcategory: data.subcategory,
    authorId: state.currentUserId,
    startPrice: data.startPrice,
    step: data.step,
    createdAt: Date.now(),
    endsAt: Date.now() + data.hours * 60 * 60 * 1000,
    status: 'active'
  }
  state.lots.push(lot)
  return lot.id
}

export function placeBid(lot, amount) {
  if (!state.currentUserId) return 'Войдите, чтобы делать ставки'
  if (lot.authorId === state.currentUserId) return 'Нельзя делать ставку на свой лот'
  if (getStatus(lot) !== 'active') return 'Аукцион уже закрыт'
  if (getLeaderId(lot) === state.currentUserId) return 'Ваша ставка и так лидирует'
  if (!amount || amount < getMinBid(lot)) {
    return 'Минимальная ставка: ' + formatMoney(getMinBid(lot))
  }
  state.bids.push({
    id: Date.now(),
    lotId: lot.id,
    userId: state.currentUserId,
    amount: amount,
    createdAt: Date.now()
  })
  return null
}

export function cancelLot(lot) {
  if (lot.authorId !== state.currentUserId) return
  if (getStatus(lot) !== 'active') return
  if (hasBids(lot)) return
  lot.status = 'cancelled'
}

/* ---------- Профиль и настройки ---------- */

export function updateProfile(data) {
  const { name, telegram, dormId } = data
  if (!name || !telegram || !dormId) return 'Заполните все поля'
  const user = getCurrentUser()
  user.name = name
  user.telegram = telegram.replace(/^@/, '')
  user.dormId = Number(dormId)
  return null
}

export function changeUsername(newUsername) {
  if (!newUsername) return 'Введите новый никнейм'
  const taken = state.users.some((u) => u.username === newUsername && u.id !== state.currentUserId)
  if (taken) return 'Такой никнейм уже занят'
  getCurrentUser().username = newUsername
  return null
}

export function changePassword(current, newPassword, confirm) {
  if (!current || !newPassword || !confirm) return 'Заполните все поля'
  if (getCurrentUser().password !== current) return 'Текущий пароль указан неверно'
  if (newPassword.length < 6) return 'Пароль должен быть не короче 6 символов'
  if (newPassword !== confirm) return 'Пароли не совпадают'
  getCurrentUser().password = newPassword
  return null
}

export function getMyLots() {
  return sortLots(
    state.lots.filter((l) => l.authorId === state.currentUserId),
    'ending'
  )
}

export function getMyBidLots() {
  return sortLots(
    state.lots.filter((l) =>
      state.bids.some((b) => b.lotId === l.id && b.userId === state.currentUserId)
    ),
    'ending'
  )
}

export function getMyMaxBid(lot) {
  const mine = state.bids.filter((b) => b.lotId === lot.id && b.userId === state.currentUserId)
  return mine.length > 0 ? Math.max(...mine.map((b) => b.amount)) : 0
}

// Положение пользователя в торгах по лоту — для «Моих ставок»
export function getMyBidState(lot) {
  const leading = getLeaderId(lot) === state.currentUserId
  const status = getStatus(lot)
  if (status === 'cancelled') return { text: 'Аукцион отменён', kind: 'neutral' }
  if (status === 'finished') {
    return leading ? { text: 'Вы выиграли', kind: 'good' } : { text: 'Не выиграли', kind: 'neutral' }
  }
  return leading ? { text: 'Вы лидируете', kind: 'good' } : { text: 'Ставку перебили', kind: 'bad' }
}

export function getUserStats(userId) {
  const created = state.lots.filter((l) => l.authorId === userId)
  const participated = new Set(state.bids.filter((b) => b.userId === userId).map((b) => b.lotId))
  const won = state.lots.filter((l) => getStatus(l) === 'finished' && getLeaderId(l) === userId)
  const sold = created.filter((l) => getStatus(l) === 'finished' && hasBids(l))
  return {
    created: created.length,
    participated: participated.size,
    won: won.length,
    rating: won.length + sold.length, // заглушка: +1 за каждую успешную сделку
    complaints: 0
  }
}
