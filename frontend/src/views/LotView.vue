<script setup>
import { ref } from 'vue'
import { useRoute } from 'vue-router'
import {
  state,
  getLot,
  getUsername,
  getLotBids,
  getStatus,
  hasBids,
  getCurrentPrice,
  getLeaderId,
  getMinBid,
  getStatusText,
  formatMoney,
  formatDate,
  formatTimeLeft,
  placeBid,
  cancelLot
} from '../store'

const route = useRoute()
const lot = getLot(route.params.id)
const amount = ref('')
const error = ref('')

function submitBid() {
  error.value = placeBid(lot, Number(amount.value)) || ''
  if (!error.value) {
    amount.value = ''
  }
}

function cancel() {
  cancelLot(lot)
}
</script>

<template>
  <div v-if="!lot">
    <h1>Лот не найден</h1>
    <router-link to="/" class="button">На главную</router-link>
  </div>

  <div v-else>
    <h1>{{ lot.title }}</h1>

    <div class="lot-top">
      <img :src="lot.image" :alt="lot.title" />
      <p class="description">{{ lot.description }}</p>
      <div class="params">
        <div class="param">
          <span class="muted">Автор</span>
          <b>{{ getUsername(lot.authorId) }}</b>
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
    </div>

    <div v-if="getStatus(lot) === 'active'" class="panel">
      <div class="muted">Текущая цена</div>
      <div class="price-big">{{ formatMoney(getCurrentPrice(lot)) }}</div>
      <p class="leader">
        Лидер:
        <b>{{ getLeaderId(lot) ? getUsername(getLeaderId(lot)) : 'ставок пока нет' }}</b>
      </p>
      <div class="timer">До завершения: {{ formatTimeLeft(lot.endsAt) }}</div>

      <div v-if="!state.currentUserId" class="muted">
        Чтобы сделать ставку, <router-link to="/login"><b>войдите</b></router-link>.
      </div>
      <div v-else-if="lot.authorId === state.currentUserId" class="muted">
        Это ваш лот, делать ставки на него нельзя.
      </div>
      <form v-else class="bid-row" @submit.prevent="submitBid">
        <input v-model="amount" type="number" :placeholder="'От ' + getMinBid(lot) + ' ₽'" />
        <button class="button" type="submit">Сделать ставку</button>
      </form>
      <div v-if="error" class="error bid-error">{{ error }}</div>

      <div v-if="lot.authorId === state.currentUserId && !hasBids(lot)" class="cancel-row">
        <button class="button light" @click="cancel">Отменить аукцион</button>
      </div>
    </div>

    <div v-else-if="getStatus(lot) === 'cancelled'" class="panel">
      <span class="status">{{ getStatusText(lot) }}</span>
      <p>Автор отменил аукцион. Ставки больше не принимаются.</p>
    </div>

    <div v-else class="panel">
      <span class="status">{{ hasBids(lot) ? 'Продан' : 'Не продан' }}</span>
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
    </div>

    <h2>История ставок</h2>
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
        <tr v-for="bid in getLotBids(lot.id)" :key="bid.id">
          <td>{{ getUsername(bid.userId) }}</td>
          <td><b>{{ formatMoney(bid.amount) }}</b></td>
          <td>{{ formatDate(bid.createdAt) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
