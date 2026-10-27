// Тонкий клиент к бэкенду. Сессия живёт в httpOnly-cookie, поэтому токен руками не храним.
// Все функции возвращают распарсенный JSON или бросают ApiError { status, code, message }.

export class ApiError extends Error {
  constructor(status, code, message) {
    super(message)
    this.status = status
    this.code = code
  }
}

async function request(method, url, body) {
  const options = { method, credentials: 'same-origin', headers: {} }
  if (body instanceof FormData) {
    options.body = body
  } else if (body !== undefined) {
    options.headers['Content-Type'] = 'application/json'
    options.body = JSON.stringify(body)
  }
  let response
  try {
    response = await fetch(url, options)
  } catch {
    throw new ApiError(0, 'network', 'Нет связи с сервером')
  }
  const data = await response.json().catch(() => null)
  if (!response.ok) {
    throw new ApiError(response.status, data?.code || 'error', data?.error || 'Ошибка сервера')
  }
  return data
}

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
  logout: () => request('POST', '/api/auth/logout'),
  me: () => request('GET', '/api/auth/me'), // 401, если не вошли

  // профиль
  updateProfile: (data) => request('PUT', '/api/me/profile', data), // { name, telegram, dormId }
  changeUsername: (newUsername) => request('PUT', '/api/me/username', { newUsername }),
  changePassword: (data) => request('PUT', '/api/me/password', data), // { current, newPassword, confirm }
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
