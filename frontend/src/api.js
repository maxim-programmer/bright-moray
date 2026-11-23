// Тонкий клиент к бэкенду. Сессия живёт в двух httpOnly-cookie (access и refresh, см. docs/AUTH.md):
// в JS токенов нет и не должно быть — браузер сам отправляет cookie, мы только реагируем на 401.
// Все функции возвращают распарсенный JSON или бросают ApiError { status, code, message }.

export class ApiError extends Error {
  constructor(status, code, message) {
    super(message)
    this.status = status
    this.code = code
  }
}

const REFRESH_URL = '/api/auth/refresh'
// На 401 от этих адресов не обновляем сессию: refresh — это и есть обновление,
// а вход и регистрация отвечают 401 на неверные данные, а не на истёкший токен.
const NO_REFRESH = new Set([REFRESH_URL, '/api/auth/login', '/api/auth/register'])
// Сколько раз один запрос может вызвать refresh: второй нужен только после 409 refresh_conflict
const MAX_REFRESHES = 2

// Коды 401, после которых сессии больше нет (cookie сервер уже стёр)
export const SESSION_GONE_CODES = new Set(['unauthorized', 'invalid_refresh', 'refresh_reused'])

// 409 refresh_conflict: другая вкладка только что обновила общие cookie — даём им «доехать»
function conflictDelay() {
  return new Promise((resolve) => setTimeout(resolve, 100 + Math.random() * 200))
}

// Фабрика нужна тестам (свой fetch, без задержек); приложение пользуется экземпляром по умолчанию ниже.
export function createHttp({ fetchImpl = (url, options) => fetch(url, options), wait = conflictDelay } = {}) {
  let generation = 0 // растёт после каждого успешного refresh
  let refreshing = null // single-flight: один общий промис на все одновременные 401
  let expiredHandler = null

  async function send(method, url, body) {
    // credentials: 'include' — cookie уходят и при same-site схемах из ALLOWED_ORIGINS
    const options = { method, credentials: 'include', headers: {} }
    if (body instanceof FormData) {
      options.body = body
    } else if (body !== undefined) {
      options.headers['Content-Type'] = 'application/json'
      options.body = JSON.stringify(body)
    }
    let response
    try {
      response = await fetchImpl(url, options)
    } catch {
      throw new ApiError(0, 'network', 'Нет связи с сервером')
    }
    const data = await response.json().catch(() => null)
    return { response, data }
  }

  function toError(response, data) {
    return new ApiError(response.status, data?.code || 'error', data?.error || 'Ошибка сервера')
  }

  // Результат: { kind: 'ok' | 'conflict' | 'expired' | 'error', error? }. Никогда не бросает.
  function refreshSession() {
    if (!refreshing) {
      refreshing = (async () => {
        try {
          const { response, data } = await send('POST', REFRESH_URL)
          if (response.ok) {
            generation++
            return { kind: 'ok' }
          }
          if (response.status === 409) return { kind: 'conflict' }
          const error = toError(response, data)
          if (response.status === 401) {
            // сессия закончилась — сообщаем один раз, а не каждому ждущему запросу
            if (expiredHandler) expiredHandler(error)
            return { kind: 'expired', error }
          }
          return { kind: 'error', error } // 5xx и прочее: из аккаунта не выкидываем
        } catch (error) {
          return { kind: 'error', error } // нет сети: тоже не выкидываем
        }
      })().finally(() => {
        refreshing = null
      })
    }
    return refreshing
  }

  async function request(method, url, body) {
    const path = url.split('?')[0]
    let refreshes = 0
    let retryAfterRefresh = false // повтор после успешного refresh — ровно один
    let staleRetried = false
    let lastConflict = false
    for (;;) {
      const sentAt = generation
      const { response, data } = await send(method, url, body)
      if (response.ok) return data
      if (response.status !== 401 || NO_REFRESH.has(path) || retryAfterRefresh) {
        throw toError(response, data)
      }
      // Запрос ушёл со старой cookie, а пока он летел, сессию уже обновили — просто повторяем
      if (generation !== sentAt && !staleRetried) {
        staleRetried = true
        continue
      }
      if (refreshes >= MAX_REFRESHES) {
        throw lastConflict
          ? new ApiError(409, 'refresh_conflict', 'Сессия обновляется, повторите действие')
          : toError(response, data)
      }
      refreshes++
      const result = await refreshSession()
      if (result.kind === 'ok') {
        retryAfterRefresh = true
        continue
      }
      if (result.kind === 'conflict') {
        lastConflict = true
        await wait()
        continue // при новом 401 ещё раз вызовем refresh
      }
      throw result.error
    }
  }

  return {
    request,
    refreshSession,
    // store.js подписывается: сбросить пользователя и отправить на вход
    onSessionExpired(handler) {
      expiredHandler = handler
    }
  }
}

const http = createHttp()
const request = http.request
export const onSessionExpired = http.onSessionExpired

function qs(params = {}) {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== null && v !== '') sp.set(k, v)
  }
  const s = sp.toString()
  return s ? `?${s}` : ''
}

