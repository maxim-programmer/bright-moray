<script setup>
import { ref, computed, watch, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import { state, getCategory, handleApiError } from '../store'

const router = useRouter()
const title = ref('')
const description = ref('')
const file = ref(null)
const preview = ref('') // blob:-ссылка только для превью, на сервер уходит url из /api/uploads
const fileName = ref('')
const startPrice = ref('')
const step = ref('')
const hours = ref('')
const category = ref('')
const subcategory = ref('')
const subs = computed(() => getCategory(category.value)?.subs || [])
watch(category, () => (subcategory.value = ''))
const error = ref('')
const busy = ref(false)

function setPreview(url) {
  if (preview.value) URL.revokeObjectURL(preview.value)
  preview.value = url
}
onUnmounted(() => setPreview(''))

// Файл грузим только при отправке формы, чтобы на сервере не копились неиспользованные картинки
function onFileChange(event) {
  const selected = event.target.files[0]
  file.value = selected || null
  setPreview(selected ? URL.createObjectURL(selected) : '')
  fileName.value = selected ? selected.name : ''
}

async function submit() {
  if (busy.value) return
  error.value = ''
  const price = Number(startPrice.value)
  const stepValue = Number(step.value)
  const hoursValue = Number(hours.value)

  if (!title.value || !description.value || !category.value || !subcategory.value || !file.value || !startPrice.value || !step.value || !hours.value) {
    error.value = 'Заполните все поля'
    return
  }
  if (price <= 0 || stepValue <= 0) {
    error.value = 'Цена и шаг ставки должны быть больше нуля'
    return
  }
  if (hoursValue <= 0) {
    error.value = 'Длительность должна быть больше нуля'
    return
  }

  busy.value = true
  try {
    const { url } = await api.uploadImage(file.value)
    const lot = await api.createLot({
      title: title.value,
      description: description.value,
      image: url,
      category: category.value,
      subcategory: subcategory.value,
      startPrice: price,
      step: stepValue,
      hours: hoursValue
    })
    router.push('/lots/' + lot.id)
  } catch (e) {
    // слишком большой файл nginx может отклонить сам, без JSON-ответа от бэкенда
    error.value = e.status === 413 && e.code === 'error' ? 'Картинка больше 5 МБ' : handleApiError(e)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <h1>Создание лота</h1>
  <form class="form" @submit.prevent="submit">
    <label class="field">
      Название
      <input v-model="title" type="text" />
    </label>
    <label class="field">
      Описание
      <textarea v-model="description"></textarea>
    </label>
    <div class="field-row">
      <label class="field">
        Категория
        <select v-model="category">
          <option value="" disabled>Выберите</option>
          <option v-for="cat in state.categories" :key="cat.id" :value="cat.id">{{ cat.name }}</option>
        </select>
      </label>
      <label class="field">
        Подкатегория
        <select v-model="subcategory" :disabled="!category">
          <option value="" disabled>{{ category ? 'Выберите' : 'Сначала категорию' }}</option>
          <option v-for="sub in subs" :key="sub.id" :value="sub.id">{{ sub.name }}</option>
        </select>
      </label>
    </div>
    <div class="field">
      Фотография
      <label class="file-box">
        <input type="file" accept="image/*" @change="onFileChange" />
        <img v-if="preview" :src="preview" class="preview" alt="Предпросмотр" />
        <span v-else class="file-hint">Нажмите, чтобы выбрать фото</span>
        <span v-if="fileName" class="muted">{{ fileName }}</span>
      </label>
    </div>
    <div class="field-row">
      <label class="field">
        Начальная цена, ₽
        <input v-model="startPrice" type="number" min="1" />
      </label>
      <label class="field">
        Шаг ставки, ₽
        <input v-model="step" type="number" min="1" />
      </label>
    </div>
    <label class="field">
      Длительность аукциона, часов
      <input v-model="hours" type="number" min="1" />
    </label>
    <div v-if="error" class="error">{{ error }}</div>
    <button class="button" type="submit" :disabled="busy">{{ busy ? 'Загрузка…' : 'Опубликовать' }}</button>
  </form>
</template>
