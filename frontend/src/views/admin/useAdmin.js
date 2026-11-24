import { ref } from 'vue'
import { api } from '../../api'
import { state, handleApiError } from '../../store'

// Сервер ответил «Недостаточно прав»: роль могли снять после входа — перечитываем профиль
function adminError(e) {
  if (e?.status === 403 && e.code === 'forbidden') {
    api.me().then((user) => (state.user = user), () => {})
  }
  return handleApiError(e)
}

// Загрузка данных: устаревшие ответы (пришли после более нового запроса) отбрасываются.
export function useLoader(fn) {
  const loading = ref(false)
  const error = ref('')
  const status = ref(0) // HTTP-статус последней ошибки (404 — не найдено)
  let seq = 0
  async function run(...args) {
    const id = ++seq
    loading.value = true
    error.value = ''
    status.value = 0
    try {
      const result = await fn(...args)
      return id === seq ? result : undefined
    } catch (e) {
      if (id === seq) {
        error.value = adminError(e)
        status.value = e?.status || 0
      }
      return undefined
    } finally {
      if (id === seq) loading.value = false
    }
  }
  return { loading, error, status, run }
}

// Действие администратора (POST/PUT/DELETE): busy на время запроса, текст ошибки при неудаче.
// exec возвращает результат или undefined, если действие не удалось.
export function useAction() {
  const busy = ref(false)
  const error = ref('')
  async function exec(fn) {
    if (busy.value) return undefined
    busy.value = true
    error.value = ''
    try {
      return (await fn()) ?? true
    } catch (e) {
      error.value = adminError(e)
      return undefined
    } finally {
      busy.value = false
    }
  }
  return { busy, error, exec }
}
