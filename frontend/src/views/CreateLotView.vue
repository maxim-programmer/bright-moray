<script setup>
import { ref, computed, watch } from 'vue'
import { useRouter } from 'vue-router'
import { createLot, categories, getCategory } from '../store'

const router = useRouter()
const title = ref('')
const description = ref('')
const image = ref('')
const fileName = ref('')
const startPrice = ref('')
const step = ref('')
const hours = ref('')
const category = ref('')
const subcategory = ref('')
const subs = computed(() => getCategory(category.value)?.subs || [])
watch(category, () => (subcategory.value = ''))
const error = ref('')

function onFileChange(event) {
  const file = event.target.files[0]
  image.value = file ? URL.createObjectURL(file) : ''
  fileName.value = file ? file.name : ''
}

function submit() {
  const price = Number(startPrice.value)
  const stepValue = Number(step.value)
  const hoursValue = Number(hours.value)

  if (!title.value || !description.value || !category.value || !subcategory.value || !image.value || !startPrice.value || !step.value || !hours.value) {
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

  const id = createLot({
    title: title.value,
    description: description.value,
    image: image.value,
    category: category.value,
    subcategory: subcategory.value,
    startPrice: price,
    step: stepValue,
    hours: hoursValue
  })
  router.push('/lots/' + id)
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
          <option v-for="cat in categories" :key="cat.id" :value="cat.id">{{ cat.name }}</option>
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
        <img v-if="image" :src="image" class="preview" alt="Предпросмотр" />
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
    <button class="button" type="submit">Опубликовать</button>
  </form>
</template>
