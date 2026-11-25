<script setup>
import { ref, watch, onUnmounted } from 'vue'
import { adminApi } from '../../api'
import { handleApiError } from '../../store'

// Выбор пользователя: поиск по имени, никнейму, Telegram или ID.
const props = defineProps({
  modelValue: { type: [String, Number], default: '' }, // ID или ''
  label: { type: String, default: 'Пользователь' },
  placeholder: { type: String, default: 'имя, никнейм или ID' }
})
const emit = defineEmits(['update:modelValue'])

const text = ref('')
const options = ref([])
const open = ref(false)
const active = ref(-1)
const error = ref('')
let picked = null // { id, username } — чей ник сейчас в поле
let timer
let seq = 0

const title = (u) => `${u.username} (№${u.id})`

// Значение задано снаружи (ссылка с ID в адресе, сброс фильтров)
watch(
  () => String(props.modelValue || ''),
  async (id) => {
    if (id === String(picked?.id || '')) return
    cancelSearch()
    picked = null
    text.value = id ? `№${id}` : ''
    if (!id) return
    const my = seq
    try {
      const { user } = await adminApi.user(id)
      if (my !== seq) return
      picked = user
      text.value = title(user)
    } catch {
      // пользователя нет или нет связи — оставляем номер
    }
  },
  { immediate: true }
)

function cancelSearch() {
  clearTimeout(timer)
  seq++
  open.value = false
  options.value = []
}

function onInput() {
  cancelSearch()
  error.value = ''
  picked = null
  if (props.modelValue) emit('update:modelValue', '')
  const q = text.value.trim()
  if (!q) return
  timer = setTimeout(() => search(q), 300)
}

async function search(q) {
  const my = ++seq
  try {
    const data = await adminApi.users({ q, pageSize: 8 })
    if (my !== seq) return
    options.value = data.users
    active.value = data.users.length ? 0 : -1
    open.value = true
  } catch (e) {
    if (my === seq) error.value = handleApiError(e)
  }
}

function pick(user) {
  cancelSearch()
  picked = user
  text.value = title(user)
  emit('update:modelValue', user.id)
}

function onKey(e) {
  if (!open.value) return
  const n = options.value.length
  if (e.key === 'ArrowDown' && n) active.value = (active.value + 1) % n
  else if (e.key === 'ArrowUp' && n) active.value = (active.value + n - 1) % n
  else if (e.key === 'Enter' && n) pick(options.value[active.value])
  else if (e.key === 'Escape') open.value = false
  else return
  e.preventDefault()
}

onUnmounted(cancelSearch)
</script>

<template>
  <div class="field user-picker">
    <label>
      {{ label }}
      <input
        v-model="text"
        type="search"
        role="combobox"
        autocomplete="off"
        aria-autocomplete="list"
        :aria-expanded="open"
        :placeholder="placeholder"
        @input="onInput"
        @keydown="onKey"
        @blur="open = false"
      />
    </label>
    <ul v-if="open" class="user-picker-list" role="listbox">
      <li v-if="!options.length" class="muted">Никого не найдено</li>
      <li
        v-for="(u, i) in options"
        :key="u.id"
        role="option"
        :aria-selected="i === active"
        :class="{ active: i === active }"
        @mousedown.prevent="pick(u)"
      >
        <b>{{ u.username }}</b> <span class="muted">№{{ u.id }} · {{ u.name }}</span>
      </li>
    </ul>
    <small v-if="error" class="error">{{ error }}</small>
  </div>
</template>