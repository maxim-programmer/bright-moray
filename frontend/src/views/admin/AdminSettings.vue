<script setup>
import { ref, onMounted } from 'vue'
import { adminApi } from '../../api'
import { state, reloadReferences, handleApiError } from '../../store'
import { minutesText } from './format'
import { useLoader, useAction } from './useAdmin'

/* ---------- параметры аукциона ---------- */

const form = ref(null) // поля формы — строки, как в <input>
const defaults = ref(null)
const saved = ref('')

const FIELDS = [
  { key: 'minBidStep', label: 'Минимальный шаг ставки, ₽', hint: 'Автор не может задать шаг меньше этого' },
  { key: 'minDurationMinutes', label: 'Минимальная длительность аукциона, мин', minutes: true },
  { key: 'maxDurationMinutes', label: 'Максимальная длительность аукциона, мин', minutes: true },
  { key: 'defaultDurationMinutes', label: 'Время автоматического завершения по умолчанию, мин', minutes: true, hint: 'Длительность, если автор её не указал' },
  { key: 'antiSnipeWindowSec', label: 'Продление: окно в конце аукциона, сек', hint: 'Ставка в последние N секунд продлевает аукцион. 0 — выключено' },
  { key: 'antiSnipeExtendSec', label: 'Продление: до конца остаётся, сек', hint: 'Новый конец = момент ставки + N секунд' }
]

const loadSettings = useLoader(() => adminApi.settings())
const settingsAction = useAction()

function fill(settings) {
  form.value = Object.fromEntries(FIELDS.map((f) => [f.key, String(settings[f.key])]))
}

async function loadAll() {
  const data = await loadSettings.run()
  if (!data) return
  defaults.value = data.defaults
  fill(data.settings)
}

async function saveSettings() {
  saved.value = ''
  const body = {}
  for (const f of FIELDS) {
    const raw = String(form.value[f.key]).trim()
    if (raw === '' || !Number.isInteger(Number(raw))) {
      settingsAction.error.value = `«${f.label}»: введите целое число`
      return
    }
    body[f.key] = Number(raw)
  }
  const result = await settingsAction.exec(() => adminApi.saveSettings(body))
  if (result && result !== true) {
    fill(result.settings)
    saved.value = 'Настройки сохранены'
  }
}

/* ---------- справочники ---------- */

const refError = ref('')
const refBusy = ref(false)
const newCategory = ref('')
const newDorm = ref('')
const newSub = ref({}) // categoryId → название

// Выполняет изменение справочника и обновляет списки в приложении
async function refAction(fn) {
  if (refBusy.value) return false
  refBusy.value = true
  refError.value = ''
  try {
    await fn()
    await reloadReferences()
    return true
  } catch (e) {
    refError.value = handleApiError(e)
    return false
  } finally {
    refBusy.value = false
  }
}

async function addCategory() {
  const name = newCategory.value.trim()
  if (!name) return
  if (await refAction(() => adminApi.createCategory({ name }))) newCategory.value = ''
}

async function renameCategory(c) {
  const name = window.prompt('Новое название категории', c.name)
  if (name === null || !name.trim() || name.trim() === c.name) return
  await refAction(() => adminApi.updateCategory(c.id, { name: name.trim(), description: c.description || '' }))
}

async function removeCategory(c) {
  if (!window.confirm(`Удалить категорию «${c.name}» вместе с подкатегориями?`)) return
  await refAction(() => adminApi.deleteCategory(c.id))
}

async function addSub(c) {
  const name = (newSub.value[c.id] || '').trim()
  if (!name) return
  if (await refAction(() => adminApi.createSub(c.id, { name }))) newSub.value[c.id] = ''
}

async function renameSub(c, s) {
  const name = window.prompt('Новое название подкатегории', s.name)
  if (name === null || !name.trim() || name.trim() === s.name) return
  await refAction(() => adminApi.updateSub(c.id, s.id, { name: name.trim() }))
}

async function removeSub(c, s) {
  if (!window.confirm(`Удалить подкатегорию «${s.name}»?`)) return
  await refAction(() => adminApi.deleteSub(c.id, s.id))
}

async function addDorm() {
  const name = newDorm.value.trim()
  if (!name) return
  if (await refAction(() => adminApi.createDorm({ name }))) newDorm.value = ''
}

async function renameDorm(d) {
  const name = window.prompt('Новое название общежития', d.name)
  if (name === null || !name.trim() || name.trim() === d.name) return
  await refAction(() => adminApi.updateDorm(d.id, { name: name.trim() }))
}

