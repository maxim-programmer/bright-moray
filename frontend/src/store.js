import { reactive } from 'vue'
import { users, lots, bids } from './mock/data'

export const state = reactive({
  currentUserId: null,
  users: users,
  lots: lots,
  bids: bids,
  now: Date.now()
})

setInterval(() => {
  state.now = Date.now()
}, 1000)

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

export function getLot(id) {
  return state.lots.find((l) => l.id === Number(id))
}

export function getLotBids(lotId) {
  return state.bids
    .filter((b) => b.lotId === lotId)
    .sort((a, b) => b.createdAt - a.createdAt)
}

export function getStatus(lot) {
  if (lot.status === 'active' && lot.endsAt <= state.now) {
    return 'finished'
  }
  return lot.status
}

export function hasBids(lot) {
  return getLotBids(lot.id).length > 0
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
  if (status === 'cancelled') return 'Отменен'
  return hasBids(lot) ? 'Завершен - продан' : 'Завершен - не продан'
}

export function getPriceText(lot) {
  if (getStatus(lot) === 'finished' && !hasBids(lot)) {
    return '—'
  }
  return formatMoney(getCurrentPrice(lot))
}

export function formatMoney(value) {
  return value.toLocaleString('ru-RU') + ' ₽'
}

export function formatDate(timestamp) {
  return new Date(timestamp).toLocaleString('ru-RU')
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

export function register(username, password, confirm) {
  if (!username || !password || !confirm) return 'Заполните все поля'
  if (password !== confirm) return 'Пароли не совпадают'
  if (state.users.some((u) => u.username === username)) return 'Такой никнейм уже занят'
  const user = { id: Date.now(), username, password }
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

export function createLot(data) {
  const lot = {
    id: Date.now(),
    title: data.title,
    description: data.description,
    image: data.image,
    authorId: state.currentUserId,
    startPrice: data.startPrice,
    step: data.step,
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

export function changeUsername(newUsername) {
  if (!newUsername) return 'Введите новый никнейм'
  const taken = state.users.some((u) => u.username === newUsername && u.id !== state.currentUserId)
  if (taken) return 'Такой никнейм уже занят'
  getCurrentUser().username = newUsername
  return null
}

export function changePassword(newPassword, confirm) {
  if (!newPassword || !confirm) return 'Заполните оба поля'
  if (newPassword !== confirm) return 'Пароли не совпадают'
  getCurrentUser().password = newPassword
  return null
}

export function getMyLots() {
  return state.lots.filter((l) => l.authorId === state.currentUserId)
}

export function getMyBidLots() {
  return state.lots.filter((l) =>
    state.bids.some((b) => b.lotId === l.id && b.userId === state.currentUserId)
  )
}

export function getActiveLots() {
  return state.lots.filter((l) => getStatus(l) === 'active')
}
