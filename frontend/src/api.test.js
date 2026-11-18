// Тесты обёртки запросов: автообновление сессии на 401. Запуск: npm test (встроенный node:test).
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { createHttp, ApiError } from './api.js'

function json(status, body) {
  return { ok: status >= 200 && status < 300, status, json: async () => body }
}

// Фальшивый сервер: access «истёк», пока не прошёл refresh. Считает вызовы по адресам.
function fakeServer({ refresh = () => json(200, { status: 'ok' }), refreshDelay = 0 } = {}) {
  const calls = []
  const server = { fresh: false, calls }
  server.fetch = async (url, options) => {
    calls.push({ url, options })
    if (url === '/api/auth/refresh') {
      if (refreshDelay) await new Promise((r) => setTimeout(r, refreshDelay))
      const res = await refresh(server)
      if (res.ok) server.fresh = true
      return res
    }
    if (url === '/api/auth/login') return json(401, { code: 'bad_credentials', error: 'Неверный никнейм или пароль' })
    return server.fresh ? json(200, { url }) : json(401, { code: 'unauthorized', error: 'Войдите в аккаунт' })
  }
  server.count = (url) => calls.filter((c) => c.url === url).length
  return server
}

const noWait = async () => {}

test('запросы идут с credentials: include и без своих заголовков авторизации', async () => {
  const server = fakeServer()
  server.fresh = true
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  await http.request('GET', '/api/me/stats')
  const { options } = server.calls[0]
  assert.equal(options.credentials, 'include')
  assert.equal(options.headers.Authorization, undefined)
})

test('401 → один refresh → один повтор исходного запроса', async () => {
  const server = fakeServer()
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  const data = await http.request('GET', '/api/me/stats')
  assert.deepEqual(data, { url: '/api/me/stats' })
  assert.equal(server.count('/api/auth/refresh'), 1)
  assert.equal(server.count('/api/me/stats'), 2)
})

test('single-flight: 10 параллельных 401 дают ровно один refresh', async () => {
  const server = fakeServer({ refreshDelay: 20 })
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  const results = await Promise.all(Array.from({ length: 10 }, (_, i) => http.request('GET', `/api/lots/${i}`)))
  assert.equal(results.length, 10)
  assert.equal(server.count('/api/auth/refresh'), 1)
  for (let i = 0; i < 10; i++) assert.equal(server.count(`/api/lots/${i}`), 2)
})

test('запрос, отправленный до refresh и получивший 401 после него, повторяется без нового refresh', async () => {
  const server = fakeServer()
  let releaseSlow
  const slowGate = new Promise((r) => (releaseSlow = r))
  const baseFetch = server.fetch
  let slowSent = false
  server.fetch = async (url, options) => {
    if (url === '/api/slow' && !slowSent) {
      slowSent = true
      server.calls.push({ url, options })
      await slowGate // ответ «старой» cookie задерживается
      return json(401, { code: 'unauthorized' })
    }
    return baseFetch(url, options)
  }
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  const slow = http.request('GET', '/api/slow')
  await http.request('GET', '/api/fast') // этот проходит refresh
  releaseSlow()
  assert.deepEqual(await slow, { url: '/api/slow' })
  assert.equal(server.count('/api/auth/refresh'), 1)
})

test('повтор только один: если после успешного refresh снова 401 — ошибка без зацикливания', async () => {
  const server = fakeServer()
  server.fetch = async (url, options) => {
    server.calls.push({ url, options })
    if (url === '/api/auth/refresh') return json(200, { status: 'ok' })
    return json(401, { code: 'unauthorized', error: 'Войдите в аккаунт' })
  }
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  await assert.rejects(http.request('GET', '/api/me/lots'), (e) => e instanceof ApiError && e.status === 401)
  assert.equal(server.count('/api/auth/refresh'), 1)
  assert.equal(server.count('/api/me/lots'), 2)
})