async function removeDorm(d) {
  if (!window.confirm(`Удалить общежитие «${d.name}»?`)) return
  await refAction(() => adminApi.deleteDorm(d.id))
}

onMounted(loadAll)
</script>

<template>
  <section class="admin-section">
    <h2>Параметры аукционов</h2>
    <div v-if="loadSettings.error.value" class="error">{{ loadSettings.error.value }}</div>
    <form v-if="form" class="admin-card" @submit.prevent="saveSettings">
      <div class="settings-grid">
        <label v-for="f in FIELDS" :key="f.key" class="field">
          {{ f.label }}
          <input v-model="form[f.key]" type="number" min="0" step="1" />
          <small>
            <template v-if="f.minutes && Number(form[f.key]) > 0">≈ {{ minutesText(Number(form[f.key])) }}. </template>
            {{ f.hint }}
            <template v-if="defaults"> По умолчанию: {{ defaults[f.key] }}.</template>
          </small>
        </label>
      </div>
      <p class="muted small">
        Настройки действуют на новые лоты и следующие ставки; уже созданные лоты не пересчитываются.
      </p>
      <div v-if="settingsAction.error.value" class="error">{{ settingsAction.error.value }}</div>
      <div v-if="saved" class="success">{{ saved }}</div>
      <div class="admin-actions">
        <button class="button" type="submit" :disabled="settingsAction.busy.value">Сохранить</button>
      </div>
    </form>
  </section>

  <div v-if="refError" class="error">{{ refError }}</div>

  <section class="admin-section">
    <h2>Категории и подкатегории</h2>
    <div class="admin-card">
      <p v-if="!state.categories.length" class="muted">Категорий пока нет — добавьте первую. Пользователи не смогут создавать лоты, пока их нет.</p>
      <ul class="ref-list">
        <template v-for="c in state.categories" :key="c.id">
          <li>
            <span class="ref-name">{{ c.name }} <span class="ref-id">{{ c.id }}</span></span>
            <button class="button small light" type="button" :disabled="refBusy" @click="renameCategory(c)">Переименовать</button>
            <button class="button small danger" type="button" :disabled="refBusy" @click="removeCategory(c)">Удалить</button>
          </li>
          <li v-for="s in c.subs" :key="s.id" class="sub">
            <span class="ref-name">— {{ s.name }} <span class="ref-id">{{ s.id }}</span></span>
            <button class="button small light" type="button" :disabled="refBusy" @click="renameSub(c, s)">Переименовать</button>
            <button class="button small danger" type="button" :disabled="refBusy" @click="removeSub(c, s)">Удалить</button>
          </li>
          <li class="sub">
            <form class="ref-add" @submit.prevent="addSub(c)">
              <input v-model="newSub[c.id]" type="text" maxlength="200" placeholder="Новая подкатегория" />
              <button class="button small" type="submit" :disabled="refBusy">Добавить</button>
            </form>
          </li>
        </template>
      </ul>
      <form class="ref-add" @submit.prevent="addCategory">
        <input v-model="newCategory" type="text" maxlength="200" placeholder="Новая категория" />
        <button class="button" type="submit" :disabled="refBusy">Добавить категорию</button>
      </form>
      <p class="muted small">Категорию или подкатегорию, в которой есть лоты, удалить нельзя.</p>
    </div>
  </section>

  <section class="admin-section">
    <h2>Общежития</h2>
    <div class="admin-card">
      <p v-if="!state.dorms.length" class="muted">Общежитий пока нет — добавьте первое. Без них никто не сможет зарегистрироваться.</p>
      <ul class="ref-list">
        <li v-for="d in state.dorms" :key="d.id">
          <span class="ref-name">{{ d.name }} <span class="ref-id">№{{ d.id }}</span></span>
          <button class="button small light" type="button" :disabled="refBusy" @click="renameDorm(d)">Переименовать</button>
          <button class="button small danger" type="button" :disabled="refBusy" @click="removeDorm(d)">Удалить</button>
        </li>
      </ul>
      <form class="ref-add" @submit.prevent="addDorm">
        <input v-model="newDorm" type="text" maxlength="100" placeholder="Название общежития" />
        <button class="button" type="submit" :disabled="refBusy">Добавить общежитие</button>
      </form>
      <p class="muted small">Общежитие, в котором есть пользователи, удалить нельзя.</p>
    </div>
  </section>
</template>
