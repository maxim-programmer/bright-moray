// Подписи для админ-панели. Чистые функции — проверяются тестами (format.test.js).

const FIELD_LABELS = {
  title: 'название',
  description: 'описание',
  image: 'картинка',
  category: 'категория',
  subcategory: 'подкатегория',
  startPrice: 'стартовая цена',
  step: 'шаг',
  durationSec: 'длительность, с',
  minBidStep: 'минимальный шаг',
  minDurationMinutes: 'мин. длительность, мин',
  maxDurationMinutes: 'макс. длительность, мин',
  defaultDurationMinutes: 'длительность по умолчанию, мин',
  antiSnipeWindowSec: 'окно продления, с',
  antiSnipeExtendSec: 'продление, с'
}

const money = (n) => Number(n).toLocaleString('ru-RU') + ' ₽'
const when = (ms) => new Date(ms).toLocaleString('ru-RU')

function short(v) {
  const s = typeof v === 'string' ? v : JSON.stringify(v)
  return s.length > 60 ? s.slice(0, 57) + '…' : s
}

function changesText(changes) {
  return Object.entries(changes || {})
    .map(([key, v]) => `${FIELD_LABELS[key] || key}: ${short(v.from)} → ${short(v.to)}`)
    .join('; ')
}

// Краткое описание события для таблицы журнала
export function describeEvent(e) {
  const d = e.details || {}
  const title = d.title ? `«${d.title}»` : ''
  const reason = d.reason ? `Причина: ${d.reason}` : ''
  const parts = []
  switch (e.type) {
    case 'user.registered':
      parts.push(`Логин ${d.username || ''}`.trim())
      break
    case 'user.login':
      if (d.ip) parts.push(`IP ${d.ip}`)
      break
    case 'user.blocked':
    case 'lot.rejected':
    case 'lot.unpublished':
    case 'lot.deleted':
    case 'auction.cancelled':
    case 'auction.annulled':
    case 'auction.dispute_resolved':
      parts.push(title, reason)
      break
    case 'lot.created':
      parts.push(title, d.startPrice != null ? `старт ${money(d.startPrice)}` : '')
      break
    case 'lot.updated':
      parts.push(changesText(d.changes))
      break
    case 'lot.approved':
      parts.push(title, d.immediate ? 'аукцион начался сразу' : d.startsAt ? `старт ${when(d.startsAt)}` : '')
      break
    case 'bid.placed':
      parts.push(money(d.amount), d.extended ? 'аукцион продлён' : '')
      break
    case 'bid.removed':
      parts.push(title, `ставка ${money(d.amount)} удалена`, reason)
      break
    case 'auction.started':
      parts.push(title, d.endsAt ? `до ${when(d.endsAt)}` : '')
      break
    case 'auction.finished':
    case 'auction.force_finished':
      parts.push(title, d.finalPrice != null ? `итог ${money(d.finalPrice)}` : 'без ставок', e.type === 'auction.force_finished' ? reason : '')
      break
    case 'settings.updated':
      parts.push(changesText(d.changes))
      break
    case 'category.created':
    case 'category.updated':
    case 'category.deleted':
    case 'subcategory.created':
    case 'subcategory.updated':
    case 'subcategory.deleted':
    case 'dorm.created':
    case 'dorm.updated':
    case 'dorm.deleted':
      parts.push(d.name || d.id || '')
      break
    default:
      if (e.type.startsWith('admin.cli_')) {
        parts.push(d.username || '')
      } else if (Object.keys(d).length) {
        parts.push(short(d))
      }
  }
  return parts.filter(Boolean).join(' · ')
}

// Дата YYYY-MM-DD из поля <input type="date"> → начало этого дня по местному времени (мс)
export function dayStart(value) {
  if (!value) return undefined
  const [y, m, d] = value.split('-').map(Number)
  const t = new Date(y, m - 1, d).getTime()
  return Number.isFinite(t) ? t : undefined
}

// Конец выбранного дня (начало следующего): верхняя граница периода не включается
export function dayEnd(value) {
  if (!value) return undefined
  const [y, m, d] = value.split('-').map(Number)
  const t = new Date(y, m - 1, d + 1).getTime()
  return Number.isFinite(t) ? t : undefined
}

// Значение <input type="datetime-local"> → мс (по местному времени); пусто — undefined
export function localDateTime(value) {
  if (!value) return undefined
  const t = new Date(value).getTime()
  return Number.isFinite(t) ? t : undefined
}

// Среднее с одним знаком после запятой: 2.5 → «2,5»
export function formatAvg(n) {
  return (Math.round(Number(n) * 10) / 10).toLocaleString('ru-RU')
}

// Минуты → «1 сутки», «2 ч», «45 мин» для подсказок в настройках
export function minutesText(min) {
  if (min % 1440 === 0) return `${min / 1440} сут.`
  if (min % 60 === 0) return `${min / 60} ч`
  return `${min} мин`
}
