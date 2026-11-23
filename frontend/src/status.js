// Статусы лота и их подписи. Чистый модуль (без Vue и сети) — его проверяют тесты.

// Лот ещё не прошёл модерацию или отклонён: публично его нет, видят только автор и администратор
export const MODERATION_STATUSES = ['pending', 'approved', 'rejected']

export function isModeration(status) {
  return MODERATION_STATUSES.includes(status)
}

// Лот, на который можно подписаться по WebSocket (сервер для остальных отвечает not_found)
export function isLive(status) {
  return !isModeration(status)
}

const TEXT = {
  pending: 'На модерации',
  approved: 'Одобрен — ждёт начала',
  rejected: 'Отклонён',
  active: 'Активен',
  cancelled: 'Отменён'
}

// status — уже с учётом срока (см. getStatus в store.js)
export function statusText(status, bidsCount = 0) {
  if (status === 'finished') return bidsCount > 0 ? 'Завершён — продан' : 'Завершён — не продан'
  return TEXT[status] || String(status)
}

// Подпись статуса для администратора: те же слова, но с пометкой о снятии с публикации
export function adminStatusText(lot, now = Date.now()) {
  let status = lot.status
  if (status === 'active' && lot.endsAt <= now) status = 'finished'
  const text = status === 'finished' ? (lot.bidsCount > 0 ? 'Завершён — продан' : 'Завершён — не продан') : statusText(status)
  return lot.hidden ? `${text} · снят с публикации` : text
}

// CSS-класс пилюли статуса
export function statusClass(lot, now = Date.now()) {
  if (lot.status === 'active' && lot.endsAt <= now) return 'finished'
  return lot.status
}