test('сам refresh, вход и регистрация на 401 не ретраятся', async () => {
  const server = fakeServer({ refresh: () => json(401, { code: 'invalid_refresh' }) })
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  await assert.rejects(http.request('POST', '/api/auth/login', {}), { code: 'bad_credentials' })
  assert.equal(server.count('/api/auth/refresh'), 0)
  await assert.rejects(http.request('POST', '/api/auth/refresh'), { code: 'invalid_refresh' })
  assert.equal(server.count('/api/auth/refresh'), 1)
})

for (const code of ['invalid_refresh', 'refresh_reused']) {
  test(`refresh 401 ${code}: сессия закончилась, обработчик вызван один раз на все запросы`, async () => {
    const server = fakeServer({ refresh: () => json(401, { code, error: 'Войдите заново' }), refreshDelay: 10 })
    const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
    const expired = []
    http.onSessionExpired((e) => expired.push(e.code))
    const results = await Promise.allSettled([http.request('GET', '/api/a'), http.request('GET', '/api/b')])
    for (const r of results) {
      assert.equal(r.status, 'rejected')
      assert.equal(r.reason.status, 401)
      assert.equal(r.reason.code, code)
    }
    assert.deepEqual(expired, [code])
    assert.equal(server.count('/api/auth/refresh'), 1)
  })
}

test('refresh 409: ждём, повторяем запрос; при новом 401 — ещё один refresh, из аккаунта не выходим', async () => {
  let refreshes = 0
  const server = fakeServer({
    refresh: () => (++refreshes === 1 ? json(409, { code: 'refresh_conflict' }) : json(200, { status: 'ok' }))
  })
  let waited = 0
  const http = createHttp({ fetchImpl: server.fetch, wait: async () => void waited++ })
  const expired = []
  http.onSessionExpired((e) => expired.push(e))
  assert.deepEqual(await http.request('GET', '/api/me/bids'), { url: '/api/me/bids' })
  assert.equal(waited, 1)
  assert.equal(server.count('/api/auth/refresh'), 2)
  assert.equal(server.count('/api/me/bids'), 3)
  assert.equal(expired.length, 0)
})

test('refresh 409, а другая вкладка уже обновила cookie: повтор проходит без второго refresh', async () => {
  const server = fakeServer({
    refresh: (s) => {
      s.fresh = true // cookie обновила соседняя вкладка
      return json(409, { code: 'refresh_conflict' })
    }
  })
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  assert.deepEqual(await http.request('GET', '/api/x'), { url: '/api/x' })
  assert.equal(server.count('/api/auth/refresh'), 1)
})

test('бесконечный 409 не зацикливается и не разлогинивает', async () => {
  const server = fakeServer({ refresh: () => json(409, { code: 'refresh_conflict' }) })
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  const expired = []
  http.onSessionExpired((e) => expired.push(e))
  await assert.rejects(http.request('GET', '/api/x'), { status: 409, code: 'refresh_conflict' })
  assert.equal(server.count('/api/auth/refresh'), 2)
  assert.equal(expired.length, 0)
})

test('5xx и сетевая ошибка refresh не разлогинивают', async () => {
  for (const refresh of [() => json(500, { code: 'internal', error: 'Ошибка сервера' }), () => Promise.reject(new TypeError('Failed to fetch'))]) {
    const server = fakeServer({ refresh })
    const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
    const expired = []
    http.onSessionExpired((e) => expired.push(e))
    await assert.rejects(http.request('GET', '/api/x'), (e) => e.status === 500 || e.code === 'network')
    assert.equal(expired.length, 0)
  }
})

test('после неудачного refresh следующий 401 снова пробует refresh (промис не залипает)', async () => {
  let n = 0
  const server = fakeServer({ refresh: () => (++n === 1 ? json(503, {}) : json(200, { status: 'ok' })) })
  const http = createHttp({ fetchImpl: server.fetch, wait: noWait })
  await assert.rejects(http.request('GET', '/api/x'), { status: 503 })
  assert.deepEqual(await http.request('GET', '/api/x'), { url: '/api/x' })
  assert.equal(server.count('/api/auth/refresh'), 2)
})
