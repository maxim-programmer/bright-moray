<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { api } from '../api'
import {
  state,
  getDormLabel,
  getMyBidState,
  getPriceText,
  getStatus,
  getStatusText,
  formatMoney,
  formatTimeShort,
  logout,
  handleApiError
} from '../store'

const router = useRouter()
const user = computed(() => state.user)
const stats = ref({ created: 0, participated: 0, won: 0, sold: 0, rating: 0, complaints: 0 })
const myLots = ref([])
const myBidLots = ref([])
const loading = ref(true)
const error = ref('')

onMounted(async () => {
  try {
    const [s, lots, bidLots] = await Promise.all([api.myStats(), api.myLots(), api.myBidLots()])
    stats.value = s
    myLots.value = lots
    myBidLots.value = bidLots
  } catch (e) {
    error.value = handleApiError(e)
  } finally {
    loading.value = false
  }
})

const exitError = ref('')
async function exit() {
  exitError.value = ''
  try {
    await logout()
    router.push('/')
  } catch (e) {
    exitError.value = handleApiError(e)
  }
}
</script>

<template>
  <div v-if="user" class="profile">
    <section class="profile-card">
      <div class="avatar">{{ user.name ? user.name[0] : user.username[0] }}</div>
      <div class="profile-info">
        <h1>{{ user.name || user.username }}</h1>
        <div class="profile-meta">
          <span>@{{ user.username }}</span>
          <span v-if="user.telegram">Telegram: @{{ user.telegram }}</span>
          <span>{{ getDormLabel(user.dormId) }}</span>
        </div>
      </div>
      <div class="profile-actions">
        <router-link to="/settings" class="button small light">Настройки</router-link>
        <button class="button small danger" @click="exit">Выйти</button>
        <div v-if="exitError" class="error">{{ exitError }}</div>
      </div>
      <div class="profile-stats">
        <div class="stat"><b>{{ stats.rating }}</b><span>рейтинг</span></div>
        <div class="stat"><b>{{ stats.created }}</b><span>создано лотов</span></div>
        <div class="stat"><b>{{ stats.participated }}</b><span>участий в торгах</span></div>
        <div class="stat"><b>{{ stats.won }}</b><span>выиграно</span></div>
        <div class="stat"><b>{{ stats.complaints }}</b><span>жалоб</span></div>
      </div>
    </section>

    <div v-if="loading" class="muted">Загрузка…</div>
    <div v-if="error" class="error">{{ error }}</div>

    <div v-if="!loading && !error" class="profile-columns">
      <!-- Слева: мои лоты -->
      <section class="profile-column">
        <div class="column-head">
          <h2>Мои лоты <span class="count">{{ myLots.length }}</span></h2>
          <router-link to="/create" class="button small">+ Создать</router-link>
        </div>
        <div v-if="myLots.length === 0" class="empty small-empty">
          <p>У вас пока нет лотов.</p>
          <router-link to="/create" class="link">Создать первый лот →</router-link>
        </div>
        <ul v-else class="mini-list">
          <li v-for="lot in myLots" :key="lot.id">
            <router-link :to="'/lots/' + lot.id" class="mini-lot">
              <img :src="lot.image" :alt="lot.title" loading="lazy" />
              <div class="mini-body">
                <div class="mini-title">{{ lot.title }}</div>
                <div class="mini-sub">
                  <span class="pill" :class="getStatus(lot)">{{ getStatusText(lot) }}</span>
                  <span v-if="getStatus(lot) === 'active'" class="muted">
                    ещё {{ formatTimeShort(lot.endsAt) }}
                  </span>
                </div>
              </div>
              <div class="mini-right">
                <b>{{ getPriceText(lot) }}</b>
                <span class="muted small">{{ lot.bidsCount }} ставок</span>
              </div>
            </router-link>
          </li>
        </ul>
      </section>

      <!-- Справа: мои ставки -->
      <section class="profile-column">
        <div class="column-head">
          <h2>Мои ставки <span class="count">{{ myBidLots.length }}</span></h2>
          <router-link to="/lots" class="button small light">К лотам</router-link>
        </div>
        <div v-if="myBidLots.length === 0" class="empty small-empty">
          <p>Вы пока не делали ставок.</p>
          <router-link to="/lots" class="link">Смотреть аукционы →</router-link>
        </div>
        <ul v-else class="mini-list">
          <li v-for="lot in myBidLots" :key="lot.id">
            <router-link :to="'/lots/' + lot.id" class="mini-lot">
              <img :src="lot.image" :alt="lot.title" loading="lazy" />
              <div class="mini-body">
                <div class="mini-title">{{ lot.title }}</div>
                <div class="mini-sub">
                  <span class="pill" :class="getMyBidState(lot).kind">{{ getMyBidState(lot).text }}</span>
                  <span v-if="getStatus(lot) === 'active'" class="muted">
                    ещё {{ formatTimeShort(lot.endsAt) }}
                  </span>
                </div>
              </div>
              <div class="mini-right">
                <b>{{ getPriceText(lot) }}</b>
                <span class="muted small">моя: {{ formatMoney(lot.myMaxBid) }}</span>
              </div>
            </router-link>
          </li>
        </ul>
      </section>
    </div>
  </div>
</template>
