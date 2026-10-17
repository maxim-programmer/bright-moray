<script setup>
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import LotCard from '../components/LotCard.vue'
import { state, categories, getActiveLots, sortLots, getRandomLotId } from '../store'

const router = useRouter()

const active = computed(() => getActiveLots())
const endingSoon = computed(() => sortLots(active.value, 'ending').slice(0, 4))
const newest = computed(() => sortLots(active.value, 'new').slice(0, 4))
// число активных лотов по категориям и подкатегориям: { computers: 12, 'computers/laptops': 3, ... }
const counts = computed(() => {
  const result = {}
  active.value.forEach((l) => {
    result[l.category] = (result[l.category] || 0) + 1
    const key = l.category + '/' + l.subcategory
    result[key] = (result[key] || 0) + 1
  })
  return result
})

function openRandom() {
  const id = getRandomLotId()
  if (id) router.push('/lots/' + id)
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
    </div>
    <div class="hero-stats">
      <div class="stat">
        <b>{{ active.length }}</b>
        <span>активных лотов</span>
      </div>
      <div class="stat">
        <b>{{ state.bids.length }}</b>
        <span>ставок сделано</span>
      </div>
      <div class="stat">
        <b>{{ state.users.length }}</b>
        <span>участников</span>
      </div>
    </div>
  </section>

  <section class="home-block">
    <div class="home-block-head">
      <h2>Категории</h2>
      <router-link to="/lots" class="link">Весь каталог →</router-link>
    </div>
    <div class="cat-tiles">
      <div v-for="cat in categories" :key="cat.id" class="cat-tile">
        <router-link :to="{ path: '/lots', query: { cat: cat.id } }" class="cat-tile-head">
          <span class="cat-tile-name">{{ cat.name }}</span>
          <span class="cat-tile-count">{{ counts[cat.id] || 0 }}</span>
        </router-link>
        <ul class="cat-tile-subs">
          <li v-for="sub in cat.subs.slice(0, 4)" :key="sub.id">
            <router-link :to="{ path: '/lots', query: { cat: cat.id, sub: sub.id } }">
              {{ sub.name }}<span>{{ counts[cat.id + '/' + sub.id] || 0 }}</span>
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
