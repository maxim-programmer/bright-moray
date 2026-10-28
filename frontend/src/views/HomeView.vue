<script setup>
import { ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import LotCard from '../components/LotCard.vue'
import { api } from '../api'
import { state, handleApiError } from '../store'

const router = useRouter()

const activeTotal = ref(0)
const endingSoon = ref([])
const newest = ref([])
const loading = ref(true)
const error = ref('')

onMounted(async () => {
  try {
    // по умолчанию сервер отдаёт активные лоты, отсортированные по близости окончания
    const [ending, fresh] = await Promise.all([api.lots(), api.lots({ sort: 'new' })])
    activeTotal.value = ending.total
    endingSoon.value = ending.lots.slice(0, 4)
    newest.value = fresh.lots.slice(0, 4)
  } catch (e) {
    error.value = handleApiError(e)
  } finally {
    loading.value = false
  }
})

const randomError = ref('')
async function openRandom() {
  randomError.value = ''
  try {
    const { id } = await api.randomLot()
    if (id) router.push('/lots/' + id)
  } catch (e) {
    randomError.value = handleApiError(e)
  }
}
</script>

<template>
  <section class="hero">
    <div class="hero-text">
      <h1>Продавай и покупай у соседей по общежитию</h1>
      <p>
        Честные торги в реальном времени: ставки, таймер и контакты победителя в Telegram.
        Забрать покупку — пара этажей, а не доставка через весь город.
      </p>
      <div class="hero-actions">
        <router-link to="/lots" class="button">Смотреть лоты</router-link>
        <router-link to="/create" class="button light">Создать лот</router-link>
        <button class="button ghost" @click="openRandom">Мне повезёт</button>
      </div>
      <div v-if="randomError" class="error">{{ randomError }}</div>
    </div>
    <div class="hero-stats">
      <div class="stat">
        <b>{{ activeTotal }}</b>
        <span>активных лотов</span>
      </div>
    </div>
  </section>

  <section class="home-block">
    <div class="home-block-head">
      <h2>Категории</h2>
      <router-link to="/lots" class="link">Весь каталог →</router-link>
    </div>
    <div class="cat-tiles">
      <div v-for="cat in state.categories" :key="cat.id" class="cat-tile">
        <router-link :to="{ path: '/lots', query: { cat: cat.id } }" class="cat-tile-head">
          <span class="cat-tile-name">{{ cat.name }}</span>
        </router-link>
        <ul class="cat-tile-subs">
          <li v-for="sub in cat.subs.slice(0, 4)" :key="sub.id">
            <router-link :to="{ path: '/lots', query: { cat: cat.id, sub: sub.id } }">
              {{ sub.name }}
            </router-link>
          </li>
        </ul>
        <router-link
          v-if="cat.subs.length > 4"
          :to="{ path: '/lots', query: { cat: cat.id } }"
          class="cat-tile-more"
        >
          ещё {{ cat.subs.length - 4 }} {{ cat.subs.length - 4 === 1 ? 'подкатегория' : 'подкатегории' }} →
        </router-link>
      </div>
    </div>
  </section>

  <section class="home-block">
    <div class="home-block-head">
      <h2>Скоро завершатся</h2>
      <router-link to="/lots" class="link">Все →</router-link>
    </div>
    <div v-if="loading" class="muted">Загрузка…</div>
    <div v-if="error" class="error">{{ error }}</div>
    <div class="grid grid-4">
      <LotCard v-for="lot in endingSoon" :key="lot.id" :lot="lot" />
    </div>
  </section>

  <section class="home-block">
    <div class="home-block-head">
      <h2>Новые лоты</h2>
      <router-link :to="{ path: '/lots', query: { sort: 'new' } }" class="link">Все →</router-link>
    </div>
    <div class="grid grid-4">
      <LotCard v-for="lot in newest" :key="lot.id" :lot="lot" />
    </div>
  </section>
</template>
