import { ref } from 'vue'
import { handleApiError } from '../../store'

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
        error.value = handleApiError(e)
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
      error.value = handleApiError(e)
      return undefined
    } finally {
      busy.value = false
    }
  }
  return { busy, error, exec }
}
