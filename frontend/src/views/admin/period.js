// Периоды и разбиение на интервалы для графиков. Чистые функции, даты — по местному времени.
import { dayStart, dayEnd } from './format'

export const MAX_BUCKETS = 14 // столбец = один запрос статистики к серверу

// Быстрый выбор периода: сколько последних дней, включая сегодня
export const PRESETS = [
  { days: 7, name: '7 дней' },
  { days: 30, name: '30 дней' },
  { days: 90, name: '90 дней' },
  { days: 365, name: 'Год' }
]

const pad = (n) => String(n).padStart(2, '0')

// Date → «ГГГГ-ММ-ДД» для <input type="date">
export function dateValue(d) {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

export function presetPeriod(days, now = Date.now()) {
  const d = new Date(now)
  return { from: dateValue(new Date(d.getFullYear(), d.getMonth(), d.getDate() - days + 1)), to: dateValue(d) }
}

export function periodError(from, to) {
  return from && to && from > to ? 'Дата «С» должна быть не позже даты «По»' : ''
}

const short = (ms) => new Date(ms).toLocaleDateString('ru-RU', { day: '2-digit', month: '2-digit' })
const full = (ms) => new Date(ms).toLocaleDateString('ru-RU')

// Делит период на равные интервалы из целых дней, не больше MAX_BUCKETS.
// Нет начала — берём 14 дней до конца периода; нет конца — по сегодня. Будущее не рисуем.
export function buckets(from, to, now = Date.now()) {
  const today = dateValue(new Date(now))
  const last = to && to < today ? to : today
  const end = new Date(dayStart(last))
  let start = from ? new Date(dayStart(from)) : new Date(end.getFullYear(), end.getMonth(), end.getDate() - MAX_BUCKETS + 1)
  if (start > end) return []
  const days = Math.round((dayEnd(last) - start.getTime()) / 86400000)
  const size = Math.ceil(days / MAX_BUCKETS)
  const result = []
  for (let i = 0; i < days; i += size) {
    const a = new Date(start.getFullYear(), start.getMonth(), start.getDate() + i).getTime()
    const b = new Date(start.getFullYear(), start.getMonth(), start.getDate() + Math.min(i + size, days)).getTime()
    result.push({ from: a, to: b, label: short(a), title: size === 1 || b - a < 90000000 ? full(a) : `${full(a)} — ${full(b - 1)}` })
  }
  return result
}

// Верх шкалы и деления: целые «круглые» числа
export function niceScale(max) {
  if (max <= 4) return { top: Math.max(max, 1), step: 1 }
  const raw = max / 4
  const pow = 10 ** Math.floor(Math.log10(raw))
  const step = Math.max(1, [1, 2, 5, 10].map((k) => k * pow).find((s) => s >= raw))
  return { top: Math.ceil(max / step) * step, step }
}