export const api = {
  // справочники и время
  categories: () => request('GET', '/api/categories'),
  dorms: () => request('GET', '/api/dorms'),
  serverTime: () => request('GET', '/api/time'), // { now } — для поправки таймеров

  // авторизация
  register: (data) => request('POST', '/api/auth/register', data),
  login: (username, password) => request('POST', '/api/auth/login', { username, password }),
  logout: () => request('POST', '/api/auth/logout'), // текущее устройство
  logoutAll: () => request('POST', '/api/auth/logout-all'), // все устройства
  me: () => request('GET', '/api/auth/me'), // 401, если не вошли

  // профиль
  updateProfile: (data) => request('PUT', '/api/me/profile', data), // { name, telegram, dormId }
  changeUsername: (newUsername) => request('PUT', '/api/me/username', { newUsername }),
  changePassword: (data) => request('PUT', '/api/me/password', data), // { current, newPassword, confirm }; другие устройства сервер разлогинит
  myStats: () => request('GET', '/api/me/stats'),
  myLots: () => request('GET', '/api/me/lots'),
  myBidLots: () => request('GET', '/api/me/bids'),

  // лоты. params: category, sub, q, status(active|finished|all), sort, priceMin, priceMax, dormId, page
  lots: (params) => request('GET', `/api/lots${qs(params)}`), // { lots, page, totalPages, total }
  lot: (id) => request('GET', `/api/lots/${id}`),
  similarLots: (id) => request('GET', `/api/lots/${id}/similar`),
  randomLot: (params) => request('GET', `/api/lots/random${qs(params)}`), // { id | null }
  cancelLot: (id) => request('DELETE', `/api/lots/${id}`),
  bids: (id) => request('GET', `/api/lots/${id}/bids`),
  placeBid: (id, amount) => request('POST', `/api/lots/${id}/bids`, { amount }), // { bid, lot }

  // Создание лота — два шага: сначала картинка, потом сам лот с полученным url
  uploadImage: (file) => {
    const form = new FormData()
    form.append('file', file)
    return request('POST', '/api/uploads', form) // { url }
  },
  createLot: (data) => request('POST', '/api/lots', data)
}

// Админ-панель (/api/admin/*, только роль admin). Списки постраничные: { <ключ>, total, page, pageSize, totalPages }.
// Периоды from/to — миллисекунды Unix (to не включается).
const A = '/api/admin'

export const adminApi = {
  // пользователи
  users: (params) => request('GET', `${A}/users${qs(params)}`), // q, role, blocked, page
  user: (id) => request('GET', `${A}/users/${id}`), // { user, stats }
  userActivity: (id, params) => request('GET', `${A}/users/${id}/activity${qs(params)}`),
  userLots: (id, params) => request('GET', `${A}/users/${id}/lots${qs(params)}`),
  userBids: (id, params) => request('GET', `${A}/users/${id}/bids${qs(params)}`),
  blockUser: (id, reason) => request('POST', `${A}/users/${id}/block`, { reason }),
  unblockUser: (id) => request('POST', `${A}/users/${id}/unblock`),

  // модерация лотов
  lots: (params) => request('GET', `${A}/lots${qs(params)}`), // status, q, page
  lot: (id) => request('GET', `${A}/lots/${id}`), // { lot, author }
  lotHistory: (id, params) => request('GET', `${A}/lots/${id}/history${qs(params)}`),
  approveLot: (id, startsAt) => request('POST', `${A}/lots/${id}/approve`, startsAt ? { startsAt } : {}),
  rejectLot: (id, reason) => request('POST', `${A}/lots/${id}/reject`, { reason }),
  editLot: (id, data) => request('PUT', `${A}/lots/${id}`, data), // { lot, changed }
  unpublishLot: (id, reason) => request('POST', `${A}/lots/${id}/unpublish`, { reason }),
  deleteLot: (id, reason) => request('DELETE', `${A}/lots/${id}`, { reason }),

  // аукционы
  auctions: (params) => request('GET', `${A}/auctions${qs(params)}`), // status, q, page → { auctions, … }
  auction: (id) => request('GET', `${A}/auctions/${id}`), // { lot, bids, author, winner, finalPrice, closed }
  finishAuction: (id, reason) => request('POST', `${A}/auctions/${id}/finish`, { reason }),
  cancelAuction: (id, reason) => request('POST', `${A}/auctions/${id}/cancel`, { reason }),
  resolveDispute: (id, data) => request('POST', `${A}/auctions/${id}/resolve`, data), // { action, reason, bidId? }

  // журнал и статистика
  events: (params) => request('GET', `${A}/events${qs(params)}`), // userId, lotId, type, byAdmin, from, to, page
  eventTypes: () => request('GET', `${A}/events/types`),
  stats: (params) => request('GET', `${A}/stats${qs(params)}`),
  userStats: (id, params) => request('GET', `${A}/stats/users/${id}${qs(params)}`),

  // настройки и справочники
  settings: () => request('GET', `${A}/settings`), // { settings, defaults }
  saveSettings: (data) => request('PUT', `${A}/settings`, data),
  createCategory: (data) => request('POST', `${A}/categories`, data),
  updateCategory: (id, data) => request('PUT', `${A}/categories/${id}`, data),
  deleteCategory: (id) => request('DELETE', `${A}/categories/${id}`),
  createSub: (categoryId, data) => request('POST', `${A}/categories/${categoryId}/subcategories`, data),
  updateSub: (categoryId, id, data) => request('PUT', `${A}/categories/${categoryId}/subcategories/${id}`, data),
  deleteSub: (categoryId, id) => request('DELETE', `${A}/categories/${categoryId}/subcategories/${id}`),
  createDorm: (data) => request('POST', `${A}/dorms`, data),
  updateDorm: (id, data) => request('PUT', `${A}/dorms/${id}`, data),
  deleteDorm: (id) => request('DELETE', `${A}/dorms/${id}`)
}
