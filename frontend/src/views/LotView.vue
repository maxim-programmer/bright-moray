<script setup>
import { ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import LotCard from '../components/LotCard.vue'
import {
  state,
  getLot,
  getUser,
  getUsername,
  getLotBids,
  getStatus,
  hasBids,
  getCurrentPrice,
  getLeaderId,
  getMinBid,
  getStatusText,
  getCategory,
  getSubcategory,
  getLotDorm,
  getRandomLotId,
  getSimilarLots,
  formatMoney,
  formatDate,
  formatTimeLeft,
  placeBid,
  cancelLot
} from '../store'

const route = useRoute()
const router = useRouter()
const lot = getLot(route.params.id)
const amount = ref('')
const error = ref('')

const category = computed(() => (lot ? getCategory(lot.category) : null))
const sub = computed(() => (lot ? getSubcategory(lot.category, lot.subcategory) : null))
const dorm = computed(() => (lot ? getLotDorm(lot) : null))
const similar = computed(() => (lot ? getSimilarLots(lot) : []))
const isLeader = computed(() => lot && getLeaderId(lot) === state.user?.id)

// После завершения продавец и победитель видят контакты друг друга
const contact = computed(() => {
  if (!lot || getStatus(lot) !== 'finished' || !hasBids(lot)) return null
  const winnerId = getLeaderId(lot)
  if (state.user?.id === lot.authorId) return { role: 'Победитель', user: getUser(winnerId) }
  if (state.user?.id === winnerId) return { role: 'Продавец', user: getUser(lot.authorId) }
  return null
})

function submitBid() {
  error.value = placeBid(lot, Number(amount.value)) || ''
  if (!error.value) {
    amount.value = ''
  }
}

function fillMin() {
  amount.value = getMinBid(lot)
}

function cancel() {
  cancelLot(lot)
}

function openRandom() {
  const id = getRandomLotId()
  if (id) router.push('/lots/' + id)
}
</script>

<template>
  <div v-if="!lot">
    <h1>Лот не найден</h1>
    <router-link to="/lots" class="button">К списку лотов</router-link>
  </div>

  <div v-else class="lot-page">
    <div class="crumbs">
      <router-link to="/lots" class="link">Лоты</router-link>
      <template v-if="category">
        <span>›</span>
        <router-link :to="{ path: '/lots', query: { cat: category.id } }" class="link">
          {{ category.name }}
        </router-link>
      </template>
      <template v-if="sub">
        <span>›</span>
        <router-link :to="{ path: '/lots', query: { cat: category.id, sub: sub.id } }" class="link">
          {{ sub.name }}
        </router-link>
      </template>
    </div>

    <div class="lot-layout">
      <div class="lot-left">
        <img class="lot-image" :src="lot.image" :alt="lot.title" />
        <h2 class="lot-section-title">Описание</h2>
        <p class="description">{{ lot.description }}</p>
      </div>

      <div class="lot-right">
        <h1 class="lot-title">{{ lot.title }}</h1>
        <div class="params">
          <div class="param">
            <span class="muted">Категория</span>
            <b>
              <router-link v-if="category" :to="{ path: '/lots', query: { cat: category.id } }" class="link">{{ category.name }}</router-link>
              <template v-if="sub">
                ·
                <router-link :to="{ path: '/lots', query: { cat: category.id, sub: sub.id } }" class="link">{{ sub.name }}</router-link>
              </template>
            </b>
          </div>
          <div class="param">
            <span class="muted">Продавец</span>
            <b>{{ getUsername(lot.authorId) }}</b>
          </div>
          <div v-if="dorm" class="param">
            <span class="muted">Общежитие</span>
            <b>{{ dorm.name }}</b>
          </div>
          <div class="param">
            <span class="muted">Начальная цена</span>
            <b>{{ formatMoney(lot.startPrice) }}</b>
          </div>
          <div class="param">
            <span class="muted">Шаг ставки</span>
            <b>{{ formatMoney(lot.step) }}</b>
          </div>
        </div>

        <!-- Активный аукцион -->
        <div v-if="getStatus(lot) === 'active'" class="panel">
          <div class="muted">Текущая цена</div>
          <div class="price-big">{{ formatMoney(getCurrentPrice(lot)) }}</div>
          <p class="leader">
            Лидер:
            <b>{{ getLeaderId(lot) ? getUsername(getLeaderId(lot)) : 'ставок пока нет' }}</b>
            <span v-if="isLeader" class="pill good">это вы</span>
          </p>
          <div class="timer"><span class="muted">До конца:</span> {{ formatTimeLeft(lot.endsAt) }}</div>

          <div v-if="!state.user" class="muted">
            Чтобы сделать ставку,
            <router-link :to="{ path: '/login', query: { redirect: route.fullPath } }"><b>войдите</b></router-link>.
          </div>
          <div v-else-if="lot.authorId === state.user?.id" class="muted">
            Это ваш лот, делать ставки на него нельзя.
          </div>
          <div v-else-if="isLeader" class="muted">Ваша ставка лидирует — ждём соперников.</div>
          <template v-else>
            <form class="bid-row" @submit.prevent="submitBid">
              <input v-model="amount" type="number" :placeholder="'От ' + getMinBid(lot) + ' ₽'" />
              <button class="button" type="submit">Сделать ставку</button>
            </form>
            <button class="link small bid-min" @click="fillMin">
              Подставить минимальную: {{ formatMoney(getMinBid(lot)) }}
            </button>
          </template>
          <div v-if="error" class="error bid-error">{{ error }}</div>

          <div v-if="lot.authorId === state.user?.id && !hasBids(lot)" class="cancel-row">
            <button class="button light small" @click="cancel">Отменить аукцион</button>
          </div>
        </div>

        <!-- Отменён -->
        <div v-else-if="getStatus(lot) === 'cancelled'" class="panel">
          <span class="pill cancelled">{{ getStatusText(lot) }}</span>
          <p>Продавец отменил аукцион. Ставки больше не принимаются.</p>
        </div>

        <!-- Завершён -->
        <div v-else class="panel">
          <span class="pill" :class="hasBids(lot) ? 'good' : 'neutral'">
            {{ hasBids(lot) ? 'Продан' : 'Не продан' }}
          </span>
          <div class="params">
            <div class="param">
              <span class="muted">Итоговая цена</span>
              <b>{{ hasBids(lot) ? formatMoney(getCurrentPrice(lot)) : '—' }}</b>
            </div>
            <div v-if="hasBids(lot)" class="param">
              <span class="muted">Победитель</span>
              <b>{{ getUsername(getLeaderId(lot)) }}</b>
            </div>
            <div class="param">
              <span class="muted">Завершён</span>
              <b>{{ formatDate(lot.endsAt) }}</b>
            </div>
          </div>
          <div v-if="contact" class="contact-box">
            {{ contact.role }}: <b>{{ contact.user.name }}</b> — Telegram
            <b>@{{ contact.user.telegram }}</b>
          </div>
        </div>

        <button class="link small" @click="openRandom">Открыть случайный лот</button>
      </div>
    </div>

    <h2 class="history-title">История ставок</h2>
    <div v-if="getLotBids(lot.id).length === 0" class="muted">Ставок пока нет.</div>
    <table v-else class="table">
      <thead>
        <tr>
          <th>Пользователь</th>
          <th>Сумма</th>
          <th>Дата и время</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(bid, i) in getLotBids(lot.id)" :key="bid.id" :class="{ top: i === 0 }">
          <td>{{ getUsername(bid.userId) }}</td>
          <td><b>{{ formatMoney(bid.amount) }}</b></td>
          <td>{{ formatDate(bid.createdAt) }}</td>
        </tr>
      </tbody>
    </table>

    <section v-if="similar.length" class="similar">
      <div class="home-block-head">
        <h2>Похожие лоты</h2>
        <router-link
          :to="{ path: '/lots', query: sub ? { cat: category.id, sub: sub.id } : { cat: lot.category } }"
          class="link"
        >
          Все в разделе «{{ sub ? sub.name : category?.name }}» →
        </router-link>
      </div>
      <div class="grid grid-4">
        <LotCard v-for="item in similar" :key="item.id" :lot="item" />
      </div>
    </section>
  </div>
</template>
