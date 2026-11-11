// Реалтайм по WebSocket (протокол — docs/API.md, раздел «WebSocket»).
//
// Одно соединение на вкладку. Открывается при первой подписке на лот и закрывается,
// когда подписок не осталось. При обрыве переподключается с экспоненциальной задержкой
// 1 с → 30 с (с джиттером) и заново шлёт subscribe на все открытые лоты: пропущенные
// события сервер не присылает, их заменяет snapshot.

import { reactive } from 'vue'
import { setServerTime } from './store'

const MIN_DELAY_MS = 1000
const MAX_DELAY_MS = 30000
const IDLE_CLOSE_MS = 5000 // при переходе между лотами не рвём соединение сразу

export const realtime = reactive({
  connected: false, // сокет открыт и прислал hello
  userId: null, // кого сервер узнал по cookie; null — аноним
  antiSnipe: null // { enabled, windowMs, extendMs } из hello
})

// lotId → Set обработчиков (message) => void
const subscriptions = new Map()

let socket = null
let attempt = 0
let reconnectTimer = null
let idleTimer = null
let reqSeq = 0

function wsUrl() {
  return (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + '/api/ws'
}

function send(message) {
  if (socket && socket.readyState === WebSocket.OPEN) {
    socket.send(JSON.stringify(message))
    return true
  }
  return false
}

function sendSubscribe(lotId) {
  send({ type: 'subscribe', lotId, reqId: String(++reqSeq) })
}

function connect() {
  clearTimeout(reconnectTimer)
  reconnectTimer = null
  if (socket || subscriptions.size === 0) return

  let ws
  try {
    ws = new WebSocket(wsUrl())
  } catch {
    scheduleReconnect()
    return
  }
  socket = ws

  ws.onopen = () => {
    // подписки отправляем сразу, hello придёт первым в любом случае
    for (const lotId of subscriptions.keys()) sendSubscribe(lotId)
  }

  ws.onmessage = (event) => {
    let message
    try {
      message = JSON.parse(event.data)
    } catch {
      return
    }
    if (!message || typeof message !== 'object') return

    if (message.type === 'hello') {
      attempt = 0 // соединение действительно живое — сбрасываем задержку
      setServerTime(message.now)
      realtime.userId = message.userId ?? null
      realtime.antiSnipe = message.antiSnipe || null
      realtime.connected = true
      return
    }

    if (message.lotId != null) {
      const handlers = subscriptions.get(Number(message.lotId))
      if (handlers) for (const handler of [...handlers]) handler(message)
    }
  }

  ws.onclose = () => {
    if (socket !== ws) return
    socket = null
    realtime.connected = false
    scheduleReconnect()
  }

  // onerror всегда сопровождается onclose — там и переподключаемся
  ws.onerror = () => {}
}

function scheduleReconnect() {
  if (reconnectTimer || subscriptions.size === 0) return
  const base = Math.min(MAX_DELAY_MS, MIN_DELAY_MS * 2 ** attempt)
  attempt++
  // «равный джиттер»: от половины до полной задержки, чтобы клиенты не ломились разом
  const delay = base / 2 + Math.random() * (base / 2)
  reconnectTimer = setTimeout(connect, delay)
}

function disconnect() {
  clearTimeout(reconnectTimer)
  reconnectTimer = null
  attempt = 0
  if (socket) {
    const ws = socket
    socket = null
    realtime.connected = false
    ws.close(1000)
  }
}

// Подписаться на события лота. Возвращает функцию отписки.
// handler получает сообщения snapshot / bid / bid_ok / lot_finished / lot_cancelled / error.
export function subscribeLot(lotId, handler) {
  lotId = Number(lotId)
  clearTimeout(idleTimer)
  idleTimer = null

  let handlers = subscriptions.get(lotId)
  if (!handlers) {
    handlers = new Set()
    subscriptions.set(lotId, handlers)
    sendSubscribe(lotId) // если сокет ещё не открыт — отправится в onopen
  } else {
    // второй подписчик на тот же лот тоже должен получить снимок
    sendSubscribe(lotId)
  }
  handlers.add(handler)
  connect()

  return () => {
    const current = subscriptions.get(lotId)
    if (!current || !current.delete(handler)) return
    if (current.size > 0) return
    subscriptions.delete(lotId)
    send({ type: 'unsubscribe', lotId })
    if (subscriptions.size === 0) {
      clearTimeout(idleTimer)
      idleTimer = setTimeout(() => {
        if (subscriptions.size === 0) disconnect()
      }, IDLE_CLOSE_MS)
    }
  }
}

// Если вкладка снова видна или сеть вернулась, а мы ждём переподключения — не тянем время
function reconnectNow() {
  if (!socket && subscriptions.size > 0) {
    attempt = 0
    connect()
  }
}

if (typeof window !== 'undefined') {
  window.addEventListener('online', reconnectNow)
  document.addEventListener('visibilitychange', () => {
    if (!document.hidden) reconnectNow()
  })
}
